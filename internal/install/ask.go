package install

import (
	"fmt"
	"strings"
)

// Form is how a question is answered.
type Form int

const (
	One    Form = iota // one of the options, or a value of one's own
	Many               // any of the options; some may be fixed in place
	Line               // a value typed
	Secret             // a value typed and never shown, not even in the plan
)

// BlockID names a block of questions: the questions of one theme, asked
// together.
type BlockID string

// Option is one answer a question offers. The options come from the machine
// where the machine knows them, and each says where it came from: the plan
// shows that beside the value.
type Option struct {
	Value  string // what is written; empty is an answer too — "no windows"
	Label  string // what the screen shows; the value when empty
	Detail string // what choosing it means, in a line or two
	Source string // where the value came from: "hostname", "found on PATH"
	Group  string // Many: the heading it stands under
	Locked bool   // Many: always in
	On     bool   // Many: checked before anyone looks
}

// Name is how the option reads on the screen.
func (o Option) Name() string {
	if o.Label != "" {
		return o.Label
	}
	return o.Value
}

// Target is a place an answer is written to: a key of host.env or of the
// .env of the clone, or a part of the install that is not a file of keys —
// the kit, the claude settings, the map.
type Target struct {
	File string // HostEnvFile, DotEnvFile, "kit", "claude settings", "map"
	Key  string
}

// The files the answers go into.
const (
	HostEnvFile = "host.env"
	DotEnvFile  = ".env"
)

// Question is a question of the run the way the engine sees it: what it
// asks, the options the machine gives, the flag that answers it without
// asking, the answer it suggests and where the answer is written. How it is
// drawn is the screen's business.
type Question struct {
	ID     string
	Block  BlockID
	Tab    string // the name on its tab
	Prompt string
	Note   string // a line under the prompt
	Form   Form

	Options []Option
	Own     string // the label of the line for a value of one's own; empty — none
	Skip    string // Many: a row that answers with the locked options alone

	// Flag answers the question on the command line. For is the account a
	// question of several alike is about: the flag then takes dir=value, or
	// a bare value that answers them all.
	Flag   string
	For    string
	Values []string // what the flag takes, when that is a fixed set

	// Default is the answer Enter gives: the option under the cursor, the
	// checked values of Many joined by commas. Required is a question with
	// no answer to suggest: --yes cannot answer it either.
	Default  string
	Required bool

	Writes []Target
	Check  func(string) error // refuses a value of one's own

	// Moot is a question an earlier answer of its own block made pointless:
	// no windows, no display. It stays in the block, hidden, so that
	// changing that answer back brings it back, and it gets no answer.
	Moot bool

	chosen bool   // Default was set on purpose, even to an empty value
	source string // where Default came from, when no option says it
}

// key is where the flags put the answer of the question.
func (q Question) key() string {
	if q.For != "" {
		return q.Flag + " " + q.For
	}
	return q.Flag
}

// Answer gives the answer to q: from its flag, else the suggested one under
// --yes, else from the person. A run with nobody to ask — no terminal, or
// the plain view — stops here and names the flag that would have answered.
func (r *Run) Answer(q Question) (string, error) {
	if v, ok := r.Flagged(q); ok {
		return v, nil
	}
	if r.Yes && !q.Required {
		return q.Default, nil
	}
	if r.Ask == nil {
		return "", Unasked(q)
	}
	return r.Ask(q)
}

// Flagged is the answer the command line gave q, if it gave one: the value
// for q's own account first, then the value for all of them.
func (r *Run) Flagged(q Question) (string, bool) {
	if v, ok := r.Answers[q.key()]; ok {
		return v, true
	}
	if q.For != "" {
		v, ok := r.Answers[q.Flag]
		return v, ok
	}
	return "", false
}

// Unasked is the stop of a question with nobody to ask it.
func Unasked(q Question) error {
	takes := "<value>"
	if len(q.Values) > 0 {
		takes = strings.Join(q.Values, "|")
	}
	if q.For != "" {
		takes = q.For + "=" + takes
	}
	if q.Required {
		return fmt.Errorf("stop: no terminal to ask %q, and it has no answer to take for granted; pass %s %s", q.Prompt, q.Flag, takes)
	}
	return fmt.Errorf("stop: no terminal to ask %q; pass %s %s or --yes", q.Prompt, q.Flag, takes)
}
