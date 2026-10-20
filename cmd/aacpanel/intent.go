package main

import (
	"encoding/json"
	"net/http"

	"aacpanel/internal/schema"
)

// The executor reads only the session's own messages. The service adds the
// map's layers so the sheet can show what clearing an override will restore.
func (s *Server) apiSessionIntent(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said which session to ask", http.StatusBadRequest)
		return
	}
	if s.exec == nil || s.host == nil {
		http.Error(w, "the executor or the host snapshot is not configured", http.StatusServiceUnavailable)
		return
	}
	live, ok := s.host.LiveSession(name)
	if !ok {
		http.Error(w, "the session is no longer live", http.StatusNotFound)
		return
	}
	own, err := s.exec.Intents(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	want, found, err := s.launchProject(r.Context(), nil, live.CWD, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var contour, project map[string]any
	if want != nil {
		for _, layer := range []struct {
			raw json.RawMessage
			out *map[string]any
		}{{found.profile.Launch, &contour}, {found.project.Launch, &project}} {
			if len(layer.raw) > 0 {
				if err := json.Unmarshal(layer.raw, layer.out); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
	}
	inherited := []schema.Value{}
	for _, v := range schema.Effective(nil, contour, project) {
		if v.Key == "intent" || v.Key == "restartIntent" {
			inherited = append(inherited, v)
		}
	}
	writeJSON(w, map[string]any{"own": own, "inherited": inherited})
}
