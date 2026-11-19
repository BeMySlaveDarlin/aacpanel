package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/notify"
)

const claimPeerID = "44444444-4444-4444-8444-444444444444"
const claimOpenerID = "55555555-5555-4555-8555-555555555555"

func newClaimsServer(t *testing.T) (*Server, *parentQuestionExecutor) {
	t.Helper()
	p := newPollProbe(t, t.Context(), t.Name())
	p.srv.host = hostWith(t, `{"sessions":[{"sessionId":"`+requestConversation+`","session":"sentinel","cwd":"/srv/claim-sender"},{"sessionId":"`+claimPeerID+`","session":"claim-peer","cwd":"/srv/claim-peer","configDir":"/account/claims"},{"sessionId":"`+questionForeignID+`","session":"claim-foreign"},{"sessionId":"`+claimOpenerID+`","session":"claim-opener"}]}`)
	e := parentQuestionClient(t, p.srv, nil)
	pool, _ := p.db.Pool()
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM acts WHERE kind='claim' AND peer='claim-peer'`) })
	return p.srv, e
}

func claimPost(t *testing.T, s *Server, target string, params map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"kind": action.SessionLetter, "target": target, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	return post(t, s, string(raw))
}

func sentClaim(t *testing.T, s *Server, expects string, due *int) act.Record {
	t.Helper()
	params := map[string]any{"from": requestConversation, "text": "Ship the release\nKeep the text.", "expects": expects}
	if due != nil {
		params["due"] = *due
	}
	w := claimPost(t, s, "claim-peer", params)
	if w.Code != http.StatusOK {
		t.Fatalf("send: %d %s", w.Code, w.Body.String())
	}
	var reply struct {
		Claim act.Record `json:"claim"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Claim.ID == "" {
		t.Fatalf("claim: %s %v", w.Body.String(), err)
	}
	return reply.Claim
}

func TestClaimLettersValidateTheirSidesAndCloseAfterDeliveryPG(t *testing.T) {
	s, e := newClaimsServer(t)
	minute := 1
	row := sentClaim(t, s, "done", &minute)
	if row.Title != "Ship the release" || row.Peer != "claim-peer" || row.Due == nil {
		t.Fatalf("claim: %+v", row)
	}
	sent := e.sent()
	if len(sent) != 1 || sent[0].From != requestConversation || sent[0].Text != row.ClaimLetter("Ship the release\nKeep the text.", time.Now()) {
		t.Fatalf("letter: %+v", sent)
	}
	for _, params := range []map[string]any{
		{"from": claimPeerID, "text": "Work", "expects": "reply", "re": row.ID},
		{"from": questionForeignID, "text": "Ready", "re": row.ID},
	} {
		w := claimPost(t, s, "", params)
		if w.Code != http.StatusBadRequest && w.Code != http.StatusConflict {
			t.Fatalf("invalid reply: %d %s", w.Code, w.Body.String())
		}
	}
	w := claimPost(t, s, "claim-foreign", map[string]any{"from": claimPeerID, "text": "Ready", "re": row.ID})
	if w.Code != http.StatusConflict {
		t.Fatalf("wrong to: %d %s", w.Code, w.Body.String())
	}
	if len(e.sent()) != 1 {
		t.Fatal("invalid claim reply reached the executor")
	}
	for _, outcome := range []string{"reply", "done"} {
		w = claimPost(t, s, "", map[string]any{"from": claimPeerID, "text": "Progress", "re": row.ID, "outcome": outcome})
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", outcome, w.Code, w.Body.String())
		}
		lease, err := s.db.LockClaim(t.Context(), row.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := "open"
		if outcome == "done" {
			want = "answered"
		}
		if lease.Record.State != want {
			t.Fatalf("%s closed incorrectly: %+v", outcome, lease.Record)
		}
		lease.Close(t.Context())
	}
	sent = e.sent()
	if sent[2].Target != "sentinel" || sent[2].Text != row.Reply("done", "Progress") {
		t.Fatalf("reply letter: %+v", sent[2])
	}
	w = claimPost(t, s, "", map[string]any{"from": claimPeerID, "text": "Again", "re": row.ID})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "answered") {
		t.Fatalf("closed reply: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Acts []act.Record `json:"acts"`
	}
	w = httptest.NewRecorder()
	s.apiActs(w, httptest.NewRequest(http.MethodGet, "/api/acts?session="+claimPeerID, nil))
	if json.Unmarshal(w.Body.Bytes(), &payload) != nil || len(payload.Acts) != 1 || !payload.Acts[0].Incoming {
		t.Fatalf("incoming acts: %s", w.Body.String())
	}
}

