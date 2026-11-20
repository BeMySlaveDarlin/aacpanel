package store

import (
	"context"

	"aacpanel/internal/question"
)

type QuestionVerdict struct {
	Verdict   string
	What      string
	Questions []question.Question
	Action    string
}

// QuestionVerdicts expires an interrupted classifier without retrying the paid call.
func (s *Store) QuestionVerdicts(ctx context.Context, conversations []string) (map[string]map[string]QuestionVerdict, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, message,
 CASE WHEN verdict='pending' AND started_at < now()-interval '90 seconds' THEN 'error' ELSE verdict END, what, questions, action
 FROM text_questions WHERE conversation=ANY($1)`, conversations)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]QuestionVerdict{}
	for rows.Next() {
		var conversation, message string
		var verdict QuestionVerdict
		if err := rows.Scan(&conversation, &message, &verdict.Verdict, &verdict.What, &verdict.Questions, &verdict.Action); err != nil {
			return nil, err
		}
		if out[conversation] == nil {
			out[conversation] = map[string]QuestionVerdict{}
		}
		out[conversation][message] = verdict
	}
	return out, rows.Err()
}

// ClaimTurnQuestion permits at most one paid classifier call per message,
// including concurrent Stops and service restarts.
func (s *Store) ClaimTurnQuestion(ctx context.Context, conversation, message string) (bool, error) {
	pool, err := s.Pool()
	if err != nil {
		return false, err
	}
	tag, err := pool.Exec(ctx, `INSERT INTO text_questions (conversation,message,verdict,started_at,hooked)
 VALUES ($1,$2,'pending',now(),true) ON CONFLICT (conversation,message) DO UPDATE
 SET verdict='pending',started_at=now(),hooked=true WHERE text_questions.verdict=''`, conversation, message)
	return tag.RowsAffected() > 0, err
}

func (s *Store) SetQuestionVerdict(ctx context.Context, conversation, message, verdict string, form question.Verdict) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	form.Questions = question.Clean(form.Questions)
	form.What, form.Action = question.Line(form.What, 140), question.Line(form.Action, 140)
	_, err = pool.Exec(ctx, `UPDATE text_questions SET verdict=$3,what=$4,questions=$5,action=$6 WHERE conversation=$1 AND message=$2`, conversation, message, verdict, form.What, form.Questions, form.Action)
	return err
}
