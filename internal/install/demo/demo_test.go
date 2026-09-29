package demo

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"aacpanel/internal/install/ui"
)

// run drives the demo without a terminal: keys and ticks go straight into
// Update, the feed is collected instead of printed, and every view drawn on
// the way is kept, so a test can say what the screen ever showed.
type run struct {
	t     *testing.T
	m     *Model
	feed  []string
	views []string
}

func start(t *testing.T, opt Options) *run {
	t.Helper()
	if opt.Speed == 0 {
		opt.Speed = 10
	}
	opt.Theme = ui.NewTheme(false)
	r := &run{t: t, m: New(opt)}
	r.m.out = func(text string, after ...tea.Cmd) tea.Cmd {
		if text != "" {
			r.feed = append(r.feed, ui.Strip(text))
		}
		return nil
	}
	r.send(tea.WindowSizeMsg{Width: 100, Height: 34})
	return r
}

func (r *run) send(msg tea.Msg) {
	r.m.Update(msg)
	r.views = append(r.views, ui.Strip(r.m.View().Content))
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func (r *run) keys(keys ...string) {
	for _, k := range keys {
		r.send(press(k))
	}
}

func (r *run) typed(s string) {
	for _, c := range s {
		r.send(press(string(c)))
	}
}

// until ticks the clock until the demo reaches the stage.
func (r *run) until(want stage) {
	r.t.Helper()
	for i := 0; i < 5000 && r.m.stage != want; i++ {
		r.send(tickMsg{})
	}
	if r.m.stage != want {
		r.t.Fatalf("the demo stands at stage %d, not %d; the screen:\n%s", r.m.stage, want, r.views[len(r.views)-1])
	}
}

// answer takes every block of questions with Enter and counts the keys.
func (r *run) answer() int {
	n := 0
	for r.m.stage == stAsk {
		r.keys("enter")
		n++
	}
	return n
}

// install goes from the plan to the report with the defaults.
func (r *run) install() {
	r.t.Helper()
	r.until(stPlan)
	r.keys("enter")
	r.until(stRoot)
	r.keys("enter")
	if r.m.stage != stSudo {
		r.t.Fatalf("Yes at the root command did not hand the terminal over: stage %d", r.m.stage)
	}
	r.send(sudoMsg{})
	r.until(stSession)
	r.keys("enter")
	r.until(stEnroll)
	r.keys("q")
}

func (r *run) said(s string) bool {
	return strings.Contains(strings.Join(r.feed, "\n"), s)
}

func TestEnterEverywhereInstalls(t *testing.T) {
	r := start(t, Options{})
	r.until(stAsk)
	// Eighteen questions on the path of Enter — two packages, four of this
	// machine, four of windows, three of claude, the kit, the offer of more
	// settings, three of the map — and a Submit for each of the five blocks
	// of several. A change here is a change of the plan's list of questions.
	if n := r.answer(); n != 18+5 {
		t.Errorf("the defaults took %d keys, want 23", n)
	}
	r.install()
	if r.m.Status() != 0 || r.m.stage != stDone {
		t.Fatalf("the run ended with %d at stage %d", r.m.Status(), r.m.stage)
	}
	for _, want := range []string{"Check the machine", "Install plan", "Root command", "Build the executor",
		"Panel stack", "Claude settings", "the map has 1 contour", "Test session", "Installed in",
		"nothing on this machine was changed"} {
		if !r.said(want) {
			t.Errorf("the feed never said %q", want)
		}
	}
	if r.said("Tailscale") {
		t.Error("a way in nobody checked reached the feed")
	}
}

func TestTheTailscaleKeyNeverShows(t *testing.T) {
	const secret = "tskey-auth-kD3mo7Q9zSECRET"
	r := start(t, Options{})
	r.until(stAsk)
	for r.m.slots[r.m.cur].name != "K" {
		r.keys("enter")
	}
	for i := 0; i < 8; i++ { // from Page copies down to Tailscale
		r.keys("down")
	}
	r.keys("space", "enter")
	if got := r.m.slots[r.m.cur].name; got != "D" {
		t.Fatalf("with Tailscale checked the next block is %s, not Access", got)
	}
	r.typed(secret)
	r.keys("enter")
	r.answer()
	r.install()

	for i, v := range r.views {
		if strings.Contains(v, "kD3mo7") {
			t.Fatalf("view %d shows the key:\n%s", i, v)
		}
	}
	if r.said("kD3mo7") {
		t.Fatal("the feed carries the key")
	}
	if !r.said(ui.SecretShown) {
		t.Error("the feed does not say the key was set")
	}
	if !r.said("aacpanel.tail3c9a1.ts.net") {
		t.Error("the Tailscale step never reached the feed")
	}
}

func TestAFailureStopsAtThePanelStack(t *testing.T) {
	r := start(t, Options{Fail: "compose"})
	r.until(stAsk)
	r.answer()
	r.until(stPlan)
	r.keys("enter")
	r.until(stRoot)
	r.keys("enter")
	r.send(sudoMsg{})
	r.until(stDone)
	if r.m.Status() != 1 {
		t.Fatalf("a failed run ended with %d", r.m.Status())
	}
	for _, want := range []string{"/healthz did not answer", "connection refused", "install.log",
		`Stopped at "Panel stack"`, "Changed in this run:", "~/bin/aacpanel-exec"} {
		if !r.said(want) {
			t.Errorf("the failure never said %q", want)
		}
	}
	if r.said("role monitor_app") {
		t.Error("a step after the failure ran")
	}
}

func TestEscOnTheFirstTabGoesToTheBlockBefore(t *testing.T) {
	r := start(t, Options{})
	r.until(stAsk)
	r.keys("enter", "enter", "enter") // tmux, jq, Submit
	if got := r.m.slots[r.m.cur].name; got != "A" {
		t.Fatalf("after the prerequisites the block is %s", got)
	}
	r.keys("esc")
	if got := r.m.slots[r.m.cur].name; got != "P" || !r.m.ask.OnSubmit() {
		t.Fatalf("Esc on the first tab of a block went to %s, not to the Submit of the one before", got)
	}
}

func TestNoToAPackageStopsBeforeAnyChange(t *testing.T) {
	r := start(t, Options{})
	r.until(stAsk)
	r.keys("2", "enter", "enter")
	if r.m.stage != stDone || r.m.Status() != 1 {
		t.Fatalf("No to tmux left the run at stage %d with %d", r.m.stage, r.m.Status())
	}
	if !r.said("stop: tmux is needed") || !r.said("Nothing on this machine was changed") {
		t.Fatalf("the refusal is not in the feed:\n%s", strings.Join(r.feed, "\n"))
	}
}

func TestCtrlOShowsWhatTheFeedFolded(t *testing.T) {
	r := start(t, Options{})
	r.until(stAsk)
	r.keys("ctrl+o")
	if r.m.pager == nil || !r.m.View().AltScreen {
		t.Fatal("ctrl+o opened no full view")
	}
	if v := r.views[len(r.views)-1]; !strings.Contains(v, "no trace of the panel") {
		t.Fatalf("the full view lacks the folded checks:\n%s", v)
	}
	r.keys("esc")
	if r.m.pager != nil || r.m.View().AltScreen {
		t.Fatal("Esc did not close the full view")
	}
}

func TestCtrlCDuringAStepStopsAfterIt(t *testing.T) {
	r := start(t, Options{})
	r.until(stAsk)
	r.answer()
	r.until(stPlan)
	r.keys("enter")
	r.until(stRoot)
	r.keys("enter")
	r.send(sudoMsg{})
	for r.m.steps[r.m.at].title != "Build the executor" {
		r.send(tickMsg{})
	}
	r.keys("ctrl+c")
	if r.m.stage != stRun {
		t.Fatal("the first Ctrl+C cut the step off")
	}
	r.until(stDone)
	if r.m.Status() != 130 || !r.said(`Interrupted during "Build the executor"`) || !r.said("~/bin/aacpanel-exec") {
		t.Fatalf("the interrupted run ended with %d:\n%s", r.m.Status(), strings.Join(r.feed, "\n"))
	}
}

// The shell that has the terminal at the root command asks the way sudo
// does, keeps the password off the screen and forgets it.
func TestTheRootShellNeverPrintsThePassword(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash here")
	}
	shell := func(stdin string) (string, error) {
		cmd := exec.Command("bash", "-c", rootScript, "root.sh", rootCommand(helios), "demo", "/var/lib/aacpanel", "tmux jq")
		cmd.Stdin = strings.NewReader(stdin)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return out.String(), err
	}
	out, err := shell("hunter2\n")
	if err != nil {
		t.Fatalf("the shell failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[sudo] password for demo: ") || !strings.Contains(out, "root.sh: done") {
		t.Fatalf("the shell does not ask and go on:\n%s", out)
	}
	if strings.Contains(out, "hunter2") {
		t.Fatalf("the shell printed the password:\n%s", out)
	}
	out, err = shell("\n\n\n")
	if err == nil || !strings.Contains(out, "3 incorrect password attempts") {
		t.Fatalf("three empty passwords went through: %v\n%s", err, out)
	}
}
