package executor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/browser"
)

func browserManager(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "systemctl")
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$BROWSER_TEST_LOG"
case "$*" in
*list-units*) printf '%s\n' "$BROWSER_TEST_UNITS" ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(systemctlEnv, path)
	t.Setenv("BROWSER_TEST_LOG", log)
	return log
}

func TestBrowserOffRemovesAccessAndStopsExistingHolders(t *testing.T) {
	log := browserManager(t)
	id := "b-0123456789abcdef"
	t.Setenv("BROWSER_TEST_UNITS", "aacpanel-browser-"+id+".service loaded active running Browser\n"+
		"unrelated.service loaded active running Other")
	dir := t.TempDir()
	socket := filepath.Join(dir, "exec.sock")
	if err := os.WriteFile(BrowserSocket(socket), []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	e := &Executor{browserConfig: browser.Config{Root: filepath.Join(dir, "profiles"), Program: "/bin/true"}}
	for _, kind := range []action.Kind{action.BrowserCreate, action.BrowserOpen, action.BrowserClose, action.BrowserDelete} {
		if _, err := e.Execute(context.Background(), action.Request{Kind: kind, Target: id, BrowserName: func() string {
			if kind == action.BrowserCreate {
				return "Test"
			}
			return ""
		}()}); err == nil {
			t.Errorf("disabled action %s accepted", kind)
		}
		if slices.Contains(e.Kinds(), kind) {
			t.Errorf("disabled action %s advertised", kind)
		}
	}
	if err := e.StartBrowsers(context.Background(), socket); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BrowserSocket(socket)); !os.IsNotExist(err) {
		t.Fatal("disabled bridge remains")
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "--user stop aacpanel-browser-"+id+".service") {
		t.Fatalf("existing holder not stopped: %s", calls)
	}
	if strings.Contains(string(calls), "stop unrelated") {
		t.Fatal("stopped a foreign unit")
	}
	if _, err := os.Stat(e.browserConfig.Root); !os.IsNotExist(err) {
		t.Fatal("off actions wrote profiles")
	}
}

func TestBrowserLaunchIsBoundedAndProfileLimitPrecedesLaunch(t *testing.T) {
	c := browser.Config{Enabled: true, Program: "/bin/true", Root: t.TempDir(), Runtime: t.TempDir()}
	id := "b-0123456789abcdef"
	args := browserArgs("/host/aacpanel-exec", c, id, "2-3")
	for _, want := range []string{
		"--property=MemoryMax=2G",
		"--property=MemorySwapMax=0",
		"--property=CPUQuota=200%",
		"--property=KillMode=control-group",
		"--setenv=AACP_BROWSER=1",
		"--setenv=AACP_CHROME=/bin/true",
		"--property=CPUAffinity=2-3",
		"-browser-hold",
		id,
	} {
		if !slices.Contains(args, want) {
			t.Errorf("launch lacks %s: %v", want, args)
		}
	}
	browserManager(t)
	t.Setenv("BROWSER_TEST_UNITS", "aacpanel-browser-b-1111111111111111.service loaded active running\n"+
		"aacpanel-browser-b-2222222222222222.service loaded active running")
	e := &Executor{browserConfig: c}
	if err := browser.Create(c, id, "Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.browserAction(context.Background(), action.Request{Kind: action.BrowserOpen, Target: id}); err == nil ||
		!strings.Contains(err.Error(), "at most 2") {
		t.Fatalf("third profile: %v", err)
	}
	if _, err := e.browserAction(context.Background(), action.Request{Kind: action.BrowserDelete, Target: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Root, id)); !os.IsNotExist(err) {
		t.Fatal("deleted profile remains")
	}
}
