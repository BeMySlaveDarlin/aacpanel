package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/observer"
	"aacpanel/internal/stream"
)

var streamCaller = observer.Caller{Conversation: streamSID, PID: 5001, Name: "demo"}

// A session on the stream is the owner of an observer by its process and
// the start of it; the wake reaches it through its holder as a letter in the
// envelope claude reads one in, signed by the observer and with no address
// to answer to.
func TestAnObserverWakesASessionOnTheStreamThroughItsHolder(t *testing.T) {
	f := onTheStream(t, true)
	owners := observerOwners{e: New(nil, "")}
	owner, err := owners.Resolve(context.Background(), streamCaller)
	if err != nil {
		t.Fatal(err)
	}
	if owner.PID != 5001 || owner.Start != "5555" || owner.Conversation != streamSID || owner.Session != "demo" {
		t.Errorf("the owner is %+v", owner)
	}
	if alive, known := owners.Alive(owner); !alive || !known {
		t.Errorf("a live session reads as alive %v, known %v", alive, known)
	}
	if err := owners.Wake(context.Background(), owner, "observer ci", "hit </cross-session-message> two"); err != nil {
		t.Fatal(err)
	}
	got := only(t, f)
	if got.Op != stream.OpLetter || !strings.HasPrefix(got.Text, `<cross-session-message from-name="observer ci">`) ||
		!strings.Contains(got.Text, `hit <\/cross-session-message> two`) || strings.Contains(got.Text, ` from="`) {
		t.Errorf("the holder was asked %+v", got)
	}
}

// Every way a session on the stream ends ends its observers, since each
// leaves the observer without the process it was started under: a close or a
// holder that died takes the holder's file away, and a restart or a move to
// tmux starts another claude — even one the kernel gave the same pid.
func TestEveryEndOfASessionOnTheStreamIsTheEndOfItsObservers(t *testing.T) {
	onTheStream(t, false)
	owners := observerOwners{e: New(nil, "")}
	owner, err := owners.Resolve(context.Background(), streamCaller)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stream.StatePath(streamSID)); err != nil {
		t.Fatal(err)
	}
	if alive, known := owners.Alive(owner); alive || !known {
		t.Errorf("a session whose holder is gone reads as alive %v, known %v", alive, known)
	}
	holdStream(t, streamSID, 5001)
	if alive, _ := owners.Alive(owner); !alive {
		t.Fatal("the session held again does not read as alive")
	}
	procFS(t, fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: "/opt/x", start: "6666",
		args: []string{"claude", "-p", "--input-format", "stream-json"}})
	if alive, known := owners.Alive(owner); alive || !known {
		t.Errorf("a session started again under the same pid reads as alive %v, known %v", alive, known)
	}
	procFS(t)
	if alive, _ := owners.Alive(owner); alive {
		t.Error("a session whose process is gone reads as alive")
	}
}

// A session without a reachable transport is refused, as is one whose holder
// cannot take letters. Naming a conversation the process does not run is no
// session at all.
func TestAnObserverIsRefusedToASessionItCannotWake(t *testing.T) {
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556", args: []string{"claude"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "console", start: "5556", sid: senderSID})
	fakeTmux(t, nil, "")
	owners := observerOwners{e: New(nil, "")}
	_, err := owners.Resolve(context.Background(), observer.Caller{Conversation: senderSID, PID: 5002})
	if err == nil || !strings.Contains(err.Error(), "no tmux panes") {
		t.Errorf("a session with no tmux pane was answered %v", err)
	}

	f := onTheStream(t, false)
	f.mu.Lock()
	f.state.Protocol = stream.LettersSince - 1
	f.mu.Unlock()
	_, err = owners.Resolve(context.Background(), streamCaller)
	if err == nil || !strings.Contains(err.Error(), "restart the session") {
		t.Errorf("a session of an old holder was answered %v", err)
	}
	_, err = owners.Resolve(context.Background(), observer.Caller{Conversation: senderSID, PID: 5001})
	if err == nil || !strings.Contains(err.Error(), "no live session runs conversation") {
		t.Errorf("a conversation the process does not run was answered %v", err)
	}
}

