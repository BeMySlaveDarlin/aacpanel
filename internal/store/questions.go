package store

import (
	"context"

	"aacpanel/internal/notify"

	"github.com/jackc/pgx/v5"
)

type QuestionVerdict struct {
	Verdict  string
	What     string
	Answers  []string
	Answered bool
}

// QuestionVerdicts includes an interrupted classifier as an error after its
// deadline: missing a question costs more than showing an uncertain one.
func (s *Store) QuestionVerdicts(ctx context.Context, conversations []string) (map[string]map[string]QuestionVerdict, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, message,
 CASE WHEN verdict='pending' AND started_at < now()-interval '90 seconds' THEN 'error' ELSE verdict END, what, answers, answered
 FROM text_questions WHERE conversation=ANY($1)`, conversations)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]QuestionVerdict{}
	for rows.Next() {
		var conversation, message string
		var verdict QuestionVerdict
		if err := rows.Scan(&conversation, &message, &verdict.Verdict, &verdict.What, &verdict.Answers, &verdict.Answered); err != nil {
			return nil, err
		}
		if out[conversation] == nil {
			out[conversation] = map[string]QuestionVerdict{}
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

func (s *Store) SetQuestionVerdict(ctx context.Context, conversation, session, message, verdict, what string, answers []string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	if answers == nil {
		answers = []string{}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE text_questions SET verdict=$3,what=$4,answers=$5 WHERE conversation=$1 AND message=$2`, conversation, message, verdict, what, answers)
	if err != nil {
		return err
	}
	if verdict == "yes" {
		_, err = tx.Exec(ctx, `INSERT INTO acts(id,conversation,session,kind,title,body,state,created_at,closed_at,answered_by)
 SELECT md5(conversation||message),conversation,$3,'ask',coalesce(nullif(left(btrim(what),120),''),'Asks in text'),
 jsonb_build_object('source','prose','message',message,'what',what,'answers',answers),
 CASE WHEN answered THEN 'answered' WHEN closed THEN 'withdrawn' ELSE 'open' END,coalesce(started_at,now()),
 CASE WHEN answered OR closed THEN now() END,CASE WHEN answered THEN 'person' ELSE '' END
 FROM text_questions WHERE conversation=$1 AND message=$2 ON CONFLICT(id) DO NOTHING`, conversation, message, session)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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
 ON CONFLICT (conversation,message) DO UPDATE SET pushed=true WHERE NOT text_questions.pushed AND NOT text_questions.answered
 RETURNING pushed`, conversation, message).Scan(&pushed)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return pushed, err
}

// CloseQuestions only trusts a complete snapshot. Its pending closures survive
// a restart until an ordinary push can carry them without waking the phone.
func (s *Store) CloseQuestions(ctx context.Context, active []string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE text_questions SET closed=true WHERE NOT closed AND NOT (conversation||':'||message=ANY($1))`, active)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE acts SET state='withdrawn',closed_at=now()
 WHERE kind='ask' AND state='open' AND NOT (conversation||':'||(body->>'message')=ANY($1))`, active)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ClaimQuestionClosures leaves room in the encrypted payload for the news.
// The update is atomic so concurrent senders never claim the same closure.
func (s *Store) ClaimQuestionClosures(ctx context.Context) ([]string, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `WITH pending AS (
 SELECT conversation,message FROM text_questions WHERE closed AND NOT close_sent
 ORDER BY conversation,message LIMIT 16 FOR UPDATE SKIP LOCKED
 ) UPDATE text_questions q SET close_sent=true FROM pending p
 WHERE q.conversation=p.conversation AND q.message=p.message
 RETURNING q.conversation,q.message`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var conversation, message string
		if err := rows.Scan(&conversation, &message); err != nil {
			return nil, err
		}
		tags = append(tags, notify.QuestionTag(conversation, message))
	}
	return tags, rows.Err()
}
