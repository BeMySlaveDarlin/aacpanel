package checklist

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"aacpanel/internal/mcp"
)

func fixed(id string, pid int) mcp.Bind {
	return func() (mcp.Binding, error) { return bound(id, pid), nil }
}

func clock() time.Time { return t0 }

// callChecklist calls the tool the way the server does, with the arguments as
// the model sends them.
func callChecklist(t *testing.T, tool mcp.Tool, bind mcp.Bind, args any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return tool.Call(context.Background(), bind, raw)
}

// hello is the handshake of a server with the checklist tool alone, and returns
// its word to the session.
func hello(t *testing.T, dir string, bind mcp.Bind) string {
	t.Helper()
	srv := &mcp.Server{Tools: []mcp.Tool{Tool(dir, clock)}, Bind: bind}
	var out strings.Builder
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"
	if err := srv.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out.String()), &reply); err != nil {
		t.Fatalf("the server answered %q: %v", out.String(), err)
	}
	return reply.Result.Instructions
}

// The tool as the server lists it: the checklist with its schema, allowed,
// since a list of steps that asked the person before every update would not
// be kept.
func TestTheChecklistToolIsListedWithItsSchema(t *testing.T) {
	tool := Tool(t.TempDir(), clock)
	if tool.Name != "checklist" || tool.Title != "Checklist" || tool.Description != Description ||
		tool.Instructions != Instructions || !tool.Allowed {
		t.Errorf("the tool is %+v", tool)
	}
	// Items are not required: a call without them reads the checklist.
	var schema map[string]any
	raw, _ := json.Marshal(tool.InputSchema)
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["type"] != "object" || schema["required"] != nil {
		t.Errorf("the input schema is %v", schema)
	}
	item := schema["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	status := item["properties"].(map[string]any)["status"].(map[string]any)
	if !reflect.DeepEqual(status["enum"], []any{"pending", "active", "done", "dropped"}) ||
		!reflect.DeepEqual(item["required"], []any{"text", "status"}) {
		t.Errorf("a step is described as %v", item)
	}
}

// A call keeps the checklist of the place the claude works in, with the
// conversation found on every call: the same process moves to another
// conversation on /clear, and the checklist it sends next says so.
func TestACallKeepsTheChecklistOfThePlaceWithTheConversationAskedOnEveryCall(t *testing.T) {
	dir := t.TempDir()
	conversations := []string{sid, "6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e"}
	calls := 0
	bind := func() (mcp.Binding, error) {
		calls++
		return bound(conversations[min(calls, 2)-1], 4242), nil
	}
	tool := Tool(dir, clock)

	said, failed := callChecklist(t, tool, bind, map[string]any{
		"items": []any{
			map[string]any{"text": "read the code", "status": "done"},
			map[string]any{"text": "write the tests", "status": "active"},
			map[string]any{"text": "mutate", "status": "pending"},
		},
		"note": "waits on nothing",
	})
	if failed || said != "The checklist is kept: 1 of 3 steps finished." {
		t.Errorf("the call answered %q", said)
	}
	got := Read(dir, lab)
	if got == nil || got.PID != 4242 || got.SessionID != sid || got.Note != "waits on nothing" || len(got.Items) != 3 ||
		got.Items[1] != (Item{Text: "write the tests", Status: Active, Since: t0.Format(Stamp)}) {
		t.Fatalf("the file holds %+v", got)
	}

	callChecklist(t, tool, bind, map[string]any{"items": []any{map[string]any{"text": "start over", "status": "active"}}})
	if next := Read(dir, lab); next == nil || next.Items[0].Text != "start over" || next.SessionID != conversations[1] {
		t.Errorf("the checklist after /clear is %+v", next)
	}
}

// standingChecklist is a checklist a session left in the place before it
// restarted.
func standingChecklist(t *testing.T, dir string, items ...Item) {
	t.Helper()
	if _, err := Keep(dir, bound("6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e", 999), items, "waits on CI", t0); err != nil {
		t.Fatal(err)
	}
}

// A session started again in a place that has a checklist learns of it with the
// handshake: how far it got, the step it stands at, and how to read the rest
// or clear it. The step is quoted short and the whole word stays bounded,
// since it stands in the system prompt of the whole session.
func TestTheHandshakeTellsARestartedSessionOfTheChecklistOfItsPlace(t *testing.T) {
	dir := t.TempDir()
	standingChecklist(t, dir, Item{Text: "read the code", Status: Done}, Item{Text: "the old idea", Status: Dropped},
		Item{Text: "write the tests", Status: Active}, Item{Text: "mutate", Status: Pending})
	said := hello(t, dir, fixed(sid, 1))
	if !strings.HasPrefix(said, Instructions+" This place already has a checklist") {
		t.Errorf("the checklist does not follow the line of the tool: %q", said)
	}
	for _, want := range []string{"2 of 4 steps finished", "the current step: “write the tests”",
		"without items", "clear it with an empty list", t0.Format(Stamp)} {
		if !strings.Contains(said, want) {
			t.Errorf("the instructions do not say %q: %q", want, said)
		}
	}

	next := t.TempDir()
	standingChecklist(t, next, Item{Text: "read the code", Status: Done}, Item{Text: "mutate", Status: Pending})
	if said := hello(t, next, fixed(sid, 1)); !strings.Contains(said, "the next step: “mutate”") {
		t.Errorf("a checklist with no step at work: %q", said)
	}

	over := t.TempDir()
	standingChecklist(t, over, Item{Text: "read the code", Status: Done})
	if said := hello(t, over, fixed(sid, 1)); !strings.Contains(said, "1 of 1 steps finished, nothing left to do") {
		t.Errorf("a finished checklist: %q", said)
	}

	long := t.TempDir()
	step := strings.Repeat("é", MaxText)
	standingChecklist(t, long, Item{Text: step, Status: Active})
	said = hello(t, long, fixed(sid, 1))
	if strings.Contains(said, step) || !strings.Contains(said, strings.Repeat("é", standingStep-1)+"…") {
		t.Errorf("the step at its longest is quoted whole: %q", said)
	}
	if extra := utf8.RuneCountInString(said) - utf8.RuneCountInString(Instructions); extra > 500 {
		t.Errorf("the checklist adds %d characters to the instructions", extra)
	}
}

// A place with no checklist, or one not known at the handshake, is told what
// the tool is for and nothing more.
func TestTheHandshakeOfAPlaceWithoutAChecklistSaysNothingOfOne(t *testing.T) {
	if said := hello(t, t.TempDir(), fixed(sid, 1)); said != Instructions {
		t.Errorf("an empty place: %q", said)
	}
	dir := t.TempDir()
	standingChecklist(t, dir, Item{Text: "one", Status: Active})
	unknown := func() (mcp.Binding, error) { return mcp.Binding{}, errors.New("no file of the session yet") }
	if said := hello(t, dir, unknown); said != Instructions {
		t.Errorf("a place not known: %q", said)
	}
}

// A call without items writes nothing and reads the checklist whole, so a
// session that goes on with a checklist it did not send can send it back with
// its changes. A place without a checklist says so.
func TestACallWithoutItemsReadsTheChecklist(t *testing.T) {
	dir := t.TempDir()
	tool := Tool(dir, clock)
	if said, failed := callChecklist(t, tool, fixed(sid, 1), map[string]any{}); failed || said != "There is no checklist in this place." {
		t.Errorf("an empty place read %q", said)
	}
	if said, failed := tool.Call(context.Background(), fixed(sid, 1), nil); failed || said != "There is no checklist in this place." {
		t.Errorf("a call without arguments read %q", said)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("reading wrote %v", entries)
	}

	standingChecklist(t, dir, Item{Text: "read the code", Status: Done}, Item{Text: "write the tests", Status: Active})
	before := Read(dir, lab)
	said, failed := callChecklist(t, tool, fixed(sid, 1), map[string]any{"note": "ignored"})
	want := "The checklist, last sent 2026-09-28T10:00:00Z: 1 of 2 steps finished.\n" +
		"1. [done] read the code\n2. [active] write the tests\nNote: waits on CI"
	if failed || said != want {
		t.Errorf("the checklist read as %q", said)
	}
	if after := Read(dir, lab); !reflect.DeepEqual(after, before) {
		t.Errorf("reading the checklist changed it: %+v", after)
	}
}

// The first call of a place takes over a checklist filed under a conversation
// of it, and a step sent again keeps the time it had there.
func TestACallTakesOverAChecklistFiledUnderAConversationOfThePlace(t *testing.T) {
	dir, config := t.TempDir(), t.TempDir()
	here := mcp.Place{ConfigDir: config, Dir: "/srv/proj/lab"}
	const before = "11111111-1111-4111-8111-111111111111"
	transcriptOf(t, here, before)
	filed(t, dir, before, "2026-09-28T09:00:00Z", Item{Text: "read the code", Status: Active, Since: "2026-09-28T08:00:00Z"})

	said := hello(t, dir, func() (mcp.Binding, error) { return mcp.Binding{Place: here, PID: 1}, nil })
	if !strings.Contains(said, "the current step: “read the code”") {
		t.Errorf("the handshake did not take the filed checklist over: %q", said)
	}
	bind := func() (mcp.Binding, error) { return mcp.Binding{Place: here, SessionID: sid, PID: 1}, nil }
	callChecklist(t, Tool(dir, clock), bind, map[string]any{"items": []any{map[string]any{"text": "read the code", "status": "active"}}})
	if got := Read(dir, here); got == nil || got.Items[0].Since != "2026-09-28T08:00:00Z" || got.SessionID != sid {
		t.Errorf("the checklist went on as %+v", got)
	}
}

// What the model got wrong, or what the host could not do, is the tool's own
// error: the model reads it and sends the checklist again. Nothing is written.
func TestWhatTheCallGotWrongComesBackAsTheToolsError(t *testing.T) {
	lost := func() (mcp.Binding, error) {
		return mcp.Binding{}, errors.New("where claude process 9 works is not known")
	}
	for name, c := range map[string]struct {
		args any
		bind mcp.Bind
		says string
	}{
		"an unknown status": {map[string]any{"items": []any{map[string]any{"text": "x", "status": "in_progress"}}},
			fixed(sid, 1), `"in_progress"`},
		"items of another shape":    {map[string]any{"items": "read, test"}, fixed(sid, 1), "not the checklist's"},
		"a place not found":         {map[string]any{"items": []any{}}, lost, "not kept: where claude process 9"},
		"a place not found to read": {map[string]any{}, lost, "not read: where claude process 9"},
		"a place that is not one": {map[string]any{"items": []any{map[string]any{"text": "x", "status": "active"}}},
			func() (mcp.Binding, error) {
				return mcp.Binding{Place: mcp.Place{ConfigDir: "/srv/claude", Dir: "lab"}}, nil
			}, "place of the session is not known"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			said, failed := callChecklist(t, Tool(dir, clock), c.bind, c.args)
			if !failed || !strings.Contains(said, c.says) {
				t.Errorf("answered %q (error %v), meant a tool error saying %q", said, failed, c.says)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("a refused checklist was written: %v", entries)
			}
		})
	}
}

func TestAnEmptyListClearsTheChecklistThroughTheTool(t *testing.T) {
	dir := t.TempDir()
	tool := Tool(dir, clock)
	callChecklist(t, tool, fixed(sid, 1), map[string]any{"items": []any{map[string]any{"text": "one", "status": "active"}}})
	said, failed := callChecklist(t, tool, fixed(sid, 1), map[string]any{"items": []any{}})
	if failed || said != "The checklist is cleared." || Read(dir, lab) != nil {
		t.Errorf("clearing answered %q and left %+v", said, Read(dir, lab))
	}
}
