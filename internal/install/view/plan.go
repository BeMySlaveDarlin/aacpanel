package view

import (
	"context"
	"errors"
	"fmt"
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
	// Survey makes the questions of the run from what the check found, with
	// the flags of the command line in it. Without it plan stops at the
	// check.
	Survey func(install.Inspection) *install.Survey
	// Save writes the plan to a file when the person asks for it, and says
	// where it went.
	Save func(text string) (string, error)
	// Begin opens the run of the plan once it is approved: install has
	// one, plan stops at the plan. Yes is --yes, which approves it without
	// a terminal.
	Begin Begin
	Yes   bool
	// Adopt approves taking over an install by hand without a terminal:
	// --yes alone does not, since a person approves the plan of that.
	Adopt bool
	// Command is the command of the run, "update" for one; empty is plan
	// or install, by whether there is Begin.
	Command string
}

// done is the title of the report of a run that went through.
func (o PlanOptions) done() string {
	if o.command() == "update" {
		return "Updated"
	}
	return "Installed"
}

// Plan shows what the installer sees of the machine, asks what the install
// would ask and shows the plan it makes of the answers. It changes nothing.
// It returns the exit status: 1 when the check or an answer stops the
// install, 130 when the person stopped it first.
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
	if o.Survey != nil {
		in = install.Offered(in)
	}
	p := &Plain{W: o.Out, T: o.Theme, Width: PlainWidth}
	p.Print(Welcome(o.Theme, in.Facts, o.command(), PlainWidth))
	(&install.Run{Sink: p.Sink}).Do(install.Check(in))
	if tr := traces(o.Theme, in.Facts, PlainWidth); tr != "" {
		p.Print(tr)
	}
	if in.Stops() > 0 || o.Survey == nil {
		p.Print(closing(o.Theme, in.Stops(), o.command(), PlainWidth))
		return statusOf(in)
	}
	s := o.Survey(in)
	stop := func(err error) int {
		p.Print(o.Theme.Entry(ui.Bad, "Answers", []string{said{install.Stop, err.Error()}.render(o.Theme)}, PlainWidth))
		p.Print(closing(o.Theme, 1, o.command(), PlainWidth))
		return 1
	}
	if s.Earlier() {
		q := s.KeepQuestion()
		g, err := s.Answer(q)
		if err != nil {
			return stop(err)
		}
		s.Keep(g)
		p.Print(o.Theme.Entry(ui.Asked, "Earlier install", s.Lines(""), PlainWidth))
	}
	// Blocks of one title — whether to tune, and the tuning — make one entry.
	var lines []string
	for i, b := range install.Blocks {
		if open, err := s.Settle(b); err != nil {
			return stop(err)
		} else if len(open) > 0 {
			return stop(install.Unasked(open[0]))
		}
		lines = append(lines, s.Lines(b)...)
		if i+1 < len(install.Blocks) && install.Blocks[i+1].Title() == b.Title() {
			continue
		}
		if len(lines) > 0 {
			p.Print(o.Theme.Entry(ui.Asked, b.Title(), lines, PlainWidth))
		}
		lines = nil
	}
	p.Print("\n" + planFrame(o.Theme, s, PlainWidth))
	if o.Begin == nil {
		p.Print(planned(o.Theme, PlainWidth))
		return 0
	}
	if !o.Yes {
		return stop(errors.New(`stop: no terminal to ask "Would you like to proceed?"; pass --yes`))
	}
	if in.Mode == install.Adopt && !o.Adopt {
		return stop(errors.New("stop: the panel here was installed by hand, and taking it over is approved by a person: " +
			"run ./install.sh at a terminal, or pass --adopt with --yes"))
	}
	p.Print(o.Theme.Step(ui.Asked, "Would you like to proceed? → Yes (--yes)", PlainWidth))
	return runPlain(o, p, s)
}

// command is the command of the run, as the welcome and the last line
// name it.
func (o PlanOptions) command() string {
	if o.Command != "" {
		return o.Command
	}
	if o.Begin != nil {
		return "install"
	}
	return "plan"
}

func statusOf(in install.Inspection) int {
	if in.Stops() > 0 {
		return 1
	}
	return 0
}

// planFrame is the plan in the frame Claude Code draws around a plan to
// approve.
func planFrame(t ui.Theme, s *install.Survey, width int) string {
	var rows []ui.Row
	for _, r := range s.Plan() {
		rows = append(rows, ui.Row{Text: r.Text, Tag: r.Tag, Head: r.Head})
	}
	return t.Frame(ui.Frame{Title: "Install plan", Edge: t.PlanEdge, Rows: rows}, width)
}

