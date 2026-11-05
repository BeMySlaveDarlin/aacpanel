package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/docker"
	"aacpanel/internal/notify"
)

const questionParentID = "11111111-1111-4111-8111-111111111111"
const questionForeignID = "22222222-2222-4222-8222-222222222222"

type parentQuestionExecutor struct {
	mu       sync.Mutex
	opening  *action.Opening
	requests []action.Request
}

func (e *parentQuestionExecutor) Execute(_ context.Context, request action.Request) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = append(e.requests, request)
	return "letter delivered", nil
}

func (e *parentQuestionExecutor) Opening(context.Context, string, string) (*action.Opening, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.opening == nil {
		return nil, nil
	}
	copy := *e.opening
	return &copy, nil
}

func (e *parentQuestionExecutor) sent() []action.Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]action.Request{}, e.requests...)
}

func parentQuestionClient(t *testing.T, s *Server, opening *action.Opening) *parentQuestionExecutor {
	t.Helper()
	e := &parentQuestionExecutor{opening: opening}
	server := action.NewServer(socketPath(t, "parent.sock"), e, time.Second)
	ln, err := server.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); server.Run(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	s.exec = action.NewClient(ln.Addr().String(), time.Second)
	return e
}

func TestParentQuestionsAreMailedOnceAndWaitAcrossServiceRestartsPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "parent-question-clock")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM parent_questions WHERE conversation=$1", requestConversation)
	})
	e := parentQuestionClient(t, p.srv, &action.Opening{Name: "parent", Conversation: questionParentID, Letters: true})
	child := notify.Session{ID: requestConversation, Name: "sentinel", CWD: "/srv/unmapped/child", ConfigDir: "/account/unmapped"}
	question := parentQuestion{Key: "text:clock", Text: "Publish the change?", Answers: []string{"Publish", "Wait"}}
	now := time.Unix(2_000_000_000, 0).UTC()
	if !p.srv.deferParentQuestion(t.Context(), child, question, now) {
		t.Fatal("the question was pushed before the parent had time to answer")
	}
	restarted := &Server{db: p.db, exec: p.srv.exec, host: p.srv.host}
	if !restarted.deferParentQuestion(t.Context(), child, question, now.Add(4*time.Minute)) {
		t.Fatal("a service restart forgot the parent's remaining time")
	}
	if restarted.deferParentQuestion(t.Context(), child, question, now.Add(5*time.Minute)) {
		t.Fatal("the question was still held after the deadline")
	}
	sent := e.sent()
	if len(sent) != 1 || sent[0].Kind != action.SessionLetter || sent[0].ParentQuestion != child.ID ||
		sent[0].Conversation != questionParentID || sent[0].From != "" {
		t.Fatalf("the repeated question was mailed more than once or as an agent: %+v", sent)
	}
	for _, text := range []string{"sentinel", "Publish the change?", "Publish", "send_to_session", "5 minutes", "do not ask them yourself"} {
		if !strings.Contains(sent[0].Text, text) {
			t.Errorf("the parent's letter lacks %q: %s", text, sent[0].Text)
		}
	}
}

