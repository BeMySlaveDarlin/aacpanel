package executor

import (
	"os"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
	"aacpanel/internal/stream"
)

func TestResumeRestoresTheArchivedOpeningWhileItsParentSleeps(t *testing.T) {
	const child = "88888888-8888-4888-8888-888888888888"
	const stranger = "99999999-9999-4999-8999-999999999999"
	for _, proof := range []string{"matching conversation", "reused child name"} {
		t.Run(proof, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			_, dir := launchDir(t, "worker")
			trustFile(t, map[string]bool{dir: true})
			log := fakeLauncher(t, launcher.Report{Session: "worker", Agent: 7000, Conversation: child, Transport: launcher.TransportTmux})
			procFS(t, fakeProc{pid: 7000, comm: "claude", ppid: 1, start: "9000", args: []string{"claude", "-n", "worker"}})
			sessionFiles(t, fakeSession{pid: 7000, name: "worker", start: "9000", sid: child})
			saved := child
			if proof == "reused child name" {
				saved = stranger
			}
			if err := stream.KeepOpened("process-6000-8000", stream.OpenedSession{SessionOwner: stream.SessionOwner{Name: "parent", Conversation: senderSID}, Session: "worker", SessionID: saved, Agent: "claude"}); err != nil {
				t.Fatal(err)
			}
			e, _ := newTest(t, "")
			request := openWith(action.SessionResume, &action.Project{Session: "worker", Path: dir, Parent: "parent", OpenedBy: senderSID}, child)
			request.Silent = true
			_, err := e.Execute(t.Context(), request)
			if proof == "reused child name" {
				if err == nil {
					t.Fatal("a reused child name inherited another conversation's opener")
				}
				if _, err := os.Stat(log); !os.IsNotExist(err) {
					t.Fatalf("a refused ownership restoration called the launcher: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resume lost its sleeping opener: %v", err)
			}
			raw, err := os.ReadFile(log)
			if err != nil || !strings.Contains(string(raw), `"parent":"parent"`) {
				t.Fatalf("the sleeping parent's name did not reach the launcher: %s %v", raw, err)
			}
			key, err := processOwnerKey(7000)
			if err != nil {
				t.Fatal(err)
			}
			if got := stream.OwnerOf(key); got.Conversation != senderSID || got.Name != "parent" {
				t.Fatalf("the resumed child has no original ownership proof: %+v", got)
			}
			opening, err := e.Opening(t.Context(), "worker", child)
			if err != nil || opening == nil || opening.Conversation != senderSID || opening.Name != "parent" {
				t.Fatalf("the opening was not restored while its parent slept: %+v %v", opening, err)
			}
			procFS(t,
				fakeProc{pid: 7000, comm: "claude", ppid: 1, start: "9000", args: []string{"claude", "-n", "worker"}},
				fakeProc{pid: 5001, comm: "claude", ppid: 1, start: "5555", args: []string{"claude", "-n", "parent"}})
			sessionFiles(t, fakeSession{pid: 7000, name: "worker", start: "9000", sid: child}, fakeSession{pid: 5001, name: "parent", start: "5555", sid: stranger})
			if _, err := e.checkChild("worker", stranger); err == nil || !strings.Contains(err.Error(), "not opened") {
				t.Fatalf("a reused parent name acquired ownership: %v", err)
			}
			sessionFiles(t, fakeSession{pid: 7000, name: "worker", start: "9000", sid: child}, fakeSession{pid: 5001, name: "parent", start: "5555", sid: senderSID})
			if got, err := e.checkChild("worker", senderSID); err != nil || got != child {
				t.Fatalf("the awakened parent cannot close or answer its child: %s %v", got, err)
			}
			newTmuxStub(t, nil, "")
			signals := withSignals(t, e, map[int]bool{7000: true}, map[int]int{7000: 1})
			close := action.Request{ID: "close-restored", Kind: action.SessionClose, Target: "worker", Caller: senderSID}
			if _, err := e.Execute(t.Context(), close); err != nil {
				t.Fatalf("the original parent's close was refused after wake: %v", err)
			}
			if strings.Join(signals.sent, ",") != "7000:terminated" {
				t.Fatalf("the parent closed another process: %v", signals.sent)
			}
		})
	}
}

func TestBoundCloseKeepsTheChildConversationInItsOpeningRecord(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	streamStand(t, func(*stream.State) {})
	key, err := processOwnerKey(5001)
	if err != nil {
		t.Fatal(err)
	}
	owner := stream.SessionOwner{Name: "parent", Conversation: senderSID}
	if err := stream.KeepOwner(key, owner); err != nil {
		t.Fatal(err)
	}
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})
	if _, err := e.Execute(t.Context(), closeAskedBy(streamSID)); err != nil {
		t.Fatal(err)
	}
	rows, err := stream.OpenedSessions()
	if err != nil || len(rows) != 1 || rows[0].SessionID != streamSID || rows[0].SessionOwner != owner {
		t.Fatalf("closing lost the child's conversation binding: %+v %v", rows, err)
	}
}
