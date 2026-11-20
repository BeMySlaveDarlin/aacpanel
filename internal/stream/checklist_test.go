package stream

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/checklist"
	"aacpanel/internal/hook"
)

func TestChecklistHookDeadline(t *testing.T) {
	if got := hook.Wait("checklist-reminder") + hookGrace; got != 20*time.Second {
		t.Fatalf("the holder waits %s for the checklist hook, want 20s", got)
	}
}

func TestChecklistClassifier(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := `#!/usr/bin/env python3
import json, os, sys
with open("args.json", "w") as f:
    json.dump(sys.argv[1:], f)
with open("input.json", "w") as f:
    f.write(sys.stdin.read())
if os.environ.get("CLAUDECODE") or os.environ.get("CLAUDE_CONFIG_DIR"):
    sys.exit(2)
print(json.dumps({"result": os.environ["CHECKLIST_VERDICT"], "is_error": os.environ.get("CHECKLIST_ERROR") == "1"}))
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "beside")
	t.Setenv("CLAUDE_CONFIG_DIR", "/wrong/account")
	t.Setenv("CHECKLIST_VERDICT", `{"current":true,"confidence":0.7}`)
	input := action.ChecklistInput{Checklist: action.ChecklistState{Items: []checklist.Item{{Text: "Fix the hook", Status: checklist.Active}}}, Log: "CALL Edit: main.go"}
	got, err := ClassifyChecklist(t.Context(), bin, dir, input)
	if err != nil || !got.Current || got.Confidence != 0.7 {
		t.Fatalf("verdict: %+v %v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "input.json"))
	var sent action.ChecklistInput
	if err != nil || json.Unmarshal(raw, &sent) != nil || !reflect.DeepEqual(sent, input) {
		t.Fatalf("review input: %s %v", raw, err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, "args.json"))
	var args []string
	if err != nil || json.Unmarshal(raw, &args) != nil {
		t.Fatalf("classifier arguments: %s %v", raw, err)
	}
	for _, arg := range []string{"-p", "haiku", "--no-session-persistence", "--strict-mcp-config", "--tools", "--max-turns", "--setting-sources"} {
		if !slices.Contains(args, arg) {
			t.Errorf("missing isolation argument %q: %v", arg, args)
		}
	}
	if slices.Contains(args, input.Log) || strings.Contains(string(raw), "Fix the hook") {
		t.Fatal("the checklist travelled on the command line")
	}
	if i := slices.Index(args, "--system-prompt"); i < 0 || i+1 == len(args) || args[i+1] != checklistPrompt {
		t.Fatalf("the reviewed prompt did not reach the classifier: %v", args)
	}
	for _, bad := range []string{"no verdict", `{"current":true}`, `{"current":true,"confidence":2}`} {
		t.Setenv("CHECKLIST_VERDICT", bad)
		if _, err := ClassifyChecklist(t.Context(), bin, dir, input); err == nil {
			t.Fatalf("an unreadable verdict was accepted: %s", bad)
		}
	}
	t.Setenv("CHECKLIST_VERDICT", `{"current":true,"confidence":0.9}`)
	t.Setenv("CHECKLIST_ERROR", "1")
	if _, err := ClassifyChecklist(t.Context(), bin, dir, input); err == nil {
		t.Fatal("a failed result became a verdict")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ClassifyChecklist(ctx, bin, dir, input); err == nil {
		t.Fatal("cancellation did not stop the classifier")
	}
}
