package chat

import (
	"context"
	"errors"
)

// ErrNoBriefs means the collector on the host does not know briefs yet.
var ErrNoBriefs = errors.New("the session collector on the host knows nothing about briefs: " +
	"update aacpanel-agent on the host (systemctl restart aacpanel-agent@<user>)")

// BriefCard says what is inside a brief without carrying the brief.
type BriefCard struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Title     string `json:"title"`
	Eyebrow   string `json:"eyebrow,omitempty"`
	At        string `json:"at,omitempty"`
	Questions int    `json:"questions"`
}

// BriefCount is one counter in the summary across the top.
type BriefCount struct {
	N     string `json:"n"`
	Label string `json:"label"`
}

// BriefLink says where a question came from.
type BriefLink struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// BriefSection is a block that asks nothing.
type BriefSection struct {
	Title string   `json:"title,omitempty"`
	Body  []string `json:"body,omitempty"`
}

// BriefChip is a mark above a question: where it came from, what it belongs to.
type BriefChip struct {
	Tone string `json:"tone"`
	Text string `json:"text"`
}

// BriefFact is what a choice rests on, together with where to check it.
type BriefFact struct {
	Text string `json:"text"`
	Src  string `json:"src,omitempty"`
	// Flag marks the fact that changes the answer rather than supports it.
	Flag bool `json:"flag,omitempty"`
}

// BriefOption is one way to answer, with what it costs.
type BriefOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
}

// BriefNote is the free field under a question.
type BriefNote struct {
	Placeholder string `json:"placeholder,omitempty"`
}

// BriefCapture is what the question takes besides a choice.
type BriefCapture struct {
	Note BriefNote `json:"note"`
}

// BriefAnswered is what the session already knew was decided, so that a reissue
// of the same brief does not lose it.
type BriefAnswered struct {
	Picks []string `json:"picks,omitempty"`
	Note  string   `json:"note,omitempty"`
	At    string   `json:"at,omitempty"`
}

// BriefQuestion is one question with everything the person needs to answer it.
type BriefQuestion struct {
	ID       string         `json:"id"`
	N        string         `json:"n,omitempty"`
	Title    string         `json:"title"`
	Kind     string         `json:"kind"`
	Chips    []BriefChip    `json:"chips,omitempty"`
	Ask      string         `json:"ask,omitempty"`
	Facts    []BriefFact    `json:"facts,omitempty"`
	Options  []BriefOption  `json:"options,omitempty"`
	Read     []string       `json:"read,omitempty"`
	Capture  *BriefCapture  `json:"capture,omitempty"`
	Answered *BriefAnswered `json:"answered,omitempty"`
}

// Brief is the whole document as the session published it.
type Brief struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Title     string `json:"title"`
	Eyebrow   string `json:"eyebrow,omitempty"`
	At        string `json:"at,omitempty"`
	// A document published a second time under the same name: when it was
	// first published and when it was replaced.
	FirstAt    string          `json:"firstAt,omitempty"`
	ReissuedAt string          `json:"reissuedAt,omitempty"`
	Lede       string          `json:"lede,omitempty"`
	Summary    []BriefCount    `json:"summary,omitempty"`
	Lineage    []BriefLink     `json:"lineage,omitempty"`
	Sections   []BriefSection  `json:"sections,omitempty"`
	Questions  []BriefQuestion `json:"questions"`
	Closing    []string        `json:"closing,omitempty"`
}

// BriefsReq asks for the shelf, of one session or of the machine.
type BriefsReq struct {
	Session string `json:"session,omitempty"`
}

// Briefs returns the card of every brief on the host, newest first.
func (c *Client) Briefs(ctx context.Context, session string) ([]BriefCard, error) {
	reply, err := c.Feed(ctx, Req{Briefs: &BriefsReq{Session: session}})
	if err != nil {
		return nil, err
	}
	// An older collector answers the request as a feed and returns no shelf at
	// all. Saying so beats handing back an empty list, which reads as "there
	// are no briefs" and sends nobody to update anything.
	if reply.Briefs == nil {
		return nil, ErrNoBriefs
	}
	return reply.Briefs, nil
}

// BriefOf returns one brief whole.
func (c *Client) BriefOf(ctx context.Context, id string) (*Brief, error) {
	reply, err := c.Feed(ctx, Req{Brief: id})
	if err != nil {
		return nil, err
	}
	if reply.Brief == nil {
		return nil, ErrNoBriefs
	}
	if reply.Brief.Questions == nil {
		reply.Brief.Questions = []BriefQuestion{}
	}
	return reply.Brief, nil
}
