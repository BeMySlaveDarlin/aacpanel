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
	"time"

	tea "charm.land/bubbletea/v2"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

// ---- the report ----

// reportFrame is the end of an install that went through: S16.
func reportFrame(t ui.Theme, rep install.Report, title string, width int) string {
	rows := []ui.Row{{Text: t.Spin.Render("✻") + " " + t.Strong.Render(title+" in "+ui.Elapsed(rep.Took))}}
	section := func(head string, lines []string) {
		if len(lines) == 0 {
			return
		}
		rows = append(rows, ui.Row{}, ui.Row{Text: head, Head: true})
		for _, l := range lines {
			rows = append(rows, ui.Row{Text: "  · " + l})
		}
	}
	changed := rep.Changed
	if len(changed) == 0 {
		changed = []string{"nothing: the machine had it all"}
	}
	section("Changed in this run", []string{strings.Join(changed, " · ")})
	if len(rep.Kept) > 0 {
		section("Left as it was", []string{strings.Join(rep.Kept, " · ")})
	}
	var warn []string
	for _, w := range rep.Warnings {
		warn = append(warn, t.Warn.Render(w))
	}
	section("Warnings", warn)
	var mind []string
	for _, m := range rep.Remind {
		mind = append(mind, "⚠ "+m)
	}
	section("Mind", mind)
	section("Next", rep.Next)
	return t.Frame(ui.Frame{Edge: t.Edge, Rows: rows}, width)
}

// ---- a question on its own ----

// onceModel asks one block of questions between lines of a feed that is
// printed as it goes: a small program of its own, gone once answered.
type onceModel struct {
	t    ui.Theme
	a    *asking
	w, h int
	out  ui.Outcome
	done bool
}

func (m *onceModel) Init() tea.Cmd { return nil }

func (m *onceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.PasteMsg:
		m.a.ui.Paste(msg.Content)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.out, m.done = ui.Back, true
			return m, tea.Quit
		}
		if o := m.a.ui.Update(msg); o != ui.Open {
			m.out, m.done = o, true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *onceModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	w := m.w
	if w == 0 {
		w = 80
	}
	return tea.NewView("\n" + m.a.ui.View(m.t, w, m.h-1))
}

// askOnce asks qs at the terminal and gives their answers in order; ok is
// false when the person stepped back or stopped.
func askOnce(t ui.Theme, out io.Writer, title string, qs []install.Question, bare bool) ([]string, bool, error) {
	m := &onceModel{t: t, a: newAsking(nil, "", title, qs)}
	m.a.ui.Bare = bare
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return nil, false, err
	}
	if m.out != ui.Submitted {
		return nil, false, nil
	}
	fmt.Fprintln(out, "\n"+m.a.ui.Summary(t, 80))
	vals := make([]string, len(qs))
	for i := range qs {
		vals[i] = m.a.value(i)
	}
	return vals, true, nil
}

// ---- the first device ----

type (
	secondMsg struct{}
	codeMsg   struct {
		code    string
		expires time.Time
		err     error
	}
)

// enrolling is the frame of the first device while it waits: the code, its
// countdown, r for a new one and q to go on. The install's screen and the
// enroll command share it.
type enrolling struct {
	e    install.Enrollment
	now  func() time.Time
	said string // a failed new code
}

func second() tea.Cmd { return tea.Tick(time.Second, func(time.Time) tea.Msg { return secondMsg{} }) }

// key takes a key and says whether the frame is done; the command gets a
// new code.
func (en *enrolling) key(k tea.KeyPressMsg) (done bool, cmd tea.Cmd) {
	switch k.String() {
	case "r":
		again := en.e.Again
		if again == nil {
			return false, nil
		}
		return false, func() tea.Msg { c, x, err := again(); return codeMsg{c, x, err} }
	case "q", "enter", "esc", "ctrl+c":
		return true, nil
	}
	return false, nil
}

func (en *enrolling) code(msg codeMsg) {
	if msg.err != nil {
		en.said = "no new code: " + msg.err.Error()
		return
	}
	en.e.Code, en.e.Expires, en.said = msg.code, msg.expires, ""
}

func (en *enrolling) frame(t ui.Theme, width int) string {
	inner := min(width, ui.MaxFrame) - 4
	left := en.e.Expires.Sub(en.now())
	valid := fmt.Sprintf("valid %d:%02d · once", int(left.Minutes()), int(left.Seconds())%60)
	if left <= 0 {
		valid = t.Warn.Render("expired")
	}
	rows := []ui.Row{{Text: "Open  " + t.Accent.Render(en.e.Open[0]) + "  and enter the code"}}
	for _, o := range en.e.Open[1:] {
		rows = append(rows, ui.Row{Text: "or    " + t.Accent.Render(o)})
	}
	rows = append(rows, ui.Row{}, ui.Row{Text: centered(t.Strong.Render(en.e.Code), inner)}, ui.Row{},
		ui.Row{Text: valid + " · r — a new code · q — finish"})
	for _, h := range en.e.Hints {
		rows = append(rows, ui.Row{Text: t.Dim.Render(h)})
	}
	if en.said != "" {
		rows = append(rows, ui.Row{Text: t.Fail.Render(en.said)})
	}
	return t.Frame(ui.Frame{Title: "First device", Edge: t.Edge, Rows: rows}, width)
}

