package codex

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func question() map[string]any {
	return map[string]any{"turnId": "turn-x", "itemId": "call_q1", "isBlocking": true, "autoResolutionMs": nil,
		"questions": []any{map[string]any{"id": "indent", "header": "Indent", "question": "Tabs or spaces?",
			"isOther": true, "isSecret": false, "options": []any{
				map[string]any{"label": "Tabs", "description": "one tab a level"},
				map[string]any{"label": "Spaces", "description": "four spaces a level"}}}}}
}

// A question of plan mode waits as the question of claude's the panel already
// draws, the thread is joined for it whatever the link believed, and the
// answer goes back by the id of each question, a note beside the pick.
func TestAQuestionOfPlanModeWaitsAsAnAskAndTakesTheAnswer(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	id := srv.Ask(threadA, methodUserInput, question())
	until(t, "the question to reach the link", func() bool { return len(l.Pending(threadA)) == 1 })
	if !srv.Subscribed(threadA) {
		t.Error("the question reached the link without a subscription")
	}

	var st State
	until(t, "the state to carry the question", func() bool {
		st, _, _ = stateOf(t, threadA)
		return st.Ask != nil
	})
	want := []AskQuestion{{ID: "indent", Text: "Tabs or spaces?", Header: "Indent", Other: true, Options: []AskOption{
		{Label: "Tabs", Description: "one tab a level"}, {Label: "Spaces", Description: "four spaces a level"}}}}
	if !slices.Equal(st.Waiting, []string{"AskUserQuestion"}) || st.Ask.ToolUseID != "call_q1" || st.Ask.SessionID != threadA ||
		!reflect.DeepEqual(st.Ask.Questions, want) {
		t.Errorf("the state says it waits on %v with %+v", st.Waiting, st.Ask)
	}

	r := l.Pending(threadA)[0]
	reply, err := r.Answers([][]int{{2}}, nil, []string{"keep it short"})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Reply(context.Background(), threadA, r.Key(), reply); err != nil {
		t.Fatal(err)
	}
	until(t, "the answer to reach the daemon", func() bool { return len(srv.Answers()) == 1 })
	if a := srv.Answers()[0]; a.ID != id ||
		string(a.Result) != `{"answers":{"indent":{"answers":["Spaces","user_note: keep it short"]}}}` {
		t.Errorf("the daemon got %d %s", a.ID, a.Result)
	}
	until(t, "the question to leave the state", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Ask == nil
	})
	if _, err := r.Answers([][]int{{3}}, nil, nil); err == nil {
		t.Error("a pick past the options went")
	}
}

// A question answered in codex's own terminal leaves the panel: the daemon
// tells every client the request is gone.
func TestAQuestionAnsweredElsewhereLeavesTheState(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	id := srv.Ask(threadA, methodUserInput, question())
	until(t, "the question to reach the link", func() bool { return len(l.Pending(threadA)) == 1 })
	srv.Resolve(id)
	until(t, "the question to go", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Ask == nil && len(st.Waiting) == 0
	})
}

func TestADismissedQuestionTellsTheModelWhy(t *testing.T) {
	r := Request{Method: methodUserInput, Params: Params{Questions: []Question{{ID: "a"}, {ID: "b"}}}}
	body, _ := json.Marshal(r.Dismiss())
	want := `{"answers":{"a":{"answers":["user_note: ` + Dismissed + `"]},"b":{"answers":["user_note: ` + Dismissed + `"]}}}`
	if string(body) != want {
		t.Errorf("a dismissal says %s", body)
	}
}

// A grant sends back what the request asked for, less what it left null; a
// refusal grants nothing.
func TestAGrantOfPermissionsSendsBackWhatWasAsked(t *testing.T) {
	r := Request{Method: methodPermissions, Params: Params{Permissions: json.RawMessage(
		`{"network":null,"fileSystem":{"read":null,"write":["/srv/out"]}}`)}}
	if !r.Grants() || r.Asks() || r.Tool() != "Permissions" {
		t.Errorf("a request for permissions reads as %s", r.Tool())
	}
	body, _ := json.Marshal(r.Grant("session"))
	if string(body) != `{"permissions":{"fileSystem":{"read":null,"write":["/srv/out"]}},"scope":"session"}` {
		t.Errorf("the grant is %s", body)
	}
	body, _ = json.Marshal(r.Deny())
	if string(body) != `{"permissions":{},"scope":"turn"}` {
		t.Errorf("the refusal is %s", body)
	}
	if got := r.Asked(); !slices.Equal(got, []string{"write /srv/out"}) {
		t.Errorf("the request reads %q", got)
	}
	net := Request{Method: methodPermissions, Params: Params{Permissions: json.RawMessage(`{"network":{"enabled":true}}`)}}
	if got := net.Asked(); !slices.Equal(got, []string{"network access"}) {
		t.Errorf("a request for the network reads %q", got)
	}
}

