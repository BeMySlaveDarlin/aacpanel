package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCCTMDispatchAndExec(t *testing.T) {
	for _, tc := range []struct {
		name, writer string
		status       int
		plugin       bool
		code         int
		message      string
	}{
		{"panel", "panel", 200, true, 1, "task_*/handoff_*"},
		{"files", "cctm", 200, true, 23, ""},
		{"outside map", "", 200, true, 23, ""},
		{"plugin disabled", "cctm", 200, false, 1, "aacpanel-exec task"},
		{"unmapped without plugin", "", 200, false, 1, "aacpanel-exec handoff"},
		{"service unavailable", "", 503, true, 23, ""},
		{"service unavailable without plugin", "", 503, false, 1, "nothing was written"},
		{"invalid reply", "unexpected", 200, true, 23, ""},
		{"connection refused", "", 0, true, 23, ""},
		{"connection refused without plugin", "", 0, false, 1, "nothing was written"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			// A link, a hard link and the same directory twice must all be
			// skipped before the actual plugin is allowed to run.
			var dirs []string
			for _, name := range []string{"shim", "alias", "plugin"} {
				path := filepath.Join(dir, name)
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
				dirs = append(dirs, path)
			}
			if err := os.Symlink(self, filepath.Join(dirs[0], "cctm")); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(self, filepath.Join(dirs[1], "cctm")); err != nil {
				t.Fatal(err)
			}
			if tc.plugin {
				script := "#!/bin/sh\nprintf '%s\\n' \"$#\" \"$1\" \"$2\" \"$3\"\nIFS= read -r input\nprintf '%s\\n' \"$input\"\nprintf 'plugin stderr\\n' >&2\nexit 23\n"
				if err := os.WriteFile(filepath.Join(dirs[2], "cctm"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					CWD string `json:"cwd"`
				}
				if r.Method != "POST" || r.URL.Path != "/api/tasks/writer" || json.NewDecoder(r.Body).Decode(&in) != nil || in.CWD != dir {
					t.Errorf("writer lookup: %s %s, cwd %q", r.Method, r.URL.Path, in.CWD)
				}
				if tc.status != 200 {
					http.Error(w, "unavailable", tc.status)
					return
				}
				id := 0
				if tc.writer != "" {
					id = 7
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "name": "Work", "writer": tc.writer})
			}))
			t.Cleanup(panel.Close)
			if tc.status == 0 {
				panel.Close()
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, self, "task", "argument with spaces", "")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "AACP_TEST_CCTM=1", "AACP_TEST_CCTM_PANEL="+panel.URL,
				"PATH="+strings.Join([]string{dirs[0], dirs[1], dirs[0], dirs[2]}, string(os.PathListSeparator)))
			cmd.Stdin = strings.NewReader("caller stdin\n")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err = cmd.Run()
			var exit *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != tc.code {
				t.Fatalf("exit: %v, context %v, stderr %s", err, ctx.Err(), stderr.String())
			}
			if tc.code == 23 {
				if stdout.String() != "3\ntask\nargument with spaces\n\ncaller stdin\n" || stderr.String() != "plugin stderr\n" {
					t.Fatalf("exec lost arguments or streams: stdout %q, stderr %q", stdout.String(), stderr.String())
				}
			} else if stdout.Len() != 0 || !strings.Contains(stderr.String(), tc.message) {
				t.Fatalf("refusal ran the plugin or lost its hint: stdout %q, stderr %q", stdout.String(), stderr.String())
			}
		})
	}
}
