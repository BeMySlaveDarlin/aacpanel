package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// BriefAnswer is what the person picked and wrote under one question.
type BriefAnswer struct {
	Picks []string `json:"picks,omitempty"`
	Note  string   `json:"note,omitempty"`
	Skip  bool     `json:"skip,omitempty"`
}

// BriefDecisions keeps the choice notes separate from the note on the task.
type BriefDecisions struct {
	Decisions map[string]BriefAnswer `json:"decisions"`
	Note      string                 `json:"note"`
}

var ErrBriefSent = errors.New("this brief was already sent")

// BriefDraft is how far the person has got through a brief.
//
// The document itself is not here and never will be: it lives on the host with
// the collector, and what the panel keeps is only the part a person typed into
// it. A brief carries whatever the session was talking about, and the database
// of a service that faces the internet is the wrong place for that.
//
// A draft belongs to one publication of its brief, named by when the document
// first went on the shelf: a reissue under the same name keeps that time, and
// a brief removed and published again starts another. A session removes its
// briefs through the collector, which never tells the panel, so the draft of a
// removed brief stays behind until the name is published again — and then it
// is the draft of another document, which reads as empty and is replaced by the
// first answer given to the new one. Every read and write here names the
// publication it means.
type BriefDraft struct {
	Answers         map[string]BriefAnswer `json:"answers"`
	DecisionAnswers *BriefDecisions        `json:"-"`
	UpdatedAt       time.Time              `json:"updatedAt"`
	SentAt          *time.Time             `json:"sentAt,omitempty"`
}

func (d BriefDraft) MarshalJSON() ([]byte, error) {
	type plain BriefDraft
	var answers any = d.Answers
	if d.DecisionAnswers != nil {
		answers = d.DecisionAnswers
	}
	return json.Marshal(struct {
		plain
		Answers any `json:"answers"`
	}{plain(d), answers})
}