func TestParentQuestionPolicyDefaultsAndProjectOverridesPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "parent-question-policy")
	pool, _ := p.db.Pool()
	var profile, group, project int
	if err := pool.QueryRow(t.Context(), `INSERT INTO profiles (name,config_dir,prefix,launch)
 VALUES ('parent-policy','/account/parent-policy','/srv/parent-policy','{"parentQuestions":false,"parentQuestionWait":2}') RETURNING id`).Scan(&profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM profile_projects WHERE id=$1", project)
		pool.Exec(context.Background(), "DELETE FROM profile_groups WHERE id=$1", group)
		pool.Exec(context.Background(), "DELETE FROM profiles WHERE id=$1", profile)
	})
	if err := pool.QueryRow(t.Context(), `INSERT INTO profile_groups (profile_id,name) VALUES ($1,'children') RETURNING id`, profile).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `INSERT INTO profile_projects (group_id,name,path,session_name,launch)
 VALUES ($1,'child','/srv/parent-policy/child','child','{"parentQuestions":true,"parentQuestionWait":3}') RETURNING id`, group).Scan(&project); err != nil {
		t.Fatal(err)
	}
	child := notify.Session{ConfigDir: "/account/parent-policy", CWD: "/srv/parent-policy/outside"}
	if on, wait := p.srv.parentQuestionPolicy(t.Context(), child); on || wait != 2*time.Minute {
		t.Fatalf("the contour policy was not used: %v %s", on, wait)
	}
	child.CWD = "/srv/parent-policy/child"
	if on, wait := p.srv.parentQuestionPolicy(t.Context(), child); !on || wait != 3*time.Minute {
		t.Fatalf("the project did not override its contour: %v %s", on, wait)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE profile_projects SET launch='{"parentQuestions":false}' WHERE id=$1`, project); err != nil {
		t.Fatal(err)
	}
	if on, wait := p.srv.parentQuestionPolicy(t.Context(), child); on || wait != 2*time.Minute {
		t.Fatalf("false was lost or the delay did not inherit: %v %s", on, wait)
	}
	e := parentQuestionClient(t, p.srv, &action.Opening{Name: "parent", Conversation: questionParentID, Letters: true})
	child.ID, child.Name = requestConversation, "sentinel"
	if p.srv.deferParentQuestion(t.Context(), child, parentQuestion{Key: "disabled", Text: "Question?"}, time.Now()) || len(e.sent()) != 0 {
		t.Fatal("a disabled project delayed its push or mailed the parent")
	}
}

func TestProseQuestionPushWaitsAndAQuestionClearedBeforeItsDeadlineStaysSilentPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "parent-question-push")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM parent_questions WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
	})
	e := parentQuestionClient(t, p.srv, &action.Opening{Name: "parent", Conversation: questionParentID, Letters: true})
	p.srv.push = notify.New(nil, "")
	p.srv.host = hostWith(t, `{"sessions":[{"sessionId":"`+requestConversation+`","session":"sentinel","status":"idle"}]}`)
	scan := func() {
		p.srv.scanTextQuestions(t.Context(), func(string, string, chat.Item) { t.Error("a stored verdict was classified again") })
	}
	assertPushed := func(message string, want bool) {
		t.Helper()
		var got bool
		if err := pool.QueryRow(t.Context(), "SELECT pushed FROM text_questions WHERE conversation=$1 AND message=$2", requestConversation, message).Scan(&got); err != nil || got != want {
			t.Fatalf("push of %s: %v, want %v (%v)", message, got, want, err)
		}
	}
	for _, message := range []string{"expires", "cleared", "no-parent", "no-mailbox"} {
		if _, err := p.db.ClaimQuestion(t.Context(), requestConversation, message); err != nil {
			t.Fatal(err)
		}
		what := "Publish?"
		if message == "cleared" {
			what = ""
		}
		if err := p.db.SetQuestionVerdict(t.Context(), requestConversation, message, "yes", what, []string{"Yes", "No"}); err != nil {
			t.Fatal(err)
		}
		card := chat.Item{Role: "question", BriefID: message, Text: "Publish?", Title: "Asks in text"}
		active := map[string]any{"ok": true, "waiting": map[string][]chat.Item{requestConversation: {card}}}
		empty := map[string]any{"ok": true, "waiting": map[string][]chat.Item{requestConversation: {}}}
		agent := startAgentSeq(t, active, empty)
		if message == "expires" {
			agent = startAgent(t, active)
		}
		p.srv.chat = chat.New(agent.path)
		e.mu.Lock()
		if message == "no-parent" {
			e.opening = nil
		} else if message == "no-mailbox" {
			e.opening = &action.Opening{Name: "parent", Conversation: questionParentID, Letters: false}
		}
		e.mu.Unlock()
		scan()
		if message == "no-parent" || message == "no-mailbox" {
			assertPushed(message, true)
			continue
		}
		assertPushed(message, false)
		var rows []map[string]json.RawMessage
		_ = json.Unmarshal([]byte(`[{"sessionId":"`+requestConversation+`","status":"idle"}]`), &rows)
		if visible := p.srv.questionCards(t.Context(), rows, map[string][]chat.Item{requestConversation: {card}}); len(visible[requestConversation]) != 1 {
			t.Fatalf("the delayed push hid the question from the panel: %v", visible)
		}
		if _, err := pool.Exec(t.Context(), "UPDATE parent_questions SET due=now()-interval '1 second' WHERE conversation=$1 AND question=$2", requestConversation, "text:"+message); err != nil {
			t.Fatal(err)
		}
		scan()
		assertPushed(message, message == "expires")
	}
	if len(e.sent()) != 2 {
		t.Fatalf("questions without a reachable parent were mailed: %+v", e.sent())
	}
	for _, letter := range e.sent() {
		if !strings.Contains(letter.Text, "Publish?") {
			t.Fatalf("a parent got no question when the verdict had no summary: %s", letter.Text)
		}
	}
}

func TestAParentsOrdinaryLetterSettlesOnlyItsChildsCurrentProseQuestionPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "parent-question-reply")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
	})
	e := parentQuestionClient(t, p.srv, &action.Opening{Name: "parent", Conversation: questionParentID, Letters: true})
	card := chat.Item{Role: "question", BriefID: "parent-reply", Text: "Use this path?", Title: "Asks in text"}
	agent := startAgent(t, map[string]any{"ok": true, "waiting": map[string][]chat.Item{requestConversation: {card}}})
	p.srv.chat = chat.New(agent.path)
	if _, err := p.db.ClaimQuestion(t.Context(), requestConversation, card.BriefID); err != nil {
		t.Fatal(err)
	}
	if err := p.db.SetQuestionVerdict(t.Context(), requestConversation, card.BriefID, "yes", card.Text, nil); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`[{"sessionId":"`+requestConversation+`","status":"idle"}]`), &rows)
	for _, from := range []string{questionForeignID, questionParentID} {
		response := post(t, p.srv, `{"kind":"session.letter","target":"sentinel","params":{"from":"`+from+`","text":"Use the saved plan."}}`)
		if response.Code != http.StatusOK {
			t.Fatalf("the ordinary letter failed: %d %s", response.Code, response.Body.String())
		}
		visible := p.srv.questionCards(t.Context(), rows, map[string][]chat.Item{requestConversation: {card}})
		if (len(visible[requestConversation]) == 0) != (from == questionParentID) {
			t.Fatalf("a sibling settled the question, or the parent did not: %v", visible)
		}
	}
	sent := e.sent()
	if len(sent) != 2 || sent[0].Caller != "" || sent[1].Caller != questionParentID || sent[1].Conversation != requestConversation {
		t.Fatalf("the parent's reply was not verified on the host: %+v", sent)
	}
	if fresh, err := p.db.PushQuestion(t.Context(), requestConversation, card.BriefID); err != nil || fresh {
		t.Fatalf("a stale scan pushed a prose question already answered by its parent: %v %v", fresh, err)
	}
}

func TestAnAnsweredChildAskCannotBePushedFromAStaleSnapshotPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "parent-ask-answer-race")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM parent_questions WHERE conversation=$1", requestConversation)
	})
	_, _, err := p.db.ClaimParentQuestion(t.Context(), requestConversation, "ask:answered", questionParentID, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.db.ParentQuestionDelivered(t.Context(), requestConversation, "ask:answered"); err != nil {
		t.Fatal(err)
	}
	executor := parentQuestionClient(t, p.srv, &action.Opening{Name: "parent", Conversation: questionParentID, Letters: true})
	response := post(t, p.srv, `{"kind":"session.answer","target":"sentinel","params":{"caller":"`+questionParentID+`","ask":"answered","picks":[[1]]}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("the child's answer failed: %d %s", response.Code, response.Body.String())
	}
	if sent := executor.sent(); len(sent) != 1 || sent[0].Kind != action.SessionAnswer || sent[0].Caller != questionParentID || sent[0].Conversation != questionParentID {
		t.Fatalf("the service lost the child answer's ownership gate: %+v", sent)
	}
	agent := startAgent(t, map[string]any{"ok": true, "state": map[string]any{"ask": chat.Ask{
		ToolUseID: "answered", Questions: []chat.AskQuestion{{Text: "Choose", Options: []chat.AskOption{{Label: "Yes"}}}},
	}}})
	p.srv.chat = chat.New(agent.path)
	cur := notify.World{Now: time.Now(), Sessions: []notify.Session{{ID: requestConversation, Name: "sentinel", Ask: &notify.Ask{ID: "answered", At: "old"}}}}
	report := notify.Report{Raise: []notify.Event{{Key: "ask:" + requestConversation + ":old", Session: "sentinel"}}}
	p.srv.deferAskQuestions(t.Context(), cur, &report)
	if len(report.Raise) != 0 || len(report.Hold) != 1 {
		t.Fatalf("the stale snapshot pushed an answered Ask: %+v", report)
	}
	if err := p.db.AnswerParentQuestion(t.Context(), requestConversation, "ask:unknown", ""); err != nil {
		t.Fatal(err)
	}
	if allowed, err := p.db.ParentQuestionMayPush(t.Context(), requestConversation, "ask:unknown", false); err != nil || !allowed {
		t.Fatalf("the ordinary push path of an undelegated question changed: %v %v", allowed, err)
	}
	if _, _, err := p.db.ClaimParentQuestion(t.Context(), requestConversation, "ask:external", questionParentID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	agent = startAgent(t, map[string]any{"ok": true, "state": map[string]any{}})
	p.srv.chat = chat.New(agent.path)
	cur.Sessions[0].Ask.ID = "external"
	report = notify.Report{Raise: []notify.Event{{Key: "ask:" + requestConversation + ":old", Session: "sentinel"}}}
	p.srv.deferAskQuestions(t.Context(), cur, &report)
	if len(report.Raise) != 0 {
		t.Fatalf("the fresh feed cleared the question but the stale snapshot pushed it: %+v", report)
	}
}

func TestAskQuestionsWaitButPermissionsAndRequestsGoStraightToThePersonPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "parent-ask-push")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM parent_questions WHERE conversation=$1", requestConversation)
	})
	e := parentQuestionClient(t, p.srv, &action.Opening{Name: "parent", Conversation: questionParentID, Letters: true})
	agent := startAgent(t, map[string]any{"ok": true, "state": map[string]any{"ask": chat.Ask{
		ToolUseID: "ask-round", Questions: []chat.AskQuestion{{Header: "Next", Text: "Choose a path", Options: []chat.AskOption{{Label: "Saved", Description: "Use the plan"}}}},
	}}})
	p.srv.chat = chat.New(agent.path)
	child := notify.Session{ID: requestConversation, Name: "sentinel", Ask: &notify.Ask{Text: "Choose a path", At: "now"}}
	cur := notify.World{Now: time.Now(), Sessions: []notify.Session{child}}
	makeReport := func() notify.Report {
		return notify.Report{Raise: []notify.Event{
			{Key: "ask:" + requestConversation + ":now", Session: "sentinel"}, {Key: "wait:permission", Session: "sentinel"}, {Key: "request:hands", Session: "sentinel"},
		}}
	}
	report := makeReport()
	p.srv.deferAskQuestions(t.Context(), cur, &report)
	if len(report.Raise) != 2 || report.Raise[0].Key != "wait:permission" || report.Raise[1].Key != "request:hands" || len(report.Hold) != 1 {
		t.Fatalf("a question was pushed early, or permissions and requests were delayed: %+v", report)
	}
	if len(e.sent()) != 1 || !strings.Contains(e.sent()[0].Text, "session_answer") || !strings.Contains(e.sent()[0].Text, "Saved") {
		t.Fatalf("the question did not reach its parent with answer instructions: %+v", e.sent())
	}
	cur.Now = cur.Now.Add(5 * time.Minute)
	report = makeReport()
	p.srv.deferAskQuestions(t.Context(), cur, &report)
	if len(report.Raise) != 3 || len(e.sent()) != 1 {
		t.Fatalf("an overdue AskUserQuestion was held or mailed twice: %+v", report)
	}
}

