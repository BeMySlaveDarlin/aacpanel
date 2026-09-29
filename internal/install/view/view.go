// Package view shows a run of the installer: the screen, where the feed of
// steps stays in the scrollback over a live part at the bottom, and the plain
// view, the same lines with no live part and no colour, for a pipe, a CI log
// or --plain. Both take the events of the run; the engine knows neither.
package view

import (
	"fmt"
	"io"
	"strings"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

// PlainWidth is the width of the plain view: a log has no terminal to ask.
const PlainWidth = 80

// shown is how many lines of a step the screen shows before it folds the
// passed ones away. A stop and a warning are never folded.
const shown = 5

// said is a line under a step.
type said struct {
	mark install.Mark
	text string
}

func (s said) render(t ui.Theme) string {
	switch s.mark {
	case install.Pass:
		return t.Result(ui.Pass, s.text)
	case install.Warn:
		return t.Result(ui.Warn, s.text)
	case install.Stop:
		return t.Result(ui.Fail, s.text)
	}
	return s.text
}

// lines is the line wrapped into width, its later lines hanging under the
// text rather than under the mark, so a long stop reads as one item.
func (s said) lines(t ui.Theme, width int) []string {
	if s.mark == install.Note {
		return ui.Indent(s.text, "", "", width)
	}
	wrapped := ui.Indent(s.text, "", "", width-2)
	out := []string{said{s.mark, wrapped[0]}.render(t)}
	for _, l := range wrapped[1:] {
		if s.mark == install.Stop {
			l = t.Fail.Render(l)
		}
		out = append(out, "  "+l)
	}
	return out
}

// feedWidth is the room a line under a step has: the width less the five
// columns of "  ⎿  ".
func feedWidth(width int) int { return width - 5 }

// fold keeps every stop and warning and as many passes as fit into shown,
// in their order, and counts the passes left out.
func fold(lines []said) (kept []said, folded int) {
	room := shown
	for _, l := range lines {
		if l.mark != install.Pass {
			room--
		}
	}
	for _, l := range lines {
		if l.mark == install.Pass {
			if room <= 0 {
				folded++
				continue
			}
			room--
		}
		kept = append(kept, l)
	}
	return kept, folded
}

func failed(lines []said) bool {
	for _, l := range lines {
		if l.mark == install.Stop {
			return true
		}
	}
	return false
}

// entry is a finished step in the feed. With fold the passes past the first
// few give way to a line that says how many there are.
func entry(t ui.Theme, title string, lines []said, doFold bool, width int) string {
	tone := ui.Good
	if failed(lines) {
		tone = ui.Bad
	}
	kept, folded := lines, 0
	if doFold {
		kept, folded = fold(lines)
	}
	var out []string
	for _, l := range kept {
		out = append(out, l.lines(t, feedWidth(width))...)
	}
	if folded > 0 {
		out = append(out, t.Collapsed(folded, "checks"))
	}
	return t.Entry(tone, title, out, width)
}

// Welcome is the frame that opens a run: where and as whom the installer
// runs, and what it found of an earlier install.
func Welcome(t ui.Theme, f install.Facts, command string, width int) string {
	clone := f.Short(f.Clone)
	if f.Version != "" {
		clone += "  (" + f.Version + ")"
	}
	rows := []ui.Row{
		{Text: t.Spin.Render("✻") + " " + t.Strong.Render("aacpanel installer")},
		{},
		{Text: "  clone    " + clone},
		{Text: fmt.Sprintf("  user     %s (uid %d)", f.Account.Name, f.Account.UID)},
		{Text: "  mode     " + modeLine(f.Mode)},
	}
	if command == "plan" {
		rows = append(rows, ui.Row{Text: "  plan     looks only, changes nothing"})
	}
	return t.Frame(ui.Frame{Edge: t.Edge, Rows: rows}, width)
}

// modeLine is the mode as the welcome says it, short enough for a frame of
// the narrowest terminal.
func modeLine(m install.Mode) string {
	switch m {
	case install.Upgrade:
		return "update — the installer's manifest is here"
	case install.Adopt:
		return "adopt — the panel is here, installed by hand"
	}
	return "fresh install — no trace of the panel here"
}

// traces is the entry of what an earlier install left, for a run that is
// not a fresh one: the person sees what the installer takes as its own.
func traces(t ui.Theme, f install.Facts, width int) string {
	if f.Mode == install.Fresh || len(f.Traces) == 0 {
		return ""
	}
	var lines []string
	for _, tr := range f.Traces {
		lines = append(lines, f.Short(tr))
	}
	return t.Entry(ui.Plain, "What is here already", lines, width)
}

// closing is the last line of a run that ends before the steps: after the
// check, or at an answer that stops the install.
func closing(t ui.Theme, stops int, command string, width int) string {
	again := "./install.sh plan"
	if command == "install" {
		again = "./install.sh"
	}
	var s string
	switch stops {
	case 0:
		s = "Nothing on this machine was changed. The machine can take the panel."
	case 1:
		s = "Nothing on this machine was changed. One line above stops the install: fix it and run " + again + " again."
	default:
		s = fmt.Sprintf("Nothing on this machine was changed. %d lines above stop the install: fix them and run %s again.", stops, again)
	}
	return "\n" + strings.Join(ui.Indent(s, " ", " ", width), "\n")
}

// Plain is the plain view of a run: each event goes out as its line the
// moment it comes, so a log shows how far a run got even if it dies. Its
// lines are the screen's with every escape taken out — bold and faint too,
// which the theme keeps without colour but a log shows as garbage.
type Plain struct {
	W     io.Writer
	T     ui.Theme
	Width int
	first bool // the next line under the step is its first
}

// Print writes text of the feed.
func (p *Plain) Print(text string) {
	fmt.Fprintln(p.W, ui.Strip(text))
}

// Sink takes the events of a run.
func (p *Plain) Sink(e install.Event) {
	switch e.Type {
	case install.Opened:
		p.Print("\n" + p.T.Step(ui.Plain, e.Title, p.Width))
		p.first = true
	case install.Said:
		p.under(said{e.Mark, e.Text}.lines(p.T, feedWidth(p.Width)))
	case install.Changed:
		p.under([]string{p.T.Dim.Render("+ " + e.Text)})
	case install.Closed:
		if e.Already {
			p.under([]string{p.T.Result(ui.Pass, "in place already: nothing to do")})
		}
	}
}

// under puts lines under the step at work: the first of them behind ⎿.
func (p *Plain) under(lines []string) {
	if p.first {
		p.Print(p.T.Output(lines, p.Width))
		p.first = false
		return
	}
	for i := range lines {
		lines[i] = "     " + lines[i]
	}
	p.Print(strings.Join(lines, "\n"))
}

// collect is the sink of the screen: it keeps what a step said until the
// step is over and the feed prints it whole.
type collect struct{ lines []said }

func (c *collect) sink(e install.Event) {
	if e.Type == install.Said {
		c.lines = append(c.lines, said{e.Mark, e.Text})
	}
}
