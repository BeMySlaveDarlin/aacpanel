package codex

import (
	"encoding/json"
	"time"
)

type reviewMoment struct {
	Turn    string
	Started int64
}

type approvalReview struct {
	ThreadID     string `json:"threadId"`
	TurnID       string `json:"turnId"`
	ID           string `json:"reviewId"`
	ItemID       string `json:"targetItemId"`
	Started      int64  `json:"startedAtMs"`
	DecisionFrom string `json:"decisionSource"`
	Review       struct {
		Status string `json:"status"`
	} `json:"review"`
	Action struct {
		Type       string `json:"type"`
		ApprovalID string `json:"approvalId"`
		Host       string `json:"host"`
		Protocol   string `json:"protocol"`
		Server     string `json:"server"`
		Tool       string `json:"toolName"`
	} `json:"action"`
}

type approvalReviews struct {
	items  map[string]approvalReview
	strict map[reviewMoment]bool
	last   time.Time
}

func (l *Link) reviewStateLocked(id string) *approvalReviews {
	if l.reviews == nil {
		l.reviews = make(map[string]*approvalReviews)
	}
	s := l.reviews[id]
	if s == nil {
		s = &approvalReviews{items: make(map[string]approvalReview), strict: make(map[reviewMoment]bool)}
		l.reviews[id] = s
	}
	return s
}

func (l *Link) onReview(raw json.RawMessage) {
	var r approvalReview
	if json.Unmarshal(raw, &r) != nil || r.ThreadID == "" || r.TurnID == "" || r.ID == "" {
		return
	}
	l.mu.Lock()
	s := l.reviewStateLocked(r.ThreadID)
	s.items[r.ID], s.last = r, time.Now()
	l.mu.Unlock()
	l.save(r.ThreadID)
}

// Strict review is still a guardian lifecycle event. It arms a matching
// server request, but creates no owner prompt by itself. Keep it even when
// it precedes the started notification on this connection.
func (l *Link) onStrictReview(raw json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Started  int64  `json:"startedAtMs"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ThreadID == "" || p.TurnID == "" {
		return
	}
	l.mu.Lock()
	s := l.reviewStateLocked(p.ThreadID)
	s.strict[reviewMoment{p.TurnID, p.Started}], s.last = true, time.Now()
	l.mu.Unlock()
	l.save(p.ThreadID)
}

func (r approvalReview) matches(p Request) bool {
	if r.TurnID != p.TurnID {
		return false
	}
	if r.Action.ApprovalID != "" {
		return p.ApprovalID == r.Action.ApprovalID
	}
	// Execve and stdin callbacks can share their parent's item. Their start
	// time prevents one child's review from consuming another child's prompt.
	if p.ApprovalID != "" && p.Started != r.Started {
		return false
	}
	if r.ItemID != "" {
		return p.ItemID == r.ItemID
	}
	if r.Action.Type == "networkAccess" {
		return p.Network != nil && p.Network.Host == r.Action.Host && p.Network.Protocol == r.Action.Protocol && p.Started == r.Started
	}
	if r.Action.Type == "mcpToolCall" {
		call := p.ToolCall()
		return call != nil && call.Server == r.Action.Server && call.Tool == r.Action.Tool
	}
	return false
}

func (l *Link) ownerRequestLocked(id string, r Request) bool {
	if r.Kind == "humanCommand" || r.Asks() {
		return true
	}
	t := l.threads[id]
	if t != nil && t.settings.known && t.settings.reviewer != presets[ModeAuto].reviewer {
		return true
	}
	s := l.reviews[id]
	if s == nil {
		// An actual server request outside a matching review is the daemon's
		// user path, including managed human checkpoints and reviewer fallback.
		return true
	}
	for _, review := range s.items {
		if !review.matches(r) {
			continue
		}
		if review.DecisionFrom == "agent" {
			return false
		}
		if review.Review.Status == "inProgress" && !s.strict[reviewMoment{review.TurnID, review.Started}] {
			return false
		}
	}
	return true
}

// Keeping guardian-owned wire requests lets a later handoff use the original
// RPC id. Only owner requests leave the link or can be answered by it.
func (l *Link) ownerPendingLocked(id string) []Request {
	var out []Request
	for _, r := range l.pending[id] {
		if l.ownerRequestLocked(id, r) {
			out = append(out, r)
		}
	}
	return out
}

func (l *Link) automaticApprovalLocked(t *thread) bool {
	if t.settings.known {
		return t.settings.reviewer == presets[ModeAuto].reviewer
	}
	return l.reviews[t.info.ID] != nil
}
