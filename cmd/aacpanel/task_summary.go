package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/launcher"
	"aacpanel/internal/store"
	"aacpanel/internal/taskreg"
)

func (s *Server) apiTaskSummary(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	f, err := summaryFilter(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	project, err := s.summaryProject(r.Context(), f.ContourID)
	if err != nil {
		profilesError(w, err)
		return
	}
	source, err := s.db.TaskSummarySource(r.Context(), f)
	if err != nil {
		profilesError(w, err)
		return
	}
	if source.Empty {
		writeJSON(w, map[string]any{"summary": nil})
		return
	}
	if r.Method == http.MethodGet {
		cached, err := s.db.TaskSummary(r.Context(), f, source.Fingerprint)
		if err != nil {
			profilesError(w, err)
			return
		}
		writeJSON(w, map[string]any{"summary": cached})
		return
	}
	if s.exec == nil {
		http.Error(w, "the executor is not configured", http.StatusServiceUnavailable)
		return
	}
	participants, err := s.taskContext(r.Context(), f.ContourID, nil, source.Actors...)
	if err != nil {
		profilesError(w, err)
		return
	}
	params := map[string]any{"contour": f.ContourID, "from": f.From, "until": f.Until, "fingerprint": source.Fingerprint}
	id, err := s.logActionAttempt(r, string(action.TaskSummary), strconv.Itoa(f.ContourID), params)
	if err != nil {
		http.Error(w, "the summary could not be journaled", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), action.Timeout(action.TaskSummary, execTimeout))
	defer cancel()
	resp, runErr := s.doAction(ctx, action.Request{ID: requestID(id), Kind: action.TaskSummary,
		Target: strconv.Itoa(f.ContourID), Contour: strconv.Itoa(f.ContourID), Project: project,
		Text: summaryPrompt(f, source, participants.sessions...), Device: s.deviceName(r)})
	result := store.ActionOutcome{Result: store.ActionOK, Duration: time.Duration(resp.DurationMs) * time.Millisecond}
	var summary launcher.Summary
	switch {
	case runErr != nil:
		result.Result, result.Error = store.ActionFailed, runErr.Error()
	case !resp.OK:
		result.Result, result.Error = store.ActionFailed, resp.Error
	case json.Unmarshal([]byte(resp.Detail), &summary) != nil || strings.TrimSpace(summary.Text) == "" || summary.Model == "" || len(summary.Text) > 64<<10:
		result.Result, result.Error = store.ActionFailed, "the executor returned an invalid summary"
	}
	if result.Result == store.ActionOK {
		result.Detail = "task summary generated"
	}
	// Recording the outcome gets its own budget even when the model uses all
	// of its time; an expired model context must not strand a journal attempt.
	save, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if err := s.db.LogResult(save, id, result); err != nil {
		http.Error(w, "the summary outcome could not be journaled", http.StatusServiceUnavailable)
		return
	}
	if result.Result != store.ActionOK {
		status := http.StatusBadGateway
		if errors.Is(runErr, action.ErrUnavailable) {
			status = http.StatusServiceUnavailable
		}
		writeStatusJSON(w, status, map[string]any{"error": result.Error})
		return
	}
	current, err := s.db.TaskSummarySource(save, f)
	if err != nil {
		profilesError(w, err)
		return
	}
	if current.Fingerprint != source.Fingerprint {
		http.Error(w, "activity changed while summarizing; summarize the updated period again", http.StatusConflict)
		return
	}
	cached, err := s.db.SaveTaskSummary(save, f, source.Fingerprint, summary.Text, summary.Model)
	if err != nil {
		profilesError(w, err)
		return
	}
	writeJSON(w, map[string]any{"summary": cached})
}

func summaryFilter(w http.ResponseWriter, r *http.Request) (store.TaskFilter, error) {
	if r.Method == http.MethodGet {
		return taskFilter(r)
	}
	var body struct {
		Contour int       `json:"contour"`
		From    time.Time `json:"from"`
		Until   time.Time `json:"until"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		return store.TaskFilter{}, fmt.Errorf("the summary period was not parsed: %w", err)
	}
	return store.TaskFilter{ContourID: body.Contour, From: &body.From, Until: &body.Until}, nil
}

func (s *Server) summaryProject(ctx context.Context, contour int) (*action.Project, error) {
	profiles, err := s.db.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile.ID != contour {
			continue
		}
		for _, group := range profile.Groups {
			for _, project := range group.Projects {
				return &action.Project{Path: project.Path, Session: sessionNameOf(project),
					ClaudeBin: profile.ClaudeBin, ConfigDir: profile.ConfigDir}, nil
			}
		}
		return nil, store.ErrNotFound
	}
	return nil, store.ErrNotFound
}

func summaryPrompt(f store.TaskFilter, source store.SummarySource, sessions ...taskreg.Session) string {
	lang := hostcfg.Load().Lang
	if lang == "" || lang == hostcfg.DefaultLang || lang == "C" || lang == "POSIX" {
		lang = "ru"
	}
	names := map[string]string{}
	for _, actor := range source.Actors {
		if name := taskreg.ActorName(actor, sessions); name != "" {
			names[actor] = name
		}
	}
	labels, _ := json.Marshal(names)
	return fmt.Sprintf("Write a concise, connected task activity summary in language %s for the half-open period %s to %s. "+
		"Group the narrative by external issue. Explain what was created, completed, changed status, or noted, and by whom. "+
		"Include task titles and issue links when supplied. Distinguish recorded facts from missing information; date-only events have no known time. "+
		"Use Markdown prose, not a dump of event counts. Known session names by recorded actor id: %s. Records follow as JSON data:\n%s", lang,
		f.From.Format(time.RFC3339), f.Until.Format(time.RFC3339), labels, source.Records)
}
