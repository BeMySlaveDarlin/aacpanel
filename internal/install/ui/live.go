package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Live keeps the live part at the bottom from leaving debris behind. Bubble
// Tea draws an inline view relative to where it left the cursor, and it
// knows that place for sure only right after a print above the view. Two
// things go wrong otherwise:
//
//   - A view that grows shorter lands lower than the old one, and the old
//     top lines stay on screen.
//   - A print scrolls the screen to make room, and a tall view scrolls into
//     the scrollback with it: the feed keeps a stale copy of a question that
//     was already answered.
//
// So between prints the live part only grows: a shorter view is padded with
// blank lines at the bottom to the height it has had since the last print.
// A print goes in two parts. First the blank line that opens every entry of
// the feed, with the view held as it was; after it the renderer stands at
// the top of the view again, so the view can go empty there. Then, once the
// empty view is on screen, the entry itself, which scrolls only the feed.
// Prints wait for one another, and so does whatever was to run after one —
// a quit, a handover of the terminal.
type Live struct {
	high  int
	last  string
	state liveState
	jobs  []job
}

type liveState int

const (
	idle    liveState = iota
	held              // the opening line is on its way; the view stays as it was
	emptied           // the view is empty until the entry is out
)

type job struct {
	text  string
	after []tea.Cmd
}

// settleFor is how long an entry waits for the empty view to reach the
// screen: a few frames of the renderer, which draws sixty a second.
const settleFor = 50 * time.Millisecond

type (
	opened  struct{}
	settled struct{}
	printed struct{}
)

// Out prints text into the feed above the live part and then runs after,
// in order. With no text it only runs after, still behind the prints before
// it.
func (l *Live) Out(text string, after ...tea.Cmd) tea.Cmd {
	if text == "" && len(after) == 0 {
		return nil
	}
	l.jobs = append(l.jobs, job{text: strings.TrimPrefix(text, "\n"), after: after})
	if l.state != idle || len(l.jobs) > 1 {
		return nil
	}
	return l.start()
}

func (l *Live) start() tea.Cmd {
	j := l.jobs[0]
	if j.text == "" {
		l.jobs = l.jobs[1:]
		cmds := append([]tea.Cmd(nil), j.after...)
		if len(l.jobs) > 0 {
			cmds = append(cmds, l.start())
		}
		return tea.Sequence(cmds...)
	}
	l.state = held
	return tea.Sequence(
		tea.Println(" "),
		func() tea.Msg { return opened{} },
		func() tea.Msg { time.Sleep(settleFor); return settled{} },
		tea.Println(j.text),
		func() tea.Msg { return printed{} },
	)
}

// Update takes the messages of a print on its way and says whether msg was
// one of them; the command runs what waited for the print.
func (l *Live) Update(msg tea.Msg) (bool, tea.Cmd) {
	switch msg.(type) {
	case opened:
		l.state = emptied
		return true, nil
	case settled:
		return true, nil
	case printed:
		l.state, l.high = idle, 0
		if len(l.jobs) == 0 {
			return true, nil
		}
		j := l.jobs[0]
		l.jobs = l.jobs[1:]
		cmds := append([]tea.Cmd(nil), j.after...)
		if len(l.jobs) > 0 {
			cmds = append(cmds, l.start())
		}
		return true, tea.Sequence(cmds...)
	}
	return false, nil
}

// Busy tells whether a print is on its way. The full screen of a pager
// opened now would take the print with it.
func (l *Live) Busy() bool { return l.state != idle || len(l.jobs) > 0 }

// View is the live part as the terminal should get it.
func (l *Live) View(content string) string {
	switch l.state {
	case held:
		return l.last
	case emptied:
		l.high = 0
		return ""
	}
	lines := strings.Count(content, "\n") + 1
	if lines < l.high {
		content += strings.Repeat("\n", l.high-lines)
	}
	l.high = max(l.high, lines)
	l.last = content
	return content
}
