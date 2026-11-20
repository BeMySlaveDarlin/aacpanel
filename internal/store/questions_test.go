package store

import (
	"context"
	"testing"

	"aacpanel/internal/question"
)

func TestTurnVerdictsKeepNoActAndExpiredClaimsCannotSpendAgainPG(t *testing.T) {
	s := requestStore(t)
	const conversation = "turn-verdict-storage"
	pool, _ := s.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1", conversation)
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", conversation)
	})
	claimed := make(chan bool, 8)
	failed := make(chan error, 8)
	for range 8 {
		go func() {
			fresh, err := s.ClaimTurnQuestion(t.Context(), conversation, "message")
			claimed <- fresh
			failed <- err
		}()
	}
	count := 0
	for range 8 {
		if <-claimed {
			count++
		}
		if err := <-failed; err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("concurrent Stops claimed %d calls", count)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE text_questions SET started_at=now()-interval '2 minutes' WHERE conversation=$1", conversation); err != nil {
		t.Fatal(err)
	}
	verdicts, err := s.QuestionVerdicts(t.Context(), []string{conversation})
	if err != nil || verdicts[conversation]["message"].Verdict != "error" {
		t.Fatalf("expired verdict: %+v %v", verdicts, err)
	}
	if fresh, err := s.ClaimTurnQuestion(t.Context(), conversation, "message"); err != nil || fresh {
		t.Fatalf("expired claim was retried: %v %v", fresh, err)
	}
	form := question.Verdict{What: "Publish?", Action: "Merge", Questions: []question.Question{{Question: "Publish?"}}}
	if err := s.SetQuestionVerdict(t.Context(), conversation, "message", "yes", form); err != nil {
		t.Fatal(err)
	}
	verdicts, err = s.QuestionVerdicts(t.Context(), []string{conversation})
	got := verdicts[conversation]["message"]
	if err != nil || got.What != form.What || got.Action != form.Action || len(got.Questions) != 1 {
		t.Fatalf("saved verdict: %+v %v", got, err)
	}
	rows, err := s.ActsOf(t.Context(), conversation)
	if err != nil || len(rows) != 0 {
		t.Fatalf("a verdict created an act: %+v %v", rows, err)
	}
}
