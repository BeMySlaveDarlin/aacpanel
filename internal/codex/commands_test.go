package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aacpanel/internal/codex/codextest"
)

// A name given in the panel reaches the row, and stays there only while codex
// calls the thread by it: a name given in codex's terminal does not; the
// session keeps the name the panel finds it by.
func TestTheNameThePanelGaveReachesTheStateWhileTheThreadKeepsIt(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	if err := l.Rename(context.Background(), threadA, "login-bug"); err != nil {
		t.Fatal(err)
	}
	if calls := srv.Calls("thread/name/set"); len(calls) != 1 || !strings.Contains(string(calls[0]), `"name":"login-bug"`) {
		t.Fatalf("thread/name/set went as %s", calls)
	}
	st, _, _ := stateOf(t, threadA)
	if st.Title != "login-bug" || st.Name != "codex-0000abcd" {
		t.Errorf("the state names the thread %q, the session %q", st.Title, st.Name)
	}
	polls()
	if st, _, _ := stateOf(t, threadA); st.Title != "login-bug" {
		t.Errorf("the name the panel gave went from the state as the daemon was read again: %q", st.Title)
	}
	srv.Update(threadA, func(th *codextest.Thread) { th.Name = "from the tui" })
	until(t, "a name given elsewhere to take the panel's off the state", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Title == ""
	})
}

// A name codex makes up by itself — its terminal titles a thread after the
// first request — is no name of the panel's, and the row does not show it.
func TestANameCodexGaveTheThreadIsNotOnTheState(t *testing.T) {
	th := idle(threadA)
	th.Name = "Look into the router"
	_, l := linked(t, th)
	until(t, "the state file", written(t, threadA))
	until(t, "the link to know the name", func() bool { return calledAs(l, threadA) == "Look into the router" })
	polls()
	if st, raw, _ := stateOf(t, threadA); st.Title != "" {
		t.Errorf("the state shows the name codex gave the thread: %s", raw)
	}
}

// A name the daemon tells its clients of a thread the panel never named does
// not reach the row; its word that a thread the panel named is called another
// way now takes the panel's name off.
func TestTheNameTheDaemonSaysToItsClientsIsShownOnlyIfThePanelGaveIt(t *testing.T) {
	srv, l := linked(t, idle(threadA), idle(threadB))
	until(t, "the state files", func() bool { return written(t, threadA)() && written(t, threadB)() })
	l.join(context.Background(), mustClient(t, l), threadA)
	l.join(context.Background(), mustClient(t, l), threadB)

	srv.Notify(threadB, "thread/name/updated", map[string]any{"threadName": "Look into the router"})
	until(t, "the link to hear the name", func() bool { return calledAs(l, threadB) == "Look into the router" })
	polls()
	if st, raw, _ := stateOf(t, threadB); st.Title != "" {
		t.Errorf("a name the daemon told of a thread the panel never named reached the state: %s", raw)
	}

	if err := l.Rename(context.Background(), threadA, "admin"); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := stateOf(t, threadA); st.Title != "admin" {
		t.Fatalf("the name the panel gave is not on the state: %q", st.Title)
	}
	srv.Update(threadA, func(th *codextest.Thread) { th.Name = "Look into the router" })
	srv.Notify(threadA, "thread/name/updated", map[string]any{"threadName": "Look into the router"})
	until(t, "the word of another name to take the panel's off the state", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Title == ""
	})
}

// The name the panel gave outlives the executor: the one started after it
// knows the name from the disk, and the row keeps it.
func TestTheNameThePanelGaveOutlivesTheExecutor(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := codextest.New(t)
	srv.Add(idle(threadA))
	first := NewLink(srv.Home, "acme")
	stop := running(t, first)
	until(t, "the state file", written(t, threadA))
	if err := first.Rename(context.Background(), threadA, "login-bug"); err != nil {
		t.Fatal(err)
	}
	stop()
	if _, _, ok := stateOf(t, threadA); ok {
		t.Fatal("the state file outlived the link that wrote it")
	}

	running(t, NewLink(srv.Home, "acme"))
	until(t, "the next link's state file", written(t, threadA))
	if st, raw, _ := stateOf(t, threadA); st.Title != "login-bug" {
		t.Errorf("the executor started again lost the name the panel gave: %s", raw)
	}
}

