package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/codex/codextest"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/install"
	streamcontract "aacpanel/internal/stream/contract"
)

func putUpdateFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// The package source and both executables are fixtures. The update still uses
// HTTP, the installer's environment, the switched symlink, the protocol checker
// and the native daemon command and the hook protocol of each contour.
func TestCodexUpdateInstallsChecksAndRestarts(t *testing.T) {
	e := New(nil, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	schema, err := os.ReadFile("../codex/contract/reference.json")
	if err != nil {
		t.Fatal(err)
	}
	// Relative socket names also work in a deep sandbox scratch directory.
	// The binaries, state and package fixtures keep absolute paths.
	t.Chdir(t.TempDir())
	t.Setenv("TMPDIR", ".")
	idleDaemon := codextest.New(t)
	idleDaemon.Home = filepath.Clean(idleDaemon.Home)
	idleDaemon.Agent("codex/1.0.0")
	busyDaemon := codextest.New(t)
	busyDaemon.Home = filepath.Clean(busyDaemon.Home)
	busyDaemon.Agent("codex/1.0.0")
	busyDaemon.Add(codextest.Thread{ID: codexThread, CWD: "/srv/work", Status: "active", Flags: []string{"waitingOnApproval"}})
	busyDaemon.Running(codexThread, "working-turn")
	homes := []string{idleDaemon.Home, busyDaemon.Home}
	daemons := map[string]*codextest.Server{homes[0]: idleDaemon, homes[1]: busyDaemon}
	ctx, cancel := context.WithCancel(t.Context())
	e.codex = codex.Start(ctx, []registry.CodexHome{{Dir: homes[0], Contour: "personal"}, {Dir: homes[1], Contour: "work"}}, nil)
	t.Cleanup(func() { cancel(); e.codex.Wait() })
	waitFor(t, "the idle daemon to be observed", func() bool {
		n, err := e.codex.Home(homes[0]).RestartIdle(func() error { return nil })
		return err == nil && n == 0
	})
	waitFor(t, "the busy thread to be observed", func() bool {
		n, err := e.codex.Home(homes[1]).RestartIdle(func() error { return nil })
		return err == nil && n == 1 && busyDaemon.Subscribed(codexThread)
	})
	t.Setenv(registry.CodexHomesEnv, strings.Join(homes, string(os.PathListSeparator)))
	t.Setenv("CODEX_HOME", "/a/contour/must/not/receive/packages")
	t.Setenv("CODEX_INSTALL_DAEMON_ONLY", "1")
	t.Setenv("CODEX_INSTALL_DEFER_SELECTION", "1")
	root := standaloneCodex()
	current := filepath.Join(root, "current")
	before := filepath.Join(root, "releases", "1.0.0")
	putUpdateFile(t, filepath.Join(before, "bin", "codex"), "#!/bin/sh\necho 'codex-cli 1.0.0'\n", 0o700)
	if err := os.Symlink(before, current); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(current, "bin", "codex")
	t.Setenv(codexEnv, program)
	fakeSystemdRun(t)

	for _, home := range homes {
		packageRoot := filepath.Join(home, "packages", "app-server-daemon")
		for _, release := range []string{"0.8.0", "0.9.0", "1.0.0"} {
			putUpdateFile(t, filepath.Join(packageRoot, "releases", release, "bin", "codex"), "#!/bin/sh\necho 'codex-cli "+release+"'\n", 0o700)
		}
		if err := os.Symlink("releases/1.0.0", filepath.Join(packageRoot, "current")); err != nil {
			t.Fatal(err)
		}
		putUpdateFile(t, filepath.Join(home, "running-version"), "1.0.0", 0o600)
		hooksPath := filepath.Join(home, "hooks.json")
		hooks, _, _, err := install.WireCodex(nil, nil, hooksPath, install.Wiring{Exec: panelTools, Kit: []string{"relay"}})
		if err != nil {
			t.Fatal(err)
		}
		putUpdateFile(t, hooksPath, string(hooks), 0o600)
		daemons[home].Hooks(map[string]any{"cwd": home, "hooks": []any{map[string]any{"key": hooksPath + ":pre_tool_use:0:0", "command": panelTools + " hook ask-hook", "handlerType": "command", "enabled": true, "trustStatus": "trusted"}}})
	}
	schemaPath := filepath.Join(t.TempDir(), "schema.json")
	putUpdateFile(t, schemaPath, string(schema), 0o600)
	logPath := filepath.Join(t.TempDir(), "calls")
	var installs atomic.Int32
	var source *httptest.Server
	var latest atomic.Value
	latest.Store("9.9.9")
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := latest.Load().(string)
		switch r.URL.Path {
		case "/channel":
			fmt.Fprintf(w, `{"tag_name":"rust-v%s"}`, version)
		case "/restart":
			daemon := daemons[r.URL.Query().Get("home")]
			if daemon == nil {
				http.Error(w, "unknown home", http.StatusBadRequest)
				return
			}
			daemon.Close()
			daemon.Agent("codex/" + version)
			daemon.Start(t)
		case "/installer":
			installs.Add(1)
			// A bad environment exits before it can switch the installation.
			fmt.Fprintf(w, `set -eu
[ "$CODEX_NON_INTERACTIVE" = 1 ]
[ -z "${CODEX_INSTALL_DEFER_SELECTION:-}" ]
[ "$CODEX_INSTALL_DIR" = %q ]
case ":$PATH:" in *":$CODEX_INSTALL_DIR:"*) ;; *) exit 13;; esac
[ "$1" = --release ] && [ "$2" = %q ]
root=%q
if [ "${CODEX_INSTALL_DAEMON_ONLY:-}" = 1 ]; then
 root="$CODEX_HOME/packages/app-server-daemon"
else
 [ -z "${CODEX_HOME:-}" ]
fi
mkdir -p "$root"
root=$(cd "$root" && pwd)
mkdir -p "$root/releases/$2/bin"
cat > "$root/releases/$2/bin/codex" <<'PROGRAM'
#!/bin/sh
set -eu
if [ "$1" = --version ]; then echo 'codex-cli %s'; exit; fi
printf '%%s\n' "$*" >> %q
if [ "$1 $2 $3" = 'app-server generate-json-schema --experimental' ]; then
 cp %q "$5/codex_app_server_protocol.schemas.json"
elif [ "$1 $2 $3" = 'app-server daemon restart' ]; then
 "$CODEX_HOME/packages/app-server-daemon/current/bin/codex" --version > "$CODEX_HOME/running-version"
 sed -i 's/codex-cli //' "$CODEX_HOME/running-version"
 curl -fsS %q --get --data-urlencode "home=$CODEX_HOME" >/dev/null
else exit 17
fi
PROGRAM
chmod +x "$root/releases/$2/bin/codex"
ln -s "$root/releases/$2" "$root/next"
mv -Tf "$root/next" "$root/current"
`, filepath.Join(root, "bin"), version, root, version, logPath, schemaPath, source.URL+"/restart")
		default:
			http.NotFound(w, r)
		}
	}))
	defer source.Close()
	was := codexSource
	codexSource.channel, codexSource.installer = source.URL+"/channel", source.URL+"/installer"
	t.Cleanup(func() { codexSource = was })
	detail, err := e.Execute(context.Background(), req(action.CodexUpdate, "host"))
	if err != nil {
		t.Fatal(err)
	}
	state := readProgram("codex")
	if state.Version != "9.9.9" || state.CheckedVersion != "9.9.9" || state.Verdict != "passed" || !strings.Contains(detail, "daemons restarted") {
		t.Fatalf("update returned %q; state %+v", detail, state)
	}
	if installs.Load() != 1 {
		t.Fatalf("installed %d times", installs.Load())
	}
	if len(state.RestartPending) != 1 || !strings.Contains(detail, "restart pending: 1 turns running") {
		t.Fatalf("busy daemon is not pending: %+v; %s", state, detail)
	}
	for i, home := range homes {
		running, err := os.ReadFile(filepath.Join(home, "running-version"))
		want := "9.9.9"
		if i == 1 {
			want = "1.0.0"
		}
		if err != nil || strings.TrimSpace(string(running)) != want {
			t.Fatalf("daemon of %s runs %q: %v", home, running, err)
		}
	}
	busyDaemon.Set(codexThread, "idle")
	waitFor(t, "the idle status to reach the link", func() bool {
		n, err := e.codex.Home(homes[1]).RestartIdle(func() error { return nil })
		return err == nil && n == 0
	})
	if _, err := e.Execute(context.Background(), req(action.CodexUpdate, "host")); err != nil {
		t.Fatal(err)
	}
	if state := readProgram("codex"); len(state.RestartPending) != 0 {
		t.Fatalf("idle daemon stayed pending: %+v", state)
	}
	for _, home := range homes {
		running, err := os.ReadFile(filepath.Join(home, "running-version"))
		if err != nil || strings.TrimSpace(string(running)) != "9.9.9" {
			t.Fatalf("idle daemon of %s runs %q: %v", home, running, err)
		}
		entries, err := os.ReadDir(filepath.Join(home, "packages", "app-server-daemon", "releases"))
		if err != nil || len(entries) != 2 || entries[0].Name() != "1.0.0" || entries[1].Name() != "9.9.9" {
			t.Fatalf("releases kept in %s: %v: %v", home, entries, err)
		}
	}
	if got, err := programVersion(context.Background(), program); err != nil || got != "9.9.9" {
		t.Fatalf("installed version %s: %v", got, err)
	}
	calls, _ := os.ReadFile(logPath)
	if !strings.Contains(string(calls), "generate-json-schema") {
		t.Fatalf("no contract check: %s", calls)
	}
	if _, err := e.Execute(context.Background(), req(action.CodexUpdate, "host")); err != nil || installs.Load() != 2 {
		t.Fatalf("current version updated again: %v", err)
	}

	// A release dropping a field the panel reads fails the shared checker. Its
	// daemon never starts and the previous current link returns atomically.
	latest.Store("9.9.10")
	var broken map[string]any
	if err := json.Unmarshal(schema, &broken); err != nil {
		t.Fatal(err)
	}
	defs := broken["definitions"].(map[string]any)["v2"].(map[string]any)
	delete(defs, "ThreadStartParams")
	bad, err := json.Marshal(broken)
	if err != nil {
		t.Fatal(err)
	}
	putUpdateFile(t, schemaPath, string(bad), 0o600)
	if _, err := e.Execute(context.Background(), req(action.CodexUpdate, "host")); err == nil {
		t.Fatal("a broken protocol was accepted")
	}
	state = readProgram("codex")
	for _, home := range homes {
		running, _ := os.ReadFile(filepath.Join(home, "running-version"))
		if strings.TrimSpace(string(running)) != "9.9.9" {
			t.Fatalf("restarted an incompatible daemon of %s: %s", home, running)
		}
	}
	if state.Version != "9.9.9" || state.CheckedVersion != "9.9.10" || state.Verdict != "failed" {
		t.Fatalf("broken update: %+v", state)
	}
}

