package demo

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"aacpanel/internal/install/ui"
)

// Options shape a run of the demo.
type Options struct {
	// Speed divides every wait of the made-up steps: 3 makes a build of a
	// minute and a half last thirty seconds. The spinner still counts the
	// time the step would take, so the screen reads as a real run.
	Speed float64
	// Fail names a step that ends in a diagnosis instead of a result:
	// "compose" stops the run at the panel stack.
	Fail  string
	Theme ui.Theme
}

// Failures are the steps --fail can name.
var Failures = []string{"compose"}

type stage int

const (
	stStart   stage = iota // the size of the terminal is not known yet
	stInspect              // the machine is being looked at
	stAsk                  // a block of questions is open
	stPlan                 // the plan waits for approval
	stRoot                 // the root command waits for approval
	stSudo                 // the terminal belongs to the shell asking for the password
	stRun                  // steps run
	stSession              // the test session is asked
	stEnroll               // the code for the first device is on screen
	stDone
)

type tickMsg struct{}

// sudoMsg comes back when the shell that had the terminal is gone.
type sudoMsg struct{ err error }

const (
	frameTick   = 100 * time.Millisecond
	inspectTook = 3 * time.Second
	codeLife    = 5 * time.Minute
)

func tick() tea.Cmd {
	return tea.Tick(frameTick, func(time.Time) tea.Msg { return tickMsg{} })
}

// slot is a block of questions in its place in the run. A block that hangs
// on earlier answers is built again when they change, and keeps its answers
// while they do not.
type slot struct {
	name  string
	when  func(m *Model) bool
	build func(m *Model) *ui.Block
	shape func(m *Model) string
	block *ui.Block
	built string
}

// Model is the whole demo as one Bubble Tea model: the feed it prints above
// itself and the live part it draws below.
type Model struct {
	opt  Options
	t    ui.Theme
	mc   machine
	w, h int

	stage stage
	slots []*slot
	cur   int
	ask   *ui.Block

	clock time.Duration // the time of the made-up run, --speed included
	frame int
	since time.Duration // when the stage or the step at work began

	a        answers
	steps    []*step
	at       int
	shown    int
	began    time.Duration
	changed  []string
	failed   int // the task that failed, or -1
	stopping bool

	peekTitle string
	peekLines []string
	pager     *ui.Pager
	follow    *step // the step whose output the pager shows as it grows
	// settle holds the feed for a few ticks after the pager closes: the way
	// back from the full screen has to reach the terminal before a print, or
	// the print lands on the full screen and is lost with it.
	settle int

	code   string
	codeAt time.Duration
	rnd    *rand.Rand

	live ui.Live
	feed []string
	then []tea.Cmd
	// out sends the feed of an update and what runs after it; a test puts
	// its own in place to read the feed.
	out func(text string, after ...tea.Cmd) tea.Cmd
	now func() time.Time

	status int
}

// New makes the demo on the made-up machine.
func New(opt Options) *Model {
	if opt.Speed <= 0 {
		opt.Speed = 1
	}
	m := &Model{
		opt: opt, t: opt.Theme, mc: helios, failed: -1,
		rnd: rand.New(rand.NewPCG(4719, 2206)),
		now: time.Now,
	}
	m.out = m.live.Out
	legsOf := func(m *Model) []string { return m.soFar().legs() }
	m.slots = []*slot{
		{name: "P", when: func(m *Model) bool { return len(m.mc.missing) > 0 },
			build: func(m *Model) *ui.Block { return prerequisites(m.mc) }},
		{name: "A", build: func(m *Model) *ui.Block { return thisMachine(m.mc) }},
		{name: "B", when: func(m *Model) bool { return len(m.mc.displays) > 0 },
			build: func(m *Model) *ui.Block { return windows(m.mc) }},
		{name: "C", build: func(m *Model) *ui.Block { return claude(m.mc) }},
		{name: "K", build: func(*Model) *ui.Block { return kit() }},
		{name: "D", when: func(m *Model) bool { return len(legsOf(m)) > 0 },
			build: func(m *Model) *ui.Block { return access(m.mc, legsOf(m)) },
			shape: func(m *Model) string { return strings.Join(legsOf(m), ",") }},
		{name: "tune", build: func(*Model) *ui.Block { return tune() }},
		{name: "F", when: func(m *Model) bool { return m.soFar().tuned },
			build: func(m *Model) *ui.Block { return more(m.mc) }},
		{name: "M",
			build: func(m *Model) *ui.Block { a := m.soFar(); return mapBlock(m.mc, a.accounts, a.roots) },
			shape: func(m *Model) string {
				a := m.soFar()
				return strings.Join(a.accounts, ",") + "|" + strings.Join(a.roots, ",")
			}},
	}
	return m
}

