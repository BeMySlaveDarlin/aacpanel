package contract

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The fixtures are the reference with one thing done to it: a field taken
// away, a kind changed, a value or a method added — what a release of codex
// does to the schema between two checks.

func panel(t *testing.T) *Contract {
	t.Helper()
	c, err := Panel()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func reference(t *testing.T) *Schema {
	t.Helper()
	s, err := Reference()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// release is the reference as a release that changed it would write it.
func release(t *testing.T, change func(root map[string]any)) *Schema {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(referenceJSON))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		t.Fatal(err)
	}
	change(root)
	delete(root, versionKey)
	body, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Load(body)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// node is the object at a path of keys and indexes.
func node(t *testing.T, root map[string]any, path ...any) map[string]any {
	t.Helper()
	var cur any = root
	for _, key := range path {
		switch k := key.(type) {
		case string:
			cur = cur.(map[string]any)[k]
		case int:
			cur = cur.([]any)[k]
		}
		if cur == nil {
			t.Fatalf("the reference has nothing at %v", path)
		}
	}
	return cur.(map[string]any)
}

func props(t *testing.T, root map[string]any, def ...any) map[string]any {
	t.Helper()
	return node(t, root, append([]any{"definitions"}, def...)...)["properties"].(map[string]any)
}

// member is the index of the method in the list of a kind of message.
func member(t *testing.T, root map[string]any, def, method string) int {
	t.Helper()
	for i, m := range node(t, root, "definitions", def)["oneOf"].([]any) {
		enum, _ := m.(map[string]any)["properties"].(map[string]any)["method"].(map[string]any)["enum"].([]any)
		if len(enum) > 0 && enum[0] == method {
			return i
		}
	}
	t.Fatalf("%s lists no %s", def, method)
	return -1
}

func find(list []Finding, place string) (Finding, bool) {
	i := slices.IndexFunc(list, func(f Finding) bool { return f.Place == place })
	if i < 0 {
		return Finding{}, false
	}
	return list[i], true
}

func TestTheReferenceHoldsEverythingThePanelUses(t *testing.T) {
	r := Check(reference(t), reference(t), panel(t))
	if len(r.Gone)+len(r.Changed)+len(r.New) > 0 {
		t.Fatalf("the reference against itself is not an empty report — a place of uses.txt it lacks, a value it "+
			"does not have, an object the panel builds without a field it requires:\n%s", r.Text())
	}
	if r.Reference == "" {
		t.Fatal("the reference does not say which release of codex it is of")
	}
}

// A reference cut again is the same reference: it holds every node a check
// reads, so a check of a release against it reads in it what it reads in the
// release.
func TestTheReferenceIsCutToWhatTheContractReads(t *testing.T) {
	ref := reference(t)
	again, err := Prune(ref, panel(t), ref.Version())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, referenceJSON) {
		t.Fatal("the reference cut to what the contract reads is not the reference: write it again with " +
			"`make codex-contract CONTRACT_ARGS=-write`")
	}
}

func TestEveryNotificationIsReadOrTurnedOff(t *testing.T) {
	c := panel(t)
	f := reference(t).facts(c)
	heard := c.Methods(ServerNotification)
	for m := range f.methods[ServerNotification] {
		if !slices.Contains(heard, m) && !slices.Contains(c.Quiet, m) {
			t.Errorf("%s is neither read nor turned off: a notification nobody turned off reaches the executor, "+
				"and most of them carry the conversation", m)
		}
	}
}

func TestAFieldThePanelReadsGoneIsRedByItsName(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		delete(props(t, root, "v2", "Thread"), "path")
	})
	r := Check(cur, reference(t), panel(t))
	if !r.Red() {
		t.Fatalf("a field the panel reads is gone, and the report is not red:\n%s", r.Text())
	}
	for _, method := range []string{"thread/read", "thread/resume", "thread/start"} {
		f, ok := find(r.Gone, method+" result thread.path")
		if !ok || !f.Red {
			t.Errorf("%s result thread.path is not among what is gone, red:\n%s", method, r.Text())
		}
	}
	if !strings.Contains(r.Text(), "!! thread/read result thread.path — the panel reads it") {
		t.Errorf("the words of the report do not name the field:\n%s", r.Text())
	}
}

