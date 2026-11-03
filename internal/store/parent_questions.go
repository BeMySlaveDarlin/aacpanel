package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type ParentQuestion struct {
	Parent    string
	Due       time.Time
	Delivered bool
}

// A claim precedes mail delivery. An interrupted delivery is not retried:
// it may already have reached the parent, and the phone remains the fallback.
func (s *Store) ClaimParentQuestion(ctx context.Context, conversation, question, parent string, due time.Time) (ParentQuestion, bool, error) {
	pool, err := s.Pool()
	if err != nil {
		return ParentQuestion{}, false, err
	}
	var state ParentQuestion
	err = pool.QueryRow(ctx, `INSERT INTO parent_questions (conversation,question,parent,due)
 VALUES ($1,$2,$3,$4) ON CONFLICT (conversation,question) DO NOTHING
 RETURNING parent,due,delivered`, conversation, question, parent, due).Scan(&state.Parent, &state.Due, &state.Delivered)
	if err == nil {
		return state, true, nil
	}
	if err != pgx.ErrNoRows {
		return ParentQuestion{}, false, err
	}
	err = pool.QueryRow(ctx, `SELECT parent,due,delivered FROM parent_questions
 WHERE conversation=$1 AND question=$2`, conversation, question).Scan(&state.Parent, &state.Due, &state.Delivered)
	return state, false, err
}

func (s *Store) ParentQuestionDelivered(ctx context.Context, conversation, question string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `UPDATE parent_questions SET delivered=true WHERE conversation=$1 AND question=$2`, conversation, question)
	return err
}

// The push claim and an answer serialize on the same row. A scan that read
// the question before the answer arrived must not announce that stale read.
// Sessions with no delegated question retain their ordinary push path.
func (s *Store) ParentQuestionMayPush(ctx context.Context, conversation, question string, standing bool) (bool, error) {
	pool, err := s.Pool()
	if err != nil {
		return false, err
	}
	var allowed bool
	err = pool.QueryRow(ctx, `WITH current_question AS (
 SELECT answered FROM parent_questions WHERE conversation=$1 AND question=$2 FOR UPDATE
 ) SELECT COALESCE((SELECT NOT answered AND $3::boolean FROM current_question),true)`, conversation, question, standing).Scan(&allowed)
	return allowed, err
}

func (s *Store) AnswerParentQuestion(ctx context.Context, conversation, question, parent string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	if parent == "" {
		_, err = pool.Exec(ctx, `UPDATE parent_questions SET answered=true WHERE conversation=$1 AND question=$2`, conversation, question)
	} else {
		_, err = pool.Exec(ctx, `INSERT INTO parent_questions (conversation,question,parent,due,answered)
 VALUES ($1,$2,$3,now(),true) ON CONFLICT (conversation,question) DO UPDATE SET answered=true`, conversation, question, parent)
	}
	return err
}

// Parent mail is not human input in the transcript. Its successful delivery
// settles only the prose questions present before that send, in the same
// verdict store that decides their visibility throughout the panel.
func (s *Store) AnswerTextQuestions(ctx context.Context, conversation string, messages []string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO text_questions (conversation,message,answered)
 SELECT $1,unnest($2::text[]),true ON CONFLICT (conversation,message)
 DO UPDATE SET answered=true,closed=text_questions.pushed`, conversation, messages)
	return err
}
