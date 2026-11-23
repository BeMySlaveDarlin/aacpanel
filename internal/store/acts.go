package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/question"
	"aacpanel/internal/request"
	"github.com/jackc/pgx/v5"
)

var ErrActClosed = errors.New("the act has already been answered or withdrawn")

const actColumns = `id, conversation, session, kind, blocking, title, body, state, created_at, closed_at, answer, answered_by, step, outputs, notice, notified, coalesce(peer,''), coalesce(peer_conversation,''), due_at, rung, rung_at`

func readAct(row pgx.Row) (act.Record, error) {
	var r act.Record
	err := row.Scan(&r.ID, &r.Conversation, &r.Session, &r.Kind, &r.Blocking, &r.Title, &r.Body, &r.State, &r.Created, &r.Closed, &r.Answer, &r.AnsweredBy, &r.Step, &r.Outputs, &r.Notice, &r.Notified, &r.Peer, &r.PeerConversation, &r.Due, &r.Rung, &r.RungAt)
	r.Overdue = r.Kind == "claim" && r.Open() && r.Due != nil && !time.Now().Before(*r.Due)
	return r, err
}

func (s *Store) CreateAct(ctx context.Context, conversation, session string, input act.Input) (act.Record, error) {
	input.Clean()
	if err := input.Validate(); err != nil {
		return act.Record{}, err
	}
	if input.ID != "" {
		return act.Record{}, errors.New("a withdrawal creates no act")
	}
	if input.Kind == "claim" {
		return act.Record{}, errors.New("a claim needs its recipient")
	}
	pool, err := s.Pool()
	if err != nil {
		return act.Record{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return act.Record{}, err
	}
	var step *int
	var outputs []*request.Output
	if input.Body.Chain != nil {
		n := 1
		step = &n
		outputs = make([]*request.Output, len(input.Body.Chain))
	}
	return readAct(pool.QueryRow(ctx, `INSERT INTO acts (id,conversation,session,kind,blocking,title,body,step,outputs) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+actColumns, hex.EncodeToString(id[:]), conversation, session, input.Kind, *input.Blocking, input.Title, input.Body, step, outputs))
}

func (s *Store) acts(ctx context.Context, conversations []string, open bool) ([]act.Record, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+actColumns+` FROM acts WHERE (conversation=ANY($1) OR (NOT $2 AND peer_conversation=ANY($1))) AND (NOT $2 OR state IN ('open','failed')) ORDER BY created_at,id`, conversations, open)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (act.Record, error) { return readAct(row) })
}

func (s *Store) OpenActs(ctx context.Context, conversations []string) ([]act.Record, error) {
	return s.acts(ctx, conversations, true)
}

func (s *Store) ActsOf(ctx context.Context, conversation string) ([]act.Record, error) {
	rows, err := s.acts(ctx, []string{conversation}, false)
	for i := range rows {
		rows[i].Incoming = rows[i].Kind == "claim" && rows[i].PeerConversation == conversation
	}
	return rows, err
}

func (s *Store) WithdrawAct(ctx context.Context, conversation, session, id string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `UPDATE acts SET state='withdrawn',closed_at=now(),answered_by=$3 WHERE id=$1 AND conversation=$2 AND kind <> 'claim' AND state IN ('open','failed')`, id, conversation, session)
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
	r, err := readAct(tx.QueryRow(ctx, `SELECT `+actColumns+` FROM acts WHERE id=$1 AND state IN ('open','failed') FOR UPDATE`, id))
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
func (a *ActAnswer) Finish(ctx context.Context, answer, note string, answers ...question.Answer) error {
	if err := a.Record.ValidateAnswer(answer, note, answers...); err != nil {
		return err
	}
	state := "answered"
	if answer == "cant" || answer == "decline" {
		state = "declined"
	}
	reply := act.Answer{Outcome: answer, Note: strings.TrimSpace(note), Answers: answers}
	if a.Record.Kind == "ask" && answer != "decline" {
		state, reply = "answered", act.Answer{Answers: answers}
	} else if a.Record.Kind == "secret" {
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

// LockChain also admits a closed chain with an undelivered notice or terminals.
func (s *Store) LockChain(ctx context.Context, id string) (*ActAnswer, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	r, err := readAct(tx.QueryRow(ctx, `SELECT `+actColumns+` FROM acts WHERE id=$1 AND step IS NOT NULL FOR UPDATE`, id))
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrActClosed
		}
		return nil, err
	}
	return &ActAnswer{Record: r, tx: tx}, nil
}

func (a *ActAnswer) SaveChain(ctx context.Context) error {
	r := a.Record
	_, err := a.tx.Exec(ctx, `UPDATE acts SET step=$2,outputs=$3,state=$4,closed_at=$5,notice=$6,notified=$7,answered_by=$8,answer=$9 WHERE id=$1`, r.ID, r.Step, r.Outputs, r.State, r.Closed, r.Notice, r.Notified, r.AnsweredBy, r.Answer)
	if err != nil {
		return err
	}
	return a.tx.Commit(ctx)
}

// ActiveChains lists the chains the poll has work on: a step that runs or is
// about to start, an undelivered notice, or a terminal its step no longer
// needs. An output saves its term only while it has one, so the key's presence
// is the test for a terminal.
func (s *Store) ActiveChains(ctx context.Context) ([]act.Record, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+actColumns+` FROM acts WHERE step IS NOT NULL AND ((state='open' AND outputs @? '$[*] ? (@.state == "running" || (@.state == "pending" && @.term != ""))') OR notice IS NOT NULL OR (state NOT IN ('open','failed') AND outputs @? '$[*].term') OR (state IN ('open','failed') AND outputs @? '$[*] ? (exists(@.term) && @.state != "running" && @.state != "pending")')) ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (act.Record, error) { return readAct(row) })
}

// QueueNotice is saved in the same transaction as the step it describes.
func (a *ActAnswer) QueueNotice() {
	r := &a.Record
	var id [16]byte
	rand.Read(id[:])
	state := r.State
	if r.Cancelled() {
		state = "cancelled"
	}
	text := request.Summary(r.ID, r.Title, r.Body.Chain, r.Outputs, *r.Step, r.Notified, state)
	if r.Cancelled() && r.Answer.Note != "" {
		text += "\n\nTheir reason:\n" + r.Answer.Note
	}
	r.Notice = &act.Notice{ID: act.MessageID(hex.EncodeToString(id[:])), Text: text}
	r.Notified = *r.Step - 1
	if !r.Open() {
		r.Notified = *r.Step
	}
}

func (a *ActAnswer) WithdrawChain(now time.Time, session string) {
	a.Record.State, a.Record.Closed, a.Record.AnsweredBy, a.Record.Notice = "withdrawn", &now, session, nil
}

// CancelChain closes the chain for the person, withdrawn as the session's own
// withdrawal is. The session did not take it back and may be waiting on it,
// so the summary that tells it is queued with the close.
func (a *ActAnswer) CancelChain(now time.Time, note string) {
	r := &a.Record
	// The failed step was in the summary of its failure and has not run since.
	if r.State == "failed" {
		r.Notified = *r.Step
	}
	r.State, r.Closed, r.AnsweredBy, r.Answer = "withdrawn", &now, "person", &act.Answer{Outcome: act.Cancel, Note: note}
	a.QueueNotice()
}
