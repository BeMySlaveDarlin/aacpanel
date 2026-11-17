package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// callsKept is how long the call of a conversation is kept. The mark of a
// call does not go stale; a conversation nobody opened in a month is gone
// from the lists long before.
const callsKept = 30 * 24 * time.Hour

// Called remembers a call of the conversation made at at under the session
// name; seen says someone had that session on screen as the call came. The
// collector lists a call for minutes and every listing comes here: a listing
// of the same call keeps what the person did about it, and a newer call
// replaces it. Closed calls older than callsKept go.
func (s *Store) CallAct(ctx context.Context, conversation, session string, at time.Time, seen bool) error {
	if s == nil {
		return ErrUnavailable
	}
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Calls are collected repeatedly; serialize the replacement before reading
	// the last time so concurrent snapshots cannot resurrect an older call.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, conversation); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO acts (id,conversation,session,kind,title,created_at,state,closed_at,answered_by)
 SELECT md5($1 || $3::timestamptz::text),$1,$2,'call','Called you',$3,
 CASE WHEN $4 THEN 'answered' ELSE 'open' END,CASE WHEN $4 THEN now() END,CASE WHEN $4 THEN 'person' ELSE '' END
 WHERE NOT EXISTS (SELECT 1 FROM acts WHERE conversation=$1 AND kind='call' AND created_at>$3)
 ON CONFLICT (id) DO UPDATE SET session=excluded.session,
 state=CASE WHEN acts.state='open' AND $4 THEN 'answered' ELSE acts.state END,
 closed_at=CASE WHEN acts.state='open' AND $4 THEN now() ELSE acts.closed_at END,
 answered_by=CASE WHEN acts.state='open' AND $4 THEN 'person' ELSE acts.answered_by END`, conversation, session, at, seen)
	if err != nil {
		return fmt.Errorf("remembering the call: %w", Unavailable(err))
	}
	if _, err = tx.Exec(ctx, `UPDATE acts SET state='withdrawn',closed_at=now(),answered_by=$2
 WHERE conversation=$1 AND kind='call' AND state='open' AND created_at<$3`, conversation, session, at); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM acts WHERE kind='call' AND state<>'open' AND created_at<now()-make_interval(secs=>$1)`, callsKept.Seconds()); err != nil {
		return fmt.Errorf("forgetting old calls: %w", Unavailable(err))
	}
	return tx.Commit(ctx)
}

// Seen marks the calls of the session as seen now: the person has it on
// screen. The session is found by its name, which is what a screen says it
// shows, or by its conversation, which outlives a new name.
func (s *Store) SeeCalls(ctx context.Context, session, conversation string) error {
	if s == nil {
		return ErrUnavailable
	}
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		UPDATE acts SET state='answered',closed_at=now(),answered_by='person'
 WHERE kind='call' AND state='open' AND (session=$1 OR conversation=$2)`,
		session, conversation)
	if err != nil {
		return fmt.Errorf("marking the calls seen: %w", Unavailable(err))
	}
	return nil
}

// Unseen returns the time of the call of every conversation the person has
// not opened since it called, by conversation.
func (s *Store) UnseenCalls(ctx context.Context) (map[string]time.Time, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation,created_at FROM acts WHERE kind='call' AND state='open'`)
	if err != nil {
		return nil, fmt.Errorf("the calls not seen: %w", Unavailable(err))
	}
	type call struct {
		conversation string
		at           time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (call, error) {
		var got call
		return got, r.Scan(&got.conversation, &got.at)
	})
	if err != nil {
		return nil, fmt.Errorf("the calls not seen: %w", Unavailable(err))
	}
	out := make(map[string]time.Time, len(list))
	for _, got := range list {
		out[got.conversation] = got.at
	}
	return out, nil
}
