package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReviewNote is one remark left on one line of code.
//
// The quote is not decoration: a branch moves while it is being read, and a
// line number alone stops meaning anything the moment somebody commits above
// it. With the text of the line in hand a note can be found again a few lines
// away, and told apart from a place that merely has the same number now.
type ReviewNote struct {
	ID    string    `json:"id"`
	Path  string    `json:"path"`
	Line  int       `json:"line"`
	Quote string    `json:"quote"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
}

// Review is one reading of a branch and the notes left on it.
//
// The notes live here rather than in the browser for the same reason the
// answers of a brief do: a reading survives a reload, a locked phone and a
// move to the desk, and what was written down is not lost because a tab was
// closed.
type Review struct {
	ID        string       `json:"id"`
	Session   string       `json:"session"`
	Cwd       string       `json:"cwd"`
	Base      string       `json:"base"`
	Notes     []ReviewNote `json:"notes"`
	UpdatedAt time.Time    `json:"updatedAt"`
	SentAt    *time.Time   `json:"sentAt,omitempty"`
	Path      string       `json:"path,omitempty"`
}

// ErrReviewSent is the answer to an attempt to change a reading that has
// already gone. What the session was handed has to stay what it reads: a note
// edited afterwards would make the file on the shelf and the panel disagree
// about what was said, with nobody able to tell which of them is right.
var ErrReviewSent = errors.New("this reading has already been sent and cannot be changed")

// ReviewOf returns one reading; a reading nobody has started comes back empty
// rather than missing.
func (s *Store) ReviewOf(ctx context.Context, id string) (review Review, err error) {
	defer func() { err = Unavailable(err) }()

	review = Review{ID: id, Notes: []ReviewNote{}}
	pool, err := s.Pool()
	if err != nil {
		return review, err
	}

	var raw []byte
	var sent *time.Time
	err = pool.QueryRow(ctx,
		`SELECT session, cwd, base, notes, updated_at, sent_at, path FROM review_draft WHERE id = $1`, id).
		Scan(&review.Session, &review.Cwd, &review.Base, &raw, &review.UpdatedAt, &sent, &review.Path)
	if errors.Is(err, pgx.ErrNoRows) {
		return review, nil
	}
	if err != nil {
		return review, err
	}
	review.SentAt = sent
	if err := json.Unmarshal(raw, &review.Notes); err != nil {
		return review, fmt.Errorf("the notes of %s are not a list: %w", id, err)
	}
	if review.Notes == nil {
		review.Notes = []ReviewNote{}
	}
	return review, nil
}

// Reviews is the index: every reading, newest first, with its status. One
// query rather than a file opened per reading — the point of an index is that
// the state of all of them is read at once.
func (s *Store) Reviews(ctx context.Context, session string, limit int) (out []Review, err error) {
	defer func() { err = Unavailable(err) }()

	out = []Review{}
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}

	query := `SELECT id, session, cwd, base, notes, updated_at, sent_at, path
	            FROM review_draft
	           WHERE ($1 = '' OR session = $1)
	        ORDER BY updated_at DESC
	           LIMIT $2`
	rows, err := pool.Query(ctx, query, session, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var r Review
		var raw []byte
		var sent *time.Time
		if err := rows.Scan(&r.ID, &r.Session, &r.Cwd, &r.Base, &raw, &r.UpdatedAt, &sent, &r.Path); err != nil {
			return out, err
		}
		r.SentAt = sent
		if err := json.Unmarshal(raw, &r.Notes); err != nil {
			return out, fmt.Errorf("the notes of %s are not a list: %w", r.ID, err)
		}
		if r.Notes == nil {
			r.Notes = []ReviewNote{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveReview writes down where a reading has got to. A reading that has been
// sent refuses the write rather than taking it quietly.
func (s *Store) SaveReview(ctx context.Context, r Review) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	if r.Notes == nil {
		r.Notes = []ReviewNote{}
	}
	raw, err := json.Marshal(r.Notes)
	if err != nil {
		return err
	}

	tag, err := pool.Exec(ctx, `
		INSERT INTO review_draft (id, session, cwd, base, notes, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (id) DO UPDATE
		   SET session = EXCLUDED.session,
		       cwd = EXCLUDED.cwd,
		       base = EXCLUDED.base,
		       notes = EXCLUDED.notes,
		       updated_at = now()
		 WHERE review_draft.sent_at IS NULL`,
		r.ID, r.Session, r.Cwd, r.Base, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrReviewSent
	}
	return nil
}

// MarkReviewSent records that a reading has gone and where it landed. Sending
// twice is refused for the same reason editing is: the session holds a file,
// and a second one under the same name is a reading nobody can quote.
func (s *Store) MarkReviewSent(ctx context.Context, id, path string) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx,
		`UPDATE review_draft SET sent_at = now(), path = $2 WHERE id = $1 AND sent_at IS NULL`, id, path)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrReviewSent
	}
	return nil
}

// DropReview removes a reading that was never sent. What has gone stays in the
// index: it is the record of what the session was handed.
func (s *Store) DropReview(ctx context.Context, id string) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `DELETE FROM review_draft WHERE id = $1 AND sent_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrReviewSent
	}
	return nil
}
