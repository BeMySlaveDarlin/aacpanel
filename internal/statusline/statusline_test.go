package statusline

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTheShellAndExecutorKeepTheSameBytes(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/claude/testdata/statusline.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Payload, Stdout string
		Env, Want             map[string]string
		Initial               map[string]struct {
			Text string
			Age  int64
		}
		Unchanged []string
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			for _, key := range []string{"AACP_RATE_SNAPSHOT", "AACP_SESSION_MODELS", "AACP_STATUSLINE_NEXT", "AACP_RATE_THROTTLE"} {
				t.Setenv(key, "")
			}
			for k, v := range c.Env {
				t.Setenv(k, v)
			}
			now := time.Now().Truncate(time.Second)
			expand := func(s string) string { return strings.ReplaceAll(s, "@NOW@", fmt.Sprint(now.Unix())) }
			for name, f := range c.Initial {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(expand(f.Text)), 0o600); err != nil {
					t.Fatal(err)
				}
				stamp := now.Add(-time.Duration(f.Age) * time.Second)
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			var out, errs strings.Builder
			if code := run(strings.NewReader(c.Payload), &out, &errs, now); code != 0 || out.String() != c.Stdout || errs.Len() != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q; want stdout %q", code, out.String(), errs.String(), c.Stdout)
			}
			got := map[string]string{}
			err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				data, err := os.ReadFile(path)
				name, _ := filepath.Rel(dir, path)
				got[name] = string(data)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{}
			for name, text := range c.Want {
				want[name] = expand(text)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("files:\n%v\nwant:\n%v", got, want)
			}
			for _, name := range c.Unchanged {
				st, err := os.Stat(filepath.Join(dir, name))
				if err != nil || !st.ModTime().Equal(now.Add(-time.Duration(c.Initial[name].Age)*time.Second)) {
					t.Errorf("%s was rewritten: %v", name, err)
				}
			}
		})
	}
}

func TestTheStatusLineNeedsNoExternalProgramAndHonoursPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("PATH", "")
	t.Setenv("AACP_STATUSLINE_NEXT", "")
	t.Setenv("AACP_RATE_THROTTLE", "0")
	for _, custom := range []bool{false, true} {
		config := filepath.Join(dir, ".claude")
		_ = os.MkdirAll(config, 0o755)
		path := filepath.Join(config, "rate-limits.json")
		models := filepath.Join(config, "session-models")
		t.Setenv("AACP_RATE_SNAPSHOT", "")
		t.Setenv("AACP_SESSION_MODELS", "")
		if custom {
			path, models = filepath.Join(dir, "limits"), filepath.Join(dir, "models")
			t.Setenv("AACP_RATE_SNAPSHOT", path)
			t.Setenv("AACP_SESSION_MODELS", models)
		}
		var out strings.Builder
		code := Run(strings.NewReader(`{"session_id":"3f2b1c40-9e1d-4a7b-8c6e-5d4f3a2b1c0e","model":{"id":"opus"},"rate_limits":{"five_hour":{"used_percentage":7}}}`), &out, &out)
		if code != 0 || out.Len() != 0 {
			t.Fatalf("exit %d, output %q", code, out.String())
		}
		if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), `"pct":7`) {
			t.Errorf("snapshot %s: %s, %v", path, raw, err)
		}
		if files, err := os.ReadDir(models); err != nil || len(files) != 1 {
			t.Errorf("models %s: %v, %v", models, files, err)
		}
	}
}
