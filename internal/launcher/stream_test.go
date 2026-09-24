package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/stream"
)

func TestStreamArgsHandQuestionsToTheHost(t *testing.T) {
	p := Params{Model: "haiku", Effort: "high", PermissionMode: "plan", Intent: "start", Args: []string{"--add-dir", "/x"}}
	fresh := strings.Join(streamArgs("demo", "11111111-1111-4111-8111-111111111111", "", p), " ")
	for _, want := range []string{
		"-p --input-format stream-json --output-format stream-json",
		"--permission-prompt-tool stdio",
		"--replay-user-messages",
		"-n demo", "--model haiku", "--effort high", "--permission-mode plan",
		"--session-id 11111111-1111-4111-8111-111111111111",
		"--add-dir /x",
	} {
		if !strings.Contains(fresh, want) {
			t.Errorf("stream arguments lack %q: %s", want, fresh)
		}
	}
	if strings.Contains(fresh, "start") {
		t.Errorf("the opening message went into the arguments; on the stream it is the first message: %s", fresh)
	}
	resumed := strings.Join(streamArgs("demo", "22222222-2222-4222-8222-222222222222", "22222222-2222-4222-8222-222222222222", p), " ")
	if !strings.Contains(resumed, "--resume 22222222") || strings.Contains(resumed, "--session-id") {
		t.Errorf("a resumed stream session does not resume its own conversation: %s", resumed)
	}
}

func TestTransportIsTmuxOrStream(t *testing.T) {
	p, warns := parseParams(json.RawMessage(`{"transport":"stream"}`))
	if p.Transport != TransportStream || len(warns) != 0 {
		t.Fatalf("stream: %q %v", p.Transport, warns)
	}
	p, warns = parseParams(json.RawMessage(`{"transport":"pipes"}`))
	if p.Transport != "" || len(warns) != 1 || !strings.Contains(warns[0], "tmux") {
		t.Fatalf("an unknown transport was not turned back to tmux out loud: %q %v", p.Transport, warns)
	}
}

// heldStream puts a holder's state file in place for a conversation, with
// this test as the live holder.
func heldStream(t *testing.T, sessionID string, pid int) {
	t.Helper()
	if err := os.MkdirAll(stream.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(stream.Summary{Protocol: stream.Protocol, SessionID: sessionID, PID: pid, Holder: os.Getpid()})
	if err := os.WriteFile(stream.StatePath(sessionID), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func shortRuntime(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
}

func TestAPrintRunIsASessionOnlyWhenAHolderKeepsIt(t *testing.T) {
	shortRuntime(t)
	const id = "33333333-3333-4333-8333-333333333333"
	stream := []string{"claude", "-p", "--input-format", "stream-json", "-n", "demo", "--session-id", id}
	if !oneShot(501, stream) {
		t.Fatal("a stream run nobody holds — an SDK reviewer, a script — was taken for a session")
	}
	heldStream(t, id, 501)
	if oneShot(501, stream) {
		t.Fatal("a held stream session was taken for a one-off run")
	}
	if !oneShot(502, stream) {
		t.Fatal("another process claiming a held conversation was taken for its session")
	}
	if !oneShot(501, []string{"claude", "-p", "what time is it"}) {
		t.Fatal("a plain one-off question was taken for a session")
	}
	if oneShot(501, []string{"claude", "-n", "demo"}) {
		t.Fatal("an interactive session was taken for a one-off run")
	}
}

// fakeHolder stands in for the holder: it takes the task and stays alive for
// a while, the way a holder waits for its claude.
func fakeHolder(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	path := filepath.Join(dir, "holder")
	body := "#!/bin/sh\ncat > " + spec + "\n" + script + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	was := holderCommand
	holderCommand = func() (string, []string, error) { return path, nil, nil }
	t.Cleanup(func() { holderCommand = was })
	return spec
}

func TestRunStartsAStreamSessionUnderAHolder(t *testing.T) {
	shortRuntime(t)
	proc := fakeProc(t)
	specPath := fakeHolder(t, "sleep 5")
	wrapper := machineClaude(t)
	dir := t.TempDir()

	// The holder starts claude; here the test plays both, as soon as it has
	// read which conversation the holder was handed.
	go func() {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			raw, err := os.ReadFile(specPath)
			if err != nil || len(raw) == 0 {
				continue
			}
			var spec stream.Spec
			if json.Unmarshal(raw, &spec) != nil {
				continue
			}
			agent := filepath.Join(proc, "300")
			_ = os.MkdirAll(agent, 0o755)
			args := append([]string{"claude"}, spec.Argv[5:]...)
			_ = os.WriteFile(filepath.Join(agent, "cmdline"), []byte(strings.Join(args, "\x00")+"\x00"), 0o644)
			_ = os.WriteFile(filepath.Join(agent, "comm"), []byte("claude\n"), 0o644)
			_ = os.WriteFile(filepath.Join(agent, "stat"), []byte("300 (claude) S 1 0 0 0\n"), 0o644)
			heldStream(t, spec.SessionID, 300)
			return
		}
	}()

	rep, err := Run(context.Background(), Spec{
		Dir: dir, Session: "demo",
		Launch: json.RawMessage(`{"transport":"stream","model":"haiku","intent":"begin","remoteControl":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var spec stream.Spec
	if err := json.Unmarshal(readFile(t, specPath), &spec); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(spec.Argv, " ")
	for _, want := range []string{"env -i sh ", wrapper, "--input-format stream-json", "-n demo", "--model haiku",
		"--session-id " + spec.SessionID} {
		if !strings.Contains(argv, want) {
			t.Errorf("the holder was handed a command without %q: %s", want, argv)
		}
	}
	if spec.Intent != "begin" || spec.Name != "demo" || spec.Dir != dir {
		t.Errorf("the holder was handed %+v", spec)
	}
	if rep.Transport != TransportStream || rep.Agent != 300 || rep.Conversation != spec.SessionID || rep.Konsole != 0 {
		t.Errorf("report %+v", rep)
	}
	said := false
	for _, w := range rep.Warnings {
		said = said || strings.Contains(w, "remote control was not switched on")
	}
	if !said {
		t.Errorf("remote control asked for a stream session was not refused out loud: %v", rep.Warnings)
	}
}

func TestAHolderThatEndsAtOnceFailsTheLaunchAtOnce(t *testing.T) {
	shortRuntime(t)
	fakeProc(t)
	fakeHolder(t, "exit 1")
	machineClaude(t)
	started := time.Now()
	_, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "demo",
		Launch: json.RawMessage(`{"transport":"stream"}`)})
	if err == nil || !strings.Contains(err.Error(), "holder ended") {
		t.Fatalf("a dead holder was not reported: %v", err)
	}
	if waited := time.Since(started); waited > startWait/2 {
		t.Errorf("the launch waited %s for a claude its dead holder would never start", waited)
	}
}

func TestATmuxLaunchIsNotTouchedByTheStream(t *testing.T) {
	was := holderCommand
	holderCommand = func() (string, []string, error) { return "", nil, fmt.Errorf("the holder was called") }
	t.Cleanup(func() { holderCommand = was })
	proc := fakeProc(t, fproc{pid: 100, comm: "konsole", args: []string{"konsole"}})
	fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	machineClaude(t)
	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel"})
	if err != nil || rep.Transport != TransportTmux {
		t.Fatalf("a launch with no transport did not go to tmux: %+v %v", rep, err)
	}
}
