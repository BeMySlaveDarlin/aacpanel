package view

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

var update = flag.Bool("update", false, "rewrite the golden files of the views")

// golden compares a view with its file in testdata, without its styling:
// the golden files are for a person to read.
func golden(t *testing.T, name, got string) {
	t.Helper()
	got = ui.Strip(got) + "\n"
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v — run go test ./internal/install/view -update to write it", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file; the view:\n%s", name, got)
	}
}

var facts = install.Facts{
	Account: install.Account{Name: "u", UID: 1000, GID: 1000, Home: "/home/u"},
	Clone:   "/home/u/aacpanel",
	Version: "3f1c2ab",
	Mode:    install.Fresh,
}

// stopped is a machine that lacks two programs and cannot reach the
// registry: two stops, a warning, and passes enough to fold.
func stopped() install.Inspection {
	tmux := install.Missing{Name: "tmux", Why: "sessions live in it, and without it the executor turns off every action on sessions", Command: "sudo apt install tmux"}
	jq := install.Missing{Name: "jq", Why: "the status line script reads the limits with it", Command: "sudo apt install jq"}
	return install.Inspection{Facts: facts, Findings: []install.Finding{
		{Mark: install.Pass, Text: "Ubuntu 24.04.3 LTS · x86_64 · systemd 255"},
		{Mark: install.Pass, Text: "docker 27.5.1 · compose 2.33.1 · system daemon"},
		{Mark: install.Pass, Text: "claude 2.1.283 · ~/.local/bin/claude"},
		{Mark: install.Stop, Text: tmux.Stop(), Missing: &tmux},
		{Mark: install.Stop, Text: jq.Stop(), Missing: &jq},
		{Mark: install.Pass, Text: "ports 8776, 8777, 8443 are free"},
		{Mark: install.Pass, Text: "python3 3.12.3 for the collector"},
		{Mark: install.Pass, Text: "sudo asks for a password: the root step runs one sudo"},
		{Mark: install.Pass, Text: "the clone ~/aacpanel belongs to u and is not on tmpfs"},
		{Mark: install.Pass, Text: "no container is named aacpanel, aacpanel-db, aacpanel-socket-proxy, aacpanel-tailscale"},
		{Mark: install.Pass, Text: "212.0 GB free under /var/lib/docker · 16.7 GB of memory"},
		{Mark: install.Warn, Text: "warn: registry-1.docker.io does not answer; pulling the images may fail."},
		{Mark: install.Pass, Text: "proxy.golang.org answers"},
		{Mark: install.Pass, Text: "no trace of the panel: a fresh install"},
	}}
}

// adopted is a machine the panel was put on by hand: nothing stops, and
// what is there is listed.
func adopted() install.Inspection {
	f := facts
	f.Mode = install.Adopt
	f.Traces = []string{"/var/lib/aacpanel/host.env", "/home/u/aacpanel/.env", "/home/u/bin/aacpanel-exec",
		"compose project aacpanel: aacpanel, aacpanel-db, aacpanel-socket-proxy", "volume aacpanel_aacpanel-db"}
	return install.Inspection{Facts: f, Findings: []install.Finding{
		{Mark: install.Pass, Text: "Debian GNU/Linux 13 (trixie) · x86_64 · systemd 257"},
		{Mark: install.Pass, Text: "docker 29.8.1 · compose 5.5.1 · system daemon"},
		{Mark: install.Pass, Text: "ports 8776, 8777, 8443 are the panel's own"},
		{Mark: install.Pass, Text: "the panel is here, installed without the installer: this run takes it over"},
	}}
}

// screen drives plan on the screen without a terminal: messages go straight
// into Update and the feed is collected instead of printed.
type screen struct {
	m    *planModel
	feed []string
	quit bool
	// later are the messages the commands after a print gave, for the
	// test to hand back: a new code of the first device.
	later []tea.Msg
}

