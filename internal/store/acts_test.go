package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/question"
	"aacpanel/internal/request"
	"aacpanel/internal/testdb"
)

func requestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRequestOwnershipAndAnswerPersistencePG(t *testing.T) {
	s := requestStore(t)
	blocking := true
	r, err := s.CreateAct(t.Context(), "request-owner", "work", act.Input{Kind: "do", Title: "Restart the router", Body: act.Body{Steps: "Run reboot"}, Blocking: &blocking})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Open() || !request.ValidID(r.ID) || r.Created.IsZero() {
		t.Fatalf("created: %+v", r)
	}
	if err := s.WithdrawAct(t.Context(), "another-session", "work", r.ID); !errors.Is(err, ErrActClosed) {
		t.Fatalf("another session withdrew: %v", err)
	}
	lease, err := s.LockAct(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(context.Background())
	if err := lease.Finish(t.Context(), "cant", "The router is unreachable."); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ActsOf(t.Context(), "request-owner")
	if err != nil {
		t.Fatal(err)
	}
	var found act.Record
	for _, row := range rows {
		if row.ID == r.ID {
			found = row
		}
	}
	if found.Open() || found.State != "declined" || found.Answer == nil || found.Answer.Outcome != "cant" || found.Answer.Note != "The router is unreachable." {
		t.Fatalf("answer: %+v", found)
	}
	if other, err := s.LockAct(t.Context(), r.ID); !errors.Is(err, ErrActClosed) {
		if other != nil {
			other.Close(t.Context())
		}
		t.Fatalf("second answer: %v", err)
	}
	if err := s.WithdrawAct(t.Context(), "request-owner", "work", r.ID); !errors.Is(err, ErrActClosed) {
		t.Fatalf("withdraw answered: %v", err)
	}
}

func TestUnsentRequestRemainsOpenAndWithdrawalWinsPG(t *testing.T) {
	s := requestStore(t)
	blocking := false
	r, err := s.CreateAct(t.Context(), "request-release", "work", act.Input{Kind: "do", Title: "Sign in", Body: act.Body{Steps: "Open the browser"}, Blocking: &blocking})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.LockAct(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A second device cannot reserve the same task while its answer is in flight.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if other, err := s.LockAct(ctx, r.ID); err == nil {
		other.Close(t.Context())
		t.Fatal("two devices reserved one request")
	}
	lease.Close(t.Context())
	if err := s.WithdrawAct(t.Context(), "request-release", "work", r.ID); err != nil {
		t.Fatal(err)
	}
	if other, err := s.LockAct(t.Context(), r.ID); !errors.Is(err, ErrActClosed) {
		if other != nil {
			other.Close(t.Context())
		}
		t.Fatalf("withdrawn request answered: %v", err)
	}
}

func TestActKindsAndAnswersPG(t *testing.T) {
	s := requestStore(t)
	blocking := false
	cases := []struct {
		kind, answer, state string
		body                act.Body
	}{
		{"do", "done", "answered", act.Body{Steps: "Run it"}},
		{"do", "cant", "declined", act.Body{Steps: "Run it"}},
		{"secret", "decline", "declined", act.Body{Name: "github-token"}},
		{"secret", "save", "answered", act.Body{Name: "db.env"}},
		{"ask", "", "open", act.Body{Source: "prose", Message: "message", What: "Publish?", Questions: []question.Question{{Question: "Publish?"}}}},
		{"call", "", "open", act.Body{}},
	}
	for _, c := range cases {
		t.Run(c.kind+"/"+c.answer, func(t *testing.T) {
			conversation := "act-kinds-" + c.kind + "-" + c.answer
			row, err := s.CreateAct(t.Context(), conversation, "work", act.Input{Kind: c.kind, Title: "A turn", Body: c.body, Blocking: &blocking})
			if err != nil {
				t.Fatal(err)
			}
			lease, err := s.LockAct(t.Context(), row.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close(t.Context())
			if c.answer == "save" {
				err = lease.SaveSecret(t.Context(), "/srv/secrets/db.env")
			} else if c.answer != "" {
				err = lease.Finish(t.Context(), c.answer, " No access. ")
			} else {
				lease.Close(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			rows, err := s.ActsOf(t.Context(), conversation)
			if err != nil || len(rows) != 1 {
				t.Fatalf("act: %+v %v", rows, err)
			}
			got := rows[0]
			if got.State != c.state || got.Open() != (got.Closed == nil) {
				t.Fatalf("state: %+v", got)
			}
			if c.answer != "" {
				if got.AnsweredBy != "person" || got.Answer == nil {
					t.Fatalf("answer: %+v", got)
				}
				if c.answer == "save" {
					if got.Answer.Path != "/srv/secrets/db.env" {
						t.Fatalf("path: %+v", got.Answer)
					}
				} else if (c.kind == "do" && got.Answer.Outcome != c.answer) || (c.kind == "secret" && got.Answer.Outcome != "") || got.Answer.Note != "No access." {
					t.Fatalf("answer body: %+v", got.Answer)
				}
				if other, err := s.LockAct(t.Context(), row.ID); !errors.Is(err, ErrActClosed) {
					if other != nil {
						other.Close(t.Context())
					}
					t.Fatalf("closed act locked: %v", err)
				}
			}
		})
	}
}

func TestSavingASecretSettlesOnlyItsOpenActsAcrossConversationsPG(t *testing.T) {
	s := requestStore(t)
	blocking := false
	ids := map[string]string{}
	for _, name := range []string{"first", "second", "declined", "other"} {
		secret := "release.env"
		if name == "other" {
			secret = "other.env"
		}
		row, err := s.CreateAct(t.Context(), "act-secret-"+name, "work", act.Input{Kind: "secret", Title: "Token", Body: act.Body{Name: secret}, Blocking: &blocking})
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = row.ID
	}
	lease, err := s.LockAct(t.Context(), ids["declined"])
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Finish(t.Context(), "decline", "No."); err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerSecrets(t.Context(), "release.env", "/srv/secrets/release.env"); err != nil {
		t.Fatal(err)
	}
	for name, id := range ids {
		rows, err := s.ActsOf(t.Context(), "act-secret-"+name)
		if err != nil || len(rows) != 1 {
			t.Fatalf("%s: %+v %v", name, rows, err)
		}
		row := rows[0]
		want := "answered"
		if name == "other" {
			want = "open"
		}
		if name == "declined" {
			want = "declined"
		}
		if row.ID != id || row.State != want {
			t.Fatalf("%s: %+v", name, row)
		}
		if want == "answered" && (row.Answer == nil || row.Answer.Path != "/srv/secrets/release.env" || row.AnsweredBy != "person") {
			t.Fatalf("saved: %+v", row)
		}
	}
}

func TestAnAskPutAwayClosesDeclinedWithoutAnswersPG(t *testing.T) {
	s := requestStore(t)
	blocking := false
	row, err := s.CreateAct(t.Context(), "act-ask-away", "work", act.Input{Kind: "ask", Title: "Who fixes it?", Blocking: &blocking,
		Body: act.Body{Source: "tool", Questions: []question.Question{{Question: "Who fixes it?", Options: []question.Option{{Label: "Max"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.LockAct(t.Context(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A failed finish must not leave the row locked for the tests after it.
	t.Cleanup(func() { lease.Close(context.Background()) })
	if err := lease.Finish(t.Context(), "decline", ""); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ActsOf(t.Context(), "act-ask-away")
	if err != nil || len(rows) != 1 {
		t.Fatalf("acts: %+v %v", rows, err)
	}
	got := rows[0]
	if got.State != "declined" || got.Closed == nil || got.AnsweredBy != "person" || got.Answer == nil ||
		got.Answer.Outcome != "decline" || len(got.Answer.Answers) != 0 {
		t.Fatalf("the question put away: %+v %+v", got, got.Answer)
	}
}

// A terminal left on a step that no longer runs is work for the poll, in an
// open chain and in a failed one alike; a chain whose steps hold no terminal is
// not.
func TestActiveChainsListATerminalItsStepNoLongerNeedsPG(t *testing.T) {
	s := requestStore(t)
	blocking := true
	exit, failed := 0, 3
	chain := func(state string, step int, first *request.Output) string {
		t.Helper()
		row, err := s.CreateAct(t.Context(), "act-chain-terms", "work", act.Input{Kind: "do", Title: "Release", Blocking: &blocking,
			Body: act.Body{Chain: []request.Step{{Who: "you", Text: "Login", Command: "login"}, {Who: "agent", Text: "Verify"}}}})
		if err != nil {
			t.Fatal(err)
		}
		lease, err := s.LockChain(t.Context(), row.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { lease.Close(context.Background()) })
		lease.Record.State, lease.Record.Step, lease.Record.Outputs[0] = state, &step, first
		if err := lease.SaveChain(t.Context()); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	onAgent := chain("open", 2, &request.Output{State: "done", Term: "t-1a2b3c4d", Exit: &exit, Tail: "signed in"})
	onFailure := chain("failed", 1, &request.Output{State: "failed", Term: "t-5e6f7a8b", Exit: &failed, Tail: "denied"})
	closed := chain("open", 2, &request.Output{State: "done", Exit: &exit, Tail: "signed in"})
	notStarted := chain("open", 1, nil)
	rows, err := s.ActiveChains(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, row := range rows {
		listed[row.ID] = true
	}
	if !listed[onAgent] || !listed[onFailure] || listed[closed] || listed[notStarted] {
		t.Fatalf("open on the agent's step %v, failed %v, terminal closed %v, not started %v",
			listed[onAgent], listed[onFailure], listed[closed], listed[notStarted])
	}
}
