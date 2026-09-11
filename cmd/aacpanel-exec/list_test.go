package main

import (
	"strings"
	"testing"

	"aacpanel/internal/action"
)

func TestShortListReasonNamesWhatIsMissing(t *testing.T) {
	without := func(drop ...action.Kind) []action.Kind {
		gone := map[action.Kind]bool{}
		for _, k := range drop {
			gone[k] = true
		}
		var out []action.Kind
		for _, k := range action.Kinds {
			if !gone[k] {
				out = append(out, k)
			}
		}
		return out
	}

	if got := shortListReason(action.Kinds); got != "" {
		t.Errorf("the full list explains itself in words: %q", got)
	}

	onlyOpen := shortListReason(without(action.WindowOpen))
	if !strings.Contains(onlyOpen, "AACP_TERMINAL") {
		t.Errorf("the reason is not named: %q", onlyOpen)
	}
	if strings.Contains(onlyOpen, "tmux") {
		t.Errorf("it sends the reader to install tmux where the matter is the terminal template: %q", onlyOpen)
	}

	var sessionsAndWindows []action.Kind
	for _, k := range action.Kinds {
		name := string(k)
		if strings.HasPrefix(name, "session.") || strings.HasPrefix(name, "window.") {
			sessionsAndWindows = append(sessionsAndWindows, k)
		}
	}
	noTmux := shortListReason(without(sessionsAndWindows...))
	if !strings.Contains(noTmux, "tmux") {
		t.Errorf("the reason is not named: %q", noTmux)
	}
	if !strings.Contains(noTmux, "windows") {
		t.Errorf("nothing is said about the windows that went missing, and that will come back as a separate question: %q", noTmux)
	}
}
