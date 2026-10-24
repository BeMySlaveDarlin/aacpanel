package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// QuestionVerdicts includes an interrupted classifier as an error after its
// deadline: missing a question costs more than showing an uncertain one.
func (s *Store) QuestionVerdicts(ctx context.Context, conversations []string) (map[string]map[string]string, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, message,
 CASE WHEN verdict='pending' AND started_at < now()-interval '90 seconds' THEN 'error' ELSE verdict END
 FROM text_questions WHERE conversation=ANY($1)`, conversations)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		var conversation, message, verdict string
		if err := rows.Scan(&conversation, &message, &verdict); err != nil {
			return nil, err
		}
		if out[conversation] == nil {
			out[conversation] = map[string]string{}
		}
		out[conversation][message] = verdict
	}
	return out, rows.Err()
}

// ClaimQuestion permits at most one classifier call per message, even when
// several snapshots or service processes see it together.
func (s *Store) ClaimQuestion(ctx context.Context, conversation, message string) (bool, error) {
	pool, err := s.Pool()
	if err != nil {
		return false, err
	}
	tag, err := pool.Exec(ctx, `INSERT INTO text_questions (conversation,message,verdict,started_at)
 VALUES ($1,$2,'pending',now()) ON CONFLICT (conversation,message) DO UPDATE
 SET verdict='pending',started_at=now() WHERE text_questions.verdict=''`, conversation, message)
	return tag.RowsAffected() > 0, err
}

func (s *Store) SetQuestionVerdict(ctx context.Context, conversation, message, verdict string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `UPDATE text_questions SET verdict=$3 WHERE conversation=$1 AND message=$2`, conversation, message, verdict)
	return err
}

// PushQuestion claims the news independently of classification: switching
// haiku off shows candidates without spending a call or losing a later verdict.
func (s *Store) PushQuestion(ctx context.Context, conversation, message string) (bool, error) {
	pool, err := s.Pool()
	if err != nil {
		return false, err
	}
	var pushed bool
	err = pool.QueryRow(ctx, `INSERT INTO text_questions (conversation,message,pushed) VALUES ($1,$2,true)
 ON CONFLICT (conversation,message) DO UPDATE SET pushed=true WHERE NOT text_questions.pushed
 RETURNING pushed`, conversation, message).Scan(&pushed)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return pushed, err
}