func (d *BriefDraft) UnmarshalJSON(raw []byte) error {
	type plain BriefDraft
	var body struct {
		plain
		Answers json.RawMessage `json:"answers"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return err
	}
	*d = BriefDraft(body.plain)
	return d.readAnswers(body.Answers)
}

func (d *BriefDraft) readAnswers(raw []byte) error {
	d.Answers = map[string]BriefAnswer{}
	d.DecisionAnswers = nil
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if decisions, ok := fields["decisions"]; ok && decisionAnswers(decisions, fields) {
		d.DecisionAnswers = &BriefDecisions{Decisions: map[string]BriefAnswer{}}
		return json.Unmarshal(raw, d.DecisionAnswers)
	}
	if err := json.Unmarshal(raw, &d.Answers); err != nil {
		return err
	}
	if d.Answers == nil {
		d.Answers = map[string]BriefAnswer{}
	}
	return nil
}

// A question may itself be named "decisions". Its answer has scalar note/skip
// fields or a picks list; a decision map holds answer objects under its keys.
func decisionAnswers(raw json.RawMessage, fields map[string]json.RawMessage) bool {
	for key := range fields {
		if key != "decisions" && key != "note" {
			return false
		}
	}
	var choices map[string]json.RawMessage
	if json.Unmarshal(raw, &choices) != nil || choices == nil {
		return false
	}
	for _, answer := range choices {
		var value map[string]json.RawMessage
		if json.Unmarshal(answer, &value) != nil || value == nil {
			return false
		}
	}
	return true
}

func (d BriefDraft) answerJSON() ([]byte, error) {
	if d.DecisionAnswers != nil {
		return json.Marshal(d.DecisionAnswers)
	}
	return json.Marshal(d.Answers)
}

func (d BriefDraft) Decided() int {
	n := 0
	if d.DecisionAnswers != nil {
		for _, a := range d.DecisionAnswers.Decisions {
			if len(a.Picks) > 0 {
				n++
			}
		}
		return n
	}
	for _, a := range d.Answers {
		if a.Skip || len(a.Picks) > 0 || strings.TrimSpace(a.Note) != "" {
			n++
		}
	}
	return n
}

// ofPublication says whether a draft kept for the publication kept belongs to
// the publication born. A draft that names none is taken for the publication
// standing under its name: it cannot be told apart, and dropping it would
// throw away answers the person is still typing. The two writes below repeat
// the rule in SQL.
func ofPublication(kept, born string) bool {
	return kept == "" || kept == born
}

// BriefDraftOf returns the draft of one publication of a brief; a brief nobody
// has answered, or whose draft was typed into an earlier publication, comes
// back empty rather than missing.
func (s *Store) BriefDraftOf(ctx context.Context, id, born string) (draft BriefDraft, err error) {
	defer func() { err = Unavailable(err) }()

	draft = BriefDraft{Answers: map[string]BriefAnswer{}}
	pool, err := s.Pool()
	if err != nil {
		return draft, err
	}
	var raw []byte
	var kept string
	var updated time.Time
	var sent *time.Time
	row := pool.QueryRow(ctx,
		`SELECT answers, born, updated_at, sent_at FROM brief_draft WHERE brief_id = $1`, id)
	if err := row.Scan(&raw, &kept, &updated, &sent); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return draft, nil
		}
		return draft, err
	}
	if !ofPublication(kept, born) {
		return draft, nil
	}
	if err := draft.readAnswers(raw); err != nil {
		return BriefDraft{Answers: map[string]BriefAnswer{}}, err
	}
	if draft.Answers == nil {
		draft.Answers = map[string]BriefAnswer{}
	}
	draft.UpdatedAt = updated
	draft.SentAt = sent
	return draft, nil
}

// BriefProgress counts decided choices, or answered questions for question documents,
// given as its id and the publication the shelf holds.
//
// One query for the whole list: a shelf of several dozen briefs would
// otherwise cost a round trip each to draw a single column of numbers.
func (s *Store) BriefProgress(ctx context.Context, briefs map[string]string) (out map[string]int, err error) {
	defer func() { err = Unavailable(err) }()

	out = map[string]int{}
	if len(briefs) == 0 {
		return out, nil
	}
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT brief_id, born, answers FROM brief_draft WHERE brief_id = ANY($1)`, briefIDs(briefs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, kept string
		var raw []byte
		if err := rows.Scan(&id, &kept, &raw); err != nil {
			return nil, err
		}
		if !ofPublication(kept, briefs[id]) {
			continue
		}
		var draft BriefDraft
		if err := draft.readAnswers(raw); err != nil {
			continue
		}
		out[id] = draft.Decided()
	}
	return out, rows.Err()
}

// BriefsSent returns which of the named briefs have gone into their session,
// given as their ids and the publications the shelf holds.
func (s *Store) BriefsSent(ctx context.Context, briefs map[string]string) (out map[string]bool, err error) {
	defer func() { err = Unavailable(err) }()

	out = map[string]bool{}
	if len(briefs) == 0 {
		return out, nil
	}
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT brief_id, born FROM brief_draft WHERE sent_at IS NOT NULL AND brief_id = ANY($1)`, briefIDs(briefs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, kept string
		if err := rows.Scan(&id, &kept); err != nil {
			return nil, err
		}
		if ofPublication(kept, briefs[id]) {
			out[id] = true
		}
	}
	return out, rows.Err()
}

func briefIDs(briefs map[string]string) []string {
	ids := make([]string, 0, len(briefs))
	for id := range briefs {
		ids = append(ids, id)
	}
	return ids
}

// SaveBriefDraft writes the answers as they stand into the draft of one
// publication. Sending is a separate mark: a person who has sent a brief and
// comes back to it must see that it went, not an empty form. The mark of an
// earlier publication does not carry over.
func (s *Store) SaveBriefDraft(ctx context.Context, id, born string, answers map[string]BriefAnswer) (err error) {
	if answers == nil {
		answers = map[string]BriefAnswer{}
	}
	return s.saveBriefDraft(ctx, id, born, answers)
}

func (s *Store) SaveBriefDecisions(ctx context.Context, id, born string, answers BriefDecisions) error {
	return s.saveBriefDraft(ctx, id, born, answers)
}

func (s *Store) saveBriefDraft(ctx context.Context, id, born string, answers any) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(answers)
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx,
		`INSERT INTO brief_draft (brief_id, born, answers, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (brief_id) DO UPDATE SET answers = EXCLUDED.answers, updated_at = now(), born = EXCLUDED.born,
		     sent_at = CASE WHEN brief_draft.born IN ('', EXCLUDED.born) THEN brief_draft.sent_at END
		 WHERE brief_draft.sent_at IS NULL OR brief_draft.born NOT IN ('', EXCLUDED.born)`,
		id, born, raw)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrBriefSent
	}
	return err
}

// BriefSend holds the row until the executor answers. A concurrent save or send
// must wait, so the text delivered and the answers marked sent stay together.
type BriefSend struct {
	Draft    BriefDraft
	tx       pgx.Tx
	id, born string
}

func (s *Store) LockBrief(ctx context.Context, id, born string) (*BriefSend, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	lease := &BriefSend{tx: tx, id: id, born: born, Draft: BriefDraft{Answers: map[string]BriefAnswer{}}}
	if _, err = tx.Exec(ctx, `INSERT INTO brief_draft (brief_id, born) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, born); err != nil {
		lease.Close(ctx)
		return nil, err
	}
	var raw []byte
	var kept string
	var updated time.Time
	var sent *time.Time
	err = tx.QueryRow(ctx, `SELECT answers, born, updated_at, sent_at FROM brief_draft WHERE brief_id=$1 FOR UPDATE`, id).Scan(&raw, &kept, &updated, &sent)
	if err == nil && ofPublication(kept, born) {
		err = lease.Draft.readAnswers(raw)
		lease.Draft.UpdatedAt, lease.Draft.SentAt = updated, sent
		if err == nil && sent != nil {
			err = ErrBriefSent
		}
	}
	if err != nil {
		lease.Close(ctx)
		return nil, err
	}
	return lease, nil
}

func (b *BriefSend) Close(ctx context.Context) { _ = b.tx.Rollback(ctx) }

func (b *BriefSend) Finish(ctx context.Context) error {
	raw, err := b.Draft.answerJSON()
	if err != nil {
		return err
	}
	if err := b.tx.QueryRow(ctx, `UPDATE brief_draft SET born=$2, answers=$3, sent_at=now() WHERE brief_id=$1 RETURNING sent_at`, b.id, b.born, raw).Scan(&b.Draft.SentAt); err != nil {
		return err
	}
	return b.tx.Commit(ctx)
}

// MarkBriefSent records that the answers of one publication have gone into
// the session. The answers of an earlier publication do not go with the mark.
func (s *Store) MarkBriefSent(ctx context.Context, id, born string) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO brief_draft (brief_id, born, sent_at) VALUES ($1, $2, now())
		 ON CONFLICT (brief_id) DO UPDATE SET sent_at = now(), born = EXCLUDED.born,
		     answers = CASE WHEN brief_draft.born IN ('', EXCLUDED.born) THEN brief_draft.answers ELSE '{}'::jsonb END`,
		id, born)
	return err
}

// DropBriefDraft removes what a person typed into a brief. It follows the
// document off the shelf: answers to a brief nobody can open are a record with
// nothing behind it.
func (s *Store) DropBriefDraft(ctx context.Context, id string) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM brief_draft WHERE brief_id = $1`, id)
	return err
}
