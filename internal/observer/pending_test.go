package observer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPendingReadsTheKeepersOwnRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "observers")
	owner := Owner{Conversation: "conversation", Session: "lab"}
	id := "0123456789abcdef"
	pending := func(want bool) {
		t.Helper()
		got, err := Pending(dir, owner.Conversation)
		if err != nil || got != want {
			t.Fatalf("pending %v, want %v: %v", got, want, err)
		}
	}
	pending(false)
	path := filepath.Join(dir, id)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	rec := Record{ID: id, Owner: owner, Spec: Spec{Name: "build", Command: "make", Dir: "/srv/proj", Wake: AtExit}}
	if err := writeRecord(path, rec); err != nil {
		t.Fatal(err)
	}
	pending(true)
	if got, err := Pending(dir, "another"); err != nil || got {
		t.Fatalf("another conversation: %v, %v", got, err)
	}
	if err := writeJSON(filepath.Join(path, endFile), Ending{At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	pending(false)
	if err := os.MkdirAll(alarmDir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	// An overdue ring is still owed; only delivery removes the alarm.
	if err := writeAlarm(alarmDir(dir), Alarm{ID: id, Owner: owner, Name: "later", Next: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	pending(true)
	if got, err := Pending(dir, "another"); err != nil || got {
		t.Fatalf("another conversation: %v, %v", got, err)
	}
}

func TestPendingDoesNotTurnUnreadableSourcesIntoEmptyOnes(t *testing.T) {
	for _, file := range []string{"observers", "alarms", "observers/0123456789abcdef/observer.json", "alarms/0123456789abcdef.json"} {
		t.Run(file, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, file)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("not JSON"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Pending(filepath.Join(root, "observers"), "conversation"); err == nil {
				t.Fatal("an unreadable source was read as empty")
			}
		})
	}
}
