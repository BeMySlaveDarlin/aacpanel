package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

const foreignParent = "33333333-3333-4333-8333-333333333333"

func parentAndChild(t *testing.T, pending stream.Pending, parentSocket string) (*Executor, *fakeHolder) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f := onTheStream(t, true, pending)
	procFS(t,
		fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5555", args: []string{"claude", "-p", "--input-format", "stream-json", "-n", "demo", "--session-id", streamSID}},
		fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556", args: []string{"claude", "-n", "parent"}},
		fakeProc{pid: 5003, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5557", args: []string{"claude", "-n", "foreign"}})
	sessionFiles(t,
		fakeSession{pid: 5001, name: "demo", start: "5555", sid: streamSID},
		fakeSession{pid: 5002, name: "parent", start: "5556", sid: senderSID, socket: parentSocket},
		fakeSession{pid: 5003, name: "foreign", start: "5557", sid: foreignParent})
	if err := stream.KeepOwner("process-5001-5555", stream.SessionOwner{Name: "parent", Conversation: senderSID}); err != nil {
		t.Fatal(err)
	}
	e, _ := newTest(t, "")
	return e, f
}

func TestAParentAnswerUsesTheScreenActionAndRefusesAnotherParentsChild(t *testing.T) {
	e, holder := parentAndChild(t, question(), "")
	r := req(action.SessionAnswer, "demo")
	r.Answer = &action.Answer{AskID: "toolu_q", Picks: [][]int{{2}, {1, 3}, {}}, Texts: []string{"", "", "run it"}}
	r.Caller = foreignParent
	if _, err := e.Execute(t.Context(), r); err == nil || !strings.Contains(err.Error(), "not opened") {
		t.Fatalf("another parent answered the child: %v", err)
	}
	if len(holder.asked()) != 0 {
		t.Fatal("a foreign answer reached the holder")
	}
	r.Caller = senderSID
	if _, err := e.Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	got := only(t, holder)
	var body struct {
		Behavior string `json:"behavior"`
		Updated  struct {
			Answers map[string]string `json:"answers"`
		} `json:"updatedInput"`
	}
	if err := json.Unmarshal(got.Response, &body); err != nil {
		t.Fatal(err)
	}
	if got.Op != stream.OpRespond || got.RequestID != "cc-9" || body.Behavior != "allow" ||
		body.Updated.Answers["Pick a colour"] != "Blue" || body.Updated.Answers["Anything else"] != "run it" {
		t.Fatalf("the parent answer took another path than the screen: %+v %s", got, got.Response)
	}
}

func TestAParentAnswerCannotAllowAChildsPermission(t *testing.T) {
	e, holder := parentAndChild(t, bash("permit", ""), "")
	r := req(action.SessionAnswer, "demo")
	r.Caller = senderSID
	r.Answer = &action.Answer{AskID: "toolu_b", Picks: [][]int{{1}}}
	if _, err := e.Execute(t.Context(), r); err == nil {
		t.Fatal("a parent answered a permission as a question")
	}
	if len(holder.asked()) != 0 {
		t.Fatal("the permission was answered on the holder")
	}
}

func TestAChildQuestionGoesOnlyToItsLiveOpenerAsPanelMail(t *testing.T) {
	socket, receivedMail := listenFake(t)
	e, _ := parentAndChild(t, question(), socket)
	opening, err := e.Opening(t.Context(), "demo", streamSID)
	if err != nil || opening == nil || opening.Conversation != senderSID || !opening.Letters {
		t.Fatalf("the parent was not found by its opening: %+v, %v", opening, err)
	}
	r := req(action.SessionLetter, "parent")
	r.ParentQuestion, r.Conversation, r.Text = streamSID, senderSID, "Your child asks which path to take."
	if _, err := e.Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	line := received(t, receivedMail)
	from, signed, body := claudeReads(t, line.Message.Content)
	if from != "" || signed != "panel question" || body != r.Text || line.From != "" {
		t.Fatalf("the panel attributed its question to an agent or person: %+v", line)
	}
	r.Conversation = foreignParent
	if _, err := e.Execute(t.Context(), r); err == nil {
		t.Fatal("a stale opening mailed another conversation")
	}
	sessionFiles(t,
		fakeSession{pid: 5001, name: "demo", start: "5555", sid: streamSID},
		fakeSession{pid: 5002, name: "parent", start: "5556", sid: senderSID})
	opening, err = e.Opening(t.Context(), "demo", streamSID)
	if err != nil || opening == nil || opening.Letters {
		t.Fatalf("a terminal with no message socket can receive questions: %+v %v", opening, err)
	}
}
