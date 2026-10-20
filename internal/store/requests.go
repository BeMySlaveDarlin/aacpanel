package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"aacpanel/internal/request"
	"github.com/jackc/pgx/v5"
)

// ErrRequestClosed means another answer or a withdrawal settled the request.
var ErrRequestClosed = errors.New("the request has already been answered or withdrawn")

const requestColumns = `id, conversation, session, title, steps, blocking, created_at, answered_at, withdrawn_at, answer, note`

func readRequest(row pgx.Row) (request.Record, error) {
	var r request.Record
	err := row.Scan(&r.ID, &r.Conversation, &r.Session, &r.Title, &r.Steps, &r.Blocking,
		&r.Created, &r.Answered, &r.Withdrawn, &r.Answer, &r.Note)
	return r, err
}

// CreateRequest keeps the task under the conversation the server found.
func (s *Store) CreateRequest(ctx context.Context, conversation, session string, input request.Input) (request.Record, error) {
	if err := input.Validate(); err != nil {
		return request.Record{}, err
	}
	if input.Withdraw {
		return request.Record{}, errors.New("a withdrawal creates no request")
	}
	pool, err := s.Pool()
	if err != nil {
		return request.Record{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return request.Record{}, err
	}
	return readRequest(pool.QueryRow(ctx, `INSERT INTO session_requests (id, conversation, session, title, steps, blocking)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING `+requestColumns,
		hex.EncodeToString(id[:]), conversation, session, input.Title, input.Steps, *input.Blocking))
}

// Requests returns the live state of every request of the named conversations.
func (s *Store) Requests(ctx context.Context, conversations []string) ([]request.Record, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+requestColumns+` FROM session_requests WHERE conversation = ANY($1) ORDER BY created_at, id`, conversations)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (request.Record, error) { return readRequest(row) })
}

// WithdrawRequest takes back only an open task of the conversation calling it.
func (s *Store) WithdrawRequest(ctx context.Context, conversation, id string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `UPDATE session_requests SET withdrawn_at=now()
		WHERE id=$1 AND conversation=$2 AND answered_at IS NULL AND withdrawn_at IS NULL`, id, conversation)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRequestClosed
	}
	return nil
}

// RequestAnswer holds the row until the message is delivered, so two devices
// cannot send two answers and an agent cannot withdraw a task being answered.
type RequestAnswer struct {
	Record request.Record
	tx     pgx.Tx
}

// LockRequest reserves an open request for one answer.
func (s *Store) LockRequest(ctx context.Context, id string) (*RequestAnswer, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	r, err := readRequest(tx.QueryRow(ctx, `SELECT `+requestColumns+` FROM session_requests WHERE id=$1 FOR UPDATE`, id))
	if err != nil || !r.Open() {
		_ = tx.Rollback(ctx)
		if err == nil || errors.Is(err, pgx.ErrNoRows) {
			err = ErrRequestClosed
		}
		return nil, err
	}
	return &RequestAnswer{Record: r, tx: tx}, nil
}

// Close releases an answer that did not reach the session.
func (a *RequestAnswer) Close(ctx context.Context) { _ = a.tx.Rollback(ctx) }

// Finish settles the task only after the executor accepts the answer message.
func (a *RequestAnswer) Finish(ctx context.Context, answer, note string) error {
	if err := request.ValidateAnswer(answer, note); err != nil {
		return err
	}
	if _, err := a.tx.Exec(ctx, `UPDATE session_requests SET answered_at=now(), answer=$2, note=$3 WHERE id=$1`, a.Record.ID, answer, note); err != nil {
		return err
	}
	return a.tx.Commit(ctx)
}
