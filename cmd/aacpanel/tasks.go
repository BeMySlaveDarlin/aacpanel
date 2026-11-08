package main

import (
	"context"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/store"
	"aacpanel/internal/taskreg"
)

const taskImportEvery = 5 * time.Minute

type taskImportReport struct {
	Projects []store.TaskImportResult `json:"projects"`
	Errors   []string                 `json:"errors"`
}

func (s *Server) importTaskStores(ctx context.Context, projectID int) (taskImportReport, error) {
	out := taskImportReport{Projects: []store.TaskImportResult{}, Errors: []string{}}
	if s.db == nil || s.chat == nil {
		return out, store.ErrUnavailable
	}
	s.taskImportMu.Lock()
	defer s.taskImportMu.Unlock()
	profiles, err := s.db.Profiles(ctx)
	if err != nil {
		return out, err
	}
	found := projectID == 0
	for _, profile := range profiles {
		for _, group := range profile.Groups {
			for _, project := range group.Projects {
				if projectID != 0 && project.ID != projectID {
					continue
				}
				found = true
				writer, e := s.db.TaskWriter(ctx, project.ID)
				if e != nil {
					out.Errors = append(out.Errors, project.Name+": "+e.Error())
					continue
				}
				if writer == taskreg.PanelWriter {
					continue
				}
				snapshot, e := s.chat.TaskStore(ctx, project.Path)
				if e != nil {
					out.Errors = append(out.Errors, project.Name+": "+e.Error())
					continue
				}
				result, e := s.db.ImportTasks(ctx, project.ID, project.Path, snapshot)
				if e != nil {
					out.Errors = append(out.Errors, project.Name+": "+e.Error())
					continue
				}
				for _, problem := range result.Errors {
					log.Printf("task import %s: %s", project.Name, problem)
				}
				out.Projects = append(out.Projects, result)
			}
		}
	}
	if !found {
		return out, store.ErrNotFound
	}
	for _, problem := range out.Errors {
		log.Printf("task import: %s", problem)
	}
	return out, nil
}

func (s *Server) runTaskImport(ctx context.Context) {
	for {
		pass, cancel := context.WithTimeout(ctx, 2*time.Minute)
		report, err := s.importTaskStores(pass, 0)
		cancel()
		wait := taskImportEvery
		if err != nil {
			log.Printf("task import: %v", err)
			wait = 15 * time.Second
		} else if len(report.Errors) > 0 {
			wait = 15 * time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func taskInt(r *http.Request, name string) (int, error) {
	text := r.URL.Query().Get(name)
	if text == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 {
		return 0, &taskParamError{name: name}
	}
	return n, nil
}

type taskParamError struct{ name string }

func (e *taskParamError) Error() string { return "invalid task parameter: " + e.name }

func taskFilter(r *http.Request) (f store.TaskFilter, err error) {
	if f.ContourID, err = taskInt(r, "contour"); err != nil {
		return f, err
	}
	if f.ProjectID, err = taskInt(r, "project"); err != nil {
		return f, err
	}
	if f.Limit, err = taskInt(r, "limit"); err != nil {
		return f, err
	}
	if f.Offset, err = taskInt(r, "offset"); err != nil {
		return f, err
	}
	q := r.URL.Query()
	f.Tag = q.Get("tag")
	f.Text = q.Get("q")
	f.External = q.Get("external")
	f.IncludeDeleted = q.Get("deleted") == "true"
	for _, status := range q["status"] {
		f.Statuses = append(f.Statuses, strings.Split(status, ",")...)
	}
	if text := q.Get("from"); text != "" {
		f.From, _, err = taskreg.Date(text)
		if err != nil {
			return f, &taskParamError{name: "from"}
		}
	}
	if text := q.Get("until"); text != "" {
		f.Until, _, err = taskreg.Date(text)
		if err != nil {
			return f, &taskParamError{name: "until"}
		}
	}
	return f, nil
}

func (s *Server) apiTasks(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	f, err := taskFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := s.db.Tasks(r.Context(), f)
	if err != nil {
		profilesError(w, err)
		return
	}
	if _, err = s.enrichTaskPage(r.Context(), page.Tasks); err != nil {
		profilesError(w, err)
		return
	}
	writeJSON(w, page)
}

func (s *Server) apiTask(w http.ResponseWriter, r *http.Request) {
	project, ok := s.profileID(w, r, "project")
	if !ok {
		return
	}
	number, err := strconv.ParseInt(r.PathValue("number"), 10, 64)
	if err != nil || number <= 0 {
		http.Error(w, "invalid task number", http.StatusBadRequest)
		return
	}
	detail, err := s.db.Task(r.Context(), project, number)
	if err != nil {
		profilesError(w, err)
		return
	}
	actors := []string{}
	for _, event := range detail.Events {
		actors = append(actors, event.Actor)
	}
	c, err := s.taskContext(r.Context(), detail.Task.ContourID, []taskreg.Task{detail.Task}, actors...)
	if err != nil {
		profilesError(w, err)
		return
	}
	taskreg.Enrich(&detail.Task, c.sessions, time.Now())
	for _, person := range detail.Task.People {
		actors = append(actors, person.Conversations...)
	}
	detail.Task.People = taskreg.People(actors, c.sessions)
	for i := range detail.Events {
		c.nameEvent(&detail.Events[i])
	}
	writeJSON(w, detail)
}

func (s *Server) apiTaskPeriod(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	f, err := taskFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	period, err := s.db.TasksPeriod(r.Context(), f)
	if err != nil {
		profilesError(w, err)
		return
	}
	flat := []taskreg.Task{}
	actors := []string{}
	for _, group := range period.Groups {
		for _, activity := range group.Tasks {
			flat = append(flat, activity.Task)
			for _, event := range activity.Events {
				actors = append(actors, event.Actor)
			}
		}
	}
	c, err := s.taskContext(r.Context(), f.ContourID, flat, actors...)
	if err != nil {
		profilesError(w, err)
		return
	}
	groups := map[string][]store.TaskActivity{}
	seen := map[string]bool{}
	for _, group := range period.Groups {
		for _, activity := range group.Tasks {
			key := strconv.Itoa(activity.Task.ProjectID) + ":" + strconv.FormatInt(activity.Task.Number, 10)
			if seen[key] {
				continue
			}
			seen[key] = true
			taskreg.Enrich(&activity.Task, c.sessions, time.Now())
			refs := activity.Task.ExternalRefs
			if len(refs) == 0 {
				refs = []string{""}
			}
			for i := range activity.Events {
				c.nameEvent(&activity.Events[i])
			}
			for _, ref := range refs {
				groups[ref] = append(groups[ref], activity)
			}
		}
	}
	period.Groups = []store.TaskPeriodGroup{}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		period.Groups = append(period.Groups, store.TaskPeriodGroup{External: key, Tasks: groups[key]})
	}
	writeJSON(w, period)
}

func (s *Server) apiTaskBoard(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	id, err := taskInt(r, "contour")
	if err != nil || id == 0 {
		http.Error(w, "a contour id is required", http.StatusBadRequest)
		return
	}
	board, err := s.taskBoard(r.Context(), id)
	if err != nil {
		profilesError(w, err)
		return
	}
	writeJSON(w, board)
}

func (s *Server) apiTaskImport(w http.ResponseWriter, r *http.Request) {
	project, err := taskInt(r, "project")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	report, err := s.importTaskStores(r.Context(), project)
	if err != nil {
		profilesError(w, err)
		return
	}
	status := http.StatusOK
	if len(report.Errors) > 0 {
		status = http.StatusServiceUnavailable
	}
	writeStatusJSON(w, status, report)
}
