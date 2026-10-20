package action

import (
	"context"
	"fmt"
	"unicode/utf8"

	"aacpanel/internal/sessionintent"
)

// AskIntent asks the host for a session's own messages; the service adds the
// map's inherited values without needing access to the host's state files.
const AskIntent = "intent"

// IntentAsker is an executor that can read the messages kept for a live
// session, so the sheet can show its overrides before the person edits them.
type IntentAsker interface {
	Intents(context.Context, string) (sessionintent.Values, error)
}

// Intents asks for the session's own messages by name. A read failure comes
// back as an error, so an unreadable override is not shown as an absent one.
func (c *Client) Intents(ctx context.Context, target string) (sessionintent.Values, error) {
	resp, err := c.Do(ctx, Request{Ask: AskIntent, Target: target})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Intents, nil
}

func validIntents(values sessionintent.Values) error {
	if len(values) == 0 {
		return badRequest("the session messages did not arrive: an empty text clears one")
	}
	for key, text := range values {
		if key != sessionintent.First && key != sessionintent.Restart {
			return badRequest("unknown session message %q", key)
		}
		if utf8.RuneCountInString(text) > IntentMax {
			return badRequest("the session message is longer than %d characters", IntentMax)
		}
		for _, r := range text {
			if r != '\n' && r != '\t' && (r < 0x20 || r == 0x7f) {
				return badRequest("the session message holds a control character")
			}
		}
	}
	return nil
}
