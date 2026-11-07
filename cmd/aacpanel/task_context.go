package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
	"aacpanel/internal/taskreg"
)

type taskContext struct {
	sessions []taskreg.Session
	notes    []string
}

func (s *Server) taskContext(ctx context.Context, contour int, tasks []taskreg.Task, actors ...string) (taskContext, error) {
	out := taskContext{sessions: []taskreg.Session{}, notes: []string{}}
	profiles, err := s.db.Profiles(ctx)
	if err != nil {
		return out, err
	}
	var profile *store.Profile
	for i := range profiles {
		if profiles[i].ID == contour {
			profile = &profiles[i]
			break
		}
	}
	if profile == nil {
		return out, store.ErrNotFound
	}
	seen := map[string]bool{}
	if s.host != nil {
		raw, e := s.host.JSON()
		var snap struct {
			At         int64    `json:"at"`
			SessionsAt int64    `json:"sessionsAt"`
			Notes      []string `json:"sessionNotes"`
			Sessions   []struct {
				host.LiveSession
				Contour int    `json:"contour"`
				Status  string `json:"status"`
			} `json:"sessions"`
		}
		if e == nil {
			e = json.Unmarshal(raw, &snap)
		}
		at := snap.SessionsAt
		if at == 0 {
			at = snap.At
		}
		if e == nil && at > 0 && time.Since(time.Unix(at, 0)) < livingFresh && !time.Unix(at, 0).After(time.Now().Add(time.Second)) && snap.Sessions != nil && len(snap.Notes) == 0 {
			for _, row := range snap.Sessions {
				project := 0
				own := row.Contour
				if found, ok := projectAt(profiles, row.CWD, s.worktrees(), s.db.ProjectRoots()); ok {
					own = found.profile.ID
					project = found.project.ID
				}
				if own != contour || row.SessionID == "" {
					continue
				}
				out.sessions = append(out.sessions, taskreg.Session{ID: row.SessionID, Name: row.Name, ProjectID: project, Live: true, Status: row.Status})
				seen[row.SessionID] = true
			}
		} else {
			out.notes = append(out.notes, "Live sessions are unavailable; activity is based on recorded events.")
		}
	}
	ids := taskreg.ActorIDs(tasks)
	for _, id := range actors {
		if id != "" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 || !s.chat.Available() {
		return out, nil
	}
	// Names come from the archive's index, never from a guessed project name.
	// Prefix filtering avoids loading unrelated conversations and their prompts.
	for first := 0; first < len(ids); first += 2000 {
		wanted := ids[first:min(first+2000, len(ids))]
		for offset := 0; ; {
			page, e := s.chat.Archive(ctx, chat.ArchiveReq{Profile: profile.ConfigDir, Conversations: wanted, Limit: 100, Offset: offset})
			if e != nil {
				out.notes = append(out.notes, "Some conversation names are unavailable; their recorded ids are shown.")
				return out, nil
			}
			for _, row := range page.Rows {
				matched := false
				for _, id := range wanted {
					if taskreg.SameConversation(id, row.SessionID) {
						matched = true
						break
					}
				}
				if !matched {
					out.notes = append(out.notes, "Some conversation names are unavailable; their recorded ids are shown.")
					return out, nil
				}
				if row.NameGuessed || row.Name == "" || seen[row.SessionID] {
					continue
				}
				seen[row.SessionID] = true
				out.sessions = append(out.sessions, taskreg.Session{ID: row.SessionID, Name: row.Name})
			}
			offset += len(page.Rows)
			if offset >= page.Total || len(page.Rows) == 0 {
				break
			}
		}
	}
	return out, nil
}

func (s *Server) enrichTaskPage(ctx context.Context, tasks []taskreg.Task) ([]string, error) {
	byContour := map[int][]taskreg.Task{}
	for _, task := range tasks {
		byContour[task.ContourID] = append(byContour[task.ContourID], task)
	}
	contexts := map[int]taskContext{}
	notes := []string{}
	for id, own := range byContour {
		c, err := s.taskContext(ctx, id, own)
		if err != nil {
			return notes, err
		}
		contexts[id] = c
		notes = append(notes, c.notes...)
	}
	for i := range tasks {
		taskreg.Enrich(&tasks[i], contexts[tasks[i].ContourID].sessions, time.Now())
	}
	slices.Sort(notes)
	return slices.Compact(notes), nil
}

func (s *Server) taskBoard(ctx context.Context, contour int) (map[string]any, error) {
	if contour <= 0 {
		return nil, fmt.Errorf("a contour id is required")
	}
	tasks := []taskreg.Task{}
	for offset := 0; ; {
		page, err := s.db.Tasks(ctx, store.TaskFilter{ContourID: contour, Limit: 500, Offset: offset})
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, page.Tasks...)
		offset += len(page.Tasks)
		if offset >= page.Total || len(page.Tasks) == 0 {
			break
		}
	}
	c, err := s.taskContext(ctx, contour, tasks)
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		taskreg.Enrich(&tasks[i], c.sessions, time.Now())
		// Bodies load on demand; a long backlog must not download them twice.
		tasks[i].Body = nil
		tasks[i].Source = nil
		tasks[i].Markdown = ""
	}
	return map[string]any{"tasks": tasks, "noTask": taskreg.WithoutTasks(tasks, c.sessions), "notes": c.notes}, nil
}
