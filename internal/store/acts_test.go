package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
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

func TestOpenAskQuestionsStayInTheirConversationAndWithinBoundsPG(t *testing.T) {
	s := requestStore(t)
	const conversation = "open-ask-questions"
	blocking := false
	create := func(conv, kind, source string, questions []question.Question) act.Record {
		t.Helper()
		body := act.Body{Source: source, Questions: questions}
		if kind == "do" {
			body.Steps = "Check the screen"
		}
		r, err := s.CreateAct(t.Context(), conv, "work", act.Input{Kind: kind, Title: "Choose the plan", Blocking: &blocking,
			Body: body})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	create("another-conversation", "ask", "tool", []question.Question{{Question: "Other conversation?"}})
	create(conversation, "do", "", nil)
	closed := create(conversation, "ask", "prose", []question.Question{{Question: "Closed question?"}})
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"answered", "declined", "withdrawn"} {
		if _, err := pool.Exec(t.Context(), `UPDATE acts SET state=$2,closed_at=now() WHERE id=$1`, closed.ID, state); err != nil {
			t.Fatal(err)
		}
		if got, err := s.OpenAskQuestions(t.Context(), conversation); err != nil || len(got) != 0 {
			t.Fatalf("closed questions (%s): %v %v", state, got, err)
		}
	}
	var want []string
	for i, source := range []string{"tool", "prose", "tool", "prose"} {
		var questions []question.Question
		for j := range 4 {
			text := fmt.Sprintf("Question %d?", i*4+j)
			questions = append(questions, question.Question{Question: text})
			want = append(want, text)
		}
		r := create(conversation, "ask", source, questions)
		if i == 0 {
			long := strings.Repeat("é", 205)
			if _, err := pool.Exec(t.Context(), `UPDATE acts SET body=jsonb_set(body,'{questions,0,question}',to_jsonb($2::text)) WHERE id=$1`, r.ID, long); err != nil {
				t.Fatal(err)
			}
			want[0] = strings.Repeat("é", 200)
		}
	}
	got, err := s.OpenAskQuestions(t.Context(), conversation)
	if err != nil || !reflect.DeepEqual(got, want[:12]) {
		t.Fatalf("open questions: %v %v; want %v", got, err, want[:12])
	}
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

func TestQuestionActsFollowTheVerdictAndTheActualAnswerPG(t *testing.T) {
	s := requestStore(t)
	conv := "act-question-lifecycle"
	for _, message := range []string{"person", "parent", "gone", "no"} {
		if _, err := s.ClaimQuestion(t.Context(), conv, message); err != nil {
			t.Fatal(err)
		}
		verdict := "yes"
		if message == "no" {
			verdict = "no"
		}
		if err := s.SetQuestionVerdict(t.Context(), conv, "child", message, verdict, question.Verdict{What: "Publish?", Questions: []question.Question{{Question: "Publish?", Options: []question.Option{{Label: "Yes"}, {Label: "No"}}}}}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.OpenActs(t.Context(), []string{conv})
	if err != nil || len(rows) != 3 {
		t.Fatalf("verdict acts: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.Kind != "ask" || row.Session != "child" || row.Body.Source != "prose" || row.Body.What != "Publish?" || len(row.Body.Questions) != 1 || len(row.Body.Questions[0].Options) != 2 {
			t.Fatalf("question body: %+v", row)
		}
	}
	if err := s.AnswerTextQuestions(t.Context(), conv, []string{"person"}, "person"); err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerTextQuestions(t.Context(), conv, []string{"parent"}, "lead"); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseQuestions(t.Context(), []string{}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ActsOf(t.Context(), conv)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		want, by := "answered", row.Body.Message
		if by == "parent" {
			by = "lead"
		}
		if by == "gone" {
			want, by = "withdrawn", ""
		}
		if row.State != want || row.AnsweredBy != by || row.Closed == nil {
			t.Fatalf("question closure: %+v", row)
		}
	}
}
