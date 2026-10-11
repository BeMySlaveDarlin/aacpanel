package executor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"

	"aacpanel/internal/codex"
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
	if th, ok := e.codex.Thread(r.Owner.Conversation); ok {
		_, err := th.Link.Send(ctx, th.ID, codex.Message{Text: "A turn report from your Codex helper, opened by you.\n\n" +
			letterEnvelope("", from, body)})
		return err
	}
	s, err := liveSessionOf(r.Owner.Conversation)
	if err != nil {
		return err
	}
	if !onStream(s) {
		return fmt.Errorf("parent %s is in tmux and receives no automatic reports", r.Owner.Name)
	}
	st, err := streamState(ctx, s)
	if err != nil {
		return err
	}
	if st.Protocol < stream.LettersSince {
		return fmt.Errorf("the holder of parent %s cannot take letters; restart the session", s.Name)
	}
	_, err = streamAsk(ctx, s, stream.Request{Op: stream.OpLetter, Text: letterEnvelope("", from, body)})
	return err
}
