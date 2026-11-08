package main

import (
	"errors"
	"net/http"

	"aacpanel/internal/store"
	"aacpanel/internal/taskreg"
)

func (s *Server) apiTaskRegistry(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	var in taskreg.Request
	if !decodeProfileBody(w, r, &in) {
		return
	}
	profiles, err := s.db.Profiles(r.Context())
	if err != nil {
		profilesError(w, err)
		return
	}
	actor := taskreg.Actor{Conversation: "person", Name: "person"}
	cwd := in.CWD
	if in.Conversation != "" {
		if s.host == nil {
			http.Error(w, "the caller needs a live session snapshot", http.StatusServiceUnavailable)
			return
		}
		live, ok := s.host.LiveSessionOf(in.Conversation)
		if !ok || live.Name == "" {
			http.Error(w, "the caller has no named live session", http.StatusConflict)
			return
		}
		actor = taskreg.Actor{Conversation: live.SessionID, Name: live.Name}
		cwd = live.CWD
	}
	var project mapProject
	var found bool
	if cwd != "" {
		project, found = projectAt(profiles, cwd, s.worktrees(), s.db.ProjectRoots())
		if found && in.Project != 0 && in.Project != project.project.ID {
			http.Error(w, "the caller belongs to another project", http.StatusForbidden)
			return
		}
	} else {
		project, found = projectByID(profiles, in.Project)
	}
	if !found {
		profilesError(w, store.ErrNotFound)
		return
	}
	id := project.project.ID
	writer, err := s.db.TaskWriter(r.Context(), id)
	if err != nil {
		profilesError(w, err)
		return
	}
	if in.Operation == "project" {
		writeJSON(w, taskreg.Context{Project: taskreg.Project{ID: id, Name: project.project.Name, Writer: writer}})
		return
	}
	if in.Operation == "context" {
		out := map[string]any{"project": taskreg.Project{ID: id, Name: project.project.Name, Writer: writer}}
		if writer == taskreg.PanelWriter {
			queue, e := s.db.Tasks(r.Context(), store.TaskFilter{ProjectID: id, Statuses: []string{"open"}, Limit: 10})
			if e != nil {
				profilesError(w, e)
				return
			}
			out["queue"] = queue
			handoff, e := s.db.Handoff(r.Context(), id, "")
			if e == nil {
				out["handoff"] = handoff
			} else if !errors.Is(e, store.ErrNotFound) {
				profilesError(w, e)
				return
			}
			if handoff.TaskNumbers != nil {
				tasks := []taskreg.Task{}
				for _, n := range handoff.TaskNumbers {
					detail, e := s.db.Task(r.Context(), id, n)
					if e != nil {
						profilesError(w, e)
						return
					}
					tasks = append(tasks, detail.Task)
				}
				out["handoffTasks"] = tasks
			}
		}
		writeJSON(w, out)
		return
	}
	if in.Operation == "switch" {
		if in.Conversation != "" {
			http.Error(w, "only the person's CLI switches the project writer", http.StatusForbidden)
			return
		}
		if writer == taskreg.PanelWriter {
			writeJSON(w, taskreg.Project{ID: id, Name: project.project.Name, Writer: writer})
			return
		}
		if s.chat == nil {
			profilesError(w, store.ErrUnavailable)
			return
		}
		s.taskImportMu.Lock()
		defer s.taskImportMu.Unlock()
		snapshot, e := s.chat.TaskStore(r.Context(), project.project.Path)
		if e != nil {
			http.Error(w, e.Error(), http.StatusServiceUnavailable)
			return
		}
		if _, e = s.db.SwitchTaskWriter(r.Context(), id, project.project.Path, snapshot); e != nil {
			profilesError(w, e)
			return
		}
		writeJSON(w, taskreg.Project{ID: id, Name: project.project.Name, Writer: taskreg.PanelWriter})
		return
	}
	if writer != taskreg.PanelWriter {
		http.Error(w, "this project uses cctm files; use cctm or switch its task writer with the CLI", http.StatusConflict)
		return
	}
	var out any
	switch in.Operation {
	case "list":
		out, err = s.db.Tasks(r.Context(), store.TaskFilter{ProjectID: id, Statuses: in.Status, Tags: in.Tags, Text: in.Text, Next: in.Next, Limit: in.Limit})
	case "show":
		out, err = s.db.Task(r.Context(), id, in.Number)
	case "create":
		out, err = s.db.WriteTask(r.Context(), id, 0, actor, in.Edit)
	case "update":
		if in.Number <= 0 {
			http.Error(w, "an update needs a positive task number", http.StatusBadRequest)
			return
		}
		out, err = s.db.WriteTask(r.Context(), id, in.Number, actor, in.Edit)
	case "handoff_write":
		out, err = s.db.WriteHandoff(r.Context(), id, actor, cwd, in.Handoff)
	case "handoff_read":
		out, err = s.db.Handoff(r.Context(), id, in.Key)
	default:
		http.Error(w, "unknown task registry operation", http.StatusBadRequest)
		return
	}
	if err != nil {
		profilesError(w, err)
		return
	}
	writeJSON(w, out)
}