func TestAFieldThePanelSendsGoneIsRed(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		delete(props(t, root, "v2", "TurnInterruptParams"), "turnId")
	})
	r := Check(cur, reference(t), panel(t))
	if f, ok := find(r.Gone, "turn/interrupt params turnId"); !ok || !f.Red || !strings.Contains(f.Detail, "sends") {
		t.Fatalf("a field the panel sends is gone, and the report does not say so in red:\n%s", r.Text())
	}
}

func TestAFieldOfAnotherKindIsRed(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		created := props(t, root, "v2", "Thread")["createdAt"].(map[string]any)
		created["type"] = "string"
	})
	r := Check(cur, reference(t), panel(t))
	f, ok := find(r.Changed, "thread/read result thread.createdAt")
	if !ok || !f.Red || f.Detail != "string, was integer; the panel reads it" {
		t.Fatalf("a number that became a string is not a red change:\n%s", r.Text())
	}
}

func TestAValueThePanelNamesGoneIsRed(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		status := node(t, root, "definitions", "v2", "McpServerConnectionStatus")
		status["enum"] = slices.DeleteFunc(status["enum"].([]any), func(v any) bool { return v == "starting" })
	})
	r := Check(cur, reference(t), panel(t))
	f, ok := find(r.Gone, "mcpServerStatus/list result data[].runtimeStatus")
	if !ok || !f.Red || f.Detail != `"starting", which the panel reads` {
		t.Fatalf("a status word the screen maps is gone, and the report is not red with it:\n%s", r.Text())
	}
}

func TestAValueThePanelDoesNotNameGoneIsAWarning(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		status := node(t, root, "definitions", "v2", "ThreadStatus")
		status["oneOf"] = slices.DeleteFunc(status["oneOf"].([]any), func(v any) bool {
			kind := v.(map[string]any)["properties"].(map[string]any)["type"].(map[string]any)["enum"].([]any)
			return kind[0] == "systemError"
		})
	})
	r := Check(cur, reference(t), panel(t))
	if r.Red() {
		t.Fatalf("a status the panel does not name is gone, and the report is red:\n%s", r.Text())
	}
	if f, ok := find(r.Gone, "thread/read result thread.status.type"); !ok || !strings.Contains(f.Detail, "systemError") {
		t.Fatalf("a status gone is not among what is gone:\n%s", r.Text())
	}
}

func TestANewKindOfItemIsAWarning(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		item := node(t, root, "definitions", "v2", "ThreadItem")
		item["oneOf"] = append(item["oneOf"].([]any), map[string]any{
			"type":     "object",
			"required": []any{"id", "type"},
			"properties": map[string]any{
				"id":   map[string]any{"type": "string"},
				"type": map[string]any{"type": "string", "enum": []any{"teleport"}},
			},
		})
	})
	r := Check(cur, reference(t), panel(t))
	if r.Red() {
		t.Fatalf("a new kind of item is red:\n%s", r.Text())
	}
	if f, ok := find(r.New, "thread/items/list result data[].item.type"); !ok || f.Detail != `"teleport"` {
		t.Fatalf("a new kind of item is not among what is new:\n%s", r.Text())
	}
}

func TestANewNotificationNobodyTurnedOffIsAWarning(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		list := node(t, root, "definitions", "ServerNotification")
		list["oneOf"] = append(list["oneOf"].([]any), map[string]any{"properties": map[string]any{
			"method": map[string]any{"enum": []any{"thread/teleported"}}}})
	})
	r := Check(cur, reference(t), panel(t))
	if r.Red() {
		t.Fatalf("a new notification is red:\n%s", r.Text())
	}
	f, ok := find(r.New, "server notification thread/teleported")
	if !ok || !strings.Contains(f.Detail, "neither read nor turned off") {
		t.Fatalf("a new notification nobody turned off is not said to reach the executor:\n%s", r.Text())
	}
}

func TestANewRequiredParamIsRed(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		params := node(t, root, "definitions", "v2", "TurnStartParams")
		params["properties"].(map[string]any)["flavour"] = map[string]any{"type": "string"}
		params["required"] = append(params["required"].([]any), "flavour")
	})
	r := Check(cur, reference(t), panel(t))
	f, ok := find(r.Changed, "turn/start params")
	if !ok || !f.Red || f.Detail != "requires flavour, which the panel does not send" {
		t.Fatalf("a call that now requires a param the panel does not send is not red:\n%s", r.Text())
	}
}

