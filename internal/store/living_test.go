package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLivingCensusAndBootClaimPG(t *testing.T) {
	s := requestStore(t)
	host, err := s.HostID(t.Context(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	live := []LivingSession{
		{Conversation: "closed", Session: "old", CWD: "/srv/lab", Agent: "claude"},
		{Conversation: "root", Session: "lead", ProjectID: 42, Project: "lab", Contour: "work", ConfigDir: "/srv/account", CWD: "/srv/lab", Agent: "claude", Transport: "stream", Pause: json.RawMessage(`{"name":"pause","next":"2099-01-01T00:00:00Z"}`)},
		{Conversation: "child", Session: "kid", Parent: "lead", ParentConversation: "root", CWD: "/srv/lab", Agent: "claude"},
	}
	keep := func(boot string, stamp time.Time, rows []LivingSession) {
		t.Helper()
		if err := s.KeepLiving(t.Context(), host, boot, stamp, rows); err != nil {
			t.Fatal(err)
		}
	}
	keep("old-boot", at, live)
	live[1].Session = "renamed"
	keep("old-boot", at.Add(time.Second), live[1:])
	// An older or repeated file must not put a deliberately closed session back.
	keep("old-boot", at, live)
	keep("new-boot", at.Add(2*time.Second), nil)
	previous, rows, claimed, err := s.ClaimRevival(t.Context(), host, "new-boot")
	if err != nil || !claimed || previous != "old-boot" || len(rows) != 2 {
		t.Fatalf("claim: %q %+v %v %v", previous, rows, claimed, err)
	}
	var root LivingSession
	for _, row := range rows {
		if row.Conversation == "root" {
			root = row
		}
		if row.Conversation == "closed" {
			t.Fatal("a closed conversation was revived")
		}
	}
	if root.Session != "renamed" || root.ProjectID != 42 || root.Project != "lab" || root.Contour != "work" || root.ConfigDir != "/srv/account" || root.Transport != "stream" || string(root.Pause) == "" || root.BootID != "old-boot" || !root.SeenAt.Equal(at.Add(time.Second)) {
		t.Fatalf("the last census lost its launch or pause: %+v", root)
	}
	_, again, claimed, err := s.ClaimRevival(t.Context(), host, "new-boot")
	if err != nil || claimed || len(again) != 0 {
		t.Fatalf("a deployment repeated recovery: %+v %v %v", again, claimed, err)
	}
	kid, err := s.LivingConversation(t.Context(), host, "child")
	if err != nil || kid == nil || kid.Parent != "lead" || kid.ParentConversation != "root" {
		t.Fatalf("manual resume lost its parent: %+v %v", kid, err)
	}
	keep("new-boot", at.Add(3*time.Second), []LivingSession{{Conversation: "root", Session: "renamed", CWD: "/srv/lab", Agent: "claude"}})
	keep("third-boot", at.Add(4*time.Second), nil)
	previous, rows, _, err = s.ClaimRevival(t.Context(), host, "third-boot")
	if err != nil || previous != "new-boot" || len(rows) != 1 || rows[0].Conversation != "root" {
		t.Fatalf("recovery used an earlier boot's census: %q %+v %v", previous, rows, err)
	}
}

func TestFirstCensusDoesNotPretendThereWasARebootPG(t *testing.T) {
	s := requestStore(t)
	host, err := s.HostID(t.Context(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.KeepLiving(t.Context(), host, "first", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	previous, rows, claimed, err := s.ClaimRevival(t.Context(), host, "first")
	if err != nil || !claimed || previous != "" || len(rows) != 0 {
		t.Fatalf("first boot: %q %+v %v %v", previous, rows, claimed, err)
	}
}
