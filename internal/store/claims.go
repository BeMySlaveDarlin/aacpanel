package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"aacpanel/internal/act"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateClaim(ctx context.Context, conversation, session, peer, peerConversation, title string, input act.ClaimInput) (act.Record, error) {
	blocking := false
	body := act.Body{Expects: input.Expects, Due: input.Due}
	if err := (act.Input{Kind: "claim", Title: title, Body: body, Blocking: &blocking}).Validate(); err != nil {
		return act.Record{}, err
	}
	if peer == "" || peerConversation == "" {
		return act.Record{}, errors.New("a claim needs its recipient session and conversation")
	}
	pool, err := s.Pool()
	if err != nil {
		return act.Record{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return act.Record{}, err
	}
	var due *time.Time
	if input.Due != nil {
		at := time.Now().Add(time.Duration(*input.Due) * time.Minute)
		due = &at
	}
	return readAct(pool.QueryRow(ctx, `INSERT INTO acts (id,conversation,session,kind,title,body,peer,peer_conversation,due_at)
 VALUES ($1,$2,$3,'claim',$4,$5,$6,$7,$8) RETURNING `+actColumns,
		hex.EncodeToString(id[:]), conversation, session, title, body, peer, peerConversation, due))
}

func (s *Store) DeleteClaim(ctx context.Context, id string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM acts WHERE id=$1 AND kind='claim' AND state='open' AND rung=0`, id)
	return err
}

// A closed claim is read too, so a refusal can name its actual state.
func (s *Store) LockClaim(ctx context.Context, id string) (*ActAnswer, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	r, err := readAct(tx.QueryRow(ctx, `SELECT `+actColumns+` FROM acts WHERE id=$1 AND kind='claim' FOR UPDATE`, id))
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			err = errors.New("the claim does not exist")
		}
		return nil, err
	}
	return &ActAnswer{Record: r, tx: tx}, nil
}

func (a *ActAnswer) FinishClaim(ctx context.Context, outcome, sender string) error {
	state, err := a.Record.ClaimState(outcome)
	if err != nil {
		return err
	}
	if state == "open" {
		return a.tx.Commit(ctx)
	}
	answer := act.Answer{Outcome: outcome}
	now := time.Now()
	if _, err := a.tx.Exec(ctx, `UPDATE acts SET state=$2,closed_at=$3,answer=$4,answered_by=$5 WHERE id=$1`, a.Record.ID, state, now, answer, sender); err != nil {
		return err
	}
	if err := a.tx.Commit(ctx); err != nil {
		return err
	}
	a.Record.State, a.Record.Closed, a.Record.Answer, a.Record.AnsweredBy, a.Record.Overdue = state, &now, &answer, sender, false
	return nil
}

func (s *Store) OpenClaims(ctx context.Context, sessions []string) ([]act.Record, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+actColumns+` FROM acts WHERE kind='claim' AND state='open' AND (session=ANY($1) OR peer=ANY($1)) ORDER BY created_at,id`, sessions)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (act.Record, error) { return readAct(row) })
}

func (s *Store) DueClaims(ctx context.Context, now time.Time) ([]act.Record, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+actColumns+` FROM acts WHERE kind='claim' AND state='open' AND due_at <= $1 AND rung < 3 ORDER BY due_at,id`, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (act.Record, error) { return readAct(row) })
}

// Persist before sending: a failed delivery counts, and a restart never repeats it.
func (a *ActAnswer) AdvanceClaim(ctx context.Context, rung int, now time.Time) error {
	if a.Record.Kind != "claim" || !a.Record.Open() || a.Record.Due == nil || rung <= a.Record.Rung || rung > 3 {
		return errors.New("the claim cannot advance to this rung")
	}
	if _, err := a.tx.Exec(ctx, `UPDATE acts SET rung=$2,rung_at=$3 WHERE id=$1`, a.Record.ID, rung, now); err != nil {
		return err
	}
	return a.tx.Commit(ctx)
}
