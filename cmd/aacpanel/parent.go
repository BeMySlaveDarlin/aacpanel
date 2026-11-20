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

// A session opened by another stands under it wherever either works. The
// child leaves its own project's block for the parent's block; its row names
// its own project, and the block's restart includes it with that project's
// launch parameters. The executor and collector carry the parent by name.

// liveSession finds a session in the host snapshot without requiring a
// project: placement follows the parent even outside the map.
func (s *Server) liveSession(name string) (host.LiveSession, error) {
	if s.host == nil {
		return host.LiveSession{}, errors.New("the live sessions are not known: the host snapshot is not configured")
	}
	live, ok := s.host.LiveSession(name)
	if !ok {
		return host.LiveSession{}, fmt.Errorf("there is no live session %q", name)
	}
	return live, nil
}

// openedUnder keeps a live or sleeping session as the parent without
// waking it just to group the child.
func (s *Server) openedUnder(ctx context.Context, parent string) string {
	if _, err := s.liveSession(parent); err == nil {
		return parent
	}
	if pauses, err := s.sessionPauses(ctx); err == nil {
		for _, p := range pauses {
			if p.Sleeping && p.Session == parent {
				return parent
			}
		}
	}
	log.Printf("%s asks for a session without a live or sleeping parent, and that session stands on its own", parent)
	return ""
}

// kinDepth bounds the climb up the parents of a session: a chain longer than
// the sessions of a machine is a loop the snapshot carries already.
const kinDepth = 64

// standUnder checks that a live session can be stood under another and
// returns the other: both live, the child a session of the panel, and the
// other not standing under it already, which would leave the two under
// each other. A run of claude inside a session's work stands under that
// session and the panel did not start it: it is stood under nothing else.
func (s *Server) standUnder(child string, params map[string]any) (string, error) {
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
	kid, err := s.liveSession(child)
	if err != nil {
		return "", err
	}
	if kid.Outside {
		return "", fmt.Errorf("session %s was started outside the panel: it stands under the session it runs in, "+
			"or under none", child)
	}
	elder, err := s.liveSession(parent)
	if err != nil {
		return "", err
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

// standAlone checks that a live session can be taken from under its parent
// and returns the parent: the session lives and the panel stands it under
// another. A run of claude inside the work of a session stands under that
// session for as long as it runs, and nothing the panel keeps changes that.
func (s *Server) standAlone(name string) (string, error) {
	if s.host == nil {
		return "", errors.New("the live sessions are not known: the host snapshot is not configured")
	}
	live, ok := s.host.LiveSession(name)
	if !ok {
		return "", fmt.Errorf("there is no live session %q", name)
	}
	if live.Outside {
		return "", fmt.Errorf("session %s was started outside the panel: it stands under the session it runs in, "+
			"or under none", name)
	}
	parent := live.OpenedBy()
	if parent == "" {
		return "", fmt.Errorf("session %s stands under no session already", name)
	}
	return parent, nil
}