// What a variant of a union requires is asked of the variant the panel sends,
// not of the others.
func TestAVariantThePanelBuildsIsHeldToWhatItRequires(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		target := node(t, root, "definitions", "v2", "ReviewTarget")
		for _, v := range target["oneOf"].([]any) {
			variant := v.(map[string]any)
			kind := variant["properties"].(map[string]any)["type"].(map[string]any)["enum"].([]any)[0]
			if kind == "commit" {
				variant["required"] = append(variant["required"].([]any), "author")
			}
		}
	})
	r := Check(cur, reference(t), panel(t))
	f, ok := find(r.Changed, "review/start params target<commit>")
	if !ok || !f.Red || !strings.Contains(f.Detail, "author") {
		t.Fatalf("a review of a commit that now requires an author is not red:\n%s", r.Text())
	}
}

func TestAMethodThePanelCallsGoneIsRed(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		list := node(t, root, "definitions", "ClientRequest")
		i := member(t, root, "ClientRequest", "thread/read")
		list["oneOf"] = slices.Delete(list["oneOf"].([]any), i, i+1)
	})
	r := Check(cur, reference(t), panel(t))
	f, ok := find(r.Gone, "client request thread/read")
	if !ok || !f.Red {
		t.Fatalf("a method the panel calls is gone, and the report is not red with it:\n%s", r.Text())
	}
	// Its fields are gone with it, and the method says it once.
	if _, ok := find(r.Gone, "thread/read result thread.path"); ok {
		t.Fatalf("the fields of a method gone are told one by one:\n%s", r.Text())
	}
}

func TestAMethodThePanelDoesNotUseGoneIsAWarning(t *testing.T) {
	cur := release(t, func(root map[string]any) {
		list := node(t, root, "definitions", "ServerNotification")
		i := member(t, root, "ServerNotification", "turn/started")
		list["oneOf"] = slices.Delete(list["oneOf"].([]any), i, i+1)
	})
	r := Check(cur, reference(t), panel(t))
	if r.Red() {
		t.Fatalf("a notification the panel turns off is gone, and the report is red:\n%s", r.Text())
	}
	if f, ok := find(r.Gone, "server notification turn/started"); !ok || f.Detail != "the panel turns it off" {
		t.Fatalf("a notification gone is not among what is gone:\n%s", r.Text())
	}
}

func TestADecisionWithAnAmendmentIsAValue(t *testing.T) {
	c := panel(t)
	f := reference(t).facts(c)
	got := f.places["item/commandExecution/requestApproval params availableDecisions[]"].values
	for _, want := range []string{"accept", "acceptWithExecpolicyAmendment", "applyNetworkPolicyAmendment"} {
		if !slices.Contains(got, want) {
			t.Errorf("the decisions of an approval are %v, without %s", got, want)
		}
	}
}

func TestTheContractRefusesWhatItCannotRead(t *testing.T) {
	for _, tc := range []struct{ text, says string }{
		{"  read thread.id\n", "a place under no message"},
		{"call thread/read\n  write threadId\n", `"write" is no place`},
		{"hear thread/name/updated\n  send threadId\n", "only read"},
		{"call thread/read\n  read thread..id\n", "names no field"},
		{"call thread/read\n  read data[x]\n", "is no step"},
		{"call thread/read\ncall thread/read\n", "written twice"},
		{"listen thread/read\n", "is no message"},
	} {
		_, err := Parse(tc.text)
		if err == nil || !strings.Contains(err.Error(), tc.says) || !strings.HasPrefix(err.Error(), "uses.txt:") {
			t.Errorf("%q: %v, want it to say %q with the line", tc.text, err, tc.says)
		}
	}
}

func TestAPathReadsItsSteps(t *testing.T) {
	steps, err := parsePath("data[].item<fileChange>.changes[].kind.move_path")
	if err != nil {
		t.Fatal(err)
	}
	if got := stepText(steps); got != "data[].item<fileChange>.changes[].kind.move_path" {
		t.Fatalf("the path reads back as %q", got)
	}
	if len(steps) != 8 || steps[3] != (step{op: '<', name: "fileChange"}) {
		t.Fatalf("the steps are %+v", steps)
	}
}
