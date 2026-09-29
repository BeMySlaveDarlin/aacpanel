package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// Pager is the full view of what the feed collapsed — the whole output of a
// step, or a file shown before it runs — the way Claude Code opens its
// transcript on ctrl+o. It takes the whole screen (the caller sets AltScreen),
// so the feed below stays as it was and comes back on Esc.
type Pager struct {
	Title string
	vp    viewport.Model
	lines []string
}

// NewPager opens a pager on lines, scrolled to the bottom: the end of the
// output is what one opens it for.
func NewPager(title string, lines []string, width, height int) *Pager {
	p := &Pager{Title: title, vp: viewport.New()}
	p.vp.SoftWrap = true
	p.Resize(width, height)
	p.SetLines(lines)
	p.vp.GotoBottom()
	return p
}

// SetLines replaces what the pager shows. A pager left at the bottom follows
// new lines, like a tail; one scrolled up stays where the person put it.
func (p *Pager) SetLines(lines []string) {
	follow := p.vp.AtBottom()
	p.lines = lines
	p.vp.SetContentLines(lines)
	if follow {
		p.vp.GotoBottom()
	}
}

// Top scrolls to the first line: a file is read from the start.
func (p *Pager) Top() { p.vp.GotoTop() }

// Resize fits the pager to the terminal: a line of title, a rule and a line
// of hints around the text.
func (p *Pager) Resize(width, height int) {
	p.vp.SetWidth(max(1, width))
	p.vp.SetHeight(max(1, height-4))
}

// Update scrolls, and says whether the key closed the pager.
func (p *Pager) Update(k tea.KeyPressMsg) (closed bool) {
	if key.Matches(k, Keys.Back, Keys.Expand) || k.String() == "q" {
		return true
	}
	p.vp, _ = p.vp.Update(k)
	return false
}

// View draws the pager at the width it was sized for.
func (p *Pager) View(t Theme) string {
	w := p.vp.Width()
	noun := "lines"
	if len(p.lines) == 1 {
		noun = "line"
	}
	where := fmt.Sprintf("%d %s · %.0f%%", len(p.lines), noun, p.vp.ScrollPercent()*100)
	head := t.Strong.Render(fit(p.Title, w-Width(where)-2))
	head = pad(head, w-Width(where)) + t.Dim.Render(where)
	return strings.Join([]string{
		head,
		t.Dim.Render(strings.Repeat("─", w)),
		p.vp.View(),
		t.Dim.Render(strings.Repeat("─", w)),
		t.Dim.Render(fit("↑/↓ PgUp/PgDn to scroll · Esc to go back", w)),
	}, "\n")
}
