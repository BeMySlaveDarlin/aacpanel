package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
)

// A session stands under another only within one project of the map: the
// project's own directory, one inside it and its worktrees. A session that
// opens one in another project hands work over to that project, and the new
// session is that project's, not a step of the work of the one that asked;
// shown under it, it would leave the block of its own project for a block of
// another. The panel holds the rule, since the map is the panel's: the
// executor and the collector carry whatever parent they are given.

// liveProject finds a live session and the project of the map it works in,
// by its directory, the way a restart of it finds the project. A session no
// project of the map holds has none.
func (s *Server) liveProject(ctx context.Context, name string) (host.LiveSession, *mapProject, error) {
	if s.host == nil {
		return host.LiveSession{}, nil, errors.New("the live sessions are not known: the host snapshot is not configured")
	}
	live, ok := s.host.LiveSession(name)
	if !ok {
		return host.LiveSession{}, nil, fmt.Errorf("there is no live session %q", name)
	}
	if s.db == nil {
		return live, nil, errors.New("the project of a session is not known: the database with the map is not configured")
	}
	list, err := s.db.Profiles(ctx)
	if err != nil {
		return live, nil, fmt.Errorf("the project of session %s cannot be found: %w", name, err)
	}
	found, ok := projectAt(list, live.CWD, s.worktrees(), s.db.ProjectRoots())
	if !ok {
		return live, nil, nil
	}
	return live, &found, nil
}

// openedUnder is the parent a session opened or resumed in a project of the
// map stands under: the session that asked for it, while it lives in that same
// project, and none otherwise. The start goes on either way: where the session
// stands in a list is no reason to refuse it.
func (s *Server) openedUnder(ctx context.Context, parent string, found mapProject) string {
	_, at, err := s.liveProject(ctx, parent)
	if err != nil || at == nil || at.project.ID != found.project.ID {
		log.Printf("%s asks for a session of project %q from outside it, and that session stands on its own",
			parent, found.project.Name)
		return ""
	}
	return parent
}

// kinDepth bounds the climb up the parents of a session: a chain longer than
// the sessions of a machine is a loop the snapshot carries already.
const kinDepth = 64

// standUnder checks that a live session can be stood under another and
// returns the other: both live, in one project of the map, the session a
// session of the panel, and the other not standing under it already, which
// would leave the two under each other. A run of claude inside the work of a
// session stands under that session, and the panel did not start it: it is
// not stood under anything else.
func (s *Server) standUnder(ctx context.Context, child string, params map[string]any) (string, error) {
	parent, isText := params["parent"].(string)
	if !isText || strings.TrimSpace(parent) == "" {
		return "", fmt.Errorf("the session to stand %s under did not arrive as a name", child)
	}
	if err := action.ValidParent(parent); err != nil {
		return "", err
	}
	if parent == child {
		return "", fmt.Errorf("session %s does not stand under itself", child)
	}
	kid, kidAt, err := s.liveProject(ctx, child)
	if err != nil {
		return "", err
	}
	if kid.Outside {
		return "", fmt.Errorf("session %s was started outside the panel: it stands under the session it runs in, "+
			"or under none", child)
	}
	elder, elderAt, err := s.liveProject(ctx, parent)
	if err != nil {
		return "", err
	}
	if kidAt == nil {
		return "", fmt.Errorf("session %s runs in %s, which no project of the map holds: a session stands under "+
			"another only within one project", child, kid.CWD)
	}
	if elderAt == nil || elderAt.project.ID != kidAt.project.ID {
		return "", fmt.Errorf("session %s does not run in project %q, as %s does: a session stands under another "+
			"only within one project", parent, kidAt.project.Name, child)
	}
	at := elder
	for range kinDepth {
		up := at.OpenedBy()
		if up == "" {
			break
		}
		if up == child {
			return "", fmt.Errorf("session %s stands under %s already: the two would stand under each other",
				parent, child)
		}
		next, ok := s.host.LiveSession(up)
		if !ok {
			break
		}
		at = next
	}
	return parent, nil
}
