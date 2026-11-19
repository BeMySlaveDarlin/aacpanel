package stream

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	t.Setenv("QUESTION_VERDICT", `{"questions":[{"question":"Publish?","header":"Release","options":[{"label":"Publish","description":""},{"label":"Wait","description":""}],"multi":false}],"action":"","what":"Publish?"}`)
	verdict, err := ClassifyQuestion(t.Context(), bin, dir, "Shall I publish? The checks passed.", nil)
	if err != nil || verdict.What != "Publish?" || len(verdict.Questions) != 1 || len(verdict.Questions[0].Options) != 2 {
		t.Fatalf("verdict: %+v %v", verdict, err)
	}
	for _, asked := range [][]string{nil, {}, {"Restart it?", "Which region?"}} {
		if _, err := ClassifyQuestion(t.Context(), bin, dir, "Publish?", asked); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "input.json"))
		if err != nil {
			t.Fatal(err)
		}
		var input map[string]json.RawMessage
		if err := json.Unmarshal(raw, &input); err != nil {
			t.Fatal(err)
		}
		if (input["already_asked"] != nil) != (len(asked) > 0) {
			t.Fatalf("open questions field: %s", raw)
		}
		if len(asked) > 0 {
			var got []string
			if err := json.Unmarshal(input["already_asked"], &got); err != nil || !reflect.DeepEqual(got, asked) {
				t.Fatalf("open questions: %s %v", raw, err)
			}
		}
	}
	t.Setenv("QUESTION_ERROR", "1")
	if _, err := ClassifyQuestion(t.Context(), bin, dir, "Publish?", nil); err == nil {
		t.Fatal("a failed result became a verdict")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ClassifyQuestion(ctx, bin, dir, "Publish?", nil); err == nil {
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

func TestQuestionClassifierUsesTheReviewedPrompt(t *testing.T) {
	const want = "8848a94caefa3859db5824c0569f797aae0624b80eb5c7b9be8b537524f4c138"
	for i, arg := range QuestionArgs {
		if arg == "--system-prompt" && i+1 < len(QuestionArgs) {
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(QuestionArgs[i+1]))); got != want {
				t.Fatalf("classifier prompt hash: %s; want %s", got, want)
			}
			return
		}
	}
	t.Fatal("the classifier has no system prompt")
}