func TestClaimDeliveryFailureLeavesNoNewClaimAndKeepsAnAnswerOpenPG(t *testing.T) {
	s, _ := newClaimsServer(t)
	good := s.exec
	failed, _ := startFakeExec(t, action.Response{OK: false, Error: "no letter input"})
	s.exec = failed
	w := claimPost(t, s, "claim-peer", map[string]any{"from": requestConversation, "text": "Work", "expects": "reply"})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("refusal: %d %s", w.Code, w.Body.String())
	}
	rows, err := s.db.OpenClaims(t.Context(), []string{"claim-peer"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("rejected claim survived: %+v %v", rows, err)
	}
	s.exec = good
	row := sentClaim(t, s, "reply", nil)
	s.exec = failed
	w = claimPost(t, s, "", map[string]any{"from": claimPeerID, "text": "Ready", "re": row.ID})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("answer refusal: %d %s", w.Code, w.Body.String())
	}
	rows, err = s.db.OpenClaims(t.Context(), []string{"claim-peer"})
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("failed answer closed: %+v %v", rows, err)
	}
}

func TestClaimSnapshotAndWithdrawalsStaySeparateFromThePersonsMovePG(t *testing.T) {
	s, e := newClaimsServer(t)
	row := sentClaim(t, s, "reply", nil)
	legacy := chainAPI(t, s, map[string]any{"conversation": requestConversation, "id": row.ID, "withdraw": true})
	if legacy.Code != http.StatusConflict {
		t.Fatalf("a request operation withdrew a claim: %d %s", legacy.Code, legacy.Body.String())
	}
	raw, _ := s.host.JSON()
	var data struct {
		Sessions []struct {
			Session string        `json:"session"`
			Claims  sessionClaims `json:"claims"`
			Waiting []waitingItem `json:"waiting"`
		} `json:"sessions"`
	}
	payload := s.sessionsWaiting(t.Context(), raw)
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatal(err)
	}
	for _, session := range data.Sessions {
		if session.Claims.Out == nil || session.Claims.In == nil {
			t.Fatalf("missing empty lists: %s", payload)
		}
		for _, item := range session.Waiting {
			if item.ID == row.ID {
				t.Fatal("a claim is the person's move")
			}
		}
		if session.Session == "sentinel" && (len(session.Claims.Out) != 1 || session.Claims.Out[0].Peer != "claim-peer") {
			t.Fatalf("outgoing snapshot: %+v", session)
		}
		if session.Session == "claim-peer" && (len(session.Claims.In) != 1 || session.Claims.In[0].From != "sentinel") {
			t.Fatalf("incoming snapshot: %+v", session)
		}
	}
	w := claimPost(t, s, "", map[string]any{"from": requestConversation, "re": row.ID, "outcome": "withdraw", "text": "No longer needed"})
	if w.Code != http.StatusOK {
		t.Fatalf("sender withdrawal: %d %s", w.Code, w.Body.String())
	}
	row = sentClaim(t, s, "done", nil)
	w = post(t, s, `{"kind":"act.withdraw","target":"sentinel","params":{"act":"`+row.ID+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("person withdrawal: %d %s", w.Code, w.Body.String())
	}
	lease, err := s.db.LockClaim(t.Context(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Record.State != "withdrawn" || lease.Record.AnsweredBy != "person" {
		t.Fatalf("person mark: %+v", lease.Record)
	}
	lease.Close(t.Context())
	sent := e.sent()
	if len(sent) != 5 || sent[3].Claim != row.ID || sent[3].Text != "The person withdrew claim "+row.ID+" from sentinel." || sent[4].Text != "The person withdrew your claim "+row.ID+" to claim-peer." {
		t.Fatalf("withdrawal letters: %+v", sent)
	}
	blocking := false
	other, err := s.db.CreateAct(t.Context(), requestConversation, "sentinel", act.Input{Kind: "do", Title: "Other", Body: act.Body{Steps: "Do it"}, Blocking: &blocking})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := s.db.Pool()
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM acts WHERE id=$1`, other.ID) })
	w = post(t, s, `{"kind":"act.withdraw","target":"sentinel","params":{"act":"`+other.ID+`"}}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("non-claim withdrawal: %d", w.Code)
	}
}

