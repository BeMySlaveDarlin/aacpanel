package codex

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/codex/codextest"
)

func ready(t *testing.T, l *Link) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.Ready(ctx); err != nil {
		t.Fatal(err)
	}
}

func params(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// polls lets the link read the daemon a few rounds.
func polls() { time.Sleep(8 * pollEvery) }

// A thread starts in the directory with only what the map chose: a choice
// left empty is not sent, so the config.toml of the home decides it.
func TestAThreadStartsWithWhatTheMapChoseAndNothingElse(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	ctx := context.Background()

	bare, err := l.Start(ctx, Begin{CWD: "/srv/proj"})
	if err != nil {
		t.Fatal(err)
	}
	full, err := l.Start(ctx, Begin{CWD: "/srv/proj", Model: "gpt-5.5", Approval: "untrusted", Sandbox: "read-only",
		Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	starts := srv.Calls("thread/start")
	if len(starts) != 2 {
		t.Fatalf("thread/start went %d times", len(starts))
	}
	if got := params(t, starts[0]); !reflect.DeepEqual(got, map[string]any{"cwd": "/srv/proj"}) {
		t.Errorf("a thread of a map that chose nothing started with %v", got)
	}
	want := map[string]any{"cwd": "/srv/proj", "model": "gpt-5.5", "approvalPolicy": "untrusted", "sandbox": "read-only",
		"config": map[string]any{"model_reasoning_effort": "high"}}
	if got := params(t, starts[1]); !reflect.DeepEqual(got, want) {
		t.Errorf("a thread started with %v, expected %v", got, want)
	}
	if n := len(srv.Calls("turn/start")) + len(srv.Calls("thread/name/set")); n != 0 {
		t.Errorf("a thread with no first message and no name had %d turns and namings", n)
	}
	if bare == full || SessionName(full) != "codex-beef0002" {
		t.Errorf("the threads are %s and %s", bare, full)
	}
	if st, _, ok := stateOf(t, full); !ok || st.CWD != "/srv/proj" || st.Model != "gpt-5.5" || st.Effort != "high" ||
		st.Name != "codex-beef0002" {
		t.Errorf("the new thread is on the map as %+v (%v)", st, ok)
	}
}

// A thread for codex in a terminal is named at its start: the naming writes
// it to the disk, and codex resumes only a thread written there. The name is
// the panel's and the row shows it, until codex in the terminal titles the
// thread after its first request.
func TestAThreadForATerminalIsNamed(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	id, err := l.Start(context.Background(), Begin{CWD: "/srv/proj", Name: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	names := srv.Calls("thread/name/set")
	if len(names) != 1 || !reflect.DeepEqual(params(t, names[0]), map[string]any{"threadId": id, "name": "shop"}) {
		t.Errorf("thread/name/set went as %s", names)
	}
	if st, raw, _ := stateOf(t, id); st.Title != "shop" {
		t.Errorf("the name the panel gave at the start is not on the state: %s", raw)
	}
	srv.Update(id, func(th *codextest.Thread) { th.Name = "Look into the router" })
	until(t, "the title codex gave to take the panel's name off the state", func() bool {
		st, _, _ := stateOf(t, id)
		return st.Title == ""
	})
}

// A first message starts the first turn at once, with the effort of the map
// when it chose one.
func TestAFirstMessageStartsTheFirstTurnAtTheEffort(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	ctx := context.Background()

	id, err := l.Start(ctx, Begin{CWD: "/srv/proj", Message: "run the tests", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Start(ctx, Begin{CWD: "/srv/proj", Message: "and the linter"}); err != nil {
		t.Fatal(err)
	}
	turns := srv.Calls("turn/start")
	if len(turns) != 2 {
		t.Fatalf("turn/start went %d times", len(turns))
	}
	first := params(t, turns[0])
	input, _ := json.Marshal(first["input"])
	if first["threadId"] != id || first["effort"] != "high" ||
		string(input) != `[{"text":"run the tests","text_elements":[],"type":"text"}]` {
		t.Errorf("the first turn went as %v", first)
	}
	if _, set := params(t, turns[1])["effort"]; set {
		t.Errorf("a map with no effort sent one: %s", turns[1])
	}
}

// The panel stays a client of a thread it holds — nothing else holds it, and
// the daemon unloads a thread nobody holds — and leaves one it does not.
// Closed, the held thread is interrupted where a turn runs, left, unmarked,
// and kept off the map until the daemon unloads it; a turn another client
// starts in it brings it back.
func TestAHeldThreadIsLeftOnlyWhenClosed(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	ctx := context.Background()

	held, err := l.Start(ctx, Begin{CWD: "/srv/proj", Message: "go", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	loose, err := l.Start(ctx, Begin{CWD: "/srv/proj"})
	if err != nil {
		t.Fatal(err)
	}
	srv.Set(held, "idle")
	until(t, "the link to leave the thread it does not hold", func() bool { return !srv.Subscribed(loose) })
	polls()
	if !srv.Subscribed(held) {
		t.Fatal("the link left a thread it holds")
	}
	if _, err := os.Stat(heldPath("acme", held)); err != nil {
		t.Errorf("the held thread is not marked: %v", err)
	}

	srv.Running(held, "turn-9")
	interrupted, err := l.Close(ctx, held)
	if err != nil || !interrupted {
		t.Fatalf("the close: %v, interrupted %v", err, interrupted)
	}
	stops := srv.Calls("turn/interrupt")
	if len(stops) != 1 || params(t, stops[0])["turnId"] != "turn-9" {
		t.Errorf("turn/interrupt went as %s", stops)
	}
	if srv.Subscribed(held) {
		t.Error("the closed thread was not left")
	}
	if _, err := os.Stat(heldPath("acme", held)); !os.IsNotExist(err) {
		t.Errorf("the mark of the closed thread stayed: %v", err)
	}
	polls()
	if _, _, ok := stateOf(t, held); ok {
		t.Error("the closed thread came back on the map before the daemon unloaded it")
	}

	srv.Running(held, "turn-10")
	until(t, "a turn of another client to bring the thread back", written(t, held))
}

// The panel takes back the threads it held when the executor starts again,
// before the daemon unloads them; a mark of a thread the daemon no longer has
// is dropped.
func TestTheNextExecutorTakesBackTheHeldThreads(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	id, err := l.Start(context.Background(), Begin{CWD: "/srv/proj", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	gone := "019a1f00-0000-7000-8000-00000000dead"
	if err := os.WriteFile(heldPath("acme", gone), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	next := NewLink(srv.Home, "acme")
	srv.Drop(id)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		next.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()
	until(t, "the next link to join the held thread", func() bool {
		for _, raw := range srv.Calls("thread/resume") {
			if params(t, raw)["threadId"] == id {
				return srv.Subscribed(id)
			}
		}
		return false
	})
	until(t, "the mark of an unloaded thread to go", func() bool {
		_, err := os.Stat(heldPath("acme", gone))
		return os.IsNotExist(err)
	})
	if _, err := os.Stat(heldPath("acme", id)); err != nil {
		t.Errorf("the mark of the held thread went: %v", err)
	}
}

// Waiting for the link dials at once rather than at the next round: a daemon
// the executor has just started is used within the action that started it.
func TestReadyDialsAtOnce(t *testing.T) {
	was := redialEvery
	redialEvery = time.Hour
	defer func() { redialEvery = was }()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := codextest.New(t)
	srv.Close()
	l := NewLink(srv.Home, "acme")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()
	time.Sleep(50 * time.Millisecond)
	if l.Up() {
		t.Fatal("a closed daemon is up")
	}
	srv.Start(t)
	if !l.Up() {
		t.Fatal("a daemon listening is not up")
	}
	wait, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := l.Ready(wait); err != nil {
		t.Fatalf("the link waited for the next round: %v", err)
	}
}

// A close may land while a poll reads the thread, between what the daemon said
// and what the link does with it. Closed before the poll takes what it read,
// the thread is not taken back; closed after, the link finds nothing to keep
// and goes on, its lock free for the next caller.
func TestAThreadClosedWhileAPollReadsItStaysGone(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	l := NewLink(t.TempDir(), "acme")
	info := threadInfo{ID: "th-closed", Status: threadStatus{Type: "idle"}}

	l.closed[info.ID] = ""
	if l.seen(info, time.Now()) || l.threads[info.ID] != nil {
		t.Error("a poll took back a thread the panel closed while it was read")
	}

	delete(l.closed, info.ID)
	if !l.seen(info, time.Now()) {
		t.Fatal("a poll did not take a thread nobody closed")
	}
	l.remove(info.ID)
	done := make(chan any)
	go func() {
		defer func() { done <- recover() }()
		l.keep(context.Background(), nil, info.ID)
	}()
	select {
	case broke := <-done:
		if broke != nil {
			t.Fatalf("keep of a thread closed since it was seen: %v", broke)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("keep of a thread closed since it was seen did not return")
	}
	if !l.mu.TryLock() {
		t.Fatal("the link's lock stayed held")
	}
	l.mu.Unlock()
}

// A thread of the archive is resumed in its daemon and held from then on: the
// daemon loads it for the panel, which stays its client while it idles, the
// way it holds a thread it started, and the thread is on the map as a live
// session under the tail of its id.
func TestAThreadOfTheArchiveIsResumedAndHeld(t *testing.T) {
	past := idle(threadA)
	past.Status = "notLoaded"
	srv, l := linked(t, past)
	ready(t, l)

	cwd, err := l.Resume(context.Background(), threadA)
	if err != nil {
		t.Fatal(err)
	}
	if cwd != "/srv/proj" {
		t.Errorf("the resumed thread runs in %q", cwd)
	}
	resumes := srv.Calls("thread/resume")
	if len(resumes) == 0 || params(t, resumes[0])["threadId"] != threadA {
		t.Fatalf("thread/resume went as %s", resumes)
	}
	if _, err := os.Stat(heldPath("acme", threadA)); err != nil {
		t.Errorf("the resumed thread is not marked as held: %v", err)
	}
	if st, raw, ok := stateOf(t, threadA); !ok || st.Name != "codex-0000abcd" || st.CWD != "/srv/proj" {
		t.Errorf("the resumed thread is on the map as %s", raw)
	}
	polls()
	if !srv.Subscribed(threadA) {
		t.Error("the panel left the idle thread it resumed, and the daemon unloads a thread nobody holds")
	}
	if _, _, ok := stateOf(t, threadA); !ok {
		t.Error("the resumed thread fell off the map")
	}
}

// A subagent's thread is part of its parent's turn: a resume of it is refused,
// and the panel does not stay its client.
func TestASubagentsThreadIsNotResumed(t *testing.T) {
	sub := idle(threadB)
	sub.Parent, sub.Status = threadA, "notLoaded"
	srv, l := linked(t, idle(threadA), sub)
	ready(t, l)
	if _, err := l.Resume(context.Background(), threadB); err == nil || !strings.Contains(err.Error(), "subagent") {
		t.Errorf("a resume of a subagent's thread answered %v", err)
	}
	if srv.Subscribed(threadB) {
		t.Error("the panel stayed a client of the subagent's thread")
	}
	if _, err := os.Stat(heldPath("acme", threadB)); !os.IsNotExist(err) {
		t.Errorf("the subagent's thread is marked as held: %v", err)
	}
}

// worked gives a thread a turn that is over: codex has written it to the disk.
func worked(srv *codextest.Server, id string) {
	srv.Running(id, "turn-1")
	srv.Set(id, "idle")
}

func panelServer(tools ...string) map[string]any {
	listed := map[string]any{}
	for _, name := range tools {
		listed[name] = map[string]any{"name": name}
	}
	return map[string]any{"name": "aacpanel", "runtimeStatus": "connected", "authStatus": "unsupported", "tools": listed}
}

func panelTools(t *testing.T, l *Link, id string) []string {
	t.Helper()
	servers, err := l.McpServers(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range servers {
		if s.Name == "aacpanel" {
			return s.Tools
		}
	}
	return nil
}

// A thread starts its servers when the daemon loads it, and a reconnect of a
// thread the panel holds takes it out of the daemon and back in: the thread
// has the servers as they start now — the panel's own with the tools of the
// program as it is after an update — and the settings it had, the permission
// profile the load loses among them.
func TestAReconnectStartsTheServersOfAHeldThreadAnew(t *testing.T) {
	th := idle(threadA)
	th.Profile = ":read-only"
	srv, l := linked(t, th)
	worked(srv, threadA)
	srv.Mcp(panelServer("send_to_session"))
	srv.McpOnLoad(panelServer("checklist", "send_to_session"))
	ready(t, l)
	ctx := context.Background()
	if _, err := l.Resume(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	if got := panelTools(t, l, threadA); !reflect.DeepEqual(got, []string{"send_to_session"}) {
		t.Fatalf("before the reconnect the thread has the tools %v", got)
	}

	if err := l.Reconnect(ctx, threadA, "/opt/panel/aacpanel-exec", []string{"checklist"}); err != nil {
		t.Fatal(err)
	}
	if got := panelTools(t, l, threadA); !reflect.DeepEqual(got, []string{"checklist", "send_to_session"}) {
		t.Errorf("after the reconnect the thread has the tools %v", got)
	}
	if n := srv.Loads(threadA); n != 1 || srv.Archived(threadA) {
		t.Errorf("the daemon loaded the thread %d times, and it is archived: %v", n, srv.Archived(threadA))
	}
	resumes := srv.Calls("thread/resume")
	config, _ := params(t, resumes[len(resumes)-1])["config"].(map[string]any)
	if config["mcp_servers.aacpanel.command"] != "/opt/panel/aacpanel-exec" ||
		config["mcp_servers.aacpanel.tools.checklist.approval_mode"] != "approve" {
		t.Errorf("the thread came back with the config %v", config)
	}
	if got := srv.Settings(threadA); got.Profile != ":read-only" {
		t.Errorf("the thread came back with the profile %q and the sandbox %q", got.Profile, got.Sandbox)
	}
	if !srv.Subscribed(threadA) {
		t.Error("the panel is not a client of the thread it holds")
	}
	if st, raw, ok := stateOf(t, threadA); !ok || st.Mode != ModeReadOnly {
		t.Errorf("the thread is on the map as %s", raw)
	}
	polls()
	if _, _, ok := stateOf(t, threadA); !ok || !srv.Subscribed(threadA) {
		t.Error("the thread fell off the map after the reconnect")
	}
}

// A poll that listed the thread before a reconnect took it out of the daemon
// and reads it while it is out keeps the session as it was: the session does
// not fall off the map for a round, which the screen would read as a session
// that closed by itself. The poll is held on an agent's thread listed first
// until the thread is out.
func TestAThreadStaysOnTheMapWhileAReconnectMovesIt(t *testing.T) {
	agent := idle(threadB)
	agent.Parent = threadA
	srv, l := linked(t, agent, idle(threadA))
	worked(srv, threadA)
	ready(t, l)
	ctx := context.Background()
	if _, err := l.Resume(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	until(t, "the state file", written(t, threadA))
	reads := func() int {
		n := 0
		for _, raw := range srv.Calls("thread/read") {
			if params(t, raw)["threadId"] == threadB {
				n++
			}
		}
		return n
	}
	before := reads()
	srv.StallOnce("thread/read", threadB, 5*pollEvery)
	srv.StallOnce("thread/unarchive", threadA, 10*pollEvery)
	until(t, "a poll to read the agent's thread", func() bool { return reads() > before })

	done := make(chan error, 1)
	go func() { done <- l.Reconnect(ctx, threadA, "", nil) }()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		case <-time.After(2 * time.Millisecond):
		}
		if _, _, ok := stateOf(t, threadA); !ok {
			t.Fatal("the thread fell off the map while the reconnect moved it")
		}
	}
}

// What taking a thread out of the daemon would cut is refused with its
// reason, and the thread stays loaded as it was: a thread another client
// holds, a turn that runs, a thread not on the disk yet, a goal codex works
// towards, background terminals and agents at work.
func TestAReconnectRefusesWhatAnUnloadCuts(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*codextest.Server)
		loose  bool
		reason string
	}{
		{"another client holds it", nil, true, "does not hold"},
		{"a turn runs", func(s *codextest.Server) { s.Running(threadA, "turn-2") }, false, "a turn runs"},
		{"a turn the status does not show yet", func(s *codextest.Server) { s.Starting(threadA, "turn-2") }, false,
			"a turn runs"},
		{"a goal", func(s *codextest.Server) {
			s.Update(threadA, func(th *codextest.Thread) {
				th.Goal = map[string]any{"objective": "ship it", "status": "active"}
			})
		}, false, "goal"},
		{"background terminals", func(s *codextest.Server) {
			s.Update(threadA, func(th *codextest.Thread) {
				th.Processes = []map[string]any{{"processId": "7", "command": "npm run dev"}}
			})
		}, false, "background terminals"},
		{"an agent at work", func(s *codextest.Server) {
			agent := idle(threadB)
			agent.Parent, agent.Status = threadA, "active"
			s.Add(agent)
		}, false, "agents"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, l := linked(t, idle(threadA))
			worked(srv, threadA)
			ready(t, l)
			ctx := context.Background()
			if !c.loose {
				if _, err := l.Resume(ctx, threadA); err != nil {
					t.Fatal(err)
				}
			}
			if c.setup != nil {
				c.setup(srv)
			}
			err := l.Reconnect(ctx, threadA, "/opt/panel/aacpanel-exec", nil)
			if err == nil || !strings.Contains(err.Error(), c.reason) {
				t.Errorf("the reconnect answered %v", err)
			}
			if n := len(srv.Calls("thread/archive")); n != 0 || srv.Settings(threadA).Status == "notLoaded" {
				t.Errorf("the thread went out of the daemon: %d archives", n)
			}
		})
	}

	t.Run("a thread not on the disk yet", func(t *testing.T) {
		srv, l := linked(t, idle(threadA))
		ready(t, l)
		ctx := context.Background()
		if _, err := l.Resume(ctx, threadA); err != nil {
			t.Fatal(err)
		}
		err := l.Reconnect(ctx, threadA, "", nil)
		if err == nil || !strings.Contains(err.Error(), "has not written the thread") {
			t.Errorf("the reconnect answered %v", err)
		}
		if n := len(srv.Calls("thread/archive")); n != 0 {
			t.Errorf("the thread went out of the daemon: %d archives", n)
		}
	})
}