// A thread the panel started at the word of a session names that session as
// its parent on its state, and the executor started after a restart of its own
// knows the parent from the disk. A thread a person started has none.
func TestTheParentOfAThreadOutlivesTheExecutor(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := codextest.New(t)
	first := NewLink(srv.Home, "acme")
	stop := running(t, first)
	ready(t, first)
	child, err := first.Start(context.Background(), Begin{CWD: "/srv/proj", Parent: "lead", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	loose, err := first.Start(context.Background(), Begin{CWD: "/srv/proj", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	if st, raw, _ := stateOf(t, child); st.Parent != "lead" {
		t.Errorf("the state of a thread started for a session does not name it: %s", raw)
	}
	if st, raw, _ := stateOf(t, loose); st.Parent != "" {
		t.Errorf("the state of a thread a person started names a parent: %s", raw)
	}
	stop()

	running(t, NewLink(srv.Home, "acme"))
	until(t, "the next link's state file", written(t, child))
	if st, raw, _ := stateOf(t, child); st.Parent != "lead" {
		t.Errorf("the executor started again lost the parent of the thread: %s", raw)
	}
}

// calledAs is the name the link last heard a thread called by, empty while
// it heard none.
func calledAs(l *Link, id string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t := l.threads[id]; t != nil && t.info.Name != nil {
		return *t.info.Name
	}
	return ""
}

func mustClient(t *testing.T, l *Link) *conn {
	t.Helper()
	c, err := l.client()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A goal is set, paused and cleared as the person's own change, and how it
// stands reaches the row — from the answer, from the daemon's word to its
// clients, and from a read when the panel heard nothing.
func TestAGoalIsSetPausedAndClearedAndReachesTheState(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()
	budget := int64(5000)
	g, err := l.SetGoal(ctx, threadA, "ship the fix", "", &budget)
	if err != nil || g.Objective != "ship the fix" || g.Status != "active" || g.TokenBudget == nil || *g.TokenBudget != 5000 {
		t.Fatalf("the goal set is %+v, %v", g, err)
	}
	if call := string(srv.Calls("thread/goal/set")[0]); !strings.Contains(call, `"origin":"user"`) ||
		strings.Contains(call, `"status"`) {
		t.Errorf("thread/goal/set went as %s", call)
	}
	st, _, _ := stateOf(t, threadA)
	if st.Goal == nil || st.Goal.Objective != "ship the fix" {
		t.Errorf("the state carries the goal %+v", st.Goal)
	}
	if g, err := l.SetGoal(ctx, threadA, "", GoalPaused, nil); err != nil || g.Status != "paused" {
		t.Fatalf("pausing: %+v, %v", g, err)
	}
	if had, err := l.ClearGoal(ctx, threadA); err != nil || !had {
		t.Fatalf("clearing: %v, %v", had, err)
	}
	if st, _, _ := stateOf(t, threadA); st.Goal != nil {
		t.Errorf("a cleared goal stays in the state: %+v", st.Goal)
	}

	srv.Update(threadA, func(th *codextest.Thread) {
		th.Goal = map[string]any{"threadId": threadA, "objective": "set in the tui", "status": "blocked",
			"tokenBudget": nil, "tokensUsed": 120, "timeUsedSeconds": 9, "createdAt": 1, "updatedAt": 2}
		th.Updated = time.Now().Unix() + 100
	})
	until(t, "a goal set elsewhere to be read", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Goal != nil && st.Goal.Status == "blocked" && st.Goal.TokensUsed == 120
	})
	l.join(ctx, mustClient(t, l), threadA)
	srv.Notify(threadA, "thread/goal/cleared", map[string]any{})
	until(t, "the word of a cleared goal to reach the state", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Goal == nil
	})
}

// The background terminals of a thread are counted on the row, listed,
// stopped one by one or all at once.
func TestBackgroundTerminalsAreCountedListedAndStopped(t *testing.T) {
	th := idle(threadA)
	th.Processes = []map[string]any{
		{"itemId": "call_1", "processId": "71", "command": "sleep 300", "cwd": "/srv/proj", "osPid": 4242,
			"cpuPercent": 0.5, "rssKb": 2048},
		{"itemId": "call_2", "processId": "72", "command": "npm run dev", "cwd": "/srv/proj", "osPid": nil,
			"cpuPercent": nil, "rssKb": nil},
	}
	srv, l := linked(t, th)
	until(t, "the row to count the terminals", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Processes != nil && *st.Processes == 2
	})
	ctx := context.Background()
	list, err := l.Processes(ctx, threadA)
	if err != nil || len(list) != 2 || list[0].ID != "71" || list[0].Command != "sleep 300" || *list[0].PID != 4242 ||
		*list[0].RSS != 2048 || list[1].PID != nil {
		t.Fatalf("the terminals are %+v, %v", list, err)
	}
	if stopped, err := l.Terminate(ctx, threadA, "71"); err != nil || !stopped {
		t.Fatalf("stopping one: %v, %v", stopped, err)
	}
	if stopped, err := l.Terminate(ctx, threadA, "71"); err != nil || stopped {
		t.Errorf("stopping one gone: %v, %v", stopped, err)
	}
	if err := l.Clean(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	until(t, "the row to count none", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Processes != nil && *st.Processes == 0
	})
	if len(srv.Calls("thread/backgroundTerminals/clean")) != 1 {
		t.Error("clean did not reach the daemon")
	}
}

// The methods of the background terminals are experimental: a daemon that
// dropped them is not asked at every poll, and the refusal says what it is.
func TestADaemonWithoutBackgroundTerminalsIsNotAskedAgain(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	srv.Refuse("thread/backgroundTerminals/list")
	// A thread that runs has its extras read at every poll.
	srv.Running(threadA, "turn-x")
	until(t, "the state file", written(t, threadA))
	time.Sleep(10 * pollEvery)
	if n := len(srv.Calls("thread/backgroundTerminals/list")); n > 1 {
		t.Errorf("a daemon that refused the list was asked it %d times", n)
	}
	if n := len(srv.Calls("thread/goal/get")); n < 3 {
		t.Errorf("the goal of a thread that runs was read %d times in ten polls", n)
	}
	if _, err := l.Processes(context.Background(), threadA); err == nil || !strings.Contains(err.Error(), "experimental") {
		t.Errorf("the refusal says %v", err)
	}
	if st, _, _ := stateOf(t, threadA); st.Processes != nil {
		t.Errorf("a count nobody read is on the row: %d", *st.Processes)
	}
}

// A thread nothing happens in is not read for its extras at every poll.
func TestAQuietThreadIsReadForItsExtrasOnlyNowAndThen(t *testing.T) {
	srv, _ := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	time.Sleep(10 * pollEvery)
	if n := len(srv.Calls("thread/goal/get")); n != 1 {
		t.Errorf("a quiet thread had its goal read %d times", n)
	}
}

// A compaction and a review go to the daemon; a review is a turn, refused on
// a busy thread and followed like a turn the panel started.
func TestCompactAndReviewGoToTheDaemon(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()
	if err := l.StartReview(ctx, threadA, Review{Target: ReviewBranch}); err == nil {
		t.Error("a review against no branch went")
	}
	if err := l.StartReview(ctx, threadA, Review{Target: ReviewCommit, SHA: "abc1234", Title: "fix it"}); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Target   map[string]any `json:"target"`
		Delivery string         `json:"delivery"`
	}
	_ = json.Unmarshal(srv.Calls("review/start")[0], &p)
	if !reflect.DeepEqual(p.Target, map[string]any{"type": "commit", "sha": "abc1234", "title": "fix it"}) ||
		p.Delivery != "inline" {
		t.Errorf("review/start went as %s", srv.Calls("review/start")[0])
	}
	if !srv.Subscribed(threadA) {
		t.Error("the panel does not follow the review it started")
	}
	if err := l.StartReview(ctx, threadA, Review{Target: ReviewUncommitted}); err == nil ||
		!strings.Contains(err.Error(), "a turn runs") {
		t.Errorf("a review into a running turn: %v", err)
	}
	if stopped, err := l.Interrupt(ctx, threadA); err != nil || !stopped {
		t.Errorf("a review was not interrupted: %v, %v", stopped, err)
	}
	if calls := srv.Calls("turn/interrupt"); len(calls) != 2 || !strings.Contains(string(calls[1]), `"turnId":"turn-1-review"`) {
		t.Errorf("turn/interrupt went as %s", calls)
	}
	if err := l.Compact(ctx, threadA); err != nil {
		t.Errorf("a compaction: %v", err)
	}
}

// The servers and the skills of a thread are codex's own lists, in the shape
// the panel shows.
func TestMcpServersAndSkillsAreCodexsLists(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	srv.Mcp(map[string]any{"name": "docker", "runtimeStatus": "connected", "authStatus": "unsupported",
		"httpOrigin": nil, "pluginId": nil, "toolsError": nil, "resources": []any{}, "resourceTemplates": []any{},
		"serverCapabilities": nil, "tools": map[string]any{"ps": map[string]any{"name": "ps"}, "logs": map[string]any{"name": "logs"}},
		"serverInfo": map[string]any{"name": "docker-mcp", "title": nil, "version": "1.2", "description": nil}})
	srv.Skills(map[string]any{"name": "pdf", "description": "read pdfs", "scope": "user", "enabled": false,
		"pluginId": nil, "path": "/srv/skills/pdf"})
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()
	servers, err := l.McpServers(ctx, threadA)
	want := []McpServer{{Name: "docker", Status: "connected", Auth: "unsupported", Title: "docker-mcp", Version: "1.2",
		Tools: []string{"logs", "ps"}}}
	if err != nil || !reflect.DeepEqual(servers, want) {
		t.Errorf("the servers are %+v, %v", servers, err)
	}
	if calls := srv.Calls("mcpServerStatus/list"); len(calls) != 1 || !strings.Contains(string(calls[0]), threadA) {
		t.Errorf("the servers were asked as %s", calls)
	}
	skills, err := l.Skills(ctx, threadA)
	if err != nil || !reflect.DeepEqual(skills, []Skill{{Name: "pdf", Description: "read pdfs", Scope: "user"}}) {
		t.Errorf("the skills are %+v, %v", skills, err)
	}
	if calls := srv.Calls("skills/list"); len(calls) != 1 || !strings.Contains(string(calls[0]), `"cwds":["/srv/proj"]`) {
		t.Errorf("the skills were asked as %s", calls)
	}
}

// How a server of a thread stands comes in codex's own words, and a server
// codex has no word for — one changed or added after the thread started it —
// comes with none rather than one the panel made up.
func TestMcpServersStandAsCodexSaysTheyDo(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	states := []any{"notStarted", "starting", "connected", "authenticationRequired", "failed", "cancelled", "disabled", nil}
	servers := []map[string]any{}
	for i, s := range states {
		servers = append(servers, map[string]any{"name": fmt.Sprintf("s%d", i), "runtimeStatus": s,
			"authStatus": "unsupported", "tools": map[string]any{}, "resources": []any{}, "resourceTemplates": []any{}})
	}
	srv.Mcp(servers...)
	until(t, "the state file", written(t, threadA))
	got, err := l.McpServers(context.Background(), threadA)
	if err != nil || len(got) != len(states) {
		t.Fatalf("the servers are %+v, %v", got, err)
	}
	for i, s := range states {
		want, _ := s.(string)
		if got[i].Status != want {
			t.Errorf("a server codex calls %v stands as %q", s, got[i].Status)
		}
	}
}

// The models of a contour come from the daemon of its home; a contour with no
// home is refused rather than answered by another one.
func TestTheModelsOfAContourComeFromItsDaemon(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	one, two := codextest.New(t), codextest.New(t)
	one.Catalogue(codextest.Model{ID: "a", Model: "gpt-a", Name: "A", Efforts: []string{"low"}, Default: "low"})
	two.Catalogue(codextest.Model{ID: "b", Model: "gpt-b", Name: "B", Efforts: []string{"high"}, Default: "high"})
	ls := &Links{list: []*Link{NewLink(one.Home, "personal"), NewLink(two.Home, "acme")}}
	ctx, cancel := context.WithCancel(context.Background())
	for _, l := range ls.list {
		ls.runs.Go(func() { l.Run(ctx) })
	}
	t.Cleanup(func() {
		cancel()
		ls.Wait()
	})
	for _, l := range ls.list {
		until(t, "the links to connect", func() bool { _, err := l.client(); return err == nil })
	}
	got, err := ls.Models(context.Background(), "acme")
	if err != nil || len(got) != 1 || got[0].Model != "gpt-b" {
		t.Errorf("the models of acme are %+v, %v", got, err)
	}
	if got, err := ls.Models(context.Background(), ""); err != nil || got[0].Model != "gpt-a" {
		t.Errorf("the models of no contour are %+v, %v", got, err)
	}
	if _, err := ls.Models(context.Background(), "nowhere"); err == nil || !strings.Contains(err.Error(), "no codex home") {
		t.Errorf("a contour with no home: %v", err)
	}
}

// A thread codex runs in a tmux session of the panel says so, and says which
// session; a failure to tell the terminals leaves the threads as they were.
func TestATerminalOfThePanelIsOnTheState(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := codextest.New(t)
	srv.Add(idle(threadA))
	srv.Add(idle(threadB))
	held := map[string]string{threadA: "shop"}
	var fail error
	var mu sync.Mutex
	l := NewLink(srv.Home, "acme")
	l.terminals = func(_ context.Context, ids []string) (map[string]string, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail != nil {
			return nil, fail
		}
		out := map[string]string{}
		for _, id := range ids {
			if name, ok := held[id]; ok {
				out[id] = name
			}
		}
		return out, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	until(t, "the terminal on the state", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Terminal == "shop"
	})
	if st, _, _ := stateOf(t, threadB); st.Terminal != "" {
		t.Errorf("a thread only the daemon holds is in a terminal %q", st.Terminal)
	}
	mu.Lock()
	fail = errors.New("tmux did not answer")
	mu.Unlock()
	time.Sleep(5 * pollEvery)
	if st, _, _ := stateOf(t, threadA); st.Terminal != "shop" {
		t.Errorf("a failure to tell the terminals moved the thread out of its terminal: %q", st.Terminal)
	}
	mu.Lock()
	fail, held = nil, map[string]string{}
	mu.Unlock()
	until(t, "the terminal to leave the state", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Terminal == ""
	})
}