// Status is the exit status of a finished run: 0 installed, 1 stopped by a
// failure or a no, 130 interrupted.
func (m *Model) Status() int { return m.status }

func (m *Model) Init() tea.Cmd { return tick() }

func (m *Model) width() int {
	if m.w > 0 {
		return m.w
	}
	return 80
}

// say queues an entry of the feed. Everything said in one update goes out
// as one print, ahead of whatever the update runs next, so the feed keeps
// its order.
func (m *Model) say(s string) { m.feed = append(m.feed, s) }

func (m *Model) end(status int) {
	m.status = status
	m.stage = stDone
	m.pager = nil
	m.then = append(m.then, tea.Quit)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var next tea.Cmd
	if ok, cmd := m.live.Update(msg); ok {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.pager != nil {
			m.pager.Resize(m.width(), m.h)
		}
		if m.stage == stStart {
			m.start()
		}
	case tickMsg:
		m.onTick()
		if m.stage != stDone {
			next = tick()
		}
	case sudoMsg:
		m.onSudo(msg.err)
	case tea.PasteMsg:
		if m.ask != nil && m.pager == nil {
			m.ask.Paste(msg.Content)
		}
	case tea.KeyPressMsg:
		m.onKey(msg)
	}

	// The feed and what follows it wait while the pager holds the screen: a
	// print made then would go nowhere.
	if m.pager != nil || m.settle > 0 {
		return m, next
	}
	text := strings.Join(m.feed, "\n")
	m.feed = nil
	out := m.out(text, m.then...)
	m.then = nil
	return m, tea.Batch(out, next)
}

func (m *Model) start() {
	m.say(welcome(m.t, m.mc, m.width()))
	m.stage = stInspect
	m.since = m.clock
}

func (m *Model) onTick() {
	m.clock += time.Duration(float64(frameTick) * m.opt.Speed)
	m.frame++
	if m.settle > 0 {
		m.settle--
	}
	switch m.stage {
	case stStart:
		// A terminal that never tells its size still gets the demo, at 80.
		if m.frame >= 10 {
			m.start()
		}
	case stInspect:
		if m.clock-m.since >= inspectTook {
			m.inspected()
		}
	case stRun:
		m.run()
	}
	if s := m.follow; m.pager != nil && s != nil {
		if m.at < len(m.steps) && m.steps[m.at] == s {
			m.pager.SetLines(s.out[:m.shown])
		} else {
			m.pager.SetLines(s.out)
		}
	}
}

func (m *Model) onKey(k tea.KeyPressMsg) {
	if m.pager != nil {
		if m.pager.Update(k) {
			m.pager, m.follow, m.settle = nil, nil, 2
		}
		return
	}
	switch {
	case key.Matches(k, ui.Keys.Stop):
		m.interrupt()
		return
	case key.Matches(k, ui.Keys.Expand):
		m.expand()
		return
	}
	switch m.stage {
	case stAsk:
		m.onAsk(m.ask.Update(k))
	case stPlan:
		m.onPlan(m.ask.Update(k))
	case stRoot:
		m.onRoot(m.ask.Update(k))
	case stSession:
		m.onSession(m.ask.Update(k))
	case stEnroll:
		switch k.String() {
		case "r":
			m.newCode()
		case "q", "enter":
			m.enrolled()
		}
	}
}

