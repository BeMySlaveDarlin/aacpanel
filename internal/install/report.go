package install

import (
	"slices"
	"time"
)

// ---- S16: the report ----

// Report is how a run that went through ends: what it changed, what it
// found done and left as it was, what to mind, and what comes next.
type Report struct {
	Took     time.Duration
	Changed  []string
	Kept     []string
	Warnings []string
	Remind   []string
	Next     []string
}

// NewReport sums a run up from what it recorded and what its steps found.
func NewReport(s *Survey, r *Run, took time.Duration) Report {
	rep := Report{Took: took, Kept: r.Kept()}
	short := func(p string) string { return p }
	if s != nil {
		short = s.Facts.Short
	}
	for _, e := range r.ChangedInRun() {
		name := short(e.Target)
		if !slices.Contains(rep.Changed, name) {
			rep.Changed = append(rep.Changed, name)
		}
	}
	for _, what := range r.gone {
		rep.Changed = append(rep.Changed, what+" taken away")
	}
	for _, e := range r.ChangedInRun() {
		if e.Kind == string(Group) {
			rep.Warnings = append(rep.Warnings, "docker came with this run: a terminal opened before it reaches docker only after you log out and in")
		}
	}
	if s != nil {
		if s.valueOr("terminal", "") == "" {
			rep.Warnings = append(rep.Warnings, "no windows: a session opens in tmux, and tmux attach reaches it")
		}
		if g, ok := s.Value("term"); ok && g.Value == yes {
			rep.Warnings = append(rep.Warnings, "the live terminal answers any device with a passkey of the panel")
		}
		if s.Has("tailscale") {
			rep.Warnings = append(rep.Warnings, "the tailnet auth key is spent: a node made again needs a new key")
		}
		rep.Remind = append(rep.Remind, s.Closing()...)
	}
	for _, l := range r.Reminders() {
		if !slices.Contains(rep.Remind, l) {
			rep.Remind = append(rep.Remind, l)
		}
	}
	rep.Next = []string{
		"another device: ./install.sh enroll · the chain again: ./install.sh check",
		"update: ./install.sh update · remove: ./install.sh uninstall",
	}
	return rep
}
