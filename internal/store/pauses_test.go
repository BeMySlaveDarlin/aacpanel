package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPassingPauseOwnersKeepsDeadlinesAndHostScopePG(t *testing.T) {
	s := requestStore(t)
	host, err := s.HostID(t.Context(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.HostID(t.Context(), t.Name()+"-other")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	until := at.Add(time.Hour)
	before := at.Add(-time.Hour)
	p := SessionPause{LivingSession: LivingSession{Conversation: "child", Session: "worker", CWD: "/srv/proj", Agent: "claude", OpenedBy: "old", Parent: "parent", ParentConversation: "old"}, Snapshot: json.RawMessage(`{"session":"worker"}`), Sleeping: true, Until: &until, CommandAt: before}
	for _, h := range []int{host, other} {
		if err := s.PutSessionPause(t.Context(), h, p); err != nil {
			t.Fatal(err)
		}
	}
	awake := p
	awake.Conversation, awake.Session, awake.Sleeping, awake.Until = "awake", "awake-worker", false, nil
	if err := s.PutSessionPause(t.Context(), host, awake); err != nil {
		t.Fatal(err)
	}
	if err := s.PassPauseOwners(t.Context(), host, "old", "new", "parent-new", at); err != nil {
		t.Fatal(err)
	}
	if err := s.PassPauseOwners(t.Context(), host, "old", "new", "parent-new", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rows, err := s.SessionPauses(t.Context(), host)
	if err != nil || len(rows) != 2 {
		t.Fatalf("pause rows: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.OpenedBy != "new" || row.Parent != "parent-new" || row.ParentConversation != "new" {
			t.Fatalf("the pause kept its old owner: %+v", row)
		}
		if row.Conversation == "child" {
			if !row.Sleeping || row.Until == nil || !row.Until.Equal(until) || !row.CommandAt.Equal(at) {
				t.Fatalf("the transfer changed the deadline, state or replay barrier: %+v", row)
			}
		} else if row.Sleeping || row.Until != nil || !row.CommandAt.Equal(before) {
			t.Fatalf("passing an awake pause changed its turn barrier: %+v", row)
		}
	}
	rows, err = s.SessionPauses(t.Context(), other)
	if err != nil || len(rows) != 1 || rows[0].OpenedBy != "old" || rows[0].ParentConversation != "old" || !rows[0].CommandAt.Equal(before) {
		t.Fatalf("another host's child moved: %+v %v", rows, err)
	}
}
