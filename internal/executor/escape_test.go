package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The screen of a session that drew a dialog of its own: there is no composer
// on it at all, and what holds the keyboard is the three lines at the end.
const draftDialogScreen = `╭──────────────────────────────────────────────────────────────╮
│ ✻ Bug report drafted: the router was stopped                 │
│                                                              │
│ - What happened: in a prod security check the model listed   │
│   four actions and stopped the router without asking         │
│                                                              │
│ 1 to review · 2 to send · 0 to dismiss                       │
╰──────────────────────────────────────────────────────────────╯
`

// tmuxSwitching is a tmux whose pane answers differently once a key has gone
// into it: that is what Esc closing a dialog looks like from the outside, and
// without it the screen read after the key is the screen that was there before.
func tmuxSwitching(t *testing.T, panes []string, before, after string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("panes", strings.Join(panes, "\n"))
	write("screen", before)
	write("after", after)

	bin := filepath.Join(dir, "tmux")
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do printf '%%s\n' "$a" >> %q; done
printf -- '--\n' >> %q
case "$1" in
  list-panes) cat %q/panes ;;
  capture-pane) cat %q/screen ;;
  send-keys) cp %q/after %q/screen ;;
esac
exit 0
`, log, log, dir, dir, dir, dir)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
	return log
}

func escaping(t *testing.T, before, after string) string {
	t.Helper()
	log := tmuxSwitching(t, []string{"800 aacpanel:0.0"}, before, after)
	procFS(t, fakeProc{pid: 800, comm: "claude", args: []string{"claude"}, ppid: 1, start: "91"})
	sessionFiles(t, fakeSession{pid: 800, name: "aacpanel", start: "91"})
	return log
}

// The message from the phone was refused because the session was showing a
// dialog of its own. Esc closes it, and the answer says the composer is back:
// the second attempt at the message only follows a "yes" to that.
func TestSessionEscapeGivesTheComposerBack(t *testing.T) {
	log := escaping(t, draftDialogScreen, trayIdleScreen)

	e := &Executor{}
	detail, err := e.sessionEscape(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("Esc into a session showing a dialog failed: %v", err)
	}
	argv := tmuxArgv(t, log)
	if !slices.Contains(argv, escKey) {
		t.Errorf("no bare Esc went into the session: %q", argv)
	}
	if !strings.Contains(detail, "composer") {
		t.Errorf("detail %q does not say the composer came back — the panel has nothing to send the message after", detail)
	}
}

// Esc means a different thing to every screen, and there are screens it does
// not close. Then the refusal carries the end of the screen as it stands now:
// a person on a phone learns which dialog is still holding the keyboard
// without walking to the machine.
func TestSessionEscapeRefusesWhenTheDialogStays(t *testing.T) {
	escaping(t, draftDialogScreen, draftDialogScreen)

	e := &Executor{}
	detail, err := e.sessionEscape(t.Context(), "aacpanel")
	if err == nil {
		t.Fatalf("a dialog that did not close was reported as a free composer: %q", detail)
	}
	said := err.Error()
	if !strings.Contains(said, "1 to review") {
		t.Errorf("the refusal %q does not show what is on the screen now", said)
	}
	if !strings.Contains(said, "did not come back") {
		t.Errorf("the refusal %q does not say the composer is still taken", said)
	}
}

// In an ordinary conversation Esc interrupts the answer being written. So the
// screen is read before the key, and a session whose composer is free is left
// alone: the button that frees the composer must not be the button that stops
// the work.
func TestSessionEscapePressesNothingOnAFreeComposer(t *testing.T) {
	log := escaping(t, trayIdleScreen, tasksScreen)

	e := &Executor{}
	detail, err := e.sessionEscape(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("a session with a free composer refused: %v", err)
	}
	if argv := tmuxArgv(t, log); slices.Contains(argv, escKey) {
		t.Errorf("Esc went into a session that was not holding the keyboard: %q — that interrupts the answer", argv)
	}
	if !strings.Contains(detail, "nothing was pressed") {
		t.Errorf("detail %q does not say the key was not needed", detail)
	}
}
