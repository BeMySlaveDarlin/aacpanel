package ui

import (
	"fmt"
	"strings"
	"time"
)

// Tone is how a step ended, and it colours the dot that opens the step.
type Tone int

const (
	Plain Tone = iota // at work, or nothing to judge
	Good
	Bad
	Asked // the person answered
)

// Mark opens a line of a step's result.
type Mark string

const (
	Pass Mark = "✓"
	Warn Mark = "⚠"
	Fail Mark = "✗"
)

// Step is the line that opens an entry of the feed.
func (t Theme) Step(tone Tone, title string, width int) string {
	dot := "⏺"
	switch tone {
	case Good:
		dot = t.OK.Render(dot)
	case Bad:
		dot = t.Fail.Render(dot)
	case Asked:
		dot = t.Accent.Render(dot)
	}
	lines := indent(title, "⏺ ", "  ", width)
	lines[0] = dot + strings.TrimPrefix(lines[0], "⏺")
	return strings.Join(lines, "\n")
}

// Output nests lines under a step: the first behind ⎿, the rest in line with
// it. A line wider than the terminal wraps under itself rather than under ⎿.
func (t Theme) Output(lines []string, width int) string {
	var out []string
	for i, line := range lines {
		prefix := "     "
		if i == 0 {
			prefix = "  " + t.Dim.Render("⎿") + "  "
		}
		out = append(out, indent(line, prefix, "     ", width)...)
	}
	return strings.Join(out, "\n")
}

// Result is one line of what a step found or did.
func (t Theme) Result(m Mark, text string) string {
	switch m {
	case Pass:
		return t.OK.Render(string(m)) + " " + text
	case Warn:
		return t.Warn.Render(string(m)) + " " + text
	case Fail:
		return t.Fail.Render(string(m)) + " " + t.Fail.Render(text)
	}
	return text
}

// Collapsed stands for the lines a step printed and the feed does not show;
// the full view opens on ctrl+o.
func (t Theme) Collapsed(hidden int, noun string) string {
	if noun == "" {
		noun = "lines"
	}
	return t.Dim.Render(fmt.Sprintf("… +%d %s (ctrl+o to expand)", hidden, noun))
}

// Entry is a whole entry of the feed: the step, its lines, and a blank line
// before it that keeps entries apart the way Claude Code keeps them.
func (t Theme) Entry(tone Tone, title string, lines []string, width int) string {
	s := "\n" + t.Step(tone, title, width)
	if len(lines) > 0 {
		s += "\n" + t.Output(lines, width)
	}
	return s
}

// Tail is what the feed shows of long output: the first lines and a count of
// the rest. Shorter output is shown whole.
func (t Theme) Tail(lines []string, keep int, noun string) []string {
	if len(lines) <= keep+1 {
		return lines
	}
	out := append([]string(nil), lines[:keep]...)
	return append(out, t.Collapsed(len(lines)-keep, noun))
}

var spinFrames = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

// Spinner is the line of a step at work: its glyph turns, its time runs, and
// the hint says what ctrl+c does now.
func (t Theme) Spinner(frame int, verb string, elapsed time.Duration, hint string, width int) string {
	glyph := t.Spin.Render(spinFrames[frame%len(spinFrames)])
	tail := Elapsed(elapsed)
	if hint != "" {
		tail += " · " + hint
	}
	return glyph + " " + fit(verb+"… "+t.Dim.Render("("+tail+")"), width-2)
}

// Elapsed is a duration the way the feed prints it: 41s, 3m 07s.
func Elapsed(d time.Duration) string {
	s := int(d / time.Second)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %02ds", s/60, s%60)
}