func (m *Model) marked(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "✓ "):
			out[i] = m.t.Result(ui.Pass, strings.TrimPrefix(l, "✓ "))
		case strings.HasPrefix(l, "⚠ "):
			out[i] = m.t.Result(ui.Warn, strings.TrimPrefix(l, "⚠ "))
		case strings.HasPrefix(l, "✗ "):
			out[i] = m.t.Result(ui.Fail, strings.TrimPrefix(l, "✗ "))
		default:
			out[i] = l
		}
	}
	return out
}

// Ctrl+O opens the full output of what the feed folded last: the step at
// work while it prints, the last step that printed otherwise.
func (m *Model) peek() (string, []string) {
	if m.stage == stRun && m.at < len(m.steps) && m.shown > 0 {
		s := m.steps[m.at]
		return s.title, s.out[:m.shown]
	}
	return m.peekTitle, m.peekLines
}

// expand opens the pager, unless a print is on its way: the full screen
// would take it.
func (m *Model) expand() {
	title, lines := m.peek()
	if len(lines) == 0 || m.live.Busy() {
		return
	}
	m.pager = ui.NewPager(title+" — full output", lines, m.width(), m.h)
	if m.stage == stRun && m.shown > 0 {
		m.follow = m.steps[m.at]
	}
}

func (m *Model) inspected() {
	shown, folded := m.mc.inspection()
	lines := append(m.marked(shown), m.t.Collapsed(len(folded), "checks"))
	m.say(m.t.Entry(ui.Good, "Check the machine", lines, m.width()))
	m.peekTitle, m.peekLines = "Check the machine", m.marked(append(shown, folded...))
	m.open(0, true)
}

// blocks are the blocks the run has asked so far and still counts: a block
// whose condition an answer since took away is left out.
func (m *Model) blocks() map[string]*ui.Block {
	out := map[string]*ui.Block{}
	for _, s := range m.slots {
		if s.block != nil && (s.when == nil || s.when(m)) {
			out[s.name] = s.block
		}
	}
	return out
}

// soFar reads the answers given so far. The conditions of the slots read it
// too, so it looks at the blocks without asking the conditions back.
func (m *Model) soFar() answers {
	out := map[string]*ui.Block{}
	for _, s := range m.slots {
		if s.block != nil {
			out[s.name] = s.block
		}
	}
	return read(out)
}

// open goes to the slot i, or on past slots whose condition does not hold.
// Forward past the last is the plan; back past the first stays where it is.
func (m *Model) open(i int, forward bool) {
	for i >= 0 && i < len(m.slots) {
		s := m.slots[i]
		if s.when == nil || s.when(m) {
			shape := ""
			if s.shape != nil {
				shape = s.shape(m)
			}
			if s.block == nil || shape != s.built {
				s.block, s.built = s.build(m), shape
			}
			if !forward {
				s.block.Reopen()
			}
			m.cur, m.ask, m.stage = i, s.block, stAsk
			return
		}
		if forward {
			i++
		} else {
			i--
		}
	}
	if forward {
		m.showPlan()
	}
}

func (m *Model) onAsk(out ui.Outcome) {
	switch out {
	case ui.Submitted:
		m.say(m.ask.Summary(m.t, m.width()))
		if m.slots[m.cur].name == "P" {
			for _, pkg := range m.mc.missing {
				if m.ask.Value("pkg:"+pkg) == "no" {
					m.refuse(pkg)
					return
				}
			}
		}
		m.open(m.cur+1, true)
	case ui.Back:
		m.open(m.cur-1, false)
	}
}

func (m *Model) refuse(pkg string) {
	why := map[string]string{
		"tmux": "sessions live in it, and without it the executor turns off every action on sessions",
		"jq":   "the status line script reads the limits with it",
	}
	m.say(m.t.Entry(ui.Bad, "Prerequisites", []string{
		m.t.Result(ui.Fail, fmt.Sprintf("stop: %s is needed: %s. sudo apt install %s, then run ./install.sh again.", pkg, why[pkg], pkg)),
		"Nothing on this machine was changed.",
	}, m.width()))
	m.end(1)
}

