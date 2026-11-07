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
	passed, err := PassOwners("old", owner)
	if err != nil || passed == nil || passed.From != "old" || passed.Name != owner.Name || passed.Conversation != owner.Conversation {
		t.Fatalf("the ownership result was not returned: %+v %v", passed, err)
	}
	if again, err := PassOwners(owner.Conversation, owner); err != nil || again != nil {
		t.Fatalf("a continued conversation emitted a transfer: %+v %v", again, err)
	}
	records, err := OpenedSessions()
	child.SessionOwner, child.Key = owner, "child"
	if err != nil || len(records) != 1 || records[0] != child || OwnerOf("child") != owner {
		t.Fatalf("passing ownership lost the child: %+v, %v", records, err)
	}
}
