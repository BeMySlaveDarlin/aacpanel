package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/codex/codextest"
	"aacpanel/internal/install"
)

// fakeCodex is a codex that says its version and writes, as its schema, the
// file it is given: the reference, or the reference with a field taken away.
func fakeCodex(t *testing.T, bundle []byte, stdio string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(src, bundle, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "codex-cli 9.9.9"; exit 0; fi
if [ "$1 $2" = "app-server --stdio" ]; then exec "` + stdio + `" "$@"; fi
[ "$1 $2 $3" = "app-server generate-json-schema --experimental" ] || { echo "unexpected: $*" >&2; exit 3; }
[ "$4" = "--out" ] || exit 3
cp "` + src + `" "$5/codex_app_server_protocol.schemas.json"
`
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

func referenceBundle(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "codex", "contract", "reference.json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func hookDaemon(t *testing.T) *codextest.Server {
	t.Helper()
	srv := codextest.New(t)
	t.Setenv("CODEX_HOME", srv.Home)
	const executor = "/home/u/bin/aacpanel-exec"
	path := filepath.Join(srv.Home, "hooks.json")
	hooks, _, _, err := install.WireCodex(nil, nil, path, install.Wiring{Exec: executor, Kit: []string{"relay", "stamp"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, hooks, 0o600); err != nil {
		t.Fatal(err)
	}
	srv.Hooks(map[string]any{"cwd": srv.Home, "errors": []any{}, "warnings": []string{}, "hooks": []any{
		map[string]any{"key": path + ":pre_tool_use:0:0", "command": executor + " hook ask-hook",
			"handlerType": "command", "enabled": true, "trustStatus": "trusted"},
		map[string]any{"key": path + ":user_prompt_submit:0:0", "command": executor + " hook prompt-stamp",
			"handlerType": "command", "enabled": true, "trustStatus": "trusted"},
	}})
	return srv
}

func TestTheReleaseAndHookTrustBothHaveToHold(t *testing.T) {
	srv := hookDaemon(t)
	t.Setenv("TMPDIR", t.TempDir())
	const executor = "/home/u/bin/aacpanel-exec"
	path := filepath.Join(srv.Home, "hooks.json")
	bin := fakeCodex(t, referenceBundle(t), "")
	for _, tc := range []struct {
		name, trust, reason string
		enabled             bool
		code                int
	}{
		{"trusted", "trusted", "present, trusted and enabled", true, 0},
		{"another formula", "modified", "codex does not trust hook prompt-stamp", true, 1},
		{"disabled", "trusted", "is disabled", false, 1},
		{"missing", "", "0 times, want once", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listed := []any{map[string]any{"key": path + ":pre_tool_use:0:0", "command": executor + " hook ask-hook",
				"handlerType": "command", "enabled": true, "trustStatus": "trusted"}}
			if tc.trust != "" {
				listed = append(listed, map[string]any{"key": path + ":user_prompt_submit:0:0", "command": executor + " hook prompt-stamp",
					"handlerType": "command", "enabled": tc.enabled, "trustStatus": tc.trust})
			}
			srv.Hooks(map[string]any{"cwd": srv.Home, "errors": []any{}, "warnings": []string{}, "hooks": listed})
			out, err := os.CreateTemp(t.TempDir(), "report")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			saved := os.Stdout
			os.Stdout = out
			defer func() { os.Stdout = saved }()
			if code := run(bin, "", true, false); code != tc.code {
				t.Fatalf("contract exits %d, want %d", code, tc.code)
			}
			raw, err := os.ReadFile(out.Name())
			if err != nil || !strings.Contains(string(raw), tc.reason) {
				t.Fatalf("report lacks %q: %s (%v)", tc.reason, raw, err)
			}
			if !strings.Contains(string(raw), "asked the running daemon") {
				t.Fatalf("report does not name the server: %s", raw)
			}
			if tc.trust == "modified" && !strings.Contains(string(raw), "the trust formula changed?") {
				t.Fatalf("a changed formula has no diagnosis: %s", raw)
			}
			if tc.trust == "modified" {
				root := t.TempDir()
				ref := filepath.Join(root, "internal", "codex", "contract", "reference.json")
				if err := os.MkdirAll(filepath.Dir(ref), 0o700); err != nil {
					t.Fatal(err)
				}
				for p, body := range map[string]string{filepath.Join(root, "go.mod"): "module probe\n", ref: "kept\n"} {
					if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				t.Chdir(root)
				if code := run(bin, "", false, true); code != 1 {
					t.Fatalf("a release with broken hook trust was written: %d", code)
				}
				if body, err := os.ReadFile(ref); err != nil || string(body) != "kept\n" {
					t.Fatalf("the reference changed: %s (%v)", body, err)
				}
			}
		})
	}
	if len(srv.Calls("hooks/list")) != 5 {
		t.Fatal("the contract did not ask the daemon on every check")
	}
}

func TestAReleaseWithoutAFieldThePanelReadsExitsOne(t *testing.T) {
	hookDaemon(t)
	t.Setenv("TMPDIR", t.TempDir())
	gone := strings.Replace(string(referenceBundle(t)), `"parentThreadId"`, `"parentThread"`, 1)
	if gone == string(referenceBundle(t)) {
		t.Fatal("the reference names no parentThreadId to take away")
	}
	bin := fakeCodex(t, []byte(gone), "")
	if code := run(bin, "", true, false); code != 1 {
		t.Fatalf("a release without a field the panel reads exits %d", code)
	}
	// Nor is it written as the reference.
	if code := run(bin, "", true, true); code != 1 {
		t.Fatalf("a release without a field the panel reads is written as the reference: %d", code)
	}
}

func TestTheContractChecksHookTrustWithoutADaemon(t *testing.T) {
	srv := hookDaemon(t)
	srv.Close()
	path := filepath.Join(srv.Home, "hooks.json")
	bundle := referenceBundle(t)
	// Neither PATH nor AACP_CODEX may replace the program selected by -codex.
	t.Setenv("AACP_CODEX", "/missing/codex")
	for _, trust := range []string{"trusted", "modified"} {
		t.Run(trust, func(t *testing.T) {
			stdio := codextest.HooksProgram(t, map[string]any{"cwd": srv.Home, "hooks": []any{
				map[string]any{"key": path + ":pre_tool_use:0:0", "command": "/home/u/bin/aacpanel-exec hook ask-hook",
					"handlerType": "command", "enabled": true, "trustStatus": "trusted"},
				map[string]any{"key": path + ":user_prompt_submit:0:0", "command": "/home/u/bin/aacpanel-exec hook prompt-stamp",
					"handlerType": "command", "enabled": true, "trustStatus": trust},
			}})
			bin := fakeCodex(t, bundle, stdio)
			out, err := os.CreateTemp(t.TempDir(), "report")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			saved := os.Stdout
			os.Stdout = out
			defer func() { os.Stdout = saved }()
			want := 0
			if trust == "modified" {
				want = 1
			}
			if code := run(bin, "", true, false); code != want {
				t.Fatalf("trust %s: contract exits %d, want %d", trust, code, want)
			}
			raw, err := os.ReadFile(out.Name())
			if err != nil || !strings.Contains(string(raw), "asked a codex app-server started for the check") {
				t.Fatalf("report does not name the server: %s (%v)", raw, err)
			}
			if trust == "modified" && !strings.Contains(string(raw), "the trust formula changed?") {
				t.Fatalf("a changed formula has no diagnosis: %s", raw)
			}
		})
	}
}

func TestNoCodexExitsTwo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if code := run(filepath.Join(t.TempDir(), "codex"), "", true, false); code != 2 {
		t.Fatalf("a check with no codex to ask exits %d", code)
	}
	if code := run("", t.TempDir(), true, false); code != 2 {
		t.Fatalf("a directory with no schema in it exits %d", code)
	}
}

// A directory of schema is not written as the reference: it does not say which
// release of codex wrote it, and its name is a path of this machine. The run
// says why and leaves the reference as it is.
func TestASchemaDirectoryIsNotWrittenAsTheReference(t *testing.T) {
	schema := t.TempDir()
	if err := os.WriteFile(filepath.Join(schema, "codex_app_server_protocol.schemas.json"), referenceBundle(t), 0o600); err != nil {
		t.Fatal(err)
	}
	// The run writes the reference under the go.mod it finds above its
	// directory: a root of its own keeps the tree's reference out of reach.
	root := t.TempDir()
	ref := filepath.Join(root, "internal", "codex", "contract", "reference.json")
	if err := os.MkdirAll(filepath.Dir(ref), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{filepath.Join(root, "go.mod"): "module probe\n", ref: "kept\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)

	if code := run("", schema, true, true); code != 2 {
		t.Errorf("-write with -schema exits %d", code)
	}
	if body, err := os.ReadFile(ref); err != nil || string(body) != "kept\n" {
		t.Errorf("the reference became %.80q (%v)", body, err)
	}
	// The check of the directory alone still runs.
	if code := run("", schema, true, false); code != 0 {
		t.Errorf("a check of a directory of schema exits %d", code)
	}
}
