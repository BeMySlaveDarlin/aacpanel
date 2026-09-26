package action

import (
	"context"
	"path"
	"strings"
)

// AskGuards hands the executor the context guard of every place the map
// knows: the service computes it from the map, and the host keeps it where
// the context guard hook and the prompt stamp read it. It is not an action —
// nothing on the host changes but that file — and it goes to no journal.
const AskGuards = "guards"

// guardsMax bounds one handing: a place is a project or a contour.
const guardsMax = 2000

// Guard is the context guard of one place: a project's directory, or a
// contour's where no project of it lies closer. Cap is the share of the
// model's window in percent; Restart is whether a session past it wraps up
// and starts afresh.
type Guard struct {
	Path    string `json:"path"`
	Cap     int    `json:"cap"`
	Restart bool   `json:"restart"`
}

// GuardKeeper is an executor that keeps the guards for the host.
type GuardKeeper interface {
	KeepGuards(ctx context.Context, guards []Guard) error
}

func validateGuards(r Request) error {
	if r.Target != "" || r.Text != "" {
		return badRequest("question %q has no target and no text", r.Ask)
	}
	if len(r.Guards) > guardsMax {
		return badRequest("%d guards at once, more than %d", len(r.Guards), guardsMax)
	}
	for _, g := range r.Guards {
		if !strings.HasPrefix(g.Path, "/") || path.Clean(g.Path) != g.Path || len(g.Path) > pathMax {
			return badRequest("guard path %q is not a clean absolute path", g.Path)
		}
		// The host reads a line a place: a tab or a line break inside a path
		// would forge another place.
		if strings.ContainsAny(g.Path, "\t\n\r") {
			return badRequest("guard path %q holds a tab or a line break", g.Path)
		}
		if g.Cap < 1 || g.Cap > 99 {
			return badRequest("guard of %s caps at %d%%, outside 1–99", g.Path, g.Cap)
		}
	}
	return nil
}
