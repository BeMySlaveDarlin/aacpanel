package codex

import (
	"context"
	"encoding/json"
	"log"
	"path/filepath"
	"time"

	"aacpanel/internal/stream"
)

// limitsEvery is how often a connected link reads the rate limits of its
// account again. The daemon tells them after a turn to the clients of the
// thread that ran it, and the panel is a client of few threads: a turn of the
// terminal client of codex would leave the limits on the panel as they were.
var limitsEvery = 5 * time.Minute

// Limits are the rate limits of the account a codex home is signed in with,
// as its daemon last told them. The link keeps them by contour beside the
// state files of the threads, and the collector puts them next to the limits
// of claude. A window is the daemon's own: codex names no five hours or week,
// only how long each of its windows lasts.
type Limits struct {
	// At is when the daemon last told them, in seconds since the epoch, as
	// the snapshot of claude's limits says it.
	At        int64        `json:"at"`
	Contour   string       `json:"contour"`
	CodexHome string       `json:"codexHome"`
	LimitID   string       `json:"limitId,omitempty"`
	Plan      string       `json:"plan,omitempty"`
	Primary   *LimitWindow `json:"primary"`
	Secondary *LimitWindow `json:"secondary"`
	// Reached names the limit the account has run into, empty while none.
	Reached string `json:"reached,omitempty"`
}

// LimitWindow is one window of the limits: how much of it is used, how many
// minutes it spans and when it starts again, in seconds since the epoch.
type LimitWindow struct {
	UsedPercent float64 `json:"usedPercent"`
	Minutes     *int64  `json:"windowDurationMins"`
	ResetsAt    *int64  `json:"resetsAt"`
}

// limitsWire is a snapshot of the limits as the daemon sends it.
type limitsWire struct {
	LimitID *string      `json:"limitId"`
	Primary *LimitWindow `json:"primary"`
	Second  *LimitWindow `json:"secondary"`
	Plan    *string      `json:"planType"`
	Reached *string      `json:"rateLimitReachedType"`
}

// LimitsPath is where the link keeps the limits of a contour.
func LimitsPath(contour string) string {
	return filepath.Join(stream.Dir(), "codex-limits", contour+".json")
}

// readLimits asks the daemon for the limits of its account. The detail of the
// credits that reset a limit is a lookup of its own the panel does not show,
// and a background read is asked to skip it. An account that has no limits to
// tell — signed in with a key rather than a plan — leaves the file as it was.
func (l *Link) readLimits(ctx context.Context, c *conn) {
	var out struct {
		RateLimits limitsWire `json:"rateLimits"`
	}
	err := within(ctx, c, "account/rateLimits/read", map[string]any{"excludeResetCreditDetails": true}, &out)
	if err != nil {
		if said := err.Error(); said != l.limitsSaid {
			l.limitsSaid = said
			log.Printf("codex %s: the limits of the account were not read: %v", l.home, err)
		}
		return
	}
	l.limitsSaid = ""
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limits = &Limits{Contour: l.contour, CodexHome: l.home}
	l.mergeLimits(out.RateLimits)
}

// mergeLimits takes a snapshot of the limits into the last one. An update
// after a turn is sparse: what it leaves out or sends as null is not known at
// that moment rather than gone, and the last value stands. Called with mu held.
func (l *Link) mergeLimits(w limitsWire) {
	if l.limits == nil {
		l.limits = &Limits{Contour: l.contour, CodexHome: l.home}
	}
	lim := l.limits
	if w.LimitID != nil {
		lim.LimitID = *w.LimitID
	}
	if w.Primary != nil {
		lim.Primary = w.Primary
	}
	if w.Second != nil {
		lim.Secondary = w.Second
	}
	if w.Plan != nil {
		lim.Plan = *w.Plan
	}
	if w.Reached != nil {
		lim.Reached = *w.Reached
	}
	lim.At = time.Now().Unix()
	body, err := json.Marshal(lim)
	if err == nil {
		err = write(LimitsPath(l.contour), body)
	}
	if err != nil {
		log.Printf("codex %s: the limits of the account were not written: %v", l.home, err)
	}
}

// onLimits takes the limits the daemon sends after a turn.
func (l *Link) onLimits(params json.RawMessage) {
	var p struct {
		RateLimits limitsWire `json:"rateLimits"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mergeLimits(p.RateLimits)
}
