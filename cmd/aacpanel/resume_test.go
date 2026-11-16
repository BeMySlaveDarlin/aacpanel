package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/chat"
)

func TestResumeBeforeTheMinuteRollupUsesTheCollectorArchivePG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	fillMap(t, srv, root)
	dir := filepath.Join(root, "aacpanel")
	const conversation = "15000000-0000-4000-8000-000000000091"
	agent := startAgent(t, map[string]any{"ok": true, "archive": chat.ArchivePage{
		Rows: []chat.ArchiveRow{{SessionID: conversation, Name: "aacpanel", CWD: dir}}, Total: 1,
	}})
	srv.chat = chat.New(agent.path)
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "resumed"})
	srv.exec, srv.auth = client, &auth.Service{}

	w := post(t, srv, `{"kind":"session.resume","target":"aacpanel","params":{"session":"`+conversation+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("resume: %d %s", w.Code, w.Body.String())
	}
	got := <-fake.got
	if got.Resume != conversation || got.Project == nil || got.Project.Path != dir {
		t.Errorf("the conversation did not reach the executor in its directory: %+v", got)
	}
	asked := <-agent.got
	if asked.Archive == nil || asked.Archive.Limit != 1 || len(asked.Archive.Conversations) != 1 || asked.Archive.Conversations[0] != conversation {
		t.Errorf("the archive was not asked for this conversation alone: %+v", asked.Archive)
	}
}

func TestResumeRefusesAnArchiveWithoutTheConversationPG(t *testing.T) {
	const conversation = "15000000-0000-4000-8000-000000000092"
	for _, c := range []struct {
		name string
		rows []chat.ArchiveRow
	}{
		{"missing", []chat.ArchiveRow{}},
		{"another conversation", []chat.ArchiveRow{{SessionID: conversation + "-other", CWD: "/srv/project"}}},
		{"no directory", []chat.ArchiveRow{{SessionID: conversation}}},
		{"a codex thread", []chat.ArchiveRow{{SessionID: conversation, Agent: "codex", CWD: "/srv/project"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := hostServer(t, `{"at":1}`)
			agent := startAgent(t, map[string]any{"ok": true, "archive": chat.ArchivePage{Rows: c.rows}})
			srv.chat = chat.New(agent.path)
			_, _, _, _, err := srv.resumeTarget(httptest.NewRequest(http.MethodPost, "/api/actions/run", nil), "demo", map[string]any{"session": conversation})
			if err == nil || !strings.Contains(err.Error(), "is not in the archive of this machine") {
				t.Fatalf("a conversation absent from the archive must be refused: %v", err)
			}
		})
	}
}