// planned is the last line of plan once the answers are in.
func planned(t ui.Theme, width int) string {
	return "\n" + strings.Join(ui.Indent("Nothing on this machine was changed: plan only looks. "+
		"./install.sh asks the same questions and goes on to install.", " ", " ", width), "\n")
}

type planStage int

const (
	looking        planStage = iota // the check is at work
	looked                          // the check stopped the install; ctrl+o opens the folded lines
	questioning                     // a block of questions is open
	proceeding                      // the plan waits for its answer
	running                         // the steps run
	handing                         // a command as root waits for its answer
	answering                       // a step waits for the answer to a question
	enrollingStage                  // the code of the first device is on the screen
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
// over, the feed with the check folded to what matters, then the questions
// block by block, and the plan in a frame with its question.
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

	s       *install.Survey
	ask     *asking
	cur     int             // the block of install.Blocks open, or -1 for the question of an earlier install
	asked   map[int]*asking // the blocks answered on the screen, by their place, to go back to
	history []int           // the places of the blocks asked, in order
	built   map[int]string  // what a block was built from, to know when to build it again
	feed    []string        // entries of this update, printed together
	then    []tea.Cmd       // what runs after them

	live ui.Live
	// out prints into the feed through the live part; a test puts its own
	// in place to read the feed.
	out    func(text string, after ...tea.Cmd) tea.Cmd
	status int

	// The run of the approved plan.
	run       *install.Run
	steps     []*install.Step
	queue     *feedQueue
	cancel    context.CancelFunc
	tasks     []ui.Task
	at        int      // the step at work
	stepSaid  []said   // what it said so far
	output    []string // what its commands printed
	since     time.Time
	ranSince  time.Time
	stopping  bool
	hand      *handMsg
	handAsk   *ui.Block
	changed   []string // what the run recorded, as its events told
	peekTitle string   // the step whose output ctrl+o opens
	peekLines []string
	listen    tea.Cmd // what the screen waits on once the run begins
	asked2    *askMsg
	runAsk    *asking
	enroll    *enrollMsg
	enrolling *enrolling
}

func newPlanModel(o PlanOptions) *planModel {
	m := &planModel{o: o, t: o.Theme, now: time.Now, asked: map[int]*asking{}, built: map[int]string{}}
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

func (m *planModel) say(s string) { m.feed = append(m.feed, s) }

func (m *planModel) end(status int) {
	m.stage, m.status = over, status
	m.then = append(m.then, tea.Quit)
}

func (m *planModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ok, cmd := m.live.Update(msg); ok {
		return m, cmd
	}
	var next tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.pager != nil {
			m.pager.Resize(m.width(), m.h)
		}
	case tickMsg:
		m.frame++
		if m.stage == looking || m.stage == running || m.stage == handing || m.stage == answering || m.stage == enrollingStage {
			next = tick()
		}
	case inspectedMsg:
		m.inspected(msg.in)
	case eventMsg, handMsg, endMsg, askMsg, enrollMsg:
		next = m.onRun(msg)
	case codeMsg:
		if m.enrolling != nil {
			m.enrolling.code(msg)
		}
	case handedMsg:
		m.onHanded(msg)
	case shownMsg:
		m.handAsk = rootConfirm()
	case tea.PasteMsg:
		if m.ask != nil && m.pager == nil {
			m.ask.ui.Paste(msg.Content)
		}
	case tea.KeyPressMsg:
		m.onKey(msg)
	}
	if m.listen != nil {
		next = tea.Batch(next, m.listen)
		m.listen = nil
	}
	if m.pager != nil || (len(m.feed) == 0 && len(m.then) == 0) {
		return m, next
	}
	out := m.out(strings.Join(m.feed, "\n"), m.then...)
	m.feed, m.then = nil, nil
	return m, tea.Batch(out, next)
}

