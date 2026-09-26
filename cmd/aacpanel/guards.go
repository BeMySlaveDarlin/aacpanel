package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

// guardsEvery is how often the guards are handed again with no change of the
// map: an executor that was down when a change came keeps the old places
// until then.
const guardsEvery = 5 * time.Minute

const guardsTimeout = 10 * time.Second

// guardsChanged tells the publisher the map changed. A change that finds one
// already waiting joins it: the publisher reads the map when it runs, not
// when it was told.
func (s *Server) guardsChanged() {
	if s.guards == nil {
		return
	}
	select {
	case s.guards <- struct{}{}:
	default:
	}
}

// runGuards hands the executor the context guard of every place the map
// knows: at start, after every change of the map, and every guardsEvery.
func (s *Server) runGuards(ctx context.Context) {
	tick := time.NewTicker(guardsEvery)
	defer tick.Stop()
	failed := ""
	for {
		failed = s.handGuards(ctx, failed)
		select {
		case <-ctx.Done():
			return
		case <-s.guards:
		case <-tick.C:
		}
	}
}

// handGuards reads the map and hands its guards on. A failure is said once
// and again only when it changes: an executor down for an hour is one line
// of the log, not twelve.
func (s *Server) handGuards(ctx context.Context, failed string) string {
	if s.db == nil || s.exec == nil {
		return failed
	}
	list, err := s.db.Profiles(ctx)
	if err == nil {
		hctx, cancel := context.WithTimeout(ctx, guardsTimeout)
		err = s.exec.KeepGuards(hctx, guardsOf(list))
		cancel()
	}
	switch {
	case err != nil && err.Error() != failed:
		log.Printf("the context guards did not reach the host: %v", err)
		return err.Error()
	case err == nil && failed != "":
		log.Print("the context guards reach the host again")
	}
	if err != nil {
		return failed
	}
	return ""
}

// guardsOf returns the guard of every place the map knows: every project's
// directory with what it starts with, and every contour's directories with
// its defaults, for the sessions under them no project lies closer to.
func guardsOf(list []store.Profile) []action.Guard {
	byPath := map[string]action.Guard{}
	var order []string
	put := func(path string, values []schema.Value) {
		if path == "" {
			return
		}
		if _, ok := byPath[path]; !ok {
			order = append(order, path)
		}
		byPath[path] = guardOf(path, values)
	}
	for _, p := range list {
		contour, ok := launchOf(p.Launch)
		if !ok {
			continue
		}
		put(p.Prefix, schema.Effective(nil, contour, nil))
	}
	for _, p := range list {
		contour, ok := launchOf(p.Launch)
		if !ok {
			continue
		}
		for _, g := range p.Groups {
			for _, project := range g.Projects {
				if own, ok := launchOf(project.Launch); ok {
					put(project.Path, schema.Effective(nil, contour, own))
				}
			}
		}
	}
	out := make([]action.Guard, 0, len(order))
	for _, path := range order {
		out = append(out, byPath[path])
	}
	return out
}

func guardOf(path string, values []schema.Value) action.Guard {
	g := action.Guard{Path: path, Cap: schema.CapDefault}
	for _, v := range values {
		switch v.Key {
		case "contextCap":
			if n, ok := v.Value.(float64); ok {
				g.Cap = int(n)
			}
		case "autoRestart":
			g.Restart, _ = v.Value.(bool)
		}
	}
	return g
}

func launchOf(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}
