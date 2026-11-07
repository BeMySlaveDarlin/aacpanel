package main

import (
	"context"
	"encoding/json"
	"time"
)

// heavySnapshot travels with each host poll, so Machine and session marks
// agree without a second browser poll or another host socket mount.
func (s *Server) heavySnapshot(ctx context.Context, payload []byte) []byte {
	if s.exec == nil {
		return payload
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	view, err := s.exec.Heavy(ctx)
	if err != nil {
		out, spliceErr := spliceField(payload, "heavyError", err.Error())
		if spliceErr == nil {
			return out
		}
		return payload
	}
	out, err := spliceField(payload, "heavy", view)
	if err != nil {
		return payload
	}
	places := map[string]int{}
	for _, w := range view.Waiting {
		if w.Session != "" && w.Place > 0 && (places[w.Session] == 0 || w.Place < places[w.Session]) {
			places[w.Session] = w.Place
		}
	}
	var snapshot map[string]json.RawMessage
	if json.Unmarshal(out, &snapshot) != nil {
		return out
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(snapshot["sessions"], &rows) != nil {
		return out
	}
	for _, row := range rows {
		if place := places[textField(row, "session")]; place > 0 {
			row["heavyPlace"], _ = json.Marshal(place)
		}
	}
	snapshot["sessions"], _ = json.Marshal(rows)
	marked, err := json.Marshal(snapshot)
	if err != nil {
		return out
	}
	return marked
}
