package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeClaude(t *testing.T, known ...string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "arguments")
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\n" +
		"echo \"$@\" >> " + log + "\n" +
		"mode=\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  if [ \"$1\" = --permission-mode ]; then mode=$2; fi\n" +
		"  shift\n" +
		"done\n" +
		"case \"$mode\" in\n" +
		"  " + strings.Join(known, "|") + ") echo '2.1.261 (Claude Code)'; exit 0;;\n" +
		"esac\n" +
		"echo \"error: option '--permission-mode <mode>' argument '$mode' is invalid." +
		" Allowed choices are " + strings.Join(known, ", ") + ".\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(claudeEnv, script)
	return log
}

func testBin(t *testing.T) string {
	t.Helper()
	choice, err := claudeBin("")
	if err != nil {
		t.Fatalf("what to call claude with: %v", err)
	}
	return choice.Path
}

func TestPermissionModeAsksClaudeInsteadOfAList(t *testing.T) {
	log := fakeClaude(t, "wayward", "auto")
	dir := t.TempDir()

	warn, err := checkPermissionMode(context.Background(), dir, testBin(t), "wayward")
	if err != nil {
		t.Errorf("a mode claude knows was rejected: %v", err)
	}
	if warn != "" {
		t.Errorf("the mode checked out, yet a complaint went into the report: %s", warn)
	}

	warn, err = checkPermissionMode(context.Background(), dir, testBin(t), "plan")
	if err == nil {
		t.Fatalf("a mode claude does not know was accepted (complaint %q) — the session then fails to come up for no visible reason", warn)
	}
	for _, want := range []string{"plan", "wayward", "profile map"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, and that is the whole explanation: %v", want, err)
		}
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("claude was not called at all: %v", err)
	}
	if !strings.Contains(string(raw), "--permission-mode plan --version") {
		t.Errorf("claude was called with something other than a probe: %s", raw)
	}
}

func TestPermissionModeAsksNothingWhenUnset(t *testing.T) {
	log := fakeClaude(t, "plan")

	warn, err := checkPermissionMode(context.Background(), t.TempDir(), testBin(t), "")
	if err != nil || warn != "" {
		t.Fatalf("an empty mode drew a complaint: %v / %s", err, warn)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("claude was called for an empty mode — 90 ms on every start for nothing")
	}
}

func TestPermissionModeUnaskedIsAWarningNotRefusal(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "no-such-file")

	warn, err := checkPermissionMode(context.Background(), t.TempDir(), gone, "plan")
	if err != nil {
		t.Fatalf("an unchecked mode turned into a refusal: %v", err)
	}
	if !strings.Contains(warn, "was not checked") {
		t.Errorf("silence instead of an explanation: %q", warn)
	}
}

func TestPermissionModeKeepsAlienFailureItsOwn(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\necho 'claude: not my day' >&2\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(claudeEnv, script)

	warn, err := checkPermissionMode(context.Background(), t.TempDir(), testBin(t), "plan")
	if err != nil {
		t.Fatalf("claude failing for its own reason was passed off as a broken mode: %v", err)
	}
	if !strings.Contains(warn, "not my day") {
		t.Errorf("the reason claude gave was lost: %q", warn)
	}
}

func TestRunRefusesUnknownPermissionModeBeforeTmux(t *testing.T) {
	proc := fakeProc(t)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	fakeClaude(t, "auto")

	_, err := Run(context.Background(), Spec{
		Dir: t.TempDir(), Session: "aacpanel",
		Launch: json.RawMessage(`{"permissionMode":"bypaas"}`),
	})
	if err == nil {
		t.Fatal("a start with an unknown mode was accepted")
	}
	if !strings.Contains(err.Error(), "bypaas") {
		t.Errorf("the refusal does not name the mode: %v", err)
	}
	if _, err := os.Stat(tmuxLog); err == nil {
		t.Error("a tmux session was started for a mode claude is going to reject")
	}
}
