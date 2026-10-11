package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"aacpanel/internal/stream"
)

// TurnReport belongs to the session that opened a thread, independently of
// where the person groups it on the screen. Text is the last agent message,
// not the preview or the concatenation of its progress updates.
type TurnReport struct {
	Thread string
	Owner  stream.SessionOwner
	Turn   string
	Status string
	Error  string
	Text   string
}

func (l *Link) completedTurn(raw json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ThreadID == "" || p.Turn.ID == "" {
		return
	}
	owner := stream.OwnerOf(p.ThreadID)
	if owner.Conversation == "" || l.deliverReport == nil {
		return
	}
	r := TurnReport{Thread: p.ThreadID, Owner: owner, Turn: p.Turn.ID, Status: p.Turn.Status}
	if p.Turn.Error != nil {
		r.Error = p.Turn.Error.Message
	}
	l.mu.Lock()
	key := r.Thread + "/" + r.Turn
	if !l.reported[key] {
		l.reported[key] = true
		l.reports = append(l.reports, r)
	}
	l.mu.Unlock()
	select {
	case l.reportWake <- struct{}{}:
	default:
	}
}

// The connection's reader cannot wait for RPC. A separate worker reads the
// completed turn and delivers its letter; a Codex parent may share this link.
// Duplicate completion notifications make one delivery attempt per turn.
func (l *Link) reportTurns(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.reportWake:
		}
		for {
			l.mu.Lock()
			if len(l.reports) == 0 {
				l.mu.Unlock()
				break
			}
			r := l.reports[0]
			l.reports = l.reports[1:]
			l.mu.Unlock()
			wctx, cancel := context.WithTimeout(ctx, callWait)
			text, err := l.lastMessage(wctx, r.Thread, r.Turn)
			if err != nil {
				r.Error += fmt.Sprintf("; the last message could not be read: %v", err)
			} else {
				r.Text = text
			}
			if err := l.deliverReport(wctx, r); err != nil {
				log.Printf("codex %s: report of thread %s turn %s to %s was not delivered: %v",
					l.home, r.Thread, r.Turn, r.Owner.Name, err)
			}
			cancel()
		}
	}
}

func (l *Link) lastMessage(ctx context.Context, threadID, turnID string) (string, error) {
	c, err := l.client()
	if err != nil {
		return "", err
	}
	cursor := ""
	for {
		params := map[string]any{"threadId": threadID, "turnId": turnID, "sortDirection": "desc"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			} `json:"data"`
			Next string `json:"nextCursor"`
		}
		if err := within(ctx, c, "thread/items/list", params, &page); err != nil {
			return "", err
		}
		for _, entry := range page.Data {
			if entry.Item.Type == "agentMessage" {
				return entry.Item.Text, nil
			}
		}
		if page.Next == "" || page.Next == cursor {
			return "", nil
		}
		cursor = page.Next
	}
}
