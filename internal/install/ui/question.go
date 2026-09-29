package ui

import "strings"

// Kind is how a question is answered.
type Kind int

const (
	Single Kind = iota // one of the options, or a line of one's own
	Multi              // any of the options; some may be fixed in place
	Text               // a line of one's own
	Secret             // a line that is never shown, not even to its author
)

// Option is one answer offered. The first option of a question is the value
// the machine suggests, with where it came from in Detail.
type Option struct {
	Label  string
	Detail string // what choosing it means, in a line or two
	Value  string // what the answer carries; the label when empty
	Group  string // Multi: the heading the option stands under
	Locked bool   // Multi: always in; the box does not clear
	On     bool   // Multi: checked before the person looks
}

// Question is a question as data. The screen draws it and reads keys for it;
// what it means and where the answer goes is the caller's business.
type Question struct {
	ID     string
	Tab    string // the name on its tab
	Prompt string
	Note   string // a line under the prompt: how to answer, what to have ready
	Kind   Kind

	Options []Option
	Default int // Single: the option the cursor starts on

	// Own is the label of the line where one types an answer of one's own:
	// "Type something." for Single, "Type a path." for Multi. Empty — the
	// options are all there is.
	Own string
	// Skip is a Multi row that answers with nothing but the locked options.
	Skip string

	Placeholder string             // Text, Secret: what an empty field shows
	Check       func(string) error // Text, Secret and the own line: refuses a value

	// Hidden takes the question out of its block: an earlier answer made it
	// moot. It is set by the block's Refresh, never by the question itself.
	Hidden bool
}

// Answer is what the person gave. It starts at the question's default, so a
// block submitted with Enter on every tab carries the suggested values.
type Answer struct {
	Given  bool   // answered on its tab rather than left at the default
	Choice int    // Single: the option; len(Options) is the own line
	Text   string // the own line, Text and Secret
	Picks  []bool // Multi: parallel to the options, own entries included
}

// SecretShown is what stands for a secret anywhere a value is shown.
const SecretShown = "set · not shown"

// Value is the answer as the caller stores it: the chosen option's value or
// the typed line. For Multi it is the picked values joined by commas.
func (q Question) Value(a Answer) string {
	switch q.Kind {
	case Single:
		if a.Choice >= len(q.Options) {
			return a.Text
		}
		if o := q.Options[a.Choice]; o.Value != "" {
			return o.Value
		}
		return q.Options[a.Choice].Label
	case Multi:
		return strings.Join(q.Values(a), ",")
	}
	return a.Text
}

// Values are the picked options of a Multi question.
func (q Question) Values(a Answer) []string {
	var out []string
	for i, o := range q.Options {
		if i < len(a.Picks) && a.Picks[i] {
			if o.Value != "" {
				out = append(out, o.Value)
			} else {
				out = append(out, o.Label)
			}
		}
	}
	return out
}

// Show is the answer the way a person reads it back: a secret never, an empty
// pick as "none".
func (q Question) Show(a Answer) string {
	switch q.Kind {
	case Secret:
		if a.Text == "" {
			return "not set"
		}
		return SecretShown
	case Single:
		if a.Choice >= len(q.Options) {
			return a.Text
		}
		return q.Options[a.Choice].Label
	case Multi:
		var picked []string
		for i, o := range q.Options {
			if i < len(a.Picks) && a.Picks[i] {
				picked = append(picked, o.Label)
			}
		}
		if len(picked) == 0 {
			return "none"
		}
		return strings.Join(picked, ", ")
	}
	return a.Text
}

// start is the answer a question has before anyone touches it.
func (q Question) start() Answer {
	a := Answer{Choice: q.Default}
	if q.Kind == Multi {
		a.Picks = make([]bool, len(q.Options))
		for i, o := range q.Options {
			a.Picks[i] = o.On || o.Locked
		}
	}
	return a
}