func (m *Model) showPlan() {
	m.a = read(m.blocks())
	m.stage = stPlan
	// The plan goes into the feed, and only the question stays live: a plan
	// is taller than a small terminal, and the feed is where it can be
	// scrolled and read back.
	m.say("\n" + planFrame(m.t, m.mc, m.a, m.width()))
	m.ask = confirm("Install plan", "Would you like to proceed?",
		"Yes", "No, back to the answers", "Save the plan to a file and quit")
	m.ask.EscHint = "go back to the answers"
}

// choice is the option a confirm under a frame ended on; Esc is its last
// option but for the plan, where it goes back to the answers.
func (m *Model) choice(out ui.Outcome, esc int) int {
	switch out {
	case ui.Submitted:
		_, a, _ := m.ask.Get("confirm")
		return a.Choice
	case ui.Back:
		return esc
	}
	return -1
}

func (m *Model) onPlan(out ui.Outcome) {
	w := m.width()
	switch m.choice(out, 1) {
	case 0:
		m.say(m.t.Step(ui.Asked, "Would you like to proceed? → "+m.t.Accent.Render("Yes"), w))
		m.install()
	case 1:
		m.open(len(m.slots)-1, false)
	case 2:
		m.say(m.t.Entry(ui.Plain, "Plan saved", []string{
			"A real run writes it to ~/.local/state/aacpanel-install/plan.txt and stops here; the demo writes no file.",
			"Nothing on this machine was changed.",
		}, w))
		m.end(0)
	}
}

func (m *Model) install() {
	m.began = m.clock
	m.steps = steps(m.mc, m.a, m.opt.Fail)
	m.at, m.shown, m.since = 0, 0, m.clock
	m.say(m.t.TaskList("Install", m.tasks(), m.width()))
	m.stage = stRun
	m.run()
}

func (m *Model) tasks() []ui.Task {
	titles := taskTitles(m.a)
	items := make([]ui.Task, len(titles))
	cur := -1
	if m.at < len(m.steps) {
		cur = m.steps[m.at].task
	}
	for i := range items {
		items[i].Title = titles[i]
		switch {
		case i == m.failed:
			items[i].State = ui.Failed
		case i < taskRoot || cur < 0 || i < cur:
			items[i].State = ui.Finished
		case i == cur:
			items[i].State = ui.Running
		}
	}
	return items
}

// run moves the steps along the clock: the step at work prints its lines as
// its time goes, and a step whose time is up puts its result in the feed.
// A gate stops the run until its frame or question is answered.
func (m *Model) run() {
	for m.stage == stRun && m.at < len(m.steps) {
		s := m.steps[m.at]
		switch s.gate {
		case rootGate:
			m.stage = stRoot
			m.say("\n" + rootFrame(m.t, m.mc, m.a, m.width()))
			m.ask = rootConfirm()
			return
		case sessionGate:
			m.stage = stSession
			m.ask = testSession()
			return
		case enrollGate:
			m.stage = stEnroll
			m.newCode()
			return
		}
		el := m.clock - m.since
		if el < s.took {
			m.shown = int(int64(len(s.out)) * int64(el) / int64(s.took))
			return
		}
		m.shown = len(s.out)
		m.finish(s)
	}
}

func rootConfirm() *ui.Block {
	return confirm("Root command", "Do you want to run it?",
		"Yes", "Show the script first", "No — print the command for an administrator and stop")
}

func (m *Model) next() {
	m.at++
	m.shown = 0
	m.since = m.clock
}

func (m *Model) finish(s *step) {
	w := m.width()
	if s.fails {
		m.fail(s)
		return
	}
	lines := m.marked(s.done)
	if len(s.out) > 0 {
		lines = append(lines, m.t.Collapsed(len(s.out), "lines"))
		m.peekTitle, m.peekLines = s.title, s.out
	}
	m.say(m.t.Entry(ui.Good, s.title, lines, w))
	m.changed = append(m.changed, s.changed...)
	m.next()
	if m.stopping {
		m.interrupted(s.title)
	}
}

