package codex

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/codex/codextest"
)

// catalogue is what the fake daemon lists: two models, each with the efforts
// it takes.
func catalogue(srv *codextest.Server) {
	srv.Catalogue(
		codextest.Model{ID: "gpt-a", Model: "gpt-a", Name: "GPT A", Efforts: []string{"low", "medium"}, Default: "medium"},
		codextest.Model{ID: "gpt-b", Model: "gpt-b", Name: "GPT B", Efforts: []string{"low", "high", "ultra"}, Default: "high"},
	)
}

// started starts a thread the panel holds on a fake daemon with the catalogue.
func started(t *testing.T, b Begin) (*codextest.Server, *Link, string) {
	t.Helper()
	srv, l := linked(t)
	catalogue(srv)
	ready(t, l)
	b.CWD, b.Hold = "/srv/proj", true
	id, err := l.Start(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	return srv, l, id
}

func stateSays(t *testing.T, id, what string, cond func(State) bool) {
	t.Helper()
	until(t, what, func() bool {
		st, _, ok := stateOf(t, id)
		return ok && cond(st)
	})
}

func lastCall(t *testing.T, srv *codextest.Server, method string) map[string]any {
	t.Helper()
	calls := srv.Calls(method)
	if len(calls) == 0 {
		t.Fatalf("%s never went", method)
	}
	return params(t, calls[len(calls)-1])
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

// A model, an effort, a mode and the plan go to the daemon with
// thread/settings/update and show in the state of the thread at once.
func TestASettingGoesToTheDaemonAndTheStateSaysIt(t *testing.T) {
	srv, l, id := started(t, Begin{Model: "gpt-a", Effort: "low"})
	ctx := context.Background()
	stateSays(t, id, "the mode the thread starts in", func(st State) bool {
		return st.Mode == ModeAsk && st.Plan != nil && !*st.Plan
	})

	if _, err := l.Configure(ctx, id, Setting{Model: "gpt-b", Effort: "ultra"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"threadId": id, "model": "gpt-b", "effort": "ultra"}
	if got := lastCall(t, srv, "thread/settings/update"); !reflect.DeepEqual(got, want) {
		t.Errorf("thread/settings/update went as %v, meant %v", got, want)
	}
	stateSays(t, id, "the new model", func(st State) bool { return st.Model == "gpt-b" && st.Effort == "ultra" })

	// gpt-a does not take ultra: picked alone, it starts at its own effort.
	done, err := l.Configure(ctx, id, Setting{Model: "gpt-a"})
	if err != nil || done.Effort != "medium" || done.Later {
		t.Fatalf("a model picked alone: %+v, %v", done, err)
	}
	if got := lastCall(t, srv, "thread/settings/update"); got["effort"] != "medium" {
		t.Errorf("a model whose efforts leave out the thread's went as %v", got)
	}
	if _, err := l.Configure(ctx, id, Setting{Effort: "low"}); err != nil {
		t.Fatal(err)
	}
	if got := lastCall(t, srv, "thread/settings/update"); got["model"] != "gpt-a" || got["effort"] != "low" {
		t.Errorf("an effort picked alone went as %v", got)
	}

	if _, err := l.Configure(ctx, id, Setting{Mode: ModeAuto}); err != nil {
		t.Fatal(err)
	}
	want = map[string]any{"threadId": id, "permissions": ":workspace", "approvalPolicy": "on-request",
		"approvalsReviewer": "auto_review"}
	if got := lastCall(t, srv, "thread/settings/update"); !reflect.DeepEqual(got, want) {
		t.Errorf("the auto mode went as %v, meant %v", got, want)
	}
	stateSays(t, id, "the auto mode", func(st State) bool { return st.Mode == ModeAuto })
	if _, err := l.Configure(ctx, id, Setting{Mode: ModeReadOnly}); err != nil {
		t.Fatal(err)
	}
	if got := lastCall(t, srv, "thread/settings/update"); got["permissions"] != ":read-only" ||
		got["approvalsReviewer"] != "user" {
		t.Errorf("the read-only mode went as %v", got)
	}
	stateSays(t, id, "the read-only mode", func(st State) bool { return st.Mode == ModeReadOnly })

	if _, err := l.Configure(ctx, id, Setting{Plan: yes()}); err != nil {
		t.Fatal(err)
	}
	want = map[string]any{"threadId": id, "collaborationMode": map[string]any{"mode": "plan",
		"settings": map[string]any{"model": "gpt-a", "reasoning_effort": "low", "developer_instructions": nil}}}
	if got := lastCall(t, srv, "thread/settings/update"); !reflect.DeepEqual(got, want) {
		t.Errorf("plan mode went as %v, meant %v", got, want)
	}
	if th := srv.Settings(id); !th.Plan || th.Model != "gpt-a" || th.Effort != "low" {
		t.Errorf("plan mode left the thread on %s at %s (plan %v)", th.Model, th.Effort, th.Plan)
	}
	stateSays(t, id, "the plan", func(st State) bool { return st.Plan != nil && *st.Plan && st.Mode == ModeReadOnly })
	if _, err := l.Configure(ctx, id, Setting{Plan: no()}); err != nil {
		t.Fatal(err)
	}
	stateSays(t, id, "the plan left", func(st State) bool { return st.Plan != nil && !*st.Plan })
}

// The daemon takes any model and any effort and fails only at the turn that
// runs on them, so a pick the catalogue does not hold is refused before it
// reaches the daemon.
func TestAPickTheCatalogueDoesNotHoldIsRefusedBeforeTheDaemon(t *testing.T) {
	srv, l, id := started(t, Begin{Model: "gpt-a", Effort: "low"})
	ctx := context.Background()
	for _, c := range []struct {
		set  Setting
		says string
	}{
		{Setting{Model: "gpt-z"}, `offers no model "gpt-z"; it offers gpt-a, gpt-b`},
		{Setting{Model: "gpt-a", Effort: "ultra"}, `gpt-a does not take the effort "ultra"; it takes low, medium`},
		{Setting{Effort: "high"}, `gpt-a does not take the effort "high"`},
		{Setting{Mode: "full-access"}, `no mode "full-access"`},
	} {
		if _, err := l.Configure(ctx, id, c.set); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%+v: %v, meant to say %q", c.set, err, c.says)
		}
	}
	srv.Update(id, func(th *codextest.Thread) { th.Model = "gpt-hidden" })
	until(t, "the thread to run a model the catalogue does not list", func() bool {
		st, _, _ := stateOf(t, id)
		return st.Model == "gpt-hidden"
	})
	if _, err := l.Configure(ctx, id, Setting{Effort: "low"}); err == nil || !strings.Contains(err.Error(), "not known") {
		t.Errorf("an effort for a model the catalogue does not list: %v", err)
	}
	if n := len(srv.Calls("thread/settings/update")); n != 0 {
		t.Errorf("%d refused picks reached the daemon", n)
	}
}

// thread/settings/update is experimental. A daemon that refuses it takes the
// model and the effort with the next turn the panel starts, and only that
// one; a mode and the plan have no such way and are refused with its reason.
func TestADaemonThatRefusesTheChangeTakesTheModelWithTheNextTurn(t *testing.T) {
	srv, l, id := started(t, Begin{Model: "gpt-a", Effort: "low"})
	srv.Refuse("thread/settings/update")
	ctx := context.Background()

	done, err := l.Configure(ctx, id, Setting{Model: "gpt-b", Effort: "high"})
	if err != nil || !done.Later {
		t.Fatalf("a refused change of the model: %+v, %v", done, err)
	}
	for _, set := range []Setting{{Mode: ModeAsk}, {Plan: yes()}} {
		_, err := l.Configure(ctx, id, set)
		if err == nil || !strings.Contains(err.Error(), "unknown variant `thread/settings/update`") ||
			strings.Contains(err.Error(), "expected one of") {
			t.Errorf("%+v on a daemon that refuses the change: %v", set, err)
		}
	}

	if _, err := l.Send(ctx, id, Message{Text: "go"}); err != nil {
		t.Fatal(err)
	}
	if got := lastCall(t, srv, "turn/start"); got["model"] != "gpt-b" || got["effort"] != "high" {
		t.Errorf("the next turn went as %v", got)
	}
	srv.Set(id, "idle")
	if _, err := l.Send(ctx, id, Message{Text: "again"}); err != nil {
		t.Fatal(err)
	}
	if got := lastCall(t, srv, "turn/start"); got["model"] != nil || got["effort"] != nil {
		t.Errorf("the pick went with a second turn too: %v", got)
	}
}

// A thread starts in the mode its permissions are; one that matches no mode
// of the panel's — full access from the map — is custom.
func TestTheModeOfAThreadIsReadFromItsStart(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	ctx := context.Background()
	for _, c := range []struct {
		begin Begin
		mode  string
	}{
		{Begin{CWD: "/srv/proj", Sandbox: "read-only"}, ModeReadOnly},
		{Begin{CWD: "/srv/proj", Sandbox: "workspace-write"}, ModeAsk},
		{Begin{CWD: "/srv/proj", Sandbox: "danger-full-access", Approval: "never"}, ModeCustom},
		{Begin{CWD: "/srv/proj", Approval: "untrusted"}, ModeCustom},
	} {
		id, err := l.Start(ctx, c.begin)
		if err != nil {
			t.Fatal(err)
		}
		stateSays(t, id, "the mode of "+c.begin.Sandbox+c.begin.Approval, func(st State) bool { return st.Mode == c.mode })
	}
	_ = srv
}

// A thread the link only reads has no mode on the map: thread/read does not
// say the permissions, and the link does not join a thread to learn them.
func TestAThreadTheLinkOnlyReadsHasNoMode(t *testing.T) {
	srv, _ := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	st, raw, _ := stateOf(t, threadA)
	if st.Mode != "" || st.Plan != nil || strings.Contains(string(raw), `"plan"`) {
		t.Errorf("a thread the link has not been told of says %s", raw)
	}
	if srv.Subscribed(threadA) {
		t.Error("the link joined a thread to learn its settings")
	}
}

// A change the panel makes is known from its success, even for a thread the
// daemon never told the link the settings of.
func TestAChangeOfThePanelsIsKnownFromItsSuccess(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	catalogue(srv)
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()
	if _, err := l.Configure(ctx, threadA, Setting{Mode: ModeAuto}); err != nil {
		t.Fatal(err)
	}
	stateSays(t, threadA, "the mode it set", func(st State) bool { return st.Mode == ModeAuto && st.Plan == nil })
	if _, err := l.Configure(ctx, threadA, Setting{Plan: yes()}); err != nil {
		t.Fatal(err)
	}
	stateSays(t, threadA, "the plan it set", func(st State) bool { return st.Plan != nil && *st.Plan })
}

// A thread the link joins — it waits on an approval — shows the settings the
// answer of thread/resume says.
func TestAThreadTheLinkJoinsShowsItsSettings(t *testing.T) {
	th := idle(threadA)
	th.Profile, th.Plan = ":read-only", true
	srv, l := linked(t, th)
	until(t, "the state file", written(t, threadA))
	srv.Ask(threadA, "item/commandExecution/requestApproval", map[string]any{"command": "ls"})
	until(t, "the request", func() bool { return len(l.Pending(threadA)) == 1 })
	stateSays(t, threadA, "the settings of the resume", func(st State) bool {
		return st.Mode == ModeReadOnly && st.Plan != nil && *st.Plan
	})
}

// Settings another client changes reach the state of a thread the link is a
// client of.
func TestSettingsChangedElsewhereReachTheState(t *testing.T) {
	srv, _, id := started(t, Begin{Model: "gpt-a"})
	srv.Update(id, func(th *codextest.Thread) { th.Profile, th.Approval = ":danger-full-access", "never" })
	stateSays(t, id, "full access set in the TUI", func(st State) bool { return st.Mode == ModeCustom })
	srv.Update(id, func(th *codextest.Thread) {
		th.Profile, th.Approval, th.Reviewer, th.Plan = ":workspace", "on-request", "auto_review", true
	})
	stateSays(t, id, "the auto mode and the plan", func(st State) bool {
		return st.Mode == ModeAuto && st.Plan != nil && *st.Plan
	})
}

// How full the context is comes with every request of a turn to the clients
// of the thread.
func TestTheFillOfTheContextComesFromTheDaemon(t *testing.T) {
	srv, _, id := started(t, Begin{})
	srv.Usage(id, 129200, 258400)
	stateSays(t, id, "the fill of the context", func(st State) bool {
		return st.Context != nil && st.Context.Tokens == 129200 && st.Context.Window == 258400 && !st.Context.At.IsZero()
	})
}
