package main

import (
	"errors"
	"fmt"
)

// pinTarget finds the live root of the group. A child never gets a competing
// pin of its own, and a missing parent leaves the live session on its own.
func (s *Server) pinTarget(name string) (string, error) {
	if s.host == nil {
		return "", errors.New("the live sessions are not known: the host snapshot is not configured")
	}
	at, ok := s.host.LiveSession(name)
	if !ok {
		return "", fmt.Errorf("there is no live session %q", name)
	}
	seen := map[string]bool{}
	for {
		if at.Outside {
			return "", fmt.Errorf("session %s was started outside the panel", at.Name)
		}
		if seen[at.Name] {
			return "", errors.New("the session group contains a parent loop")
		}
		seen[at.Name] = true
		parent := at.OpenedBy()
		if parent == "" {
			return at.Name, nil
		}
		next, alive := s.host.LiveSession(parent)
		if !alive {
			return at.Name, nil
		}
		at = next
	}
}