func (m *Model) fail(s *step) {
	w := m.width()
	diagnosis, tail, more := failure()
	lines := []string{m.t.Result(ui.Fail, diagnosis)}
	lines = append(lines, tail...)
	log := installLog + m.now().Format("0102-1504") + "-install.log"
	lines = append(lines, m.t.Dim.Render(fmt.Sprintf("… %d more lines in %s", more, log)))
	m.say(m.t.Entry(ui.Bad, s.title, lines, w))
	m.peekTitle, m.peekLines = s.title, s.out
	m.failed = s.task
	m.say(m.t.TaskList("Install", m.tasks(), w))
	m.say("")
	m.say(strings.Join(ui.Indent(fmt.Sprintf("Stopped at %q. Fix it and run ./install.sh again — finished steps are skipped.", s.title), " ", " ", w), "\n"))
	m.say(strings.Join(ui.Indent("Changed in this run: "+strings.Join(m.changed, " · "), " ", " ", w), "\n"))
	m.end(1)
}

func (m *Model) onRoot(out ui.Outcome) {
	w := m.width()
	switch m.choice(out, 2) {
	case 0:
		m.say(m.t.Step(ui.Asked, "Do you want to run it? → "+m.t.Accent.Render("Yes")+" · the terminal is sudo's until it is done", w))
		m.stage = stSudo
		c := exec.Command("bash", "-c", rootScript, "root.sh",
			rootCommand(m.mc), m.mc.user, m.a.state, strings.Join(m.mc.missing, " "))
		m.then = append(m.then, tea.ExecProcess(c, func(err error) tea.Msg { return sudoMsg{err} }))
	case 1:
		if !m.live.Busy() {
			m.pager = ui.NewPager("deploy/install/root.sh — what runs as root", rootScriptText(m.mc, m.a), w, m.h)
			m.pager.Top()
		}
		m.ask = rootConfirm()
	case 2:
		m.say(m.t.Entry(ui.Bad, "Stopped before the root part", []string{
			"An administrator runs, as root:",
			"sudo bash ~" + m.mc.user + "/aacpanel/deploy/install/root.sh apply --user " + m.mc.user +
				" --staged ~" + m.mc.user + "/.local/state/aacpanel-install/host.env.staged",
			"and then ./install.sh goes on from here.",
		}, w))
		m.end(1)
	}
}

func (m *Model) onSudo(err error) {
	w := m.width()
	s := m.steps[m.at]
	if err != nil {
		why := "the shell asking for the password failed: " + err.Error()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			switch exit.ExitCode() {
			case 1:
				why = "sudo: 3 incorrect password attempts"
			case -1, 130:
				why = "interrupted at the password"
			}
		}
		m.say(m.t.Entry(ui.Bad, s.title, []string{
			m.t.Result(ui.Fail, why),
			"Nothing ran as root. Run ./install.sh again when sudo can go through.",
		}, w))
		m.end(1)
		return
	}
	m.say(m.t.Entry(ui.Good, s.title, append(m.marked([]string{
		"✓ " + strings.Join(m.mc.missing, ", ") + " installed with apt",
		"✓ " + m.a.state + ", owner " + m.mc.user,
		"✓ aacpanel-agent@" + m.mc.user + " active · state.json fresh",
		"✓ linger for " + m.mc.user,
	}), m.t.Dim.Render("demo: the password was read and dropped; nothing ran as root")), w))
	m.changed = append(m.changed, s.changed...)
	m.stage = stRun
	m.next()
	m.run()
}

func (m *Model) onSession(out ui.Outcome) {
	choice := "no"
	switch out {
	case ui.Open:
		return
	case ui.Submitted:
		choice = m.ask.Value("session")
	}
	m.stage = stRun
	if choice == "yes" {
		m.steps[m.at] = sessionStep()
		m.since = m.clock
	} else {
		m.say(m.t.Entry(ui.Plain, "Test session", []string{"skipped: ./install.sh check --session runs it later"}, m.width()))
		m.next()
	}
	m.run()
}

