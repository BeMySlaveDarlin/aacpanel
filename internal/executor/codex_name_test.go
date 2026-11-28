package executor

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/codex/codextest"
	"aacpanel/internal/stream"
)

func TestDaemonCodexThreadsTakeTheFirstFreeProjectName(t *testing.T) {
	for _, helper := range []bool{false, true} {
		t.Run(fmt.Sprintf("helper=%t", helper), func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			procFS(t, fakeProc{pid: 7001, comm: "claude", args: []string{"claude", "-n", "aacpanel"}, start: "71"})
			sessionFiles(t, fakeSession{pid: 7001, name: "aacpanel", start: "71", sid: senderSID})
			p.Session = "aacpanel"
			if helper {
				p.OpenedBy, p.Parent = senderSID, "aacpanel"
			}
			for _, title := range []string{"aacpanel-2", "aacpanel-3"} {
				_, address, err := open(t, e, p, `{"agent":"codex"}`)
				if err != nil {
					t.Fatal(err)
				}
				found := e.codex.Find(title)
				if len(found) != 1 || found[0].Name != address {
					t.Fatalf("%s is not the new thread %s: %+v", title, address, found)
				}
				calls := srv.Calls("thread/name/set")
				if got := paramsOf(t, calls[len(calls)-1])["name"]; got != title {
					t.Fatalf("thread title = %v, want %s", got, title)
				}
			}
			p.Session = "review"
			for _, title := range []string{"review", "review-2"} {
				if _, _, err := open(t, e, p, `{"agent":"codex"}`); err != nil {
					t.Fatal(err)
				}
				if len(e.codex.Find(title)) != 1 {
					t.Fatalf("the explicit session name %s was ignored", title)
				}
			}
			if _, err := e.Execute(t.Context(), action.Request{Kind: action.SessionClose, Target: "review"}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := open(t, e, p, `{"agent":"codex"}`); err != nil {
				t.Fatal(err)
			}
			calls := srv.Calls("thread/name/set")
			if got := paramsOf(t, calls[len(calls)-1])["name"]; got != "review" {
				t.Fatalf("the closed session did not free its name: %v", got)
			}
		})
	}
}

func TestCodexLettersAndChildCloseAcceptTitleAndAddress(t *testing.T) {
	for _, target := range []string{"review", codexName} {
		t.Run(target, func(t *testing.T) {
			srv, e := onCodex(t, nil)
			procFS(t, fakeProc{pid: 7001, comm: "claude", args: []string{"claude"}, start: "71"})
			sessionFiles(t, fakeSession{pid: 7001, name: "lead", start: "71", sid: senderSID, socket: senderSocket})
			th := e.codex.Find(codexName)[0]
			if err := th.Link.Rename(t.Context(), th.ID, "review"); err != nil {
				t.Fatal(err)
			}
			if err := stream.KeepOpened(th.ID, stream.OpenedSession{
				SessionOwner: stream.SessionOwner{Name: "lead", Conversation: senderSID}, SessionID: th.ID}); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Execute(t.Context(), letterReq(target, "check the names")); err != nil {
				t.Fatal(err)
			}
			calls := srv.Calls("turn/start")
			if len(calls) != 1 || paramsOf(t, calls[0])["threadId"] != codexThread {
				t.Fatalf("the letter reached another thread: %s", calls)
			}
			if _, err := e.Execute(t.Context(), action.Request{Kind: action.SessionClose, Target: target, Caller: senderSID}); err != nil {
				t.Fatal(err)
			}
			if srv.Subscribed(th.ID) {
				t.Fatal("the child close did not leave the thread")
			}
		})
	}
}

func TestDuplicateCodexTitlesAreRefusedAndLeftAlone(t *testing.T) {
	const second = "019a2000-0000-7000-8000-000000000002"
	_, e := onCodex(t, func(s *codextest.Server) {
		s.Add(codextest.Thread{ID: second, CWD: "/srv/proj", Model: "gpt-test"})
	})
	for _, id := range []string{codexThread, second} {
		waitFor(t, "the second thread", func() bool { _, found := e.codex.Thread(id); return found })
		th, _ := e.codex.Thread(id)
		if err := th.Link.Rename(t.Context(), id, "review"); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []action.Kind{action.SessionLetter, action.SessionClose, action.SessionAnswer} {
		if _, err := e.Execute(t.Context(), action.Request{Kind: kind, Target: "review"}); !errors.Is(err, action.ErrSessionAmbiguous) || !strings.Contains(err.Error(), codexName) ||
			!strings.Contains(err.Error(), codex.SessionName(second)) {
			t.Fatalf("ambiguous %s: %v", kind, err)
		}
	}
	if len(e.codex.Find("review")) != 2 {
		t.Fatal("existing threads were renamed or closed")
	}
}

func TestCodexChildAnswerAcceptsTitleAndAddress(t *testing.T) {
	for _, target := range []string{"review", codexName} {
		t.Run(target, func(t *testing.T) {
			srv, e := onCodex(t, func(s *codextest.Server) {
				s.Ask(codexThread, "item/tool/requestUserInput", map[string]any{
					"turnId": "turn-one", "itemId": "question-one", "isBlocking": true,
					"questions": []any{map[string]any{"id": "choice", "header": "Choice", "question": "Which path?", "isOther": true}},
				})
			})
			procFS(t, fakeProc{pid: 7001, comm: "claude", args: []string{"claude"}, start: "71"})
			sessionFiles(t, fakeSession{pid: 7001, name: "lead", start: "71", sid: senderSID})
			th := e.codex.Find(codexName)[0]
			if err := th.Link.Rename(t.Context(), th.ID, "review"); err != nil {
				t.Fatal(err)
			}
			if err := stream.KeepOpened(th.ID, stream.OpenedSession{
				SessionOwner: stream.SessionOwner{Name: "lead", Conversation: senderSID}, SessionID: th.ID}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the child question", func() bool { return len(th.Link.Pending(th.ID)) == 1 })
			_, err := e.Execute(t.Context(), action.Request{Kind: action.SessionAnswer, Target: target, Caller: senderSID,
				Answer: &action.Answer{AskID: "question-one", Picks: [][]int{{}}, Texts: []string{"the short path"}}})
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the child answer", func() bool { return len(srv.Answers()) == 1 })
			if got := string(srv.Answers()[0].Result); !strings.Contains(got, "the short path") {
				t.Fatalf("the child received %s", got)
			}
		})
	}
}

func TestArchivedCodexResumeTakesAFreeNameAndLeavesLiveTitlesAlone(t *testing.T) {
	const past = "019a2000-0000-7000-8000-000000000002"
	srv, e, p := onCodexContour(t)
	procFS(t, fakeProc{pid: 7001, comm: "claude", args: []string{"claude", "-n", "shop"}})
	srv.Add(codextest.Thread{ID: past, CWD: p.Path, Model: "gpt-test", Status: "notLoaded"})
	r := action.Request{Kind: action.SessionResume, Target: "shop", Resume: past, Agent: action.ResumeCodex, Contour: "acme"}
	if _, _, err := e.Open(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if found := e.codex.Find("shop-2"); len(found) != 1 || found[0].ID != past {
		t.Fatalf("the archive resume reused a live name: %+v", found)
	}
	if _, _, err := e.Open(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if calls := srv.Calls("thread/name/set"); len(calls) != 1 {
		t.Fatalf("resuming a live thread renamed it: %s", calls)
	}
}
