package stream

import "testing"

func TestPassingOwnershipPreservesTheChildsIdentity(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	child := OpenedSession{SessionOwner: SessionOwner{Name: "lead", Conversation: "old"},
		Session: "worker", SessionID: "child", Agent: "codex", Profile: "work"}
	if err := KeepOpened("child", child); err != nil {
		t.Fatal(err)
	}
	owner := SessionOwner{Name: "lead", Conversation: "new"}
	if err := PassOwners("old", owner); err != nil {
		t.Fatal(err)
	}
	records, err := OpenedSessions()
	child.SessionOwner, child.Key = owner, "child"
	if err != nil || len(records) != 1 || records[0] != child || OwnerOf("child") != owner {
		t.Fatalf("passing ownership lost the child: %+v, %v", records, err)
	}
}