func form(schema string) Request {
	return Request{ID: json.RawMessage(`7`), Method: methodElicitation, Params: Params{ThreadID: threadA,
		Elicitation: Elicitation{Server: "tracker", Mode: "form", Message: "File the bug", Schema: json.RawMessage(schema)}}}
}

const bugForm = `{"type":"object","properties":{
	"title":{"type":"string","title":"Title"},
	"severity":{"type":"string","title":"Severity","oneOf":[{"const":"low","title":"Low"},{"const":"high","title":"High"}]},
	"count":{"type":"integer","title":"How many"},
	"urgent":{"type":"boolean","description":"Wake somebody up"},
	"labels":{"type":"array","items":{"enum":["ui","api"]}}},
	"required":["title","severity"]}`

// A form of an MCP server is a question a field, in the order the server
// named them, and its answer is a value of the kind each field is of.
func TestAFormOfAnMcpServerIsAQuestionAField(t *testing.T) {
	r := form(bugForm)
	if !r.Asks() || r.Tool() != "AskUserQuestion" {
		t.Fatalf("a form with fields reads as %s", r.Tool())
	}
	a := r.Ask()
	var ids []string
	for _, q := range a.Questions {
		ids = append(ids, q.ID)
	}
	if a.Server != "tracker" || a.Message != "File the bug" || a.ToolUseID != "7" ||
		strings.Join(ids, " ") != "title severity count urgent labels" {
		t.Fatalf("the form reads as %+v", a)
	}
	if q := a.Questions[1]; q.Text != "Severity" || !q.Required || len(q.Options) != 2 || q.Options[1].Label != "High" || q.Other {
		t.Errorf("a field with titled values reads as %+v", q)
	}
	if q := a.Questions[3]; q.Text != "urgent — Wake somebody up" || len(q.Options) != 2 || q.Options[0].Label != "Yes" {
		t.Errorf("a yes or no reads as %+v", q)
	}
	if q := a.Questions[4]; !q.Multi || len(q.Options) != 2 {
		t.Errorf("a list to pick from reads as %+v", q)
	}
	if q := a.Questions[0]; !q.Other || len(q.Options) != 0 || !q.Required {
		t.Errorf("a field of words reads as %+v", q)
	}

	content, err := r.Content([][]int{{}, {2}, {}, {1}, {1, 2}}, []string{"It breaks", "", "3", "", ""})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(Elicit("accept", content))
	if string(body) != `{"_meta":null,"action":"accept","content":{"count":3,"labels":["ui","api"],"severity":"high",`+
		`"title":"It breaks","urgent":true}}` {
		t.Errorf("the form went as %s", body)
	}
	for _, c := range []struct {
		picks [][]int
		texts []string
		says  string
	}{
		{[][]int{{}, {}, {}, {}, {}}, []string{"x", "", "", "", ""}, "Severity is required"},
		{[][]int{{}, {1}, {}, {}, {}}, []string{"x", "", "three", "", ""}, "takes a whole number"},
		{[][]int{{}, {1, 2}, {}, {}, {}}, []string{"x", "", "", "", ""}, "takes one option"},
		{[][]int{{}, {}, {}, {}, {}}, []string{"x", "loud", "", "", ""}, "answered with a pick"},
	} {
		if _, err := r.Content(c.picks, c.texts); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%v %q: %v, meant to say %q", c.picks, c.texts, err, c.says)
		}
	}
	body, _ = json.Marshal(Elicit("decline", map[string]any{"x": 1}))
	if string(body) != `{"_meta":null,"action":"decline","content":null}` {
		t.Errorf("a decline went as %s", body)
	}
}

// A page to open, a verification and a form with nothing to fill are a choice,
// not a question; a form of a kind the panel does not fill says why.
func TestWhatAnMcpServerAsksWithNoFieldsIsAChoice(t *testing.T) {
	page := Request{Method: methodElicitation, Params: Params{Elicitation: Elicitation{Server: "s", Mode: "url",
		Message: "Sign in", URL: "https://example.test/login"}}}
	empty := form(`{"type":"object","properties":{}}`)
	odd := form(`{"type":"object","properties":{"where":{"type":"object"}}}`)
	for name, r := range map[string]Request{"page": page, "empty form": empty, "odd form": odd} {
		if r.Asks() || r.Tool() != "MCP" || !r.Elicits() {
			t.Errorf("%s reads as %s", name, r.Tool())
		}
	}
	if _, err := odd.Fields(); err == nil || !strings.Contains(err.Error(), "does not fill") {
		t.Errorf("a field of an object: %v", err)
	}
	if verify := (Request{Method: methodElicitation, Params: Params{Elicitation: Elicitation{Mode: "openai/userVerification"}}}); !verify.Verification() {
		t.Error("a verification reads as something else")
	}
}

