package install

import (
	"fmt"
	"strings"
)

// Question is a question of the run the way the engine sees it: what it
// asks, the flag that answers it without asking, and the answer it suggests.
// How it is drawn is the screen's business.
type Question struct {
	Prompt  string
	Flag    string   // "--access"
	Values  []string // what the flag takes; empty means a value of one's own
	Default string
}

// Answer gives the answer to q: from its flag, else the suggested one under
// --yes, else from the person. A run with nobody to ask — no terminal, or
// the plain view — stops here and names the flag that would have answered.
func (r *Run) Answer(q Question) (string, error) {
	if v, ok := r.Answers[q.Flag]; ok {
		return v, nil
	}
	if r.Yes {
		return q.Default, nil
	}
	if r.Ask == nil {
		return "", Unasked(q)
	}
	return r.Ask(q)
}

// Unasked is the stop of a question with nobody to ask it.
func Unasked(q Question) error {
	takes := "<value>"
	if len(q.Values) > 0 {
		takes = strings.Join(q.Values, "|")
	}
	return fmt.Errorf("stop: no terminal to ask %q; pass %s %s or --yes", q.Prompt, q.Flag, takes)
}