func TestClaimLadderWaitsTellsTheParentOnceAndStopsOnClosurePG(t *testing.T) {
	s, e := newClaimsServer(t)
	e.opening = &action.Opening{Name: "claim-opener", Conversation: claimOpenerID, Letters: true}
	minute := 1
	row := sentClaim(t, s, "reply", &minute)
	start := len(e.sent())
	poll := func(at time.Time, want int) {
		t.Helper()
		if err := s.pollClaims(t.Context(), at); err != nil {
			t.Fatal(err)
		}
		if got := len(e.sent()) - start; got != want {
			t.Fatalf("at %s: got %d letters, want %d: %+v", at, got, want, e.sent())
		}
	}
	poll(row.Due.Add(-time.Second), 0)
	poll(*row.Due, 1)
	poll(row.Due.Add(4*time.Minute), 1)
	poll(row.Due.Add(5*time.Minute+time.Second), 3)
	poll(row.Due.Add(5*time.Minute+time.Second), 3)
	sent := e.sent()[start:]
	if sent[0].Target != "claim-peer" || sent[1].Target != "claim-opener" || sent[2].Target != "sentinel" || !strings.Contains(sent[2].Text, "The parent claim-opener was told.") || !strings.Contains(sent[2].Text, "overdue for 5 min;") || !strings.Contains(sent[2].Text, "The person is told in 5 min.") || !strings.Contains(sent[1].Text, "due 5 min ago.") {
		t.Fatalf("ladder letters: %+v", sent)
	}
	for i, letter := range sent {
		if letter.Claim != row.ID || letter.MessageID == "" || (i > 0 && letter.MessageID == sent[i-1].MessageID) {
			t.Fatalf("rung identity: %+v", letter)
		}
	}
	w := claimPost(t, s, "", map[string]any{"from": claimPeerID, "text": "Ready", "re": row.ID})
	if w.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	poll(row.Due.Add(20*time.Minute), 4)
}

func TestClaimLadderSkipsTheParentWhenDisabledOrAbsentPG(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		name := "absent"
		if disabled {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			s, e := newClaimsServer(t)
			if disabled {
				e.opening = &action.Opening{Name: "claim-opener", Conversation: claimOpenerID, Letters: true}
				pool, _ := s.db.Pool()
				var profile int
				if err := pool.QueryRow(t.Context(), `INSERT INTO profiles(name,config_dir,prefix,launch) VALUES ($1,'/account/claims','/srv/claims','{"parentQuestions":false,"parentQuestionWait":2}') RETURNING id`, t.Name()).Scan(&profile); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM profiles WHERE id=$1`, profile) })
			}
			store := &pushStore{taken: make(chan time.Time, 8)}
			s.push = notify.New(store, "")
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() { defer close(done); s.push.Run(ctx) }()
			t.Cleanup(func() { cancel(); <-done })
			minute := 1
			row := sentClaim(t, s, "done", &minute)
			noDue := sentClaim(t, s, "reply", nil)
			if err := s.pollClaims(t.Context(), *row.Due); err != nil {
				t.Fatal(err)
			}
			lease, err := s.db.LockClaim(t.Context(), row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if lease.Record.Rung != 1 {
				t.Fatalf("first rung: %+v", lease.Record)
			}
			lease.Close(t.Context())
			wait := 5 * time.Minute
			if disabled {
				wait = 2 * time.Minute
			}
			if err := s.pollClaims(t.Context(), row.Due.Add(wait)); err != nil {
				t.Fatal(err)
			}
			taken(t, store, time.Second, "the overdue claim")
			sent := e.sent()
			if len(sent) != 4 || sent[2].Target != "claim-peer" || sent[3].Target != "sentinel" || !strings.Contains(sent[3].Text, "The person is told now.") {
				t.Fatalf("skipped parent: %+v", sent)
			}
			lease, err = s.db.LockClaim(t.Context(), row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if lease.Record.Rung != 3 {
				t.Fatalf("parent not skipped: %+v", lease.Record)
			}
			lease.Close(t.Context())
			lease, err = s.db.LockClaim(t.Context(), noDue.ID)
			if err != nil {
				t.Fatal(err)
			}
			if lease.Record.Rung != 0 {
				t.Fatal("claim without due escalated")
			}
			lease.Close(t.Context())
			if err := s.pollClaims(t.Context(), row.Due.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if len(e.sent()) != 4 {
				t.Fatal("a rung was delivered twice")
			}
			nothingTaken(t, store, 20*time.Millisecond, "a repeated ladder pass")
		})
	}
}

func TestClaimDueInheritsFromTheRecipientAndAnExplicitDueWinsPG(t *testing.T) {
	s, _ := newClaimsServer(t)
	pool, _ := s.db.Pool()
	var profile, group, project int
	if err := pool.QueryRow(t.Context(), `INSERT INTO profiles(name,config_dir,prefix,launch) VALUES ($1,'/account/claims','/srv/claims','{"claimDue":7}') RETURNING id`, t.Name()).Scan(&profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM profile_projects WHERE id=$1`, project)
		pool.Exec(context.Background(), `DELETE FROM profile_groups WHERE id=$1`, group)
		pool.Exec(context.Background(), `DELETE FROM profiles WHERE id=$1`, profile)
	})
	if err := pool.QueryRow(t.Context(), `INSERT INTO profile_groups(profile_id,name) VALUES ($1,'work') RETURNING id`, profile).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if row := sentClaim(t, s, "reply", nil); row.Body.Due == nil || *row.Body.Due != 7 {
		t.Fatalf("contour due: %+v", row)
	}
	if err := pool.QueryRow(t.Context(), `INSERT INTO profile_projects(group_id,name,path,session_name,launch) VALUES ($1,'peer','/srv/claim-peer','claim-peer','{"claimDue":3}') RETURNING id`, group).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if row := sentClaim(t, s, "reply", nil); row.Body.Due == nil || *row.Body.Due != 3 {
		t.Fatalf("project due: %+v", row)
	}
	explicit := 2
	if row := sentClaim(t, s, "reply", &explicit); row.Body.Due == nil || *row.Body.Due != 2 {
		t.Fatalf("explicit due: %+v", row)
	}
}

