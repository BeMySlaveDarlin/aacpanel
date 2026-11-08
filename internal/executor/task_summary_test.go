package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/launcher"
)

// The fake unit executes the launcher's real print path in a fresh process,
// with a script in place of Claude. No test starts a model or a live unit.
func TestTaskSummaryLauncherHelper(t *testing.T) {
	if os.Getenv("AACP_SUMMARY_HELPER") != "1" {
		return
	}
	var spec launcher.SummarySpec
	if err := json.NewDecoder(os.Stdin).Decode(&spec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	report, err := launcher.Summarize(context.Background(), spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func summaryScene(t *testing.T) (action.Request, string, string) {
	t.Helper()
	root, dir := launchDir(t, "summary-project")
	for _, key := range []string{"HOME", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", procEnv} {
		place := filepath.Join(root, key)
		if err := os.MkdirAll(place, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, place)
	}
	t.Setenv(hostcfg.PathEnv, filepath.Join(root, "no-host.env"))
	t.Setenv("AACP_SUMMARY_HELPER", "1")
	t.Setenv("CLAUDECODE", "must-not-leak")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "wrong-account"))
	t.Setenv("INVOCATION_ID", "must-not-leak")
	log := filepath.Join(root, "claude")
	bin := filepath.Join(root, "claude-wrapper")
	body := `#!/bin/sh
umask 077
printf '%s\n' "$@" > '` + log + `.args'
pwd > '` + log + `.cwd'
env > '` + log + `.env'
cat > '` + log + `.prompt'
printf 'A connected summary.\n'
`
	if err := os.WriteFile(bin, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "launcher")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec '"+exe+"' -test.run='^TestTaskSummaryLauncherHelper$'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(launcherEnv, helper)
	unitLog := fakeSystemdRun(t)
	request := action.Request{ID: "summary", Kind: action.TaskSummary, Target: "7", Contour: "7", Text: "Registry facts only",
		Project: &action.Project{Path: dir, Session: "summary-project", ClaudeBin: bin, ConfigDir: filepath.Join(root, "account")}}
	return request, log, unitLog
}

func TestTaskSummaryRunsTheContourLauncherWithoutAConversation(t *testing.T) {
	for _, model := range []string{"", "haiku"} {
		t.Run("model-"+model, func(t *testing.T) {
			request, log, unitLog := summaryScene(t)
			t.Setenv("AACP_TASK_SUMMARY_MODEL", model)
			if err := request.Validate(); err != nil {
				t.Fatal(err)
			}
			result, err := New(nil, "").Execute(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			var got launcher.Summary
			if err := json.Unmarshal([]byte(result), &got); err != nil {
				t.Fatal(err)
			}
			if model == "" {
				model = "sonnet"
			}
			if got.Text != "A connected summary." || got.Model != model {
				t.Fatalf("summary: %+v", got)
			}
			read := func(path string) string {
				t.Helper()
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return string(raw)
			}
			args := strings.Split(strings.TrimSuffix(read(log+".args"), "\n"), "\n")
			for _, flag := range []string{"-p", "--no-session-persistence", "--strict-mcp-config", "--disable-slash-commands", "--no-chrome"} {
				if !slices.Contains(args, flag) {
					t.Errorf("missing required flag %s", flag)
				}
			}
			for flag, value := range map[string]string{"--model": model, "--tools": "", "--mcp-config": `{"mcpServers":{}}`, "--max-turns": "1", "--settings": `{"disableAllHooks":true}`} {
				i := slices.Index(args, flag)
				if i < 0 || i+1 >= len(args) || args[i+1] != value {
					t.Errorf("%s did not carry %q", flag, value)
				}
			}
			if strings.TrimSpace(read(log+".cwd")) != request.Project.Path || read(log+".prompt") != request.Text {
				t.Error("the contour directory or the prompt did not reach Claude")
			}
			env := read(log + ".env")
			if strings.Contains(env, "must-not-leak") || strings.Contains(env, "CLAUDE_CONFIG_DIR=") || !strings.Contains(env, "CLAUDE_CODE_MAX_OUTPUT_TOKENS=4096\n") {
				t.Error("the print run did not use the launcher's clean environment and output limit")
			}
			unit := read(unitLog)
			for _, want := range []string{"--property=RuntimeMaxSec=120s", "--property=KillMode=control-group", "--setenv=XDG_RUNTIME_DIR=", "-summary"} {
				if !strings.Contains(unit, want) {
					t.Errorf("unit is missing %s", want)
				}
			}
		})
	}
}

func TestTaskSummaryStopsAHungLauncher(t *testing.T) {
	request, _, _ := summaryScene(t)
	if err := os.WriteFile(os.Getenv(launcherEnv), []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := New(nil, "").Execute(ctx, request); err == nil {
		t.Fatal("the hung launcher returned success")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("cancelling a hung launcher did not bound its lifetime")
	}
}

func TestTaskSummaryUsesContourConfigurationWithoutAWrapper(t *testing.T) {
	request, log, _ := summaryScene(t)
	t.Setenv("AACP_CLAUDE", request.Project.ClaudeBin)
	request.Project.ClaudeBin = ""
	if err := os.MkdirAll(request.Project.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil, "").Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log + ".env")
	if err != nil || !strings.Contains(string(raw), "CLAUDE_CONFIG_DIR="+request.Project.ConfigDir+"\n") {
		t.Fatal("the map's contour configuration did not reach the print run")
	}
}

func TestTaskSummaryBoundsThePrintRun(t *testing.T) {
	request, _, _ := summaryScene(t)
	if err := os.WriteFile(request.Project.ClaudeBin, []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := launcher.Summarize(ctx, launcher.SummarySpec{Dir: request.Project.Path, ClaudeBin: request.Project.ClaudeBin, Prompt: request.Text, Model: "sonnet"})
	if err == nil || !strings.Contains(err.Error(), "timed out or was cancelled") {
		t.Fatalf("the hung print process escaped its deadline: %v", err)
	}
}

func TestTaskSummaryRejectsEmptyAndOversizedReplies(t *testing.T) {
	for name, script := range map[string]string{"empty": "printf '  '\n", "oversized": "head -c 65537 /dev/zero | tr '\\000' x\n"} {
		t.Run(name, func(t *testing.T) {
			request, _, _ := summaryScene(t)
			if err := os.WriteFile(request.Project.ClaudeBin, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := launcher.Summarize(t.Context(), launcher.SummarySpec{Dir: request.Project.Path, ClaudeBin: request.Project.ClaudeBin, Prompt: request.Text, Model: "sonnet"}); err == nil {
				t.Fatal("an unusable model reply was accepted")
			}
		})
	}
}