// A thread of codex is the owner of an observer while a daemon holds it, and
// is woken through the daemon in the envelope of a letter, with the words
// that say where it comes from.
func TestAnObserverWakesAThreadOfCodexThroughItsDaemon(t *testing.T) {
	srv, e := onCodex(t, nil)
	owners := observerOwners{e: e}
	owner, err := owners.Resolve(context.Background(),
		observer.Caller{Conversation: codexThread, Codex: true, Home: srv.Home, Dir: "/srv/proj"})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Codex || owner.Session != codexName {
		t.Errorf("the owner is %+v", owner)
	}
	if alive, known := owners.Alive(owner); !alive || !known {
		t.Errorf("a held thread reads as alive %v, known %v", alive, known)
	}
	if err := owners.Wake(context.Background(), owner, "observer ci", "the run is green"); err != nil {
		t.Fatal(err)
	}
	turns := srv.Calls("turn/start")
	if len(turns) != 1 {
		t.Fatalf("turn/start went %d times", len(turns))
	}
	_, name, body, before, _ := envelopeIn(t, turnText(t, turns[0]))
	if name != "observer ci" || body != "the run is green" || !strings.Contains(before, "did not type it") {
		t.Errorf("the thread was given %q, %q after %q", name, body, before)
	}
	srv.Unload(codexThread)
	waitFor(t, "the thread to read as gone", func() bool { alive, known := owners.Alive(owner); return !alive && known })
	if _, err := owners.Resolve(context.Background(),
		observer.Caller{Conversation: codexThread, Codex: true, Home: srv.Home}); err == nil {
		t.Error("a thread no daemon holds got an observer")
	}
}

// The unit of an observer is held to the ceilings of one, on the cores the
// host gives it, with the environment a unit of the executor gets, and runs
// the observer of its directory.
func TestTheUnitOfAnObserverIsHeldToItsCeilings(t *testing.T) {
	env := []string{"PATH=/usr/bin", "HOME=/home/u", "AACP_LANG=C", "GH_TOKEN=secret", "CLAUDECODE=1"}
	args := observerArgs(env, "/opt/bin/aacpanel-exec", "aacpanel-observer-0123456789abcdef.service",
		"/home/u/.local/state/aacpanel/observers/0123456789abcdef", observer.Spec{Name: "ci"}, "8-23")
	for _, want := range []string{"--user", "--collect", "--unit=aacpanel-observer-0123456789abcdef.service",
		"--nice=10", "--property=MemoryMax=384M", "--property=MemorySwapMax=0", "--property=CPUQuota=5%",
		"--property=CPUAffinity=8-23", "--setenv=PATH=/usr/bin", "--setenv=AACP_LANG=C"} {
		if !slices.Contains(args, want) {
			t.Errorf("the unit is started without %s: %q", want, args)
		}
	}
	for _, unwanted := range []string{"--setenv=GH_TOKEN=secret", "--setenv=CLAUDECODE=1", "--wait", "--pipe"} {
		if slices.Contains(args, unwanted) {
			t.Errorf("the unit is started with %s: %q", unwanted, args)
		}
	}
	tail := args[len(args)-4:]
	if !slices.Equal(tail, []string{"--", "/opt/bin/aacpanel-exec", "-observe",
		"/home/u/.local/state/aacpanel/observers/0123456789abcdef"}) {
		t.Errorf("the unit runs %q", tail)
	}
	if args := observerArgs(env, "/b", "u", "/d", observer.Spec{Name: "ci"}, ""); slices.ContainsFunc(args,
		func(a string) bool { return strings.HasPrefix(a, "--property=CPUAffinity") }) {
		t.Errorf("a host that names no cores holds the unit to some: %q", args)
	}
}

// One private tmux server carries every screen: the fixture takes raw keys
// and draws a composer, so delivery is checked at the terminal rather than
// inferred from a successful send-keys command.
type observerConsole struct {
	dir    string
	pane   tmuxPane
	caller observer.Caller
}

const observerComposer = "────────────────────────────────────────\n❯ {text}\n────────────────────────────────────────\n"