func TestTheWatcherPushesOnlyAnAskStillStandingAfterTheParentDeadlinePG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "parent-watcher")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM parent_questions WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM push_state WHERE key LIKE $1", "ask:"+requestConversation+":%")
	})
	parentQuestionClient(t, p.srv, &action.Opening{Name: "parent", Conversation: questionParentID, Letters: true})
	p.srv.push = notify.New(nil, "")
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			t.Errorf("unexpected engine request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(engine.Close)
	p.srv.docker = docker.New(engine.URL)
	w := newWatcher(p.srv, p.db.PushJournal())
	setQuestion := func(id string, active bool) {
		t.Helper()
		row := map[string]any{"sessionId": requestConversation, "session": "sentinel", "status": "idle"}
		if active {
			row["status"], row["ask"] = "waiting", map[string]any{"at": id, "text": "Choose a path", "count": 1}
		}
		body, _ := json.Marshal(map[string]any{"at": time.Now().Unix(), "sessionsAt": time.Now().Unix(), "sessions": []any{row}})
		p.srv.host = hostWith(t, string(body))
		agent := startAgent(t, map[string]any{"ok": true, "briefs": []any{}, "state": map[string]any{
			"ask": chat.Ask{ToolUseID: id, Questions: []chat.AskQuestion{{Text: "Choose a path", Options: []chat.AskOption{{Label: "Saved"}}}}},
		}})
		p.srv.chat = chat.New(agent.path)
	}
	assertRaised := func(id string, want bool) {
		t.Helper()
		var raised bool
		if err := pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM push_state WHERE key=$1)", "ask:"+requestConversation+":"+id).Scan(&raised); err != nil || raised != want {
			t.Fatalf("announced Ask %s: %v, want %v (%v)", id, raised, want, err)
		}
	}
	setQuestion("cleared-ask", true)
	w.once(t.Context())
	assertRaised("cleared-ask", false)
	setQuestion("cleared-ask", false)
	if _, err := pool.Exec(t.Context(), "UPDATE parent_questions SET due=now()-interval '1 second' WHERE conversation=$1", requestConversation); err != nil {
		t.Fatal(err)
	}
	w.once(t.Context())
	assertRaised("cleared-ask", false)
	setQuestion("overdue-ask", true)
	w.once(t.Context())
	assertRaised("overdue-ask", false)
	if _, err := pool.Exec(t.Context(), "UPDATE parent_questions SET due=now()-interval '1 second' WHERE conversation=$1", requestConversation); err != nil {
		t.Fatal(err)
	}
	w.once(t.Context())
	assertRaised("overdue-ask", true)
}