func centered(s string, width int) string {
	if gap := (width - ui.Width(s)) / 2; gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

// enrollModel is the frame of the first device as a program of its own.
type enrollModel struct {
	t    ui.Theme
	en   *enrolling
	w    int
	done bool
}

func (m *enrollModel) Init() tea.Cmd { return second() }

func (m *enrollModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w = msg.Width
	case secondMsg:
		return m, second()
	case codeMsg:
		m.en.code(msg)
	case tea.KeyPressMsg:
		done, cmd := m.en.key(msg)
		if done {
			m.done = true
			return m, tea.Quit
		}
		return m, cmd
	}
	return m, nil
}

func (m *enrollModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	w := m.w
	if w == 0 {
		w = 80
	}
	return tea.NewView("\n" + m.en.frame(m.t, w))
}

// ---- the lifecycle commands ----

// Lines shape a run of check, enroll or uninstall: its feed printed as it
// goes, a line an event, with a small program only for what needs a key —
// a question, sudo's password, the frame of the first device.
type Lines struct {
	Theme ui.Theme
	Out   io.Writer
	// Tty is a person at the terminal; without one every answer comes from
	// the command line.
	Tty bool
}

func (o Lines) plain() *Plain {
	return &Plain{W: o.Out, T: o.Theme, Width: PlainWidth, Color: o.Tty}
}

// Run takes steps through a run in the feed. A Ctrl+C lets the step at work
// reach its end; a second ends the command at work.
func (o Lines) Run(r *install.Run, steps []*install.Step) error {
	p := o.plain()
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
	if o.Tty {
		r.Hand = o.hand
		r.Ask = func(q install.Question) (string, error) {
			vals, ok, err := askOnce(o.Theme, o.Out, q.Prompt, []install.Question{q}, true)
			if err != nil || !ok {
				return "no", err
			}
			return vals[0], nil
		}
		r.Enroll = func(e install.Enrollment) error {
			_, err := tea.NewProgram(&enrollModel{t: o.Theme, en: &enrolling{e: e, now: time.Now}}).Run()
			return err
		}
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
	return install.Perform(r, steps)
}

// hand frames a command as root, asks, and gives it the terminal.
func (o Lines) hand(h install.Handover) error {
	if !h.Direct {
		fmt.Fprintln(o.Out, "\n"+rootFrame(o.Theme, h, 80))
		for {
			q := install.Question{ID: "confirm", Prompt: "Do you want to run it?", Form: install.One, Options: []install.Option{
				{Value: "yes", Label: "Yes"}, {Value: "show", Label: "Show the script first"},
				{Value: "no", Label: "No — print the command for an administrator and stop"}}}
			vals, ok, err := askOnce(o.Theme, o.Out, "Root command", []install.Question{q}, true)
			if err != nil {
				return err
			}
			if !ok || vals[0] == "no" {
				return install.ErrDeclined
			}
			if vals[0] == "yes" {
				break
			}
			if less, err := exec.LookPath("less"); err == nil {
				cmd := exec.Command(less, h.Script)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
				_ = cmd.Run()
			}
		}
	}
	c := &handCmd{h: h}
	c.SetStdin(os.Stdin)
	c.SetStdout(o.Out)
	c.SetStderr(os.Stderr)
	return c.Run()
}

// CheckOptions shape ./install.sh check and ./install.sh enroll.
type CheckOptions struct {
	Lines
	// Begin opens the run of the command, and gives its steps; an error is
	// a stop before any of them, said as it is.
	Begin func() (*install.Run, []*install.Step, error)
	// Title heads a stop before the steps; Done is the last line of a run
	// that went through.
	Title, Done string
}

// Check runs the check, or the enrolment of a device: the steps Begin gives,
// in the feed. It returns the exit status: 0 when every step went through.
func Check(o CheckOptions) int {
	p := o.plain()
	r, steps, err := o.Begin()
	if err != nil {
		p.Print(o.Theme.Entry(ui.Bad, o.Title, []string{said{install.Stop, err.Error()}.render(o.Theme)}, PlainWidth))
		return 1
	}
	err = o.Run(r, steps)
	var stopped *install.Interrupted
	switch {
	case err == nil:
		if o.Done != "" {
			p.Print("\n" + strings.Join(ui.Indent(o.Done, " ", " ", PlainWidth), "\n"))
		}
		return 0
	case errors.As(err, &stopped):
		return 130
	}
	return 1
}
