package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/codex/codextest"
	"aacpanel/internal/stream"
)

func TestHeldThreadRecoversWithItsLaunchSettings(t *testing.T) {
	for _, executor := range []bool{false, true} {
		for _, sandbox := range []string{"workspace-write", "read-only"} {
			t.Run(fmt.Sprintf("executor=%t/%s", executor, sandbox), func(t *testing.T) {
				t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				srv := codextest.New(t)
				l := NewLink(srv.Home, "acme")
				stop := running(t, l)
				ready(t, l)
				b := Begin{CWD: "/srv/proj", Hold: true, Name: "worker", Parent: "lead",
					Owner: stream.SessionOwner{Name: "lead", Conversation: "lead-conversation"},
					Model: "gpt-helper", Effort: "high", Sandbox: sandbox, Workspace: sandbox == "workspace-write",
					WritableRoots: []string{"/srv/shared", "/srv/proj/.git"}, Tools: "/opt/panel/exec", Allowed: []string{"notify"}}
				b.ReviewAutomatically()
				id, err := l.Start(t.Context(), b)
				if err != nil {
					t.Fatal(err)
				}
				mark, err := os.ReadFile(heldPath("acme", id))
				if err != nil || strings.Contains(string(mark), "lead-conversation") || strings.Contains(string(mark), "worker") {
					t.Fatalf("the held mark contains opening metadata or is missing: %s, %v", mark, err)
				}
				if executor {
					stop()
				} else {
					srv.Close()
					until(t, "the old connection to go", func() bool { return !written(t, id)() })
				}
				srv.Unload(id)
				if executor {
					l = NewLink(srv.Home, "acme")
					running(t, l)
				} else {
					srv.Start(t)
				}
				until(t, "the held thread to return", func() bool {
					return srv.Loads(id) == 1 && srv.Subscribed(id) && written(t, id)()
				})
				want := params(t, srv.Calls("thread/start")[0])
				want["threadId"], want["excludeTurns"] = id, true
				resumed := false
				for _, raw := range srv.Calls("thread/resume") {
					if reflect.DeepEqual(params(t, raw), want) {
						resumed = true
					}
				}
				if !resumed {
					t.Fatalf("the helper did not recover with its launch settings: %s, want %v", srv.Calls("thread/resume"), want)
				}
				after, err := os.ReadFile(heldPath("acme", id))
				if err != nil || string(after) != string(mark) {
					t.Fatalf("recovery changed the held mark: %s, %v", after, err)
				}
				st, _, _ := stateOf(t, id)
				if st.Title != b.Name || st.Parent != b.Parent || stream.OwnerOf(id) != b.Owner || st.SessionID != id {
					t.Fatalf("recovery changed the helper's identity: %+v, owner %+v", st, stream.OwnerOf(id))
				}
				if len(srv.Calls("turn/start")) != 0 || len(srv.Calls("turn/steer")) != 0 {
					t.Fatal("recovery sent a message")
				}
				if _, err := l.Send(t.Context(), id, Message{Text: "the next task"}); err != nil {
					t.Fatal(err)
				}
				if got := turnTexts(t, srv); len(got) != 1 || got[0] != "text:the next task" {
					t.Fatalf("the recovered helper did not take its letter: %q", got)
				}
			})
		}
	}
}

