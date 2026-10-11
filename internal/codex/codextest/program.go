package codextest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// HooksProgram runs this test executable as a stdio app-server. The child
// validates the home, directory and handshake and refuses every call except
// hooks/list: accidentally loading a thread cannot look like a successful check.
func HooksProgram(t testing.TB, entry map[string]any) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "codex")
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nAACP_CODEX_TEST_PROGRAM=" + quote(path) + " exec " + quote(bin) + " \"$@\"\n"
	for p, body := range map[string][]byte{path: []byte(script), path + ".json": data} {
		if err := os.WriteFile(p, body, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func init() {
	path := os.Getenv("AACP_CODEX_TEST_PROGRAM")
	if path == "" {
		return
	}
	if err := serveHooks(path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	os.Exit(0)
}

func serveHooks(path string) error {
	if !slices.Equal(os.Args[1:], []string{"app-server", "--stdio"}) {
		return fmt.Errorf("unexpected arguments: %q", os.Args[1:])
	}
	data, err := os.ReadFile(path + ".json")
	if err != nil {
		return err
	}
	var entry struct{ CWD string }
	if err := json.Unmarshal(data, &entry); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil || cwd != entry.CWD || os.Getenv("CODEX_HOME") != entry.CWD {
		return fmt.Errorf("wrong home or directory: %q, %q (%v)", os.Getenv("CODEX_HOME"), cwd, err)
	}
	if err := os.WriteFile(path+".pid", []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
		return err
	}
	calls, err := os.OpenFile(path+".calls", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer calls.Close()
	lines := bufio.NewScanner(os.Stdin)
	order := []string{"initialize", "initialized", "hooks/list"}
	n := 0
	for lines.Scan() {
		var msg struct {
			ID     json.RawMessage
			Method string
			Params struct {
				CWDs         []string                       `json:"cwds"`
				Capabilities struct{ ExperimentalAPI bool } `json:"capabilities"`
			}
		}
		if err := json.Unmarshal(lines.Bytes(), &msg); err != nil {
			return err
		}
		if n >= len(order) || msg.Method != order[n] {
			return fmt.Errorf("unexpected call: %s", msg.Method)
		}
		n++
		if _, err := fmt.Fprintln(calls, msg.Method); err != nil {
			return err
		}
		switch msg.Method {
		case "initialize":
			if !msg.Params.Capabilities.ExperimentalAPI {
				return fmt.Errorf("experimental API was not enabled")
			}
			fmt.Printf("{\"id\":%s,\"result\":{\"userAgent\":\"codex-cli/9.9.9\"}}\n", msg.ID)
		case "hooks/list":
			if !slices.Equal(msg.Params.CWDs, []string{entry.CWD}) {
				return fmt.Errorf("wrong discovery directory: %q", msg.Params.CWDs)
			}
			if os.Getenv("AACP_CODEX_TEST_WAIT") == "1" {
				time.Sleep(time.Hour)
			}
			fmt.Printf("{\"id\":%s,\"result\":{\"data\":[%s]}}\n", msg.ID, data)
		}
	}
	return lines.Err()
}