// inspected prints the feed of the check: the welcome with the mode, the
// check, what an earlier install left. A check that stops the install ends
// there; otherwise the questions begin.
func (m *planModel) inspected(in install.Inspection) {
	if m.stage != looking {
		return
	}
	if m.o.Survey != nil {
		in = install.Offered(in)
	}
	w := m.width()
	var c collect
	(&install.Run{Sink: c.sink}).Do(install.Check(in))
	m.say("\n" + Welcome(m.t, in.Facts, m.o.command(), w))
	m.say(entry(m.t, "Check the machine", c.lines, true, w))
	if tr := traces(m.t, in.Facts, w); tr != "" {
		m.say(tr)
	}
	for _, l := range c.lines {
		m.all = append(m.all, l.render(m.t))
	}
	m.status = statusOf(in)
	if in.Stops() > 0 || m.o.Survey == nil {
		m.say(closing(m.t, in.Stops(), m.o.command(), w))
		if _, folded := fold(c.lines); folded > 0 {
			m.stage = looked
			return
		}
		m.end(m.status)
		return
	}
	m.s = m.o.Survey(in)
	if m.s.Earlier() {
		q := m.s.KeepQuestion()
		if g, err := m.s.Answer(q); err == nil {
			m.s.Keep(g)
		} else {
			m.cur = -1
			m.ask = newAsking(m.s, "", "Earlier install", []install.Question{q})
			m.stage = questioning
			return
		}
	}
	m.open(0)
}

// open goes to the first block from i on that has something to ask; past
// the last, the plan.
func (m *planModel) open(i int) {
	for ; i < len(install.Blocks); i++ {
		b := install.Blocks[i]
		qs, err := m.s.Settle(b)
		if err != nil {
			m.stopped(err)
			return
		}
		if len(qs) == 0 {
			continue
		}
		shape := shapeOf(qs)
		a := m.asked[i]
		if a == nil || m.built[i] != shape {
			a = newAsking(m.s, b, b.Title(), qs)
			m.asked[i], m.built[i] = a, shape
		}
		a.refresh(m.s)
		m.cur, m.ask, m.stage = i, a, questioning
		return
	}
	m.showPlan()
}

// shapeOf is what a block is built from: its questions and their options.
// A block met again with the same shape keeps the answers given on it.
func shapeOf(qs []install.Question) string {
	var b strings.Builder
	for _, q := range qs {
		b.WriteString(q.ID)
		for _, o := range q.Options {
			b.WriteString("\x00" + o.Value)
		}
		b.WriteString("\x01")
	}
	return b.String()
}

func (m *planModel) stopped(err error) {
	w := m.width()
	m.say(m.t.Entry(ui.Bad, "Answers", []string{said{install.Stop, err.Error()}.render(m.t)}, w))
	m.say(closing(m.t, 1, m.o.command(), w))
	m.end(1)
}

func (m *planModel) showPlan() {
	w := m.width()
	m.say("\n" + planFrame(m.t, m.s, w))
	opts := []install.Option{{Value: "yes", Label: "Yes"}, {Value: "back", Label: "No, back to the answers"},
		{Value: "save", Label: "Save the plan to a file and quit"}}
	m.ask = newAsking(m.s, "", "Install plan", []install.Question{{ID: "proceed", Prompt: "Would you like to proceed?", Options: opts}})
	m.ask.ui.Bare = true
	m.ask.ui.EscHint = "go back to the answers"
	m.stage = proceeding
}

func (m *planModel) onKey(k tea.KeyPressMsg) {
	if m.pager != nil {
		if m.pager.Update(k) {
			m.pager = nil
		}
		return
	}
	switch {
	case m.stage == enrollingStage:
		// The panel is in and checked: a Ctrl+C here only ends the frame.
		m.onEnrollKey(k)
		return
	case key.Matches(k, ui.Keys.Stop) && m.stage == answering:
		m.stopping = true
		m.run.Stop()
		m.onAnswer(ui.Back)
		return
	case key.Matches(k, ui.Keys.Stop) && m.stage == handing:
		m.stopping = true
		m.run.Stop()
		m.onHand(ui.Back)
		return
	case key.Matches(k, ui.Keys.Stop) && m.stage == running:
		m.stopRun()
		return
	case key.Matches(k, ui.Keys.Expand) && (len(m.peekLines) > 0 || (m.stage == running && len(m.output) > 0)):
		// The step at work first, else the last one that printed.
		title, lines := m.peekTitle, m.peekLines
		if m.stage == running && len(m.output) > 0 {
			title, lines = m.tasks[m.at].Title, append([]string(nil), m.output...)
		}
		if !m.live.Busy() {
			m.pager = ui.NewPager(title+" — every line", lines, m.width(), m.h)
		}
		return
	case key.Matches(k, ui.Keys.Stop):
		if m.stage != over {
			m.say(m.t.Entry(ui.Bad, "Stopped", []string{"Nothing on this machine was changed."}, m.width()))
			m.end(130)
		}
		return
	case key.Matches(k, ui.Keys.Expand):
		if len(m.all) > 0 && !m.live.Busy() {
			m.pager = ui.NewPager("Check the machine — every line", m.all, m.width(), m.h)
			m.pager.Top()
		}
		return
	}
	switch m.stage {
	case looked:
		if key.Matches(k, ui.Keys.Pick, ui.Keys.Back) || k.String() == "q" {
			m.end(m.status)
		}
	case questioning:
		m.onAsk(m.ask.ui.Update(k))
	case proceeding:
		m.onProceed(m.ask.ui.Update(k))
	case handing:
		m.onHand(m.handAsk.Update(k))
	case answering:
		m.onAnswer(m.runAsk.ui.Update(k))
	}
}