func TestHeldHelperKeepsTheModelChosenByTheHome(t *testing.T) {
	srv, l, c := heldPoll(t)
	id, err := l.Start(t.Context(), Begin{CWD: "/srv/proj", Name: "worker", Hold: true,
		Owner: stream.SessionOwner{Name: "lead", Conversation: "lead-conversation"}})
	if err != nil {
		t.Fatal(err)
	}
	srv.Unload(id)
	srv.Update(id, func(th *codextest.Thread) { th.Model = "gpt-new-home-default" })
	if err := l.poll(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if got := srv.Settings(id).Model; got != "gpt-home" {
		t.Fatalf("the helper's inherited model changed to %s", got)
	}
}

func TestReconnectedHeldThreadRecoversWithCurrentPanelTools(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tools  string
		legacy bool
	}{
		{name: "updated server", tools: "/opt/panel/current"},
		{name: "home servers only"},
		{name: "empty mark", tools: "/opt/panel/current", legacy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, l, _ := heldPoll(t)
			begin := Begin{CWD: "/srv/proj", Hold: true, Name: "worker", Model: "gpt-helper", Effort: "high",
				Sandbox: "workspace-write", Workspace: true, WritableRoots: []string{"/srv/shared", "/srv/proj/.git"},
				Tools: "/opt/panel/start", Allowed: []string{"notify", "retired"}}
			begin.ReviewAutomatically()
			id, err := l.Start(t.Context(), begin)
			if err != nil {
				t.Fatal(err)
			}
			worked(srv, id)
			want := params(t, srv.Calls("thread/start")[0])
			config := want["config"].(map[string]any)
			config["mcp_servers.aacpanel.cwd"] = "/srv/old-tools"
			config["mcp_servers.aacpanel_extra.command"] = "/opt/another-server"
			mark, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			if tc.legacy {
				mark = nil
			}
			if err := os.WriteFile(heldPath("acme", id), mark, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := l.Reconnect(t.Context(), id, tc.tools, []string{"notify", "checklist"}); err != nil {
				t.Fatal(err)
			}
			delete(config, "mcp_servers.aacpanel.cwd")
			delete(config, "mcp_servers.aacpanel.tools.retired.approval_mode")
			if tc.tools == "" {
				delete(config, "mcp_servers.aacpanel.command")
				delete(config, "mcp_servers.aacpanel.args")
				delete(config, "mcp_servers.aacpanel.tools.notify.approval_mode")
			} else {
				config["mcp_servers.aacpanel.command"] = tc.tools
				config["mcp_servers.aacpanel.tools.checklist.approval_mode"] = "approve"
			}
			after, err := os.ReadFile(heldPath("acme", id))
			if err != nil {
				t.Fatal(err)
			}
			if tc.legacy {
				if len(after) != 0 {
					t.Fatalf("reconnect filled an empty mark: %s", after)
				}
				want = map[string]any{}
			} else if got := params(t, after); !reflect.DeepEqual(got, want) {
				t.Fatalf("reconnect kept the wrong recovery settings: %s, want %v", after, want)
			}
			srv.Close()
			l.forget()
			srv.Unload(id)
			srv.Start(t)
			c, err := dial(t.Context(), srv.Socket(), l.handle)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(c.close)
			if _, err := initialize(t.Context(), c); err != nil {
				t.Fatal(err)
			}
			l.conn = c
			if err := l.poll(t.Context(), c); err != nil {
				t.Fatal(err)
			}
			want["threadId"], want["excludeTurns"] = id, true
			calls := srv.Calls("thread/resume")
			if got := params(t, calls[len(calls)-1]); !reflect.DeepEqual(got, want) {
				t.Fatalf("the restarted daemon recovered with %v, want %v", got, want)
			}
			if len(srv.Calls("turn/start")) != 0 {
				t.Fatal("reconnecting or recovering sent a message")
			}
		})
	}
}