func TestClaimLadderPushesAfterTheParentsWaitAndConsumesFailedDeliveryPG(t *testing.T) {
	s, e := newClaimsServer(t)
	e.opening = &action.Opening{Name: "claim-opener", Conversation: claimOpenerID, Letters: true}
	store := &pushStore{taken: make(chan time.Time, 8)}
	s.push = notify.New(store, "")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.push.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	minute := 1
	row := sentClaim(t, s, "reply", &minute)
	if err := s.pollClaims(t.Context(), row.Due.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	nothingTaken(t, store, 20*time.Millisecond, "the parent's wait")
	if err := s.pollClaims(t.Context(), row.Due.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	taken(t, store, time.Second, "the unanswered parent stage")
	if err := s.pollClaims(t.Context(), row.Due.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	nothingTaken(t, store, 20*time.Millisecond, "a completed ladder")
	failed, _ := startFakeExec(t, action.Response{OK: false, Error: "no input"})
	s.exec = failed
	row, err := s.db.CreateClaim(t.Context(), requestConversation, "sentinel", "claim-peer", claimPeerID, "Undelivered reminder", act.ClaimInput{Expects: "reply", Due: &minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.pollClaims(t.Context(), *row.Due); err != nil {
		t.Fatal(err)
	}
	lease, err := s.db.LockClaim(t.Context(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Record.Rung != 1 {
		t.Fatal("a failed delivery did not consume its rung")
	}
	lease.Close(t.Context())
}

func TestClaimProtocolHeaderDoesNotTakeTheLettersTextAllowancePG(t *testing.T) {
	s, e := newClaimsServer(t)
	text := strings.Repeat("x", action.TextMax)
	w := claimPost(t, s, "claim-peer", map[string]any{"from": requestConversation, "text": text, "expects": "reply"})
	if w.Code != http.StatusOK {
		t.Fatalf("maximum letter: %d %s", w.Code, w.Body.String())
	}
	sent := e.sent()
	if len(sent) != 1 || !strings.HasSuffix(sent[0].Text, "\n\n"+text) {
		t.Fatal("the claim protocol cut the letter")
	}
}

func TestClaimLettersKeepTheOrdinaryLettersReachPG(t *testing.T) {
	s, _ := newClaimsServer(t)
	s.host = hostWith(t, `{"sessions":[{"sessionId":"`+requestConversation+`","session":"sentinel","outside":true},{"sessionId":"`+claimPeerID+`","session":"claim-peer","outside":true}]}`)
	sentClaim(t, s, "reply", nil)
	s.host = hostWith(t, `{"sessions":[{"sessionId":"`+requestConversation+`","session":"sentinel"},{"sessionId":"`+claimPeerID+`","session":"claim-peer","agent":"codex","outside":true}]}`)
	w := claimPost(t, s, "claim-peer", map[string]any{"from": requestConversation, "text": "Work", "expects": "reply"})
	if w.Code != http.StatusConflict {
		t.Fatalf("a claim reached an independent codex: %d %s", w.Code, w.Body.String())
	}
}
