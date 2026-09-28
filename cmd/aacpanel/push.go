package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"slices"

	"aacpanel/internal/auth"
	"aacpanel/internal/notify"
	"aacpanel/internal/store"
)

func (s *Server) apiPushKey(w http.ResponseWriter, r *http.Request) {
	key := s.push.PublicKey()
	if key == "" {
		http.Error(w, "push notifications are not ready yet", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"key": key})
}

func (s *Server) apiPushTest(w http.ResponseWriter, r *http.Request) {
	if !s.push.Ready() {
		http.Error(w, "push notifications are not ready yet", http.StatusServiceUnavailable)
		return
	}
	s.push.Send(notify.Message{
		Title:    "aacpanel is on the line",
		Body:     "Test notification: delivery works",
		Tag:      "test",
		Severity: notify.Info,
	})
	log.Printf("%s: test notification sent", auth.ClientIP(r))
	w.WriteHeader(http.StatusAccepted)
}

// pushPrefs is the person's choice of pushes. Where it cannot be read every
// push goes: a choice that failed to load must not silence the panel.
func (s *Server) pushPrefs(ctx context.Context) notify.Prefs {
	if s.db == nil {
		return notify.Prefs{}
	}
	raw, err := s.db.PushPrefs(ctx)
	if err != nil {
		log.Printf("notify: the choice of pushes is not readable, every push goes: %v", err)
		return notify.Prefs{}
	}
	var p notify.Prefs
	if err := json.Unmarshal(raw, &p); err != nil {
		log.Printf("notify: the choice of pushes does not parse, every push goes: %v", err)
		return notify.Prefs{}
	}
	return p.Clean()
}

func (s *Server) savePushPrefs(ctx context.Context, p notify.Prefs) (notify.Prefs, error) {
	p = p.Clean()
	raw, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	return p, s.db.SavePushPrefs(ctx, raw)
}

// pushSources are what the choice of pushes can name: the contours with their
// projects, the stacks the panel sees, the rules and the probes.
type pushSources struct {
	Contours []pushContour    `json:"contours"`
	Stacks   []string         `json:"stacks"`
	Rules    []store.PushRule `json:"rules"`
	Probes   []pushProbe      `json:"probes"`
}

type pushContour struct {
	Name      string        `json:"name"`
	ConfigDir string        `json:"configDir"`
	Projects  []pushProject `json:"projects"`
}

type pushProject struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type pushProbe struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (s *Server) apiPushPrefs(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "the choice of pushes needs the database", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	src := pushSources{Contours: []pushContour{}, Stacks: []string{}, Probes: []pushProbe{}}
	if profiles, err := s.db.Profiles(ctx); err == nil {
		for _, p := range profiles {
			c := pushContour{Name: p.Name, ConfigDir: p.ConfigDir, Projects: []pushProject{}}
			for _, g := range p.Groups {
				for _, pr := range g.Projects {
					c.Projects = append(c.Projects, pushProject{Name: pr.Name, Path: pr.Path})
				}
			}
			src.Contours = append(src.Contours, c)
		}
	}
	if s.watch != nil {
		src.Stacks = s.watch.Stacks()
		slices.Sort(src.Stacks)
	}
	rules, err := s.db.PushRules(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	src.Rules = rules
	if probes, err := s.db.Probes(ctx); err == nil {
		for _, p := range probes {
			src.Probes = append(src.Probes, pushProbe{ID: p.ID, Name: p.Name})
		}
	}
	writeJSON(w, map[string]any{"prefs": s.pushPrefs(ctx), "sources": src})
}

func (s *Server) apiPushPrefsSave(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "the choice of pushes needs the database", http.StatusServiceUnavailable)
		return
	}
	var p notify.Prefs
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&p); err != nil {
		http.Error(w, "the choice does not parse: "+err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.savePushPrefs(r.Context(), p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	log.Printf("%s: the choice of pushes changed", auth.ClientIP(r))
	writeJSON(w, map[string]any{"prefs": saved})
}

// apiPushQuiet takes the button of a push: its source is turned off, and the
// push says so. The worker of the phone presses it, with the session of the
// panel it lives in.
func (s *Server) apiPushQuiet(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "the choice of pushes needs the database", http.StatusServiceUnavailable)
		return
	}
	var q notify.Quiet
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&q); err != nil {
		http.Error(w, "the button does not parse: "+err.Error(), http.StatusBadRequest)
		return
	}
	muted, ok := s.pushPrefs(r.Context()).Mute(q)
	if !ok {
		http.Error(w, "the button names nothing the choice knows", http.StatusBadRequest)
		return
	}
	saved, err := s.savePushPrefs(r.Context(), muted)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	log.Printf("%s: quieted from a push: %s %s", auth.ClientIP(r), q.What, q.Key)
	writeJSON(w, map[string]any{"prefs": saved})
}
