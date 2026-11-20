package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/checklist"
	"aacpanel/internal/stream"
)

func TestChecklistContour(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "account")
	t.Setenv(sessionsEnv, "")
	t.Setenv("AACP_CLAUDE_HOME", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	reg := filepath.Join(root, "registry")
	if err := os.WriteFile(reg, []byte("work|"+root+"|"+config+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(registryEnv, reg)
	procFS(t, fakeProc{pid: 7001, comm: "claude", args: []string{"claude"}, start: "71"})
	sessionFilesIn(t, filepath.Join(config, "sessions"), fakeSession{pid: 7001, name: "demo", start: "71", sid: senderSID})
	bin := filepath.Join(root, "claude")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
cat > review.json
printf '%s' '{"result":"{\"current\":true,\"confidence\":0.9}"}'
`), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(probeClaudeEnv, bin)
	e, _ := newTest(t, "")
	input := action.ChecklistInput{Checklist: action.ChecklistState{Items: []checklist.Item{{Text: "Fix the hook", Status: checklist.Active}}}, Log: "CALL Edit: main.go"}
	verdict, err := e.ChecklistCurrent(t.Context(), "demo", senderSID, input)
	if err != nil || !verdict.Current || verdict.Confidence != 0.9 {
		t.Fatalf("verdict: %+v %v", verdict, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "review.json"))
	var sent action.ChecklistInput
	if err != nil || json.Unmarshal(raw, &sent) != nil || !reflect.DeepEqual(sent, input) {
		t.Fatalf("wrong contour or input: %s %v", raw, err)
	}
	if _, err := e.ChecklistCurrent(t.Context(), "demo", streamSID, input); err == nil {
		t.Fatal("a replacement conversation spent a review")
	}
	input.Log = ""
	if _, err := e.ChecklistCurrent(t.Context(), "demo", senderSID, input); err == nil {
		t.Fatal("invalid input reached the classifier")
	}
	for _, record := range []struct{ key, owner, child string }{{"process-42-71", senderSID, streamSID}, {"codex-child", senderSID, ""}, {"process-43-71", streamSID, senderSID}} {
		if err := stream.KeepOpened(record.key, stream.OpenedSession{SessionOwner: stream.SessionOwner{Name: "demo", Conversation: record.owner}, SessionID: record.child}); err != nil {
			t.Fatal(err)
		}
	}
	children, err := e.Children(t.Context(), "demo", senderSID)
	want := []action.ChildSession{{Key: "codex-child", Conversation: "codex-child"}, {Key: "process-42-71", Conversation: streamSID}}
	if err != nil || !reflect.DeepEqual(children, want) {
		t.Fatalf("children: %+v %v", children, err)
	}
	if _, err := e.Children(t.Context(), "other", senderSID); err == nil {
		t.Fatal("a replacement session read the opening records")
	}
}