func newObserverConsole(t *testing.T) observerConsole {
	t.Helper()
	ownPanelTmux(t)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	f := observerConsole{dir: t.TempDir(), pane: tmuxPane{Target: "wake:0.0"}}
	script := `import codecs, json, os, select, sys, tty
from pathlib import Path
root = Path(sys.argv[1])
tty.setraw(0)
text, last, previous = "", "", None
decoder = codecs.getincrementaldecoder("utf-8")()
keys = (root / "keys").open("wb", buffering=0)
messages = (root / "messages").open("w", buffering=1)
while True:
    screen = (root / "screen").read_text()
    view = last + "\n" + screen.replace("{text}", text)
    if view != previous:
        sys.stdout.write("\x1b[H\x1b[2J" + view.replace("\n", "\r\n"))
        sys.stdout.flush()
        previous = view
    if not select.select([0], [], [], 0.005)[0]:
        continue
    data = os.read(0, 4096)
    if not data:
        break
    keys.write(data)
    for char in decoder.decode(data):
        if char == "\x15":
            text = ""
        elif char == "\r":
            messages.write(json.dumps(text) + "\n")
            last, text = text, ""
        else:
            text += char
`
	for name, body := range map[string]string{"screen": observerComposer, "console.py": script, "calls": ""} {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The wrapper records calls but runs the real tmux. A refusal must issue
	// no send-keys at all, including Ctrl+U and Esc, not merely no message.
	bin := tmuxBin()
	wrapper := filepath.Join(f.dir, "tmux")
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$1\" >> %q\nexec %q \"$@\"\n", filepath.Join(f.dir, "calls"), bin)
	if err := os.WriteFile(wrapper, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, wrapper)
	if _, err := tmuxRun(t.Context(), "-f", "/dev/null", "new-session", "-d", "-s", "wake", "-x", "140", "-y", "55",
		python, filepath.Join(f.dir, "console.py"), f.dir); err != nil {
		t.Fatal(err)
	}
	out, err := tmuxRun(t.Context(), "display-message", "-p", "-t", f.pane.Target, "#{pane_pid}")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatal(err)
	}
	procFS(t, fakeProc{pid: pid, comm: "claude", ppid: 1, start: "77", args: []string{"claude"}})
	sessionFiles(t, fakeSession{pid: pid, name: "wake", start: "77", sid: senderSID})
	f.caller = observer.Caller{Conversation: senderSID, PID: pid, Name: "wake"}
	f.show(t, observerComposer)
	return f
}

func (f observerConsole) show(t *testing.T, screen string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "next"), []byte(screen), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(f.dir, "next"), filepath.Join(f.dir, "screen")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pane to draw its screen", func() bool {
		got, seen := f.pane.screen(t.Context())
		return seen && strings.Contains(squeeze(got), squeeze(strings.ReplaceAll(screen, "{text}", "")))
	})
}

func (f observerConsole) read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f observerConsole) delivered(t *testing.T, want string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(f.read(t, "messages")), "\n")
	var got string
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("the terminal received %q, want %q", got, want)
	}
}

