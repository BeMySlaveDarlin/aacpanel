package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

// With the panel down, a terminal asks the running executor to move a session
// to the console, and is told how to reach it there.
func TestConsoleAsksTheExecutorToMoveTheSession(t *testing.T) {
	dir, err := os.MkdirTemp("", "cons")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan action.Request, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req action.Request
		_ = json.NewDecoder(conn).Decode(&req)
		got <- req
		_ = json.NewEncoder(conn).Encode(action.Response{ID: req.ID, OK: true, Detail: "session person moved in the console"})
	}()

	var out, errs bytes.Buffer
	if code := runConsole(sock, "person", &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	req := <-got
	if req.Kind != action.SessionSwitch || req.Target != "person" || req.Switch == nil ||
		req.Switch.To != action.SwitchConsole || req.Project != nil {
		t.Errorf("the executor was asked %+v", req)
	}
	if !strings.Contains(out.String(), "moved in the console") || !strings.Contains(out.String(), "tmux attach -t person") {
		t.Errorf("the terminal was told %q", out.String())
	}
}

// An executor that is down is named, with how to bring it back.
func TestConsoleNamesAnExecutorThatIsDown(t *testing.T) {
	var out, errs bytes.Buffer
	if code := runConsole(filepath.Join(t.TempDir(), "none"), "person", &out, &errs); code == 0 {
		t.Fatal("a move through an executor that is not there was reported")
	}
	if !strings.Contains(errs.String(), "systemctl --user restart aacpanel-exec") {
		t.Errorf("the terminal was not told how to bring the executor back: %q", errs.String())
	}
}