func (m *planModel) onAsk(out ui.Outcome) {
	switch out {
	case ui.Submitted:
		m.say(m.ask.ui.Summary(m.t, m.width()))
		if m.cur < 0 {
			v := m.ask.value(0)
			m.s.Keep(install.Given{Value: v, Source: m.s.Source(m.ask.specs[0], v)})
			m.history = nil
			m.open(0)
			return
		}
		m.ask.give(m.s)
		if err := m.s.Refused(); err != nil {
			m.stopped(err)
			return
		}
		m.history = append(m.history, m.cur)
		m.open(m.cur + 1)
	case ui.Back:
		m.back()
	}
}

// back reopens the block answered last, at its last tab; before the first
// block is the question of an earlier install, if there was one, and
// otherwise the first block stays.
func (m *planModel) back() {
	if n := len(m.history); n > 0 {
		i := m.history[n-1]
		m.history = m.history[:n-1]
		m.cur, m.ask, m.stage = i, m.asked[i], questioning
		m.ask.ui.Reopen()
		return
	}
	switch {
	case m.stage == proceeding:
		// Nothing was asked on the screen: --yes or the kept settings took
		// every answer, and going back is asking to see them.
		m.s.Review()
		m.open(0)
	case m.s.Earlier() && m.cur >= 0:
		m.cur = -1
		m.ask = newAsking(m.s, "", "Earlier install", []install.Question{m.s.KeepQuestion()})
		m.stage = questioning
	}
}

func (m *planModel) onProceed(out ui.Outcome) {
	w := m.width()
	choice := ""
	switch out {
	case ui.Submitted:
		choice = m.ask.value(0)
	case ui.Back:
		choice = "back"
	default:
		return
	}
	switch choice {
	case "yes":
		m.say(m.t.Step(ui.Asked, "Would you like to proceed? → "+m.t.Accent.Render("Yes"), w))
		if m.o.Begin != nil {
			m.startRun()
			return
		}
		m.say(planned(m.t, w))
		m.end(0)
	case "back":
		m.back()
	case "save":
		m.save()
	}
}

func (m *planModel) save() {
	w := m.width()
	if m.o.Save == nil {
		m.stopped(errors.New("this run has nowhere to save the plan"))
		return
	}
	where, err := m.o.Save(ui.Strip(planFrame(m.t, m.s, PlainWidth)) + "\n")
	if err != nil {
		m.say(m.t.Entry(ui.Bad, "Plan not saved", []string{m.t.Result(ui.Fail, err.Error())}, w))
		m.end(1)
		return
	}
	m.say(m.t.Entry(ui.Good, "Plan saved", []string{fmt.Sprintf("in %s; nothing else on this machine was changed.", where)}, w))
	m.end(0)
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
// the block of questions or the plan's question, or the keys that are left.
func (m *planModel) bottom() string {
	w := m.width()
	switch m.stage {
	case looking:
		return "\n" + m.t.Spinner(m.frame, "Checking the machine", m.now().Sub(m.began), "", w)
	case looked:
		return "\n" + m.t.Dim.Render(ui.Fit(" ctrl+o to expand the check · enter to finish", w))
	case questioning, proceeding:
		return "\n" + m.ask.ui.View(m.t, w, m.h-1)
	case running:
		return m.runView(w)
	case handing:
		return "\n" + m.handAsk.View(m.t, w, m.h-1)
	case answering:
		return "\n" + m.runAsk.ui.View(m.t, w, m.h-1)
	case enrollingStage:
		return "\n" + m.enrolling.frame(m.t, w)
	}
	return ""
}
