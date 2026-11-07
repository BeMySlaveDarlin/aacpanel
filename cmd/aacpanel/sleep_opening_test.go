package main

import (
	"encoding/json"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func TestSleepingChildKeepsItsParentAndOpeningOnWakePG(t *testing.T) {
	for _, scene := range []string{"live parent", "sleeping parent", "parent outside project", "different visual parent", "fresh parent"} {
		t.Run(scene, func(t *testing.T) {
			s, p := pausedScene(t, true)
			owner := s.row(2)
			visual := owner
			if scene == "different visual parent" {
				visual = s.row(3)
			}
			if scene == "parent outside project" {
				owner.CWD = "/srv/another-project"
				visual = owner
			}
			s.exec.resp.Opening = &action.Opening{Name: owner.Session, Conversation: owner.Conversation}
			s.snap.Sessions[0].Parent = &host.SessionRef{Session: visual.Session}
			if scene != "fresh parent" {
				p.Parent, p.ParentConversation = visual.Session, visual.Conversation
				savePause(t, s, p)
			}
			parent := livingRow{LiveSession: host.LiveSession{Name: owner.Session, SessionID: owner.Conversation, CWD: owner.CWD}}
			if scene == "sleeping parent" {
				raw, err := json.Marshal(parent)
				if err != nil {
					t.Fatal(err)
				}
				savePause(t, s, store.SessionPause{LivingSession: owner, Snapshot: raw, Sleeping: true, CommandAt: time.Now().Add(-time.Minute)})
			} else {
				s.snap.Sessions = append(s.snap.Sessions, parent)
				if visual.Conversation != owner.Conversation {
					s.snap.Sessions = append(s.snap.Sessions, livingRow{LiveSession: host.LiveSession{Name: visual.Session, SessionID: visual.Conversation, CWD: s.dir}})
				}
			}
			finishPausedTurn(t, s, p)
			if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
				t.Fatal(err)
			}
			s.drain()
			s.publish()
			rows, err := s.srv.sessionPauses(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			saved, ok := pauseConversation(rows, p.Conversation)
			if !ok || !saved.Sleeping || saved.OpenedBy != owner.Conversation {
				t.Fatalf("the sleep lost its opening proof: %+v", saved)
			}
			raw, err := s.srv.host.JSON()
			if err != nil {
				t.Fatal(err)
			}
			raw, err = s.srv.sessionsPaused(t.Context(), raw)
			if err != nil {
				t.Fatal(err)
			}
			var card struct {
				Sessions []host.LiveSession `json:"sessions"`
			}
			if err := json.Unmarshal(raw, &card); err != nil {
				t.Fatal(err)
			}
			for _, row := range card.Sessions {
				if row.SessionID == p.Conversation && (row.Parent == nil || row.Parent.Session != visual.Session) {
					t.Fatalf("the sleeping card lost its parent: %+v", row.Parent)
				}
			}
			saved.CommandAt = time.Now().Add(-2 * time.Second)
			savePause(t, s, saved)
			before := len(s.requests)
			if err := s.srv.wakePaused(t.Context(), p.Conversation); err != nil {
				t.Fatal(err)
			}
			s.drain()
			s.publish()
			sent := s.requests[before:]
			if len(sent) != 1 || sent[0].Kind != action.SessionResume || sent[0].Project == nil {
				t.Fatalf("wake actions: %+v", sent)
			}
			resumed := sent[0].Project
			if resumed.OpenedBy != owner.Conversation || resumed.Parent != visual.Session {
				t.Fatalf("the resumed child lost its opener or visual parent: %+v; want opener %s and parent %s", resumed, owner.Conversation, visual.Session)
			}
			if scene == "sleeping parent" {
				if len(s.started) != 1 || s.started[0].Conversation != p.Conversation {
					t.Fatalf("restoring ownership woke its parent: %+v", s.started)
				}
			}
		})
	}
}
