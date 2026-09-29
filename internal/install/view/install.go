package view

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

// Begin opens the run of an approved plan: the manifest and the journal,
// and the steps the answers make.
type Begin func(*install.Survey) (*install.Run, []*install.Step, error)

// The messages of a run on the screen.
type (
	eventMsg struct{ e install.Event }
	handMsg  struct {
		h     install.Handover
		reply chan error
	}
	handedMsg struct{ err error }
	shownMsg  struct{}
	endMsg    struct{ err error }
)

// feedQueue carries what the run tells the screen and never makes the run
// wait: a command that has the terminal prints while the screen is away,
// and a line it could not hand over would stop it.
type feedQueue struct {
	mu    sync.Mutex
	items []tea.Msg
	ready chan struct{}
}

func newQueue() *feedQueue { return &feedQueue{ready: make(chan struct{}, 1)} }

func (q *feedQueue) push(m tea.Msg) {
	q.mu.Lock()
	q.items = append(q.items, m)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// pop is the next message, or nil when there is none yet.
func (q *feedQueue) pop() tea.Msg {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	m := q.items[0]
	q.items = q.items[1:]
	return m
}

// next waits for the next message.
func (q *feedQueue) next() tea.Cmd {
	return func() tea.Msg {
		for {
			if m := q.pop(); m != nil {
				return m
			}
			<-q.ready
		}
	}
}

// shownOutput is how much of the step at work the live part shows.
const shownOutput = 3

// ---- the run on the screen ----

// startRun begins the steps once the plan is approved: they run apart from
// the screen and tell it what happens through the queue.
func (m *planModel) startRun() {
	r, steps, err := m.o.Begin(m.s)
	if err != nil {
		m.stopped(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := newQueue()
	r.Ctx = ctx
	r.Sink = func(e install.Event) { q.push(eventMsg{e}) }
	r.Hand = func(h install.Handover) error {
		reply := make(chan error, 1)
		q.push(handMsg{h: h, reply: reply})
		return <-reply
	}
	m.run, m.steps, m.queue, m.cancel = r, steps, q, cancel
	m.tasks = make([]ui.Task, len(steps))
	for i, s := range steps {
		m.tasks[i] = ui.Task{Title: s.Title}
	}
	m.stage, m.ranSince = running, m.now()
	go func() { q.push(endMsg{install.Perform(r, steps)}) }()
	m.listen = tea.Batch(tick(), q.next())
}

// onRun takes a message of the run and gives what to wait for next.
func (m *planModel) onRun(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case eventMsg:
		m.onEvent(msg.e)
	case handMsg:
		m.hand = &msg
		if msg.h.Direct {
			// Agreed to among the answers: the terminal goes at once.
			m.say(m.t.Step(ui.Asked, msg.h.Title+" → "+msg.h.Says, m.width()))
			m.then = append(m.then, tea.Exec(&handCmd{h: msg.h}, func(err error) tea.Msg { return handedMsg{err} }))
			break
		}
		m.say("\n" + rootFrame(m.t, msg.h, m.width()))
		m.handAsk = rootConfirm()
		m.stage = handing
	case endMsg:
		if m.stage != over {
			m.finish(msg.err)
		}
		return nil
	}
	return m.queue.next()
}

func (m *planModel) onEvent(e install.Event) {
	w := m.width()
	switch e.Type {
	case install.Opened:
		for i, t := range m.tasks {
			if t.Title == e.Title && t.State == ui.Waiting {
				m.tasks[i].State, m.at = ui.Running, i
				break
			}
		}
		m.stepSaid, m.output, m.since = nil, nil, m.now()
	case install.Said:
		m.stepSaid = append(m.stepSaid, said{e.Mark, e.Text})
	case install.Output:
		m.output = append(m.output, e.Text)
	case install.Changed:
		m.changed = append(m.changed, e.Text)
	case install.Closed:
		m.closeStep(e, w)
	}
}

func (m *planModel) closeStep(e install.Event, w int) {
	state := ui.Finished
	var lines []string
	tone := ui.Good
	switch {
	case e.Already:
		lines = []string{m.t.Result(ui.Pass, "in place already: nothing to do")}
	case e.Err != nil:
		state, tone = ui.Failed, ui.Bad
		lines = failureLines(m.t, e.Err, m.run.Journal)
	default:
		for _, s := range m.stepSaid {
			lines = append(lines, s.lines(m.t, feedWidth(w))...)
		}
	}
	if len(m.output) > 0 {
		if e.Err == nil {
			lines = append(lines, m.t.Collapsed(len(m.output), "lines"))
		}
		m.peekTitle, m.peekLines = e.Title, m.output
	}
	for i, t := range m.tasks {
		if t.Title == e.Title && t.State != ui.Finished {
			m.tasks[i].State = state
			break
		}
	}
	m.say(m.t.Entry(tone, e.Title, lines, w))
}

// failureLines are what a step that stopped the run says under itself: the
// diagnosis, what to do, the last lines of the command, and the journal.
func failureLines(t ui.Theme, err error, j *install.Journal) []string {
	lines := []string{t.Result(ui.Fail, err.Error())}
	var f *install.Failed
	if errors.As(err, &f) {
		lines = append(lines, f.Fix...)
		for _, l := range f.Tail {
			lines = append(lines, t.Dim.Render(l))
		}
	}
	if j != nil {
		lines = append(lines, t.Dim.Render("the full output is in "+j.Path))
	}
	return lines
}

// changedLine names what the run changed, for a stop and for the end.
func changedLine(names []string) string {
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, " · ")
}

// targets are the targets of what a run that is over recorded.
func targets(r *install.Run) []string {
	var names []string
	for _, e := range r.ChangedInRun() {
		names = append(names, e.Target)
	}
	return names
}

// closingLines are the last words of a run of the steps, by how it ended,
// and what the person is left to mind.
func closingLines(err error, changed []string, took time.Duration, mind []string) (tone ui.Tone, title string, lines []string, status int) {
	var stopped *install.Interrupted
	switch {
	case err == nil:
		tone, title, status = ui.Good, "Installed in "+ui.Elapsed(took), 0
		lines = []string{
			"Changed in this run: " + changedLine(changed),
			"The panel runs: http://localhost:8776. The first device signs in with a code: INSTALL.md §14.",
		}
	case errors.As(err, &stopped):
		tone, title, status = ui.Bad, fmt.Sprintf("Interrupted after %q", stopped.After), 130
		lines = []string{
			"Changed so far in this run: " + changedLine(changed),
			"Run ./install.sh again to go on, or ./install.sh uninstall to take it back.",
		}
	default:
		tone, title, status = ui.Bad, "Stopped", 1
		lines = []string{
			"Fix it and run ./install.sh again — finished steps are skipped.",
			"Changed in this run: " + changedLine(changed),
		}
	}
	for _, l := range mind {
		lines = append(lines, "⚠ "+l)
	}
	return tone, title, lines, status
}

// mindOf is what the end of a run leaves the person: what the answers left
// undone and what the steps could not do.
func mindOf(s *install.Survey, r *install.Run) []string {
	var out []string
	if s != nil {
		out = append(out, s.Closing()...)
	}
	if r != nil {
		out = append(out, r.Reminders()...)
	}
	return out
}

func (m *planModel) finish(err error) {
	tone, title, lines, status := closingLines(err, m.changed, m.now().Sub(m.ranSince), mindOf(m.s, m.run))
	m.say(m.t.Entry(tone, title, lines, m.width()))
	m.end(status)
}

// stopRun is Ctrl+C while the steps run: the first lets the step at work
// reach its end, the second ends the command at work and the run with it.
func (m *planModel) stopRun() {
	if !m.stopping {
		m.stopping = true
		m.run.Stop()
		return
	}
	m.cancel()
	title := "the run"
	if m.at < len(m.tasks) {
		title = m.tasks[m.at].Title
	}
	m.say(m.t.Entry(ui.Bad, fmt.Sprintf("Interrupted during %q", title), []string{
		"Changed so far in this run: " + changedLine(m.changed),
		"Run ./install.sh again to go on, or ./install.sh uninstall to take it back.",
	}, m.width()))
	m.end(130)
}

func rootConfirm() *ui.Block {
	opts := []ui.Option{{Label: "Yes"}, {Label: "Show the script first"}, {Label: "No — print the command for an administrator and stop"}}
	b := ui.NewBlock("Root command", ui.Question{ID: "confirm", Prompt: "Do you want to run it?", Options: opts})
	b.Bare = true
	b.EscHint = "say no"
	return b
}

// rootFrame is a command as root in the frame Claude Code draws around a
// command it asks to run.
func rootFrame(t ui.Theme, h install.Handover, width int) string {
	return t.Frame(ui.Frame{Title: h.Title, Edge: t.RootEdge, Rows: []ui.Row{
		{Text: strings.Join(h.Argv, " ")}, {}, {Text: h.Says},
	}}, width)
}

// onHand takes the answer to the frame of a root command.
func (m *planModel) onHand(out ui.Outcome) {
	w := m.width()
	choice := -1
	switch out {
	case ui.Submitted:
		choice = m.handAsk.Answers[0].Choice
	case ui.Back:
		choice = 2
	default:
		return
	}
	switch choice {
	case 0:
		m.say(m.t.Step(ui.Asked, "Do you want to run it? → "+m.t.Accent.Render("Yes")+" · the terminal is sudo's until it is done", w))
		m.stage = running
		h := m.hand.h
		m.then = append(m.then, tea.Exec(&handCmd{h: h}, func(err error) tea.Msg { return handedMsg{err} }))
	case 1:
		if less, err := exec.LookPath("less"); err == nil {
			m.then = append(m.then, tea.ExecProcess(exec.Command(less, m.hand.h.Script), func(error) tea.Msg { return shownMsg{} }))
			return
		}
		raw, err := os.ReadFile(m.hand.h.Script)
		if err == nil && !m.live.Busy() {
			m.pager = ui.NewPager(m.hand.h.Script, strings.Split(string(raw), "\n"), w, m.h)
			m.pager.Top()
		}
		m.handAsk = rootConfirm()
	default:
		m.say(m.t.Step(ui.Asked, "Do you want to run it? → "+m.t.Accent.Render("No"), w))
		m.stage = running
		m.hand.reply <- install.ErrDeclined
		m.hand = nil
	}
}

func (m *planModel) onHanded(err error) {
	if m.hand == nil {
		return
	}
	m.hand.reply <- err
	m.hand = nil
}

// handCmd is a command given the terminal: sudo asks for its password
// there, and what the command prints goes to the terminal as it comes and,
// line by line, to the run — which keeps the MANIFEST lines off the screen.
type handCmd struct {
	h              install.Handover
	stdin          io.Reader
	stdout, stderr io.Writer
}

func (c *handCmd) SetStdin(r io.Reader)  { c.stdin = r }
func (c *handCmd) SetStdout(w io.Writer) { c.stdout = w }
func (c *handCmd) SetStderr(w io.Writer) { c.stderr = w }

func (c *handCmd) Run() error {
	fmt.Fprintf(c.stdout, "$ %s\n", strings.Join(c.h.Argv, " "))
	cmd := exec.Command(c.h.Argv[0], c.h.Argv[1:]...)
	cmd.Env = install.Environ(os.Environ(), c.h.Unset, c.h.Env)
	if c.h.Line == nil {
		// A program that draws its own screen gets the terminal whole.
		cmd.Stdin, cmd.Stdout, cmd.Stderr = c.stdin, c.stdout, c.stderr
		return cmd.Run()
	}
	out := &lineTee{to: c.stdout, line: c.h.Line, hide: "MANIFEST\t"}
	errOut := &lineTee{to: c.stderr, line: c.h.Line}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.stdin, out, errOut
	err := cmd.Run()
	out.flush()
	errOut.flush()
	return err
}

// lineTee writes what a command prints to the terminal a line at a time and
// hands each line on; a line of hide's prefix is handed on only.
type lineTee struct {
	to   io.Writer
	line func(string)
	hide string
	part []byte
}

func (l *lineTee) Write(p []byte) (int, error) {
	l.part = append(l.part, p...)
	for {
		i := strings.IndexByte(string(l.part), '\n')
		if i < 0 {
			return len(p), nil
		}
		l.emit(string(l.part[:i]))
		l.part = l.part[i+1:]
	}
}

func (l *lineTee) emit(s string) {
	if l.line != nil {
		l.line(s)
	}
	if l.hide == "" || !strings.HasPrefix(s, l.hide) {
		fmt.Fprintln(l.to, s)
	}
}

func (l *lineTee) flush() {
	if len(l.part) > 0 {
		l.emit(string(l.part))
		l.part = nil
	}
}

// runView is the live part while the steps run: the step at work with the
// last lines it printed, its spinner, and the list of steps.
func (m *planModel) runView(w int) string {
	if m.at >= len(m.tasks) {
		return ""
	}
	title := m.tasks[m.at].Title
	entry := m.t.Step(ui.Plain, title, w)
	if n := len(m.output); n > 0 {
		from := max(0, n-shownOutput)
		var lines []string
		if from > 0 {
			lines = append(lines, m.t.Collapsed(from, "lines"))
		}
		for _, l := range m.output[from:] {
			lines = append(lines, ui.Fit(l, w-5))
		}
		entry += "\n" + m.t.Output(lines, w)
	}
	hint := "ctrl+c stops after a safe point"
	if m.stopping {
		hint = "stopping when this step is done · ctrl+c again stops at once"
	}
	return strings.Join([]string{"", entry, "", m.t.Spinner(m.frame, title, m.now().Sub(m.since), hint, w), m.t.Tasks(m.tasks, w)}, "\n")
}

// ---- the run without a terminal ----

// runPlain runs the steps of an approved plan in the plain view. A Ctrl+C
// lets the step at work reach its end; a second ends the command at work.
func runPlain(o PlanOptions, p *Plain, s *install.Survey) int {
	r, steps, err := o.Begin(s)
	if err != nil {
		p.Print(o.Theme.Entry(ui.Bad, "Install", []string{said{install.Stop, err.Error()}.render(o.Theme)}, PlainWidth))
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Ctx = ctx
	r.Sink = func(e install.Event) {
		if e.Type == install.Closed && e.Err != nil {
			p.under(failureLines(o.Theme, e.Err, r.Journal))
			return
		}
		p.Sink(e)
	}
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		for range sig {
			if r.Stopping() {
				cancel()
				return
			}
			r.Stop()
			p.Print("stopping after the step at work; ctrl+c again stops at once")
		}
	}()
	began := time.Now()
	err = install.Perform(r, steps)
	tone, title, lines, status := closingLines(err, targets(r), time.Since(began), mindOf(s, r))
	p.Print(o.Theme.Entry(tone, title, lines, PlainWidth))
	return status
}