// parentQuestionOf reads the mark of a question held at the parent off the
// row of a conversation, empty when it has none.
func parentQuestionOf(t *testing.T, body []byte, conversation string) string {
	t.Helper()
	var snap struct {
		Sessions []struct {
			SessionID string          `json:"sessionId"`
			Held      json.RawMessage `json:"parentQuestion"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("the snapshot %s: %v", body, err)
	}
	for _, s := range snap.Sessions {
		if s.SessionID == conversation {
			return string(s.Held)
		}
	}
	t.Fatalf("the snapshot lost %s: %s", conversation, body)
	return ""
}

// A child whose question waits at its parent says so on its row, with the
// parent's name as it lives now and the moment the question goes to the
// person; a question
// not mailed yet, answered, due already or no longer asked marks nothing.
func TestAQuestionHeldAtTheParentMarksTheChildPG(t *testing.T) {
	const child, other = "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"
	p := newPollProbe(t, t.Context(), "parent-question-row")
	pool, _ := p.db.Pool()
	forget := func() {
		pool.Exec(context.Background(), "DELETE FROM parent_questions WHERE conversation = ANY($1)", []string{child, other})
	}
	forget()
	t.Cleanup(forget)
	p.srv.host = hostWith(t, `{"at":1,"sessions":[`+
		`{"session":"lead","sessionId":"`+questionParentID+`","cwd":"/srv/lead"},`+
		`{"session":"worker","sessionId":"`+child+`","cwd":"/srv/lead/w","parent":{"session":"lead-before-rename"},"ask":{"toolUseId":"toolu_held","text":"Ship?"}},`+
		`{"session":"helper","sessionId":"`+other+`","cwd":"/srv/lead/h","parent":{"session":"lead"},"ask":{"toolUseId":"toolu_other","text":"Ship?"}}]}`)
	due := time.Now().Add(3 * time.Minute).UTC().Truncate(time.Second)
	claim := func(conversation, question string, due time.Time, delivered, answered bool) {
		t.Helper()
		forget()
		if _, err := pool.Exec(t.Context(), `INSERT INTO parent_questions (conversation,question,parent,due,delivered,answered)
 VALUES ($1,$2,$3,$4,$5,$6)`, conversation, question, questionParentID, due, delivered, answered); err != nil {
			t.Fatal(err)
		}
	}
	held := func() (string, string) {
		t.Helper()
		raw, err := p.srv.hostSnapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return parentQuestionOf(t, raw, child), parentQuestionOf(t, raw, other)
	}

	claim(child, "ask:toolu_held", due, true, false)
	want := `{"parent":"lead","due":"` + due.Format(time.RFC3339) + `"}`
	if mark, rest := held(); mark != want || rest != "" {
		t.Fatalf("the held question marks %q and its neighbour %q, want %s", mark, rest, want)
	}
	for _, c := range []struct {
		why                 string
		question            string
		due                 time.Time
		delivered, answered bool
	}{
		{"not mailed", "ask:toolu_held", due, false, false},
		{"answered", "ask:toolu_held", due, true, true},
		{"due already", "ask:toolu_held", time.Now().Add(-time.Second), true, false},
		{"no longer asked", "ask:toolu_gone", due, true, false},
	} {
		claim(child, c.question, c.due, c.delivered, c.answered)
		if mark, _ := held(); mark != "" {
			t.Errorf("a question %s marks the child: %s", c.why, mark)
		}
	}

	// A question in prose is named by the waiting list; a parent that is not
	// live is named as the row names it.
	claim(child, "text:brief-1", due, true, false)
	payload := []byte(`{"sessions":[{"sessionId":"` + child + `","parent":{"session":"gone-lead"},` +
		`"waiting":[{"kind":"question","id":"brief-1","title":"Ship?","answers":[]}]}]}`)
	p.srv.host = hostWith(t, `{"at":1,"sessions":[]}`)
	got := parentQuestionOf(t, p.srv.sessionsAtParent(t.Context(), payload, time.Now()), child)
	if want := `{"parent":"gone-lead","due":"` + due.Format(time.RFC3339) + `"}`; got != want {
		t.Fatalf("the question in prose marks %q, want %s", got, want)
	}
}