func (m *Model) newCode() {
	m.code = fmt.Sprintf("%04d · %04d", m.rnd.IntN(10000), m.rnd.IntN(10000))
	m.codeAt = m.clock
}

func (m *Model) enrolled() {
	w := m.width()
	m.say(m.t.Entry(ui.Good, "First device", m.marked([]string{
		"✓ a code was on screen; another device: ./install.sh enroll",
	}), w))
	m.say("\n" + reportFrame(m.t, m.mc, m.a, m.clock-m.began, w))
	m.say(m.t.Dim.Render(" This was the demo: nothing on this machine was changed, and nothing was sent."))
	m.end(0)
}

// interrupt is Ctrl+C. Before the plan is approved nothing is written, so
// the run just ends. During a step the step goes on to its safe point and
// the run stops after it; a second Ctrl+C stops at once.
func (m *Model) interrupt() {
	w := m.width()
	switch m.stage {
	case stStart, stInspect, stAsk, stPlan:
		m.say(m.t.Entry(ui.Bad, "Stopped", []string{
			"Nothing on this machine was changed: the installer writes nothing before the plan is approved.",
		}, w))
		m.end(130)
	case stRoot:
		m.interrupted("Root part")
	case stRun:
		if m.stopping {
			m.interrupted(m.steps[m.at].title)
			return
		}
		m.stopping = true
	case stSession, stEnroll:
		m.say(m.t.Entry(ui.Plain, "Stopped after the check", []string{
			"The panel is installed and running.",
			"Another device: ./install.sh enroll",
		}, w))
		m.end(0)
	}
}

func (m *Model) interrupted(during string) {
	changed := "nothing yet"
	if len(m.changed) > 0 {
		changed = strings.Join(m.changed, " · ")
	}
	m.say(m.t.Entry(ui.Bad, fmt.Sprintf("Interrupted during %q", during), []string{
		"Changed so far in this run: " + changed,
		"Run ./install.sh again to go on, or ./install.sh uninstall to take it back.",
	}, m.width()))
	m.end(130)
}

func (m *Model) View() tea.View {
	if m.pager != nil {
		v := tea.NewView(m.pager.View(m.t))
		v.AltScreen = true
		return v
	}
	return tea.NewView(m.live.View(m.bottom()))
}

// bottom is the part at the bottom that is drawn again on every change.
func (m *Model) bottom() string {
	w := m.width()
	switch m.stage {
	case stInspect:
		return "\n" + m.t.Spinner(m.frame, "Checking the machine", m.clock-m.since, "", w)
	case stAsk, stSession, stPlan, stRoot:
		return "\n" + m.ask.View(m.t, w, m.h-1)
	case stRun:
		return m.runView(w)
	case stEnroll:
		return "\n" + enrollFrame(m.t, m.mc, m.a, m.code, codeLife-(m.clock-m.codeAt), w)
	}
	return ""
}

func (m *Model) runView(w int) string {
	if m.at >= len(m.steps) {
		return ""
	}
	s := m.steps[m.at]
	entry := m.t.Step(ui.Plain, s.title, w)
	if m.shown > 0 {
		from := max(0, m.shown-3)
		var lines []string
		for _, l := range s.out[from:m.shown] {
			lines = append(lines, ui.Fit(l, w-5))
		}
		if from > 0 {
			lines = append(lines, m.t.Collapsed(from, "lines"))
		}
		entry += "\n" + m.t.Output(lines, w)
	}
	el := m.clock - m.since
	verb := s.verb
	if s.then != "" && el*5 >= s.took*4 {
		verb = s.then
	}
	hint := "ctrl+c stops after a safe point"
	if m.stopping {
		hint = "stopping when this step is done"
	}
	return strings.Join([]string{
		"",
		entry,
		"",
		m.t.Spinner(m.frame, verb, el, hint, w),
		m.t.Tasks(m.tasks(), w),
	}, "\n")
}
