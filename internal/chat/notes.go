package chat

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNoNotes is the answer of a collector that does not hand over the calls
// of the sessions: until it is updated, no call reaches a phone.
var ErrNoNotes = errors.New("the session collector on the host does not hand over the calls of the sessions: " +
	"update aacpanel-agent on the host (systemctl restart aacpanel-agent@<user>)")

// NotesReq waits for the calls of the sessions. The collector answers as soon
// as the count of calls it has taken is not After, and otherwise when Wait,
// in seconds, is out; without After it answers at once.
type NotesReq struct {
	After *int64  `json:"after,omitempty"`
	Wait  float64 `json:"wait"`
}

// SessionNote is a call of a session for the person, as the collector keeps it.
type SessionNote struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
	At        string `json:"at"`
}

// NoteBoard is every call standing on the collector and the count of calls it
// has taken, which the next request waits on.
type NoteBoard struct {
	Seq   int64
	Notes []SessionNote
}

// MaxNoteWait is the longest a request for the calls waits: under the reply
// timeout of the socket, with room for the answer once the wait is out, so an
// idle board is not taken for a collector that does not answer.
const MaxNoteWait = replyTimeout - 2*time.Second

// Notes returns the standing calls once a call past after is taken or wait is
// out.
func (c *Client) Notes(ctx context.Context, after *int64, wait time.Duration) (NoteBoard, error) {
	wait = min(wait, MaxNoteWait)
	reply, err := c.Feed(ctx, Req{Notes: &NotesReq{After: after, Wait: wait.Seconds()}})
	if errors.Is(err, ErrUnavailable) {
		return NoteBoard{}, err
	}
	// A collector that predates the request takes it for a feed of no
	// session and refuses it, or answers one with no count.
	if err != nil {
		return NoteBoard{}, fmt.Errorf("%w (it answered: %v)", ErrNoNotes, err)
	}
	if reply.Seq == nil {
		return NoteBoard{}, ErrNoNotes
	}
	return NoteBoard{Seq: *reply.Seq, Notes: reply.Notes}, nil
}