// Polling by hand makes the attempt limit and a stale closed mark observable
// without waiting for a timer or racing a background poll.
func heldPoll(t *testing.T) (*codextest.Server, *Link, *conn) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := codextest.New(t)
	l := NewLink(srv.Home, "acme")
	c, err := dial(t.Context(), srv.Socket(), l.handle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initialize(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	l.conn = c
	t.Cleanup(func() { c.close(); l.forget() })
	return srv, l, c
}

func TestClosedHeldThreadIsNotRecovered(t *testing.T) {
	srv, l, c := heldPoll(t)
	id, err := l.Start(t.Context(), Begin{CWD: "/srv/proj", Name: "worker", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Close(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	// A stale mark must not overrule the explicit close in memory.
	l.hold(id, nil)
	srv.Unload(id)
	if err := l.poll(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if len(srv.Calls("thread/resume")) != 0 || written(t, id)() {
		t.Fatal("a closed helper was resumed")
	}
	if _, err := os.Stat(heldPath("acme", id)); !os.IsNotExist(err) {
		t.Fatalf("a closed helper retained its mark: %v", err)
	}
}

func TestUnrecoverableHeldThreadIsReleasedAfterBoundedAttempts(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			srv, l, c := heldPoll(t)
			id := threadA
			if !missing {
				var err error
				id, err = l.Start(t.Context(), Begin{CWD: "/srv/proj", Name: "worker", Hold: true})
				if err != nil {
					t.Fatal(err)
				}
				srv.Unload(id)
				srv.Refuse("thread/resume")
			} else {
				l.hold(id, nil)
			}
			file := filepath.Join(t.TempDir(), "executor.log")
			out, err := os.Create(file)
			if err != nil {
				t.Fatal(err)
			}
			prior := log.Writer()
			log.SetOutput(out)
			t.Cleanup(func() { log.SetOutput(prior); _ = out.Close() })
			l.enqueue(id, Queued{Message: Message{ID: "one", Text: "wait"}, Since: time.Now()})
			for attempt := 1; attempt <= resumeAttempts+2; attempt++ {
				if err := l.poll(t.Context(), c); err != nil {
					t.Fatal(err)
				}
				if attempt < resumeAttempts {
					if _, err := os.Stat(heldPath("acme", id)); err != nil || l.queued(id) != 1 {
						t.Fatalf("recovery abandoned the helper before its limit: %v", err)
					}
				}
			}
			if got := len(srv.Calls("thread/resume")); got != resumeAttempts {
				t.Fatalf("resume attempts = %d, want %d", got, resumeAttempts)
			}
			for _, path := range []string{heldPath("acme", id), outboxPath("acme", id)} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("an unrecoverable helper kept %s: %v", path, err)
				}
			}
			raw, err := os.ReadFile(file)
			if err != nil || !strings.Contains(string(raw), "held thread "+id+" could not be resumed after 3 attempts") {
				t.Fatalf("the executor did not log the failed recovery: %s, %v", raw, err)
			}
		})
	}
}

func TestHeldThreadQueueSurvivesFailedResumeAndExecutorReplacement(t *testing.T) {
	srv, l, c := heldPoll(t)
	id, err := l.Start(t.Context(), Begin{CWD: "/srv/proj", Name: "worker", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	l.enqueue(id, Queued{Message: Message{ID: "one", Text: "the queued task"}, Since: time.Now()})
	l.later(id, "gpt-next", "high")
	srv.Unload(id)
	// A missing mark makes the first attempt fail before contacting the daemon.
	mark, err := os.ReadFile(heldPath("acme", id))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(heldPath("acme", id)); err != nil {
		t.Fatal(err)
	}
	if err := l.poll(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if l.queued(id) != 1 || !written(t, id)() {
		t.Fatal("the helper or its queue disappeared while recovery was pending")
	}
	if err := os.WriteFile(heldPath("acme", id), mark, 0o600); err != nil {
		t.Fatal(err)
	}
	next := NewLink(srv.Home, "acme")
	next.held = heldMarks("acme")
	next.conn = c
	if err := next.poll(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if got := turnTexts(t, srv); len(got) != 1 || got[0] != "text:the queued task" {
		t.Fatalf("the queued letter did not survive recovery: %q", got)
	}
	turn := params(t, srv.Calls("turn/start")[0])
	if turn["model"] != "gpt-next" || turn["effort"] != "high" {
		t.Fatalf("recovery lost the outbox settings: %v", turn)
	}
	if _, err := os.Stat(outboxPath("acme", id)); !os.IsNotExist(err) {
		t.Fatalf("the delivered queue stayed on disk: %v", err)
	}
}
