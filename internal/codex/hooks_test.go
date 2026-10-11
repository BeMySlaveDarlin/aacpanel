package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"aacpanel/internal/codex/codextest"
)

func TestHookTrustComesFromTheDaemon(t *testing.T) {
	srv := codextest.New(t)
	want := HookCheck{Name: "ask-hook", Key: srv.Home + "/hooks.json:pre_tool_use:0:0", Command: "/home/u/bin/aacpanel-exec hook ask-hook"}
	for _, tc := range []struct {
		name, trust, reason string
		enabled             bool
	}{
		{"trusted", "trusted", "", true},
		{"another formula", "modified", "the trust formula changed?", true},
		{"no trust", "untrusted", "does not trust hook ask-hook", true},
		{"managed is not user trust", "managed", "does not trust hook ask-hook", true},
		{"disabled", "trusted", "hook ask-hook at " + want.Key + " is disabled", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.Hooks(map[string]any{"cwd": srv.Home, "errors": []any{}, "warnings": []string{}, "hooks": []any{
				map[string]any{"key": want.Key, "command": want.Command, "handlerType": "command", "enabled": tc.enabled, "trustStatus": tc.trust},
			}})
			source, err := CheckHooks(context.Background(), "/missing/codex", srv.Home, []HookCheck{want})
			if source != "asked the running daemon" {
				t.Fatalf("the check did not name its source: %q", source)
			}
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("got %v, want %q", err, tc.reason)
			}
		})
	}
	for _, tc := range []struct {
		name, reason string
		change       func(map[string]any, map[string]any)
	}{
		{"missing", "0 times, want once", func(entry, h map[string]any) { entry["hooks"] = []any{} }},
		{"other command", "does not run the installed command", func(entry, h map[string]any) { h["command"] = "echo other" }},
		{"other handler", "does not run the installed command", func(entry, h map[string]any) { h["handlerType"] = "prompt" }},
		{"other key", "0 times, want once", func(entry, h map[string]any) { h["key"] = "other" }},
		{"other cwd", "did not return the hooks", func(entry, h map[string]any) { entry["cwd"] = "/srv/other" }},
		{"load failure", "broken hooks file", func(entry, h map[string]any) {
			entry["errors"] = []any{map[string]any{"path": "hooks.json", "message": "broken hooks file"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := map[string]any{"key": want.Key, "command": want.Command, "handlerType": "command", "enabled": true, "trustStatus": "trusted"}
			entry := map[string]any{"cwd": srv.Home, "errors": []any{}, "warnings": []string{}, "hooks": []any{h}}
			tc.change(entry, h)
			srv.Hooks(entry)
			if _, err := CheckHooks(context.Background(), "/missing/codex", srv.Home, []HookCheck{want}); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("got %v, want %q", err, tc.reason)
			}
		})
	}
	for _, raw := range srv.Calls("hooks/list") {
		var params struct {
			CWDs []string `json:"cwds"`
		}
		if err := json.Unmarshal(raw, &params); err != nil || len(params.CWDs) != 1 || params.CWDs[0] != srv.Home {
			t.Fatalf("hooks were discovered outside the home: %s (%v)", raw, err)
		}
	}
	for _, method := range []string{"thread/start", "thread/resume", "thread/loaded/list", "turn/start"} {
		if len(srv.Calls(method)) != 0 {
			t.Errorf("a hook check called %s", method)
		}
	}
	srv.Refuse("hooks/list")
	if _, err := CheckHooks(context.Background(), "/missing/codex", srv.Home, []HookCheck{want}); err == nil || !strings.Contains(err.Error(), "hooks/list") {
		t.Fatalf("a daemon without hooks/list: %v", err)
	}
	srv.Close()
	if _, err := CheckHooks(context.Background(), "/missing/codex", srv.Home, []HookCheck{want}); err == nil || !strings.Contains(err.Error(), "codex could not be started:") {
		t.Fatalf("a missing program has no diagnosis: %v", err)
	}
	program := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexit 17\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckHooks(context.Background(), program, srv.Home, []HookCheck{want}); err == nil || !strings.Contains(err.Error(), "codex could not be started: initialize failed:") {
		t.Fatalf("a program that exits before initialize has no diagnosis: %v", err)
	}
}

func TestHookTrustWithoutADaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "/another/home")
	want := HookCheck{Name: "ask-hook", Key: home + "/hooks.json:pre_tool_use:0:0", Command: "/home/u/bin/aacpanel-exec hook ask-hook"}
	for _, trust := range []string{"trusted", "modified"} {
		t.Run(trust, func(t *testing.T) {
			program := codextest.HooksProgram(t, map[string]any{"cwd": home, "hooks": []any{
				map[string]any{"key": want.Key, "command": want.Command, "handlerType": "command", "enabled": true, "trustStatus": trust},
			}})
			source, err := CheckHooks(context.Background(), program, home, []HookCheck{want})
			if source != "asked a codex app-server started for the check" {
				t.Fatalf("the check did not name its source: %q (%v)", source, err)
			}
			if trust == "trusted" && err != nil || trust == "modified" && (err == nil || !strings.Contains(err.Error(), "the trust formula changed?")) {
				t.Fatalf("trust %s: %v", trust, err)
			}
			assertHookProgramExited(t, program)
		})
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("the check left files or sockets in the home: %v (%v)", entries, err)
	}
}

func TestAHookCheckKillsItsServerAtTheDeadline(t *testing.T) {
	home := t.TempDir()
	program := codextest.HooksProgram(t, map[string]any{"cwd": home})
	t.Setenv("AACP_CODEX_TEST_WAIT", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := CheckHooks(ctx, program, home, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an app-server that never answers hooks/list: %v", err)
	}
	assertHookProgramExited(t, program)
}

func assertHookProgramExited(t *testing.T, program string) {
	t.Helper()
	calls, err := os.ReadFile(program + ".calls")
	if err != nil || string(calls) != "initialize\ninitialized\nhooks/list\n" {
		t.Fatalf("unexpected calls: %s (%v)", calls, err)
	}
	raw, err := os.ReadFile(program + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("the check left process %d behind: %v", pid, err)
	}
}
