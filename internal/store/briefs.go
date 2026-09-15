package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// BriefAnswer is what the person picked and wrote under one question.
type BriefAnswer struct {
	Picks []string `json:"picks,omitempty"`
	Note  string   `json:"note,omitempty"`
	Skip  bool     `json:"skip,omitempty"`
}

// BriefDraft is how far the person has got through a brief.
//
// The document itself is not here and never will be: it lives on the host with
// the collector, and what the panel keeps is only the part a person typed into
// it. A brief carries whatever the session was talking about, and the database
// of a service that faces the internet is the wrong place for that.
type BriefDraft struct {
	Answers   map[string]BriefAnswer `json:"answers"`
	UpdatedAt time.Time              `json:"updatedAt"`
	SentAt    *time.Time             `json:"sentAt,omitempty"`
}

// BriefDraftOf returns the draft of one brief; a brief nobody has answered
// comes back empty rather than missing.
func (s *Store) BriefDraftOf(ctx context.Context, id string) (draft BriefDraft, err error) {
	defer func() { err = Unavailable(err) }()

	draft = BriefDraft{Answers: map[string]BriefAnswer{}}
	pool, err := s.Pool()
	if err != nil {
		return draft, err
	}
	var raw []byte
	var updated time.Time
	var sent *time.Time
	row := pool.QueryRow(ctx,
		`SELECT answers, updated_at, sent_at FROM brief_draft WHERE brief_id = $1`, id)
	if err := row.Scan(&raw, &updated, &sent); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return draft, nil
		}
		return draft, err
	}
	if err := json.Unmarshal(raw, &draft.Answers); err != nil {
		return BriefDraft{Answers: map[string]BriefAnswer{}}, err
	}
	if draft.Answers == nil {
		draft.Answers = map[string]BriefAnswer{}
	}
	draft.UpdatedAt = updated
	draft.SentAt = sent
	return draft, nil
}

// BriefProgress returns how many questions are answered in each named brief.
//
// One query for the whole list: a shelf of several dozen briefs would
// otherwise cost a round trip each to draw a single column of numbers.
func (s *Store) BriefProgress(ctx context.Context, ids []string) (out map[string]int, err error) {
	defer func() { err = Unavailable(err) }()

	out = map[string]int{}
	if len(ids) == 0 {
		return out, nil
	}
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT brief_id, answers FROM brief_draft WHERE brief_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		answers := map[string]BriefAnswer{}
		if err := json.Unmarshal(raw, &answers); err != nil {
			continue
		}
		done := 0
		for _, a := range answers {
			if a.Skip || len(a.Picks) > 0 || a.Note != "" {
				done++
			}
		}
		out[id] = done
	}
	return out, rows.Err()
}

// BriefsSent returns which of the named briefs have gone into their session.
func (s *Store) BriefsSent(ctx context.Context, ids []string) (out map[string]bool, err error) {
	defer func() { err = Unavailable(err) }()

	out = map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT brief_id FROM brief_draft WHERE sent_at IS NOT NULL AND brief_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SaveBriefDraft writes the answers as they stand. Sending is a separate mark:
// a person who has sent a brief and comes back to it must see that it went,
// not an empty form.
func (s *Store) SaveBriefDraft(ctx context.Context, id string, answers map[string]BriefAnswer) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	if answers == nil {
		answers = map[string]BriefAnswer{}
	}
	raw, err := json.Marshal(answers)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO brief_draft (brief_id, answers, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (brief_id) DO UPDATE SET answers = EXCLUDED.answers, updated_at = now()`,
		id, raw)
	return err
}

// MarkBriefSent records that the answers have gone into the session.
func (s *Store) MarkBriefSent(ctx context.Context, id string) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO brief_draft (brief_id, sent_at) VALUES ($1, now())
		 ON CONFLICT (brief_id) DO UPDATE SET sent_at = now()`, id)
	return err
}
