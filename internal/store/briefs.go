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
	Answers   map[string]BriefAnswer `json:"answers"`
	UpdatedAt time.Time              `json:"updatedAt"`
	SentAt    *time.Time             `json:"sentAt,omitempty"`
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

// BriefProgress returns how many questions are answered in each named brief,
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
		`INSERT INTO brief_draft (brief_id, born, answers, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (brief_id) DO UPDATE SET answers = EXCLUDED.answers, updated_at = now(), born = EXCLUDED.born,
		     sent_at = CASE WHEN brief_draft.born IN ('', EXCLUDED.born) THEN brief_draft.sent_at END`,
		id, born, raw)
	return err
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
