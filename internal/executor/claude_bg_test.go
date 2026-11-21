package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

func backgroundStand(t *testing.T, stop string) (dir, binary, calls, stat string) {
	t.Helper()
	_, dir = launchDir(t, "proj")
	procFS(t, fakeProc{pid: 4200, comm: "claude", args: []string{"claude", "bg-spare"},
		ppid: 1, cwd: dir, start: "1234"})
	sessionFiles(t, fakeSession{pid: 4200, name: "lead", start: "1234", sid: consoleSID, cwd: dir, kind: "bg"})
	file := filepath.Join(os.Getenv(sessionsEnv), "4200.json")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], []byte(`,"jobId":"1234abcd"}`)...)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	binary, calls = filepath.Join(bin, "claude"), filepath.Join(bin, "calls")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s:%s:%s\\n' \"$CLAUDE_CONFIG_DIR\" \"$PWD\" \"$*\" >> '"+calls+"'\n"+stop+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(nativeClaude()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, nativeClaude()); err != nil {
		t.Fatal(err)
	}
	binary = nativeClaude()
	stat = filepath.Join(procDir(), "4200", "stat")
	return
}

func TestBackgroundStopUsesThePublicCLIAndSessionAccount(t *testing.T) {
	dir, _, calls, _ := backgroundStand(t, "echo stopped")
	e, _ := newTest(t, "")
	detail, err := e.Execute(t.Context(), req(action.SessionBGStop, "lead"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Dir(os.Getenv(sessionsEnv)) + ":" + dir + ":stop 1234abcd"
	if strings.TrimSpace(string(raw)) != want || !strings.Contains(detail, "stopped") {
		t.Fatalf("CLI %q; detail %q", raw, detail)
	}
}

func TestBackgroundControlNeverFallsBackToAPathWrapper(t *testing.T) {
	_, _, calls, _ := backgroundStand(t, "exit 0")
	if err := os.Remove(nativeClaude()); err != nil {
		t.Fatal(err)
	}
	e, _ := newTest(t, "")
	if _, err := e.Execute(t.Context(), req(action.SessionBGStop, "lead")); err == nil || !strings.Contains(err.Error(), "native Claude Code") {
		t.Fatalf("missing native binary did not refuse the PATH wrapper: %v", err)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatalf("the PATH wrapper was called: %v", err)
	}
}

func TestBackgroundAttachStartsAPanelTerminalWithAttach(t *testing.T) {
	dir, binary, _, _ := backgroundStand(t, "exit 0")
	log := fakeLauncher(t, launcher.Report{Session: "t-1234abcd"})
	fakeWindowTmux(t, nil, nil, dir)
	e, _ := newTest(t, "")
	r := req(action.SessionBGAttach, "lead")
	r.Terminal = "t-1234abcd"
	if _, err := e.Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`-term`, `"session":"t-1234abcd"`, dir, binary, "CLAUDE_CONFIG_DIR=", "attach '1234abcd'"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("terminal launch %s does not contain %q", raw, want)
		}
	}
}

func TestBackgroundAdoptionWaitsForExitBeforeResuming(t *testing.T) {
	dir, binary, calls, stat := backgroundStand(t, "echo stopped")
	log := launcherScript(t, "test ! -f '"+stat+"' || { echo 'old process still alive' >&2; exit 1; }\ncat > %s\necho '{\"session\":\"lead\"}'\n")
	e, _ := newTest(t, "")
	e.soft, e.poll = time.Second, time.Millisecond
	exit := make(chan error, 1)
	go func() {
		limit := time.NewTimer(time.Second)
		defer limit.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			if _, err := os.Stat(calls); err == nil {
				break
			}
			select {
			case <-limit.C:
				exit <- os.ErrDeadlineExceeded
				return
			case <-tick.C:
			}
		}
		time.Sleep(20 * time.Millisecond)
		exit <- os.Remove(stat)
	}()
	r := req(action.SessionBGAdopt, "lead")
	r.Project = &action.Project{Path: dir, Session: "project-name", ClaudeBin: "/wrong/wrapper",
		ConfigDir: "/wrong/account", Launch: json.RawMessage(`{"transport":"tmux","model":"opus","effort":"high","intent":"start new work"}`)}
	detail, err := e.Execute(t.Context(), r)
	if exitErr := <-exit; exitErr != nil {
		t.Fatal(exitErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	got := launched(t, log)
	for key, want := range map[string]any{"_resume": consoleSID, "_session": "lead", "transport": "stream", "model": "opus", "effort": "high"} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var spec launcher.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.ClaudeBin != binary || spec.ConfigDir != filepath.Dir(os.Getenv(sessionsEnv)) || !spec.ConfigOverride {
		t.Fatalf("resume lost the real binary or account: %+v", spec)
	}
	if intent, ok := got["intent"]; ok && intent != "" {
		t.Error("a resumed background conversation received a new opening message")
	}
	if !strings.Contains(detail, consoleSID) {
		t.Errorf("resume report %q lost its conversation", detail)
	}
}

func TestBackgroundAdoptionRefusesAProcessThatDoesNotExit(t *testing.T) {
	dir, _, calls, _ := backgroundStand(t, "echo stopped")
	log := fakeLauncher(t, launcher.Report{Session: "lead"})
	e, _ := newTest(t, "")
	e.soft, e.poll = 15*time.Millisecond, time.Millisecond
	r := req(action.SessionBGAdopt, "lead")
	r.Project = &action.Project{Path: dir, Session: "lead"}
	_, err := e.Execute(t.Context(), r)
	if err == nil || !strings.Contains(err.Error(), "has not exited") {
		t.Fatalf("a live process was resumed: %v", err)
	}
	if _, err := os.Stat(calls); err != nil {
		t.Fatal("stop was not called")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("resume ran before the old process exited: %v", err)
	}
}

func TestBackgroundSessionsRefuseOrdinaryPanelControl(t *testing.T) {
	_, _, calls, _ := backgroundStand(t, "exit 0")
	e, _ := newTest(t, "")
	for _, kind := range []action.Kind{action.SessionSend, action.SessionStop, action.SessionClose, action.SessionKill, action.SessionRestart, action.SessionSwitch, action.SessionSet, action.SessionAnswer, action.SessionPermit} {
		r := req(kind, "lead")
		if _, err := e.Execute(t.Context(), r); err == nil || !strings.Contains(err.Error(), "background session") {
			t.Errorf("%s did not refuse the background transport: %v", kind, err)
		}
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Errorf("a read-only refusal called claude: %v", err)
	}
}