// A daemon that refused the terminals is asked again on its next connection:
// it may have updated itself.
func TestANewConnectionAsksAgainWhatTheOldDaemonDidNotKnow(t *testing.T) {
	th := idle(threadA)
	th.Processes = []map[string]any{{"processId": "71", "command": "sleep 300", "cwd": "/srv/proj"}}
	srv, _ := linked(t, th)
	srv.Refuse("thread/backgroundTerminals/list")
	until(t, "the state file", written(t, threadA))
	time.Sleep(5 * pollEvery)
	if st, _, _ := stateOf(t, threadA); st.Processes != nil {
		t.Fatal("a daemon that refused the terminals has them counted")
	}
	srv.Accept("thread/backgroundTerminals/list")
	srv.Close()
	srv.Start(t)
	until(t, "the terminals to be counted on the new connection", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Processes != nil && *st.Processes == 1
	})
}

// A daemon with no thread loaded has no terminals to look for.
func TestNoThreadNoLookAtTmux(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := codextest.New(t)
	var asked atomic.Int32
	l := NewLink(srv.Home, "acme")
	l.terminals = func(context.Context, []string) (map[string]string, error) {
		asked.Add(1)
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	until(t, "the link to connect", func() bool { _, err := l.client(); return err == nil })
	time.Sleep(5 * pollEvery)
	if n := asked.Load(); n != 0 {
		t.Errorf("tmux was looked at %d times with no thread loaded", n)
	}
}
