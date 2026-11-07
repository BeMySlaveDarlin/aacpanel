package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/heavy"
)

type heavyExecutor struct {
	view *heavy.View
	err  error
}

func (e *heavyExecutor) Execute(context.Context, action.Request) (string, error) {
	panic("a host poll must not execute an action")
}

func (e *heavyExecutor) Heavy(context.Context) (*heavy.View, error) { return e.view, e.err }

type heavyAPIReply struct {
	Heavy    *heavy.View `json:"heavy"`
	Error    string      `json:"heavyError"`
	Sessions []struct {
		Name  string `json:"session"`
		Place int    `json:"heavyPlace"`
	} `json:"sessions"`
}

func TestHeavyQueueReachesTheHostAPIAndMarksOnlyWaitingSessions(t *testing.T) {
	e := &heavyExecutor{view: &heavy.View{Slots: 2,
		Running: []heavy.RunningView{{Unit: "aacp-heavy-1.scope", Session: "working", Ask: 4096, Taken: 1024}},
		Waiting: []heavy.WaitingView{{Place: 1, Session: "", Name: "terminal"}, {Place: 2, Session: "lab", Name: "check"},
			{Place: 3, Session: "lab", Name: "build"}}, Head: "a slot"}}
	path := socketPath(t, "heavy.sock")
	server := action.NewServer(path, e, time.Second)
	ln, err := server.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); server.Run(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	s := &Server{host: hostWith(t, `{"at":1,"sessions":[{"session":"lab"},{"session":"working"},{"session":"quiet"}]}`),
		exec: action.NewClient(path, time.Second)}
	read := func() heavyAPIReply {
		t.Helper()
		rec := httptest.NewRecorder()
		s.apiHost(rec, httptest.NewRequest(http.MethodGet, "/api/host", nil))
		var got heavyAPIReply
		if rec.Code != http.StatusOK {
			t.Fatalf("the host poll failed: %d %s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := read()
	if got.Heavy == nil || got.Heavy.Slots != 2 || got.Heavy.Running[0].Session != "working" ||
		len(got.Heavy.Waiting) != 3 || got.Heavy.Waiting[1].Session != "lab" || got.Heavy.Head != "a slot" {
		t.Fatalf("the host API loses the queue: %+v", got)
	}
	for _, s := range got.Sessions {
		want := 0
		if s.Name == "lab" {
			want = 2
		}
		if s.Place != want {
			t.Errorf("%s is marked at %d, want %d", s.Name, s.Place, want)
		}
	}
	e.view = &heavy.View{Slots: 2, Running: []heavy.RunningView{}, Waiting: []heavy.WaitingView{}}
	got = read()
	if got.Heavy == nil || len(got.Heavy.Waiting) != 0 {
		t.Fatalf("the empty queue is lost: %+v", got)
	}
	for _, s := range got.Sessions {
		if s.Place != 0 {
			t.Errorf("%s keeps a queue mark after admission", s.Name)
		}
	}
	e.err = errors.New("host read failed")
	got = read()
	if got.Heavy != nil || got.Error != e.err.Error() {
		t.Errorf("an unavailable queue reads %+v", got)
	}
}