// A question is named by the item of its call, the id the rollout names the
// call by; a request with no item is named by its own id.
func TestAQuestionIsNamedByItsCall(t *testing.T) {
	r := Request{ID: json.RawMessage(`4`), Method: methodUserInput, Params: Params{ItemID: "call_98cb", Questions: []Question{{ID: "a"}}}}
	if r.AskID() != "call_98cb" || r.Ask().ToolUseID != "call_98cb" {
		t.Errorf("the question is named %q", r.AskID())
	}
	r.ItemID = "item with spaces"
	if r.AskID() != "4" {
		t.Errorf("an item id the panel cannot carry names the question %q", r.AskID())
	}
}

// A form the panel cannot show whole is no form on the screen: a field left
// out could be one the server requires.
func TestAFormTooBigIsNoQuestion(t *testing.T) {
	var props []string
	for i := range 9 {
		props = append(props, `"f`+string(rune('a'+i))+`":{"type":"string"}`)
	}
	wide := form(`{"type":"object","properties":{` + strings.Join(props, ",") + `}}`)
	if wide.Asks() {
		t.Error("a form of nine fields is offered as a question")
	}
	if _, err := wide.Fields(); err == nil || !strings.Contains(err.Error(), "9 fields") {
		t.Errorf("a form of nine fields: %v", err)
	}
	var values []string
	for i := range 13 {
		values = append(values, `"v`+string(rune('a'+i))+`"`)
	}
	many := form(`{"type":"object","properties":{"pick":{"type":"string","enum":[` + strings.Join(values, ",") + `]}}}`)
	if _, err := many.Fields(); many.Asks() || err == nil || !strings.Contains(err.Error(), "13 values") {
		t.Errorf("a field of thirteen values: %v", err)
	}
}

// The words of a dismissal are one: the collector reads them back from a
// rollout, and its copy is held to the executor's here.
func TestTheCollectorKeepsTheWordsOfADismissal(t *testing.T) {
	src, err := os.ReadFile("../../agent/chat/codex.py")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "\nDISMISSED = \""+Dismissed+"\"\n") {
		t.Errorf("agent/chat/codex.py does not say DISMISSED = %q", Dismissed)
	}
}

// slowPolls keeps the link from reading the daemon again during a test: what
// the state says then comes from what the link was told, not from a read.
func slowPolls(t *testing.T) {
	prev := pollEvery
	pollEvery = time.Hour
	t.Cleanup(func() { pollEvery = prev })
}

// A question answered leaves the row busy at once: the thread runs on, and
// waits for nobody.
func TestAnAnsweredQuestionLeavesTheThreadBusyAtOnce(t *testing.T) {
	slowPolls(t)
	th := idle(threadA)
	th.Status, th.Flags = "active", []string{"waitingOnUserInput"}
	srv, l := linked(t, th)
	srv.Ask(threadA, methodUserInput, question())
	until(t, "the question to reach the link", func() bool { return len(l.Pending(threadA)) == 1 })
	r := l.Pending(threadA)[0]
	reply, _ := r.Answers([][]int{{1}}, nil, nil)
	if err := l.Reply(context.Background(), threadA, r.Key(), reply); err != nil {
		t.Fatal(err)
	}
	if st, raw, _ := stateOf(t, threadA); !st.Busy || len(st.Waiting) != 0 || st.Ask != nil {
		t.Errorf("after the answer the row says %s", raw)
	}
}

// The status the daemon tells its clients reaches the row whatever it is: a
// thread that waited and runs on is busy again before any read.
func TestTheStatusTheDaemonTellsReachesTheRow(t *testing.T) {
	slowPolls(t)
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	l.join(context.Background(), mustClient(t, l), threadA)
	srv.Set(threadA, "active", "waitingOnApproval")
	until(t, "the row to wait", func() bool {
		st, _, _ := stateOf(t, threadA)
		return slices.Equal(st.Waiting, []string{"waitingOnApproval"})
	})
	srv.Set(threadA, "active")
	until(t, "the row to be busy and wait for nobody", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Busy && len(st.Waiting) == 0
	})
}