func TestClaudeUpdateUsesTheNativeProgramAndRefreshesItsVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	version := filepath.Join(t.TempDir(), "version")
	log := filepath.Join(t.TempDir(), "calls")
	putUpdateFile(t, version, "2.0.0", 0o600)
	putUpdateFile(t, nativeClaude(), fmt.Sprintf(`#!/bin/sh
if [ "$1" = --version ]; then cat %q; echo ' (Claude Code)'; exit; fi
printf '%%s\n' "$*" >> %q
[ "$1" = update ] || exit 7
echo 2.0.1 > %q
`, version, log, version), 0o700)
	wrapper := filepath.Join(t.TempDir(), "claude")
	putUpdateFile(t, wrapper, "#!/bin/sh\necho 'the wrapper must not run' >&2\nexit 19\n", 0o700)
	t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := New(nil, "")
	unitLog := fakeSystemdRun(t)
	launcher := filepath.Join(t.TempDir(), "check-claude")
	report := filepath.Join(t.TempDir(), "report.json")
	putUpdateFile(t, report, `[{"check":"turn","status":"ok","detail":"feed works","required":true}]`, 0o600)
	putUpdateFile(t, launcher, fmt.Sprintf("#!/bin/sh\n[ \"$1\" = -check-claude ] || exit 23\ncat %q\n", report), 0o700)
	t.Setenv(launcherEnv, launcher)
	if err := refreshProgram(context.Background(), "claude"); err != nil {
		t.Fatal(err)
	}
	if state := readProgram("claude"); state.Version != "2.0.0" {
		t.Fatal(state)
	}
	if _, err := e.Execute(context.Background(), req(action.ClaudeUpdate, "host")); err != nil {
		t.Fatal(err)
	}
	if state := readProgram("claude"); state.Version != "2.0.1" || state.CheckedVersion != "2.0.1" || state.Verdict != "passed" || !strings.Contains(state.Detail, "feed works") {
		t.Fatal(state)
	}
	calls, err := os.ReadFile(log)
	if err != nil || string(calls) != "update\n" {
		t.Fatalf("calls %q: %v", calls, err)
	}
	args, err := os.ReadFile(unitLog)
	if err != nil || !strings.Contains(string(args), "--property=KillMode=control-group\n") || !strings.Contains(string(args), "--property=RuntimeMaxSec=12min\n") {
		t.Fatalf("feed check is not bounded: %s: %v", args, err)
	}
	putUpdateFile(t, report, `[{"check":"turn","status":"FAIL","detail":"feed broke","required":true}]`, 0o600)
	if _, err := e.Execute(context.Background(), req(action.ClaudeUpdate, "host")); err == nil {
		t.Fatal("a broken feed was reported as updated")
	}
	if state := readProgram("claude"); state.Verdict != "failed" || !strings.Contains(state.Detail, "feed broke") {
		t.Fatal(state)
	}
	// Claude's own automatic update is visible without pressing the button.
	putUpdateFile(t, version, "2.0.2", 0o600)
	if err := refreshProgram(context.Background(), "claude"); err != nil {
		t.Fatal(err)
	}
	if state := readProgram("claude"); state.Version != "2.0.2" {
		t.Fatal(state)
	}
}

func TestClaudeFeedCheckUsesTheSharedRunner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Stop at the binary check: this exercises the live runner's entry without
	// opening an account or creating any session that could spend tokens.
	putUpdateFile(t, nativeClaude(), "#!/bin/sh\nexit 24\n", 0o700)
	var out bytes.Buffer
	if err := CheckClaude(&out); err != nil {
		t.Fatal(err)
	}
	var report streamcontract.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || report[0].Check != "binary" || report.ExitCode() != 1 || !strings.Contains(report[0].Detail, "exit status 24") {
		t.Fatalf("runner's verdict: %+v", report)
	}
}
