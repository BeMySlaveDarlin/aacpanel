package view

import (
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

// PlanOptions shape a run of plan.
type PlanOptions struct {
	Theme ui.Theme
	// Plain asks for the plain view; the command line sets it for --plain
	// and for a run without a terminal.
	Plain bool
	// Out is where the plain view writes.
	Out io.Writer
	// Inspect looks the machine over. It is the slow part: the screen turns
	// its spinner meanwhile.
	Inspect func() install.Inspection
}

// Plan shows what the installer sees of the machine and changes nothing. It
// returns the exit status: 1 when the check stops the install, 130 when the
// person stopped it first.
func Plan(o PlanOptions) (int, error) {
	if o.Plain {
		return planPlain(o), nil
	}
	m := newPlanModel(o)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return 1, err
	}
	return m.status, nil
}

func planPlain(o PlanOptions) int {
	in := o.Inspect()
	p := &Plain{W: o.Out, T: o.Theme, Width: PlainWidth}
	p.Print(Welcome(o.Theme, in.Facts, "plan", PlainWidth))
	(&install.Run{Sink: p.Sink}).Do(install.Check(in))
	if tr := traces(o.Theme, in.Facts, PlainWidth); tr != "" {
		p.Print(tr)
	}
	p.Print(closing(o.Theme, in.Stops(), PlainWidth))
	return statusOf(in)
}

func statusOf(in install.Inspection) int {
	if in.Stops() > 0 {
		return 1
	}
	return 0
}

type planStage int

const (
	looking planStage = iota // the check is at work
	looked                   // the feed is printed; ctrl+o opens the folded lines
	over
)

type (
	tickMsg      struct{}
	inspectedMsg struct{ in install.Inspection }
)

const frameTick = 100 * time.Millisecond

func tick() tea.Cmd {
	return tea.Tick(frameTick, func(time.Time) tea.Msg { return tickMsg{} })
}

// planModel is plan on the screen: a spinner while the machine is looked
// over, then the feed, and the check folded to what matters with the rest a
// ctrl+o away.
type planModel struct {
	o     PlanOptions
	t     ui.Theme
	w, h  int
	stage planStage
	frame int
	began time.Time
	now   func() time.Time

	all   []string // every line of the check, for the pager
	pager *ui.Pager

	live ui.Live
	// out prints into the feed through the live part; a test puts its own
	// in place to read the feed.
	out    func(text string, after ...tea.Cmd) tea.Cmd
	status int
}

func newPlanModel(o PlanOptions) *planModel {
	m := &planModel{o: o, t: o.Theme, now: time.Now}
	m.began = m.now()
	m.out = m.live.Out
	return m
}

func (m *planModel) Init() tea.Cmd {
	inspect := m.o.Inspect
	return tea.Batch(tick(), func() tea.Msg { return inspectedMsg{inspect()} })
}

func (m *planModel) width() int {
	if m.w > 0 {
		return m.w
	}
	return 80
}

func (m *planModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ok, cmd := m.live.Update(msg); ok {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.pager != nil {
			m.pager.Resize(m.width(), m.h)
		}
	case tickMsg:
		m.frame++
		if m.stage != over {
			return m, tick()
		}
	case inspectedMsg:
		return m, m.inspected(msg.in)
	case tea.KeyPressMsg:
		return m, m.onKey(msg)
	}
	return m, nil
}

// inspected prints the feed of the run: the welcome with the mode, the
// check, what an earlier install left, and the closing line.
func (m *planModel) inspected(in install.Inspection) tea.Cmd {
	if m.stage != looking {
		return nil
	}
	w := m.width()
	var c collect
	(&install.Run{Sink: c.sink}).Do(install.Check(in))
	parts := []string{"\n" + Welcome(m.t, in.Facts, "plan", w), entry(m.t, "Check the machine", c.lines, true, w)}
	if tr := traces(m.t, in.Facts, w); tr != "" {
		parts = append(parts, tr)
	}
	parts = append(parts, closing(m.t, in.Stops(), w))
	m.status = statusOf(in)
	for _, l := range c.lines {
		m.all = append(m.all, l.render(m.t))
	}
	if _, folded := fold(c.lines); folded > 0 {
		m.stage = looked
		return m.out(strings.Join(parts, "\n"))
	}
	m.stage = over
	return m.out(strings.Join(parts, "\n"), tea.Quit)
}

func (m *planModel) onKey(k tea.KeyPressMsg) tea.Cmd {
	if m.pager != nil {
		if m.pager.Update(k) {
			m.pager = nil
		}
		return nil
	}
	switch m.stage {
	case looking:
		if key.Matches(k, ui.Keys.Stop) {
			m.stage, m.status = over, 130
			return m.out(m.t.Entry(ui.Bad, "Stopped", []string{"Nothing on this machine was changed."}, m.width()), tea.Quit)
		}
	case looked:
		switch {
		case key.Matches(k, ui.Keys.Expand):
			if !m.live.Busy() {
				m.pager = ui.NewPager("Check the machine — every line", m.all, m.width(), m.h)
				m.pager.Top()
			}
		case key.Matches(k, ui.Keys.Stop, ui.Keys.Pick, ui.Keys.Back) || k.String() == "q":
			m.stage = over
			return m.out("", tea.Quit)
		}
	}
	return nil
}

func (m *planModel) View() tea.View {
	if m.pager != nil {
		v := tea.NewView(m.pager.View(m.t))
		v.AltScreen = true
		return v
	}
	return tea.NewView(m.live.View(m.bottom()))
}

// bottom is the live part: the spinner while the machine is looked over,
// then the keys that are left.
func (m *planModel) bottom() string {
	w := m.width()
	switch m.stage {
	case looking:
		return "\n" + m.t.Spinner(m.frame, "Checking the machine", m.now().Sub(m.began), "", w)
	case looked:
		return "\n" + m.t.Dim.Render(ui.Fit(" ctrl+o to expand the check · enter to finish", w))
	}
	return ""
}
