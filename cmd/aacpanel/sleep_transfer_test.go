package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
	"aacpanel/internal/stream"
)

func TestRestartPassesSleepingChildrenToItsNewConversationPG(t *testing.T) {
	for _, mode := range []string{"manual fresh", "automatic fresh", "same conversation", "failed after pass"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			s := newReviveScene(t)
			parent, other := s.row(2), s.row(3)
			next := s.row(20)
			next.Session = "parent-new"
			if mode == "same conversation" {
				next = parent
			}
			s.snap.Sessions = []livingRow{{LiveSession: host.LiveSession{Name: parent.Session, SessionID: parent.Conversation, CWD: s.dir, Transport: "stream"}, Status: "idle"}, {LiveSession: host.LiveSession{Name: other.Session, SessionID: other.Conversation, CWD: s.dir}}}
			s.publish()
			at := time.Now().Add(-time.Hour).Truncate(time.Second)
			until := time.Now().Add(time.Hour).Truncate(time.Second)
			rows := []store.SessionPause{}
			for i, kind := range []string{"owned", "another visual parent", "visual only", "unrelated"} {
				row := s.row(4 + i)
				row.OpenedBy, row.Parent, row.ParentConversation = parent.Conversation, parent.Session, parent.Conversation
				if kind == "another visual parent" || kind == "unrelated" {
					row.Parent, row.ParentConversation = other.Session, other.Conversation
				}
				if kind == "visual only" || kind == "unrelated" {
					row.OpenedBy = other.Conversation
				}
				raw, err := json.Marshal(host.LiveSession{Name: row.Session, SessionID: row.Conversation, CWD: s.dir, Parent: &host.SessionRef{Session: row.Parent}})
				if err != nil {
					t.Fatal(err)
				}
				p := store.SessionPause{LivingSession: row, Snapshot: raw, Sleeping: true, CommandAt: at}
				if i == 0 {
					p.Until = &until
				}
				savePause(t, s, p)
				rows = append(rows, p)
				openerName := parent.Session
				if row.OpenedBy == other.Conversation {
					openerName = other.Session
				}
				if err := stream.KeepOpened(row.Conversation, stream.OpenedSession{SessionOwner: stream.SessionOwner{Name: openerName, Conversation: row.OpenedBy}, Session: row.Session, SessionID: row.Conversation, Agent: "claude"}); err != nil {
					t.Fatal(err)
				}
			}
			socket := socketPath(t, "transfer.sock")
			ln, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			var workers sync.WaitGroup
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					conn, err := ln.Accept()
					if err != nil {
						return
					}
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer conn.Close()
						var req action.Request
						if err := json.NewDecoder(conn).Decode(&req); err != nil {
							t.Error(err)
							return
						}
						response := map[string]any{"id": req.ID, "ok": true, "detail": "completed"}
						if req.Kind == action.SessionRestart {
							ownership, err := stream.PassOwners(parent.Conversation, stream.SessionOwner{Name: next.Session, Conversation: next.Conversation})
							if err != nil {
								t.Error(err)
								return
							}
							if ownership != nil {
								response["ownership"] = ownership
							}
							if mode == "failed after pass" {
								response["ok"], response["error"] = false, "the old turn could not be closed"
							}
						}
						s.exec.got <- req
						if err := json.NewEncoder(conn).Encode(response); err != nil {
							t.Error(err)
						}
					}()
				}
			}()
			t.Cleanup(func() { ln.Close(); <-done; workers.Wait() })
			s.srv.exec = action.NewClient(socket, time.Second)
			if mode == "automatic fresh" {
				if err := s.srv.restartAction(t.Context(), store.RestartCycle{Conversation: parent.Conversation, Session: parent.Session}, restartStep{Kind: action.SessionRestart}); err != nil {
					t.Fatal(err)
				}
			} else {
				raw, _ := json.Marshal(map[string]any{"kind": action.SessionRestart, "target": parent.Session, "params": map[string]any{"conversation": parent.Conversation, "resume": mode == "same conversation"}})
				want := http.StatusOK
				if mode == "failed after pass" {
					want = http.StatusBadGateway
				}
				if w := post(t, s.srv, string(raw)); w.Code != want {
					t.Fatalf("restart: %d %s", w.Code, w.Body.String())
				}
			}
			s.drain()
			if got := stream.OwnerOf(rows[0].Conversation); got.Conversation != next.Conversation {
				t.Fatalf("the host did not pass its opening: %+v", got)
			}
			saved, err := s.srv.sessionPauses(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for i, before := range rows {
				p, ok := pauseConversation(saved, before.Conversation)
				if !ok {
					t.Fatalf("the sleeping child disappeared: %s", before.Session)
				}
				opener, visual, conversation := before.OpenedBy, before.Parent, before.ParentConversation
				if opener == parent.Conversation {
					opener = next.Conversation
				}
				if conversation == parent.Conversation {
					visual, conversation = next.Session, next.Conversation
				}
				if p.OpenedBy != opener || p.Parent != visual || p.ParentConversation != conversation {
					t.Fatalf("child %d kept stale ownership or changed an independent parent: %+v; want %s / %s / %s", i, p, opener, visual, conversation)
				}
				if !p.Sleeping || (p.Until == nil) != (before.Until == nil) || p.Until != nil && !p.Until.Equal(*before.Until) {
					t.Fatalf("passing ownership changed the pause: %+v", p)
				}
			}
			s.snap.Sessions[0].Name, s.snap.Sessions[0].SessionID = next.Session, next.Conversation
			s.snap.At = time.Now().Unix() + 1
			s.publish()
			start := len(s.requests)
			if err := s.srv.wakePaused(context.Background(), rows[0].Conversation); err != nil {
				t.Fatal(err)
			}
			s.drain()
			resumed := s.requests[start:]
			if len(resumed) != 1 || resumed[0].Kind != action.SessionResume || resumed[0].Project == nil || resumed[0].Project.OpenedBy != next.Conversation || resumed[0].Project.Parent != next.Session {
				t.Fatalf("the sleeping child woke under its old opener: %+v", resumed)
			}
		})
	}
}
