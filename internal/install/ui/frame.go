package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Row is a line of a frame. Tag stands at the right edge — "new", "changed" —
// so a column of them reads at a glance; Head sets the row off as a heading.
type Row struct {
	Text string
	Tag  string
	Head bool
}

// Frame is a box with rounded corners, like the ones Claude Code draws around
// a plan and around a command it asks to run. Title, when there is one, sits
// in the top edge.
type Frame struct {
	Title string
	Rows  []Row
	Edge  lipgloss.Style
}

// Frame renders a box as wide as the terminal up to MaxFrame. Below MinWidth
// there is no box: the title and the rows go as plain lines, since a frame
// the terminal wraps is a frame no more.
func (t Theme) Frame(f Frame, width int) string {
	if width < MinWidth {
		return t.bare(f, width)
	}
	outer := min(width, MaxFrame)
	inner := outer - 4
	edge := f.Edge.Render

	top := edge("╭" + strings.Repeat("─", outer-2) + "╮")
	if f.Title != "" {
		title := " " + fit(f.Title, outer-6) + " "
		top = edge("╭─") + t.Strong.Render(title) + edge(strings.Repeat("─", outer-3-Width(title))+"╮")
	}
	out := []string{top}
	for _, r := range f.Rows {
		for _, line := range t.frameRow(r, inner) {
			out = append(out, edge("│")+" "+pad(line, inner)+" "+edge("│"))
		}
	}
	out = append(out, edge("╰"+strings.Repeat("─", outer-2)+"╯"))
	return strings.Join(out, "\n")
}

func (t Theme) frameRow(r Row, inner int) []string {
	text := r.Text
	if r.Head {
		text = t.Strong.Render(text)
	}
	lead := leadingSpaces(r.Text)
	cont := lead
	if strings.HasPrefix(strings.TrimLeft(r.Text, " "), "· ") {
		cont += "  " // a bullet's lines hang under its text, not under the dot
	}
	if r.Tag == "" {
		return indent(strings.TrimLeft(text, " "), lead, cont, inner)
	}
	room := inner - Width(r.Tag) - 2
	lines := indent(strings.TrimLeft(text, " "), lead, cont, room)
	lines[0] = pad(lines[0], room) + "  " + t.Dim.Render(r.Tag)
	return lines
}

func leadingSpaces(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " "))]
}

// bare is a frame on a terminal too narrow for one.
func (t Theme) bare(f Frame, width int) string {
	var out []string
	if f.Title != "" {
		out = append(out, t.Strong.Render(f.Title))
	}
	for _, r := range f.Rows {
		out = append(out, t.frameRow(r, width-2)...)
	}
	for i, line := range out {
		out[i] = strings.TrimRight("  "+line, " ")
	}
	if f.Title != "" {
		out[0] = strings.TrimPrefix(out[0], "  ")
	}
	return strings.Join(out, "\n")
}
