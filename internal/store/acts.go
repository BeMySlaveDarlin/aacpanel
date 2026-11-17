package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"aacpanel/internal/act"
	"github.com/jackc/pgx/v5"
)

var ErrActClosed = errors.New("the act has already been answered or withdrawn")

const actColumns = `id, conversation, session, kind, blocking, title, body, state, created_at, closed_at, answer, answered_by`

func readAct(row pgx.Row) (act.Record, error) {
	var r act.Record
	err := row.Scan(&r.ID, &r.Conversation, &r.Session, &r.Kind, &r.Blocking, &r.Title, &r.Body, &r.State, &r.Created, &r.Closed, &r.Answer, &r.AnsweredBy)
	return r, err
}

func (s *Store) CreateAct(ctx context.Context, conversation, session string, input act.Input) (act.Record, error) {
	if err := input.Validate(); err != nil {
		return act.Record{}, err
	}
	if input.Withdraw {
		return act.Record{}, errors.New("a withdrawal creates no act")
	}
	pool, err := s.Pool()
	if err != nil {
		return act.Record{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return act.Record{}, err
	}
	return readAct(pool.QueryRow(ctx, `INSERT INTO acts (id,conversation,session,kind,blocking,title,body) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+actColumns, hex.EncodeToString(id[:]), conversation, session, input.Kind, *input.Blocking, input.Title, input.Body))
}

func (s *Store) acts(ctx context.Context, conversations []string, open bool) ([]act.Record, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+actColumns+` FROM acts WHERE conversation=ANY($1) AND (NOT $2 OR state='open') ORDER BY created_at,id`, conversations, open)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (act.Record, error) { return readAct(row) })
}

func (s *Store) OpenActs(ctx context.Context, conversations []string) ([]act.Record, error) {
	return s.acts(ctx, conversations, true)
}
func (s *Store) ActsOf(ctx context.Context, conversation string) ([]act.Record, error) {
	return s.acts(ctx, []string{conversation}, false)
}

func (s *Store) WithdrawAct(ctx context.Context, conversation, session, id string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `UPDATE acts SET state='withdrawn',closed_at=now(),answered_by=$3 WHERE id=$1 AND conversation=$2 AND state='open'`, id, conversation, session)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrActClosed
	}
	return nil
}

// ActAnswer holds the row until delivery, so another device cannot send a
// second answer while the executor is still accepting the first one.
type ActAnswer struct {
	Record act.Record
	tx     pgx.Tx
}

func (s *Store) LockAct(ctx context.Context, id string) (*ActAnswer, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	r, err := readAct(tx.QueryRow(ctx, `SELECT `+actColumns+` FROM acts WHERE id=$1 AND state='open' FOR UPDATE`, id))
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrActClosed
		}
		return nil, err
	}
	return &ActAnswer{Record: r, tx: tx}, nil
}
func (a *ActAnswer) Close(ctx context.Context) { _ = a.tx.Rollback(ctx) }
func (a *ActAnswer) Finish(ctx context.Context, answer, note string) error {
	if err := a.Record.ValidateAnswer(answer, note); err != nil {
		return err
	}
	state := "answered"
	if answer == "cant" || answer == "decline" {
		state = "declined"
	}
	reply := act.Answer{Outcome: answer, Note: strings.TrimSpace(note)}
	if a.Record.Kind == "secret" {
		reply.Outcome = ""
	}
	return a.finish(ctx, state, reply)
}
func (a *ActAnswer) SaveSecret(ctx context.Context, path string) error {
	if a.Record.Kind != "secret" || path == "" {
		return errors.New("a saved secret needs its act and path")
	}
	return a.finish(ctx, "answered", act.Answer{Path: path})
}
func (a *ActAnswer) finish(ctx context.Context, state string, answer act.Answer) error {
	if _, err := a.tx.Exec(ctx, `UPDATE acts SET state=$2,closed_at=now(),answer=$3,answered_by='person' WHERE id=$1`, a.Record.ID, state, answer); err != nil {
		return err
	}
	return a.tx.Commit(ctx)
}

// Saving from the Secrets screen settles requests by name across conversations.
func (s *Store) AnswerSecrets(ctx context.Context, name, path string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `UPDATE acts SET state='answered',closed_at=now(),answer=jsonb_build_object('path',$2::text),answered_by='person' WHERE kind='secret' AND state='open' AND body->>'name'=$1`, name, path)
	return err
}

func (s *Store) OpenSecretActs(ctx context.Context, name string) ([]act.Record, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+actColumns+` FROM acts WHERE kind='secret' AND state='open' AND body->>'name'=$1 ORDER BY created_at,id`, name)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (act.Record, error) { return readAct(row) })
}
