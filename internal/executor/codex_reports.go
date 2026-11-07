package executor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/session"
	"aacpanel/internal/stream"
)

// Reports stay below both the action's 50,000-character letter ceiling and
// the holder's 1 MiB JSON request ceiling, even after escaping. A larger one
// lives in an owner-readable file; the letter names it without a truncation.
const reportInlineMax = 32 << 10

func reportBody(r codex.TurnReport) (string, error) {
	body := fmt.Sprintf("Codex helper %s ended turn %s: %s.", codex.SessionName(r.Thread), r.Turn, r.Status)
	if r.Error != "" {
		body += "\n" + r.Error
	}
	if r.Text != "" {
		body += "\n\n" + r.Text
	} else {
		body += "\nThe turn has no agent message."
	}
	if len(body) <= reportInlineMax {
		return body, nil
	}
	path := filepath.Join(filepath.Dir(stream.OpenedPath(r.Thread)), "reports", r.Thread,
		fmt.Sprintf("%x.txt", sha256.Sum256([]byte(r.Turn))))
	if err := stream.Keep(path, []byte(body)); err != nil {
		return "", fmt.Errorf("the full report could not be saved: %w", err)
	}
	return fmt.Sprintf("Codex helper %s ended a turn: %s. Read the full report at %s",
		codex.SessionName(r.Thread), r.Status, path), nil
}

// Automatic reports use the holder's peer-letter input, without an inbound
// permission prompt. The observer keeper owns no part of this channel.
func (e *Executor) reportCodexTurn(ctx context.Context, r codex.TurnReport) error {
	body, err := reportBody(r)
	if err != nil {
		return err
	}
	from := codex.SessionName(r.Thread)
	if _, held := e.codex.Thread(r.Owner.Conversation); !held {
		if _, err := liveSessionOf(r.Owner.Conversation); err != nil {
			h := session.Host{Panel: session.PanelURL()}
			return h.Report(ctx, action.HelperReport{Thread: r.Thread, Parent: r.Owner.Conversation}, body)
		}
	}
	return e.deliverCodexReport(ctx, r.Owner.Conversation, "", from, body)
}

func (e *Executor) helperReportLetter(ctx context.Context, req action.Request) (string, error) {
	owner := stream.OwnerOf(req.Report.Thread)
	if owner.Conversation == "" || owner.Conversation != req.Report.Parent {
		return "", fmt.Errorf("the helper's opening does not name this report recipient")
	}
	err := e.deliverCodexReport(ctx, owner.Conversation, req.Target, codex.SessionName(req.Report.Thread), req.Text)
	return "the helper's report was delivered to " + req.Target, err
}

func (e *Executor) deliverCodexReport(ctx context.Context, parent, target, from, body string) error {
	if th, ok := e.codex.Thread(parent); ok {
		if target != "" && th.Name != target {
			return fmt.Errorf("the report's recipient now has another name")
		}
		_, err := th.Link.Send(ctx, th.ID, codex.Message{Text: "A turn report from your Codex helper, opened by you.\n\n" +
			letterEnvelope("", from, body)})
		return err
	}
	s, err := liveSessionOf(parent)
	if err != nil {
		return err
	}
	if target != "" && s.Name != target {
		return fmt.Errorf("the report's recipient now has another name")
	}
	// A report returned by the panel follows the wake's letter path, so a
	// restored tmux parent can receive it through its native message socket.
	if !onStream(s) && target == "" {
		return fmt.Errorf("parent %s is in tmux and receives no automatic reports", s.Name)
	}
	if onStream(s) {
		st, err := streamState(ctx, s)
		if err != nil {
			return err
		}
		if st.Protocol < stream.LettersSince {
			return fmt.Errorf("the holder of parent %s cannot take letters; restart the session", s.Name)
		}
	}
	return peerLetter(ctx, s, from, body)
}