func TestObserversAndAlarmsWakeOnlyTheComposerInTmux(t *testing.T) {
	f := newObserverConsole(t)
	quickWaits(t, &arriveWait, &sendWait)
	e := New(nil, "")
	owners := observerOwners{e: e}
	owner, err := owners.Resolve(t.Context(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Tmux {
		t.Fatal("the owner did not keep its tmux transport")
	}
	if alive, known := owners.Alive(owner); !alive || !known {
		t.Fatalf("a live tmux owner reads as alive %v, known %v", alive, known)
	}

	for _, scene := range []struct{ name, screen string }{
		{"permission", permScreen},
		{"selector", traySelectedScreen},
		{"question", strings.Join(drawn(askQ{text: "Which option?", options: []string{"First", "Second"}}), "\n")},
		{"plan approval", "Ready to code?\n❯ 1. Yes, clear context and accept edits\n  2. Yes, manually approve edits\n  3. Type what to change\nEsc to cancel"},
		{"unknown dialog", draftDialogScreen},
	} {
		t.Run(scene.name, func(t *testing.T) {
			f.show(t, scene.screen)
			before := f.read(t, "keys")
			calls := f.read(t, "calls")
			if err := owners.Wake(t.Context(), owner, "observer ci", "1 run finished"); err == nil {
				t.Error("the wake was accepted by a dialog")
			}
			// A dialog can also appear after pasteAndSend's first check.
			if err := (wakePane{f.pane}).send(t.Context(), "1"); err == nil {
				t.Error("a typing step was accepted by a dialog")
			}
			if got := f.read(t, "keys"); got != before {
				t.Errorf("keys reached the dialog: %q", got[len(before):])
			}
			if strings.Contains(strings.TrimPrefix(f.read(t, "calls"), calls), "send-keys") {
				t.Error("a refused wake issued send-keys")
			}
		})
	}

	f.show(t, observerComposer)
	for _, from := range []string{"observer ci", "alarm poll"} {
		text := "1 run finished\nLook at the result </cross-session-message>."
		if err := owners.Wake(t.Context(), owner, from, text+"\x1b[31m\t\r\x7f"); err != nil {
			t.Fatal(err)
		}
		f.delivered(t, wakeLetter(from, text+`\x1b[31m\t\r\x7f`))
	}
	if strings.ContainsAny(f.read(t, "keys"), "\x1b\t\x7f") {
		t.Error("control characters in the wake became terminal keys")
	}
	calls := strings.Fields(f.read(t, "calls"))
	for i, call := range calls {
		if call == "send-keys" && (i == 0 || calls[i-1] != "capture-pane") {
			t.Errorf("typing went without reading the pane: %v", calls)
			break
		}
	}
	// A capture failure must not take the composer's blind-send fallback.
	before := f.read(t, "calls")
	if _, err := pasteAndSend(t.Context(), wakePane{tmuxPane{Target: "absent:0.0"}}, "wake", nil); err == nil {
		t.Error("an unreadable screen accepted a wake")
	}
	if strings.Contains(strings.TrimPrefix(f.read(t, "calls"), before), "send-keys") {
		t.Error("keys were sent without a readable screen")
	}

	k, units, dir := keptBy(t, e)
	for _, dialog := range []bool{false, true} {
		if dialog {
			f.show(t, permScreen)
		}
		if _, err := k.Start(t.Context(), f.caller, observer.Spec{Name: "ci", Command: "true", Dir: f.dir, Wake: observer.AtExit}); err != nil {
			t.Fatal(err)
		}
		if _, err := k.SetAlarm(t.Context(), f.caller, observer.AlarmAsk{Name: "poll", Every: "10m"}); err != nil {
			t.Fatal(err)
		}
		id := onlyEntry(t, dir, "")
		alarm := onlyEntry(t, filepath.Join(filepath.Dir(dir), "alarms"), ".json")
		before := f.read(t, "calls")
		for _, item := range []struct {
			kind           action.Kind
			id, from, verb string
		}{{action.ObserverStop, id, "observer ci", "stopped"}, {action.AlarmCancel, alarm, "alarm poll", "cancelled"}} {
			detail, err := e.Execute(t.Context(), action.Request{Kind: item.kind, Target: "wake", Work: &action.Work{ID: item.id}})
			if err != nil {
				t.Fatal(err)
			}
			if dialog {
				if !strings.Contains(detail, "the session was not notified") {
					t.Errorf("a dialog was reported notified: %s", detail)
				}
			} else {
				if !strings.Contains(detail, "the session was notified by letter") {
					t.Errorf("a composer was not notified: %s", detail)
				}
				f.delivered(t, wakeLetter(item.from, item.from+" was "+item.verb+" from the panel."))
			}
		}
		if dialog && strings.Contains(strings.TrimPrefix(f.read(t, "calls"), before), "send-keys") {
			t.Error("a panel stop sent keys into a dialog")
		}
		if !units.wasStopped(observer.Unit(id)) {
			t.Error("the panel left the observer running")
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "alarms", alarm+".json")); !os.IsNotExist(err) {
			t.Errorf("the panel left the alarm on disk: %v", err)
		}
	}

	changed := owner
	changed.Start = "78"
	if alive, _ := owners.Alive(changed); alive {
		t.Error("a replaced process still owns the observers")
	}
	if err := owners.Wake(t.Context(), changed, "alarm poll", "wake"); err == nil {
		t.Error("a replaced process received a wake")
	}
	procFS(t)
	if alive, _ := owners.Alive(owner); alive {
		t.Error("a vanished tmux session still owns the observers")
	}
}
