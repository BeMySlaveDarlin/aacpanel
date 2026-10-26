package stream

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuestionClassifierAcceptsOnlyAVerdictAndKeepsNoConversation(t *testing.T) {
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
print(json.dumps({"result": os.environ["QUESTION_VERDICT"], "is_error": os.environ.get("QUESTION_ERROR") == "1"}))
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "beside")
	t.Setenv("CLAUDE_CONFIG_DIR", "/wrong/account")
	for _, tc := range []struct {
		verdict, what string
		yes, failed   bool
	}{
		{`{"asks":true,"what":"Publish?"}`, "Publish?", true, false},
		{`{"asks":false,"what":""}`, "", false, false},
		{`{"asks":true,"what":"  Publish?\n Now?  "}`, "Publish? Now?", true, false},
		{`{"asks":true,"what":"` + strings.Repeat("é", 141) + `"}`, strings.Repeat("é", 140), true, false},
		{`{"asks":true,"what":""}`, "", false, true},
		{`{"asks":true,"what":" \n "}`, "", false, true},
		{`{"asks":true}`, "", false, true},
		{`{"asks":"true","what":"Publish?"}`, "", false, true},
		{`{"asks":null}`, "", false, true},
		{`{"what":"Publish?"}`, "", false, true},
		{`{"asks":false,"what":17}`, "", false, true},
		{`null`, "", false, true},
		{"yes", "", false, true},
		{"```json\n{}\n```", "", false, true},
		{`{"asks":false} trailing`, "", false, true},
	} {
		t.Setenv("QUESTION_VERDICT", tc.verdict)
		yes, what, err := ClassifyQuestion(t.Context(), bin, dir, "Shall I publish? The checks passed.")
		if yes != tc.yes || what != tc.what || (err != nil) != tc.failed {
			t.Fatalf("%s: %v, %v", tc.verdict, yes, err)
		}
	}
	t.Setenv("QUESTION_VERDICT", `{"asks":true,"what":"Publish?"}`)
	t.Setenv("QUESTION_ERROR", "1")
	if _, _, err := ClassifyQuestion(t.Context(), bin, dir, "Publish?"); err == nil {
		t.Fatal("a failed result became a verdict")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := ClassifyQuestion(ctx, bin, dir, "Publish?"); err == nil {
		t.Fatal("cancellation did not stop the classifier")
	}
	args, err := os.ReadFile(filepath.Join(dir, "args.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"haiku", "--no-session-persistence", "--strict-mcp-config", "--tools", "--setting-sources"} {
		if !strings.Contains(string(args), flag) {
			t.Errorf("missing %s in %s", flag, args)
		}
	}
	if strings.Contains(string(args), "Publish?") {
		t.Fatal("the message travelled on the command line")
	}
	input, err := os.ReadFile(filepath.Join(dir, "input.json"))
	if err != nil || !strings.Contains(string(input), "Publish?") {
		t.Fatalf("stdin: %s, %v", input, err)
	}
}
