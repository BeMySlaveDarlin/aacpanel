package action

import (
	"context"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AskTerms asks for the live tmux terminals of the user.
const AskTerms = "terms"

// TermNameMax is how long a name given to a terminal of the panel may be, in characters.
const TermNameMax = 40

// Term is a live tmux session. Foreign terminals carry an opaque id, their
// server name, and the panel session they belong to when one is known.
// Created and Activity are unix seconds, as tmux keeps them.
type Term struct {
	Foreign   bool   `json:"foreign,omitempty"`
	Server    string `json:"server,omitempty"`
	Session   string `json:"session,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	ID        string `json:"id"`
	Place     string `json:"place"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Activity  int64  `json:"activity"`
	Created   int64  `json:"created"`
	Clients   int    `json:"clients"`
	Busy      bool   `json:"busy,omitempty"`
	Last      string `json:"last,omitempty"`
}

// TermsAsker is an executor that lists the user's live tmux terminals.
type TermsAsker interface {
	Terms(ctx context.Context) ([]Term, error)
}

// TermKind reports whether an action is about a tmux terminal. Those
// are raw input into a shell of the machine, so they live only where the
// terminal does.
func TermKind(k Kind) bool {
	return k == TermStart || k == TermClose || k == TermRename || k == TermConsole || k == TmuxClose
}

// TermID reports whether s is the id of a terminal of the panel: "t-" and
// eight lowercase hexadecimal digits.
func TermID(s string) bool {
	rest, ok := strings.CutPrefix(s, "t-")
	if !ok || len(rest) != 8 {
		return false
	}
	for _, r := range rest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// TmuxID accepts only the digest the executor gives an enumerated terminal.
// Its contents never become a socket path or a tmux target.
func TmuxID(s string) bool {
	rest, ok := strings.CutPrefix(s, "tm-")
	if !ok || len(rest) != 64 || rest != strings.ToLower(rest) {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil
}

func safeTermName(name string) error {
	if strings.TrimSpace(name) == "" {
		return badRequest("the new name of the terminal is empty")
	}
	if !utf8.ValidString(name) {
		return badRequest("the new name of the terminal is not UTF-8")
	}
	if n := utf8.RuneCountInString(name); n > TermNameMax {
		return badRequest("the new name of the terminal is %d characters, longer than %d", n, TermNameMax)
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return badRequest("the new name of the terminal holds %q, which is not a printable character", r)
		}
	}
	return nil
}

func validateTerm(r Request) error {
	if r.Kind == TmuxClose {
		if !TmuxID(r.Target) {
			return badRequest("%q is not the id of a listed tmux terminal", r.Target)
		}
		return nil
	}
	if !TermID(r.Target) {
		return badRequest("%q is not the id of a terminal of the panel", r.Target)
	}
	if r.Kind != TermStart {
		return nil
	}
	if r.Place == "" {
		return badRequest("action %s without the place to open the terminal in", r.Kind)
	}
	if len(r.Place) > pathMax {
		return badRequest("the place is longer than %d characters", pathMax)
	}
	if !strings.HasPrefix(r.Place, "/") {
		return badRequest("the place %q is not absolute", r.Place)
	}
	return safePath(r.Place)
}
