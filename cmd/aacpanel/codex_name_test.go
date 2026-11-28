package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/chat"
	"aacpanel/internal/store"
)

func TestChatTargetFindsCodexTitleAndRefusesAmbiguity(t *testing.T) {
	for _, extra := range []string{"", `,{"session":"review","sessionId":"claude-one"}`} {
		s := &Server{host: hostWith(t, `{"sessions":[{"session":"codex-00000001","sessionId":"thread-one","agent":"codex","title":"review"}`+extra+`]}`)}
		r := httptest.NewRequest("GET", "/api/chat?session=review", nil)
		got, err := s.chatTarget(r, "review")
		if extra == "" {
			if err != nil || got.Session != "thread-one" {
				t.Fatalf("the question lookup did not find the Codex title: %+v, %v", got, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "ambiguous") ||
			!strings.Contains(err.Error(), "codex-00000001") || !strings.Contains(err.Error(), "claude-one") {
			t.Fatalf("an ambiguous title did not list its conversations: %+v, %v", got, err)
		}
	}
}

func TestCodexArchiveResumeUsesTheMappedSessionNamePG(t *testing.T) {
	s, root := profilesServer(t)
	profileName, groupName, projectName, sessionName := "work", "services", "Shop", "shop"
	profile, err := s.db.CreateProfile(t.Context(), store.ProfileEdit{Name: &profileName, ConfigDir: &root})
	if err != nil {
		t.Fatal(err)
	}
	group, err := s.db.CreateGroup(t.Context(), profile.ID, store.GroupEdit{Name: &groupName})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.CreateProject(t.Context(), group.ID, store.ProjectEdit{Name: &projectName, Path: &root, Session: &sessionName}); err != nil {
		t.Fatal(err)
	}
	const conversation = "019a2000-0000-7000-8000-000000000001"
	agent := startAgent(t, map[string]any{"ok": true, "archive": chat.ArchivePage{Rows: []chat.ArchiveRow{
		{SessionID: conversation, Agent: "codex", CWD: root, Name: "shop-3"},
	}}})
	s.chat = chat.New(agent.path)
	client, fake := startFakeExec(t, action.Response{OK: true, Session: "codex-00000001"})
	s.exec, s.auth = client, &auth.Service{}
	w := post(t, s, `{"kind":"session.resume","target":"shop-3","params":{"agent":"codex","contour":"work","session":"`+conversation+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("archive resume: %d %s", w.Code, w.Body.String())
	}
	got := <-fake.got
	if got.Target != "shop" || got.Project != nil || got.ResumeAt != nil {
		t.Fatalf("the archive resume did not take the mapped session name: %+v", got)
	}
}