func newScreen(in install.Inspection, width int) *screen {
	s := &screen{}
	s.m = newPlanModel(PlanOptions{Theme: ui.NewTheme(false), Inspect: func() install.Inspection { return in }})
	s.m.out = func(text string, after ...tea.Cmd) tea.Cmd {
		if text != "" {
			s.feed = append(s.feed, text)
		}
		for _, a := range after {
			if _, ok := a().(tea.QuitMsg); ok {
				s.quit = true
			}
		}
		return nil
	}
	s.m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	return s
}

func (s *screen) send(msg tea.Msg) { s.m.Update(msg) }

func press(k string) tea.KeyPressMsg {
	switch k {
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func TestTheCheckOnTheScreen(t *testing.T) {
	for _, width := range []int{80, 60} {
		s := newScreen(stopped(), width)
		if v := ui.Strip(s.m.View().Content); !strings.Contains(v, "Checking the machine…") {
			t.Errorf("while the machine is looked over the live part is:\n%s", v)
		}
		s.send(inspectedMsg{stopped()})
		if len(s.feed) != 1 {
			t.Fatalf("the feed went out in %d prints", len(s.feed))
		}
		golden(t, "plan_stopped_"+strconv.Itoa(width), s.feed[0])
		if s.m.status != 1 {
			t.Errorf("a check with stops ends with %d", s.m.status)
		}
	}
	s := newScreen(adopted(), 80)
	s.send(inspectedMsg{adopted()})
	golden(t, "plan_adopt_80", s.feed[0])
	if !s.quit || s.m.status != 0 {
		t.Errorf("a check with nothing folded waits: quit %v, status %d", s.quit, s.m.status)
	}
}

func TestTheFoldedLinesOpenOnCtrlO(t *testing.T) {
	s := newScreen(stopped(), 80)
	s.send(inspectedMsg{stopped()})
	if s.quit {
		t.Fatal("the run ended before the folded lines could be opened")
	}
	if v := ui.Strip(s.m.View().Content); !strings.Contains(v, "ctrl+o to expand the check · enter to finish") {
		t.Errorf("the live part after the check:\n%s", v)
	}
	s.send(press("ctrl+o"))
	v := s.m.View()
	if !v.AltScreen || !strings.Contains(ui.Strip(v.Content), "✓ proxy.golang.org answers") {
		t.Errorf("ctrl+o did not open every line of the check:\n%s", ui.Strip(v.Content))
	}
	s.send(press("esc"))
	if s.m.View().AltScreen || s.quit {
		t.Error("Esc did not bring the feed back")
	}
	s.send(press("enter"))
	if !s.quit || s.m.status != 1 {
		t.Errorf("Enter did not end the run: quit %v, status %d", s.quit, s.m.status)
	}
}

func TestCtrlCBeforeTheCheckEnds(t *testing.T) {
	s := newScreen(stopped(), 80)
	s.send(press("ctrl+c"))
	if !s.quit || s.m.status != 130 || !strings.Contains(ui.Strip(strings.Join(s.feed, "")), "Nothing on this machine was changed.") {
		t.Errorf("quit %v, status %d, feed %q", s.quit, s.m.status, s.feed)
	}
	s.send(inspectedMsg{stopped()})
	if len(s.feed) != 1 {
		t.Errorf("the check printed after the run was stopped: %q", s.feed)
	}
}

func TestThePlainViewIsTheSameLinesWhole(t *testing.T) {
	var out bytes.Buffer
	status := planPlain(PlanOptions{Theme: ui.NewTheme(false), Out: &out, Inspect: stopped})
	if status != 1 {
		t.Errorf("status %d", status)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Errorf("the plain view carries escapes:\n%q", out.String())
	}
	if strings.Contains(out.String(), "ctrl+o") {
		t.Error("the plain view folds lines it cannot open")
	}
	golden(t, "plan_stopped_plain", strings.TrimSuffix(out.String(), "\n"))
}
