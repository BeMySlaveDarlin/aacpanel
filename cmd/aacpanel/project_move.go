package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

// runProjectMove takes map IDs from the phone; config directories and the
// project path are read under the map transaction's locks.
func (s *Server) runProjectMove(w http.ResponseWriter, r *http.Request, target string, params map[string]any) {
	if !s.profilesReady(w) {
		return
	}
	id, err := strconv.Atoi(target)
	group, ok := params["group"].(float64)
	if err != nil || id <= 0 || !ok || group <= 0 || group != float64(int(group)) {
		http.Error(w, "a project move needs the project and destination group IDs", http.StatusBadRequest)
		return
	}
	if s.exec == nil {
		http.Error(w, "the executor is unavailable", http.StatusServiceUnavailable)
		return
	}
	profiles, err := s.db.Profiles(r.Context())
	if err != nil {
		profilesError(w, err)
		return
	}
	found, exists := projectByID(profiles, id)
	if !exists {
		http.Error(w, "the project is no longer on the map", http.StatusNotFound)
		return
	}
	journal, err := s.logActionAttempt(r, string(action.ProjectMove), found.project.Name,
		map[string]any{"project": id, "group": int(group), "from": found.profile.Name, "path": found.project.Path})
	if err != nil {
		profilesError(w, err)
		return
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()
	ctx = store.WithActor(ctx, s.deviceName(r))
	var req action.Request
	var detail string
	call := func(finish string) error {
		req.Move.Finish = finish
		req.ID = req.Move.ID + "-" + finish
		// Undo must still reach the host when copying used the request's budget.
		callctx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer done()
		resp, err := s.exec.Do(callctx, req)
		if err != nil {
			return err
		}
		if !resp.OK {
			return errors.New(resp.Error)
		}
		if finish == "" {
			detail = resp.Detail
		}
		return nil
	}
	project, err := s.db.TransferProject(ctx, id, int(group), func(p store.ProfileProject, from, to string) (func() error, error) {
		if err := s.projectMoveQuiet(p); err != nil {
			return nil, err
		}
		req = action.Request{ID: requestID(journal), Kind: action.ProjectMove, Target: p.Path, Device: s.deviceName(r),
			Move: &action.ProjectMoveSpec{ID: requestID(journal), From: from, To: to}}
		if err := req.Validate(); err != nil {
			return nil, err
		}
		undo := func() error { return call("rollback") }
		if err := call(""); err != nil {
			return nil, errors.Join(err, undo())
		}
		return undo, nil
	})
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: detail, Duration: time.Since(started)}
	if err != nil {
		outcome.Result, outcome.Error = store.ActionFailed, err.Error()
	} else if err := call("commit"); err != nil {
		outcome.Detail += "; the map committed, but the recovery backup could not be removed: " + err.Error()
	}
	if err := s.db.LogResult(context.WithoutCancel(ctx), journal, outcome); err != nil {
		log.Printf("project move journal: %v", err)
	}
	if outcome.Result != store.ActionOK {
		writeStatusJSON(w, http.StatusConflict, map[string]any{"error": outcome.Error, "logged": true})
		return
	}
	s.writeProfiles(w, r, map[string]any{"ok": true, "logged": true, "detail": outcome.Detail, "project": project})
}

func (s *Server) projectMoveQuiet(p store.ProfileProject) error {
	if s.host == nil {
		return nil
	}
	raw, err := s.host.JSON()
	if err != nil {
		return fmt.Errorf("cannot check the host's live sessions: %w", err)
	}
	var snapshot struct {
		Sessions []host.LiveSession `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	for _, live := range snapshot.Sessions {
		dir := live.CWD
		if main := s.host.Worktrees()[dir]; main != "" {
			dir = main
		}
		if dir == p.Path || strings.HasPrefix(dir, p.Path+"/") {
			return fmt.Errorf("close live session %s before moving this project", live.Name)
		}
	}
	return nil
}
