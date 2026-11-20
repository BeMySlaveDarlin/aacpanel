package executor

import (
	"os"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
	"aacpanel/internal/stream"
)

func TestRecoveryOpenKeepsOnlyTheAbsentConversationsOpener(t *testing.T) {
	const anew = "88888888-8888-4888-8888-888888888888"
	archived := stream.SessionOwner{Name: "parent", Conversation: senderSID}
	caller := stream.SessionOwner{Name: "parent", Conversation: consoleSID}
	for _, c := range []struct {
		name, saved, conversation, caller string
		live, refused                     bool
		want                              stream.SessionOwner
	}{
		{name: "archived opener", saved: streamSID, conversation: streamSID, want: archived},
		{name: "no opening record", conversation: streamSID},
		{name: "reused child name", saved: anew, conversation: streamSID},
		{name: "no old conversation", saved: streamSID},
		{name: "old conversation is live under another name", saved: streamSID, conversation: streamSID, live: true},
		{name: "calling opener wins", saved: streamSID, conversation: streamSID, caller: consoleSID, want: caller},
		{name: "an absent caller cannot fall back to recovery", saved: streamSID, conversation: streamSID, caller: senderSID, refused: true},
	} {
		for _, entry := range []string{"Open", "Execute"} {
			t.Run(c.name+"/"+entry, func(t *testing.T) {
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				_, dir := launchDir(t, "worker")
				trustFile(t, map[string]bool{dir: true})
				procFS(t,
					fakeProc{pid: 7000, comm: "claude", ppid: 1, start: "9000", args: []string{"claude", "-n", "worker"}},
					fakeProc{pid: 5001, comm: "claude", ppid: 1, start: "5555", args: []string{"claude", "-n", "parent"}},
					fakeProc{pid: 6000, comm: "claude", ppid: 1, start: "8000", args: []string{"claude", "-n", "renamed-worker"}},
				)
				files := []fakeSession{{pid: 7000, name: "worker", start: "9000", sid: anew},
					{pid: 5001, name: "parent", start: "5555", sid: consoleSID}}
				if c.live {
					files = append(files, fakeSession{pid: 6000, name: "renamed-worker", start: "8000", sid: streamSID})
				}
				sessionFiles(t, files...)
				if c.saved != "" {
					if err := stream.KeepOpened("process-6000-8000", stream.OpenedSession{SessionOwner: archived,
						Session: "worker", SessionID: c.saved, Agent: "claude"}); err != nil {
						t.Fatal(err)
					}
				}
				log := fakeLauncher(t, launcher.Report{Session: "worker", Agent: 7000, Conversation: anew})
				e, _ := newTest(t, "")
				request := action.Request{ID: "recovery", Kind: action.SessionOpen, Target: "worker", Conversation: c.conversation,
					Project: &action.Project{Path: dir, Session: "worker", Parent: "parent", OpenedBy: c.caller}}
				if err := request.Validate(); err != nil {
					t.Fatalf("an open naming its old conversation was rejected: %v", err)
				}
				var err error
				if entry == "Open" {
					_, _, err = e.Open(t.Context(), request)
				} else {
					_, err = e.Execute(t.Context(), request)
				}
				if c.refused {
					if err == nil {
						t.Fatal("an absent caller inherited its archived rights through a new open")
					}
					if _, err := os.Stat(log); !os.IsNotExist(err) {
						t.Fatalf("the refused open called the launcher: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(log); err != nil {
					t.Fatalf("the open did not launch a new session: %v", err)
				}
				key, err := processOwnerKey(7000)
				if err != nil {
					t.Fatal(err)
				}
				if got := stream.OwnerOf(key); got != c.want {
					t.Fatalf("the recovered process belongs to %+v, meant %+v", got, c.want)
				}
				if c.want.Conversation == "" {
					if _, err := os.Stat(stream.OpenedPath(key)); !os.IsNotExist(err) {
						t.Fatalf("an open without an opener acquired an opening record: %v", err)
					}
				}
			})
		}
	}
}

func TestRecoveryOpenCannotInheritALiveCodexThreadsOpener(t *testing.T) {
	_, e, p := onCodexContour(t)
	_, name, err := open(t, e, p, `{"agent":"codex"}`)
	if err != nil {
		t.Fatal(err)
	}
	thread := e.codex.Find(name)[0]
	if err := stream.KeepOpened("process-6000-8000", stream.OpenedSession{
		SessionOwner: stream.SessionOwner{Name: "parent", Conversation: senderSID}, SessionID: thread.ID}); err != nil {
		t.Fatal(err)
	}
	trustFile(t, map[string]bool{p.Path: true})
	procFS(t, fakeProc{pid: 7000, comm: "claude", ppid: 1, start: "9000", args: []string{"claude", "-n", "worker"}})
	log := fakeLauncher(t, launcher.Report{Session: "worker", Agent: 7000})
	_, _, err = e.Open(t.Context(), action.Request{Kind: action.SessionOpen, Target: "worker", Conversation: thread.ID,
		Project: &action.Project{Path: p.Path, Session: "worker"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(log); err != nil {
		t.Fatalf("the open did not launch a Claude session: %v", err)
	}
	key, err := processOwnerKey(7000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stream.OpenedPath(key)); !os.IsNotExist(err) {
		t.Fatalf("a live Codex conversation gave a Claude process its opening record: %v", err)
	}
}
