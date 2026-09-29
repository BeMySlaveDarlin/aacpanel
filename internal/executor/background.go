package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aacpanel/internal/stream"
)

// What claude writes under a call it can move to the background, dimmed, in
// brackets: "(ctrl+b to run in background)". The key before the words is
// what the terminal is configured to take for it.
const (
	backgroundHintOpen = "("
	backgroundHintEnd  = " to run in background)"
	// In tmux claude shows every ctrl+b twice — the first press goes to tmux
	// as its prefix — and says so; claude itself takes it once.
	backgroundTwice = "(twice)"
	tmuxPrefixKey   = "ctrl+b"
)

// sessionBackground moves the calls a session runs in the foreground to the
// background: a shell command or a subagent its turn waits on. The call
// answers at once, the turn goes on, and the work runs on beside it as a
// background task. One call is named by the id of its tool_use block; none
// named is every call in the foreground, as Ctrl+B in the terminal.
func (e *Executor) sessionBackground(ctx context.Context, target, use string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if onStream(s) {
		return streamBackground(ctx, s, use)
	}
	if use != "" {
		return "", fmt.Errorf("session %s lives in tmux, where one key moves every call in the foreground at once: "+
			"one call is moved only on the stream — move them all", s.Name)
	}
	return consoleBackground(ctx, s)
}

// streamBackground asks claude to move a call, or every call, to the
// background. Claude answers whether a named call was moved; for every call
// it answers nothing, and the holder's list of the calls in the foreground
// says what went.
func streamBackground(ctx context.Context, s liveSession, use string) (string, error) {
	before, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	fields := map[string]any{}
	if use != "" {
		fields["tool_use_id"] = use
	}
	reply, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "background_tasks", Fields: fields})
	if err != nil {
		return "", err
	}
	if use != "" {
		var body struct {
			Response struct {
				Backgrounded bool `json:"backgrounded"`
			} `json:"response"`
		}
		_ = json.Unmarshal(reply.Response, &body)
		if !body.Response.Backgrounded {
			return "", fmt.Errorf("the call no longer runs in the foreground in %s — it has ended or gone to the "+
				"background already: the panel lags behind", s.Name)
		}
		return fmt.Sprintf("%s moved to the background in %s: the turn goes on, and the work runs on beside it",
			callName(before.Foreground, use), s.Name), nil
	}
	if len(before.Foreground) == 0 {
		return "", fmt.Errorf("nothing ran in the foreground in %s — the calls have ended or gone to the "+
			"background already: the panel lags behind", s.Name)
	}
	return waitForeground(ctx, s, before.Foreground)
}

// waitForeground waits for the calls that were in the foreground to leave it.
// A call that stays is said, not taken for a failure of the rest: a call a
// subagent makes is its own, and goes with the subagent rather than by itself.
func waitForeground(ctx context.Context, s liveSession, moved []stream.Task) (string, error) {
	deadline := time.Now().Add(workOpenWait)
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("the move was sent, but there was no time to see the calls leave the foreground of %s", s.Name)
		case <-time.After(workStepWait):
		}
		st, err := streamState(ctx, s)
		if err != nil {
			return "", fmt.Errorf("the move was sent, but whether the calls left the foreground is unknown: %w", err)
		}
		left := stillForeground(st.Foreground, moved)
		if left == 0 {
			return fmt.Sprintf("%s moved to the background in %s: the turn goes on, and the work runs on beside it",
				plural(len(moved), "call", "calls"), s.Name), nil
		}
		if time.Now().After(deadline) {
			if left == len(moved) {
				return "", fmt.Errorf("claude took the move, but %s of the foreground of %s stayed there after %s",
					plural(left, "call", "calls"), s.Name, workOpenWait)
			}
			return fmt.Sprintf("%s of %d moved to the background in %s, %d still in the foreground: the turn goes on",
				plural(len(moved)-left, "call", "calls"), len(moved), s.Name, left), nil
		}
	}
}

func stillForeground(now, moved []stream.Task) int {
	left := 0
	for _, m := range moved {
		for _, t := range now {
			if t.ID == m.ID {
				left++
				break
			}
		}
	}
	return left
}

// callName is how a call is named in the answer: by its description when the
// holder knows it.
func callName(foreground []stream.Task, use string) string {
	for _, t := range foreground {
		if t.ToolUseID != use || t.Description == "" {
			continue
		}
		if t.Type == "local_agent" {
			return fmt.Sprintf("subagent %q", t.Description)
		}
		return fmt.Sprintf("%q", t.Description)
	}
	return "the call"
}

// consoleBackground presses the key claude moves the calls in the foreground
// with, and only when claude shows its hint for it: the hint stands under a
// call that can go to the background, and names the key. The same key where
// no call runs is a key into the composer, so nothing is pressed without the
// hint, and the move counts once the hint has left the screen.
func consoleBackground(ctx context.Context, s liveSession) (string, error) {
	t, err := termFor(ctx, s.PID)
	if err != nil {
		return "", fmt.Errorf("the calls of session %s cannot be moved from the panel: %w", s.Name, err)
	}
	return moveOnScreen(ctx, t, s.Name)
}

// moveOnScreen presses the key when the screen shows the hint for it, and
// waits for the hint to leave.
func moveOnScreen(ctx context.Context, t term, name string) (string, error) {
	screen, seen := t.screen(ctx)
	if !seen {
		return "", fmt.Errorf("the screen of session %s cannot be read, and the key would go in blind", name)
	}
	keys, shown, err := backgroundKeys(screen, t.kind())
	if err != nil {
		return "", fmt.Errorf("session %s: %w", name, err)
	}
	if shown == 0 {
		return "", fmt.Errorf("nothing runs in the foreground on the screen of session %s: claude shows no hint to "+
			"run a call in the background there, and no key was pressed", name)
	}
	if err := t.send(ctx, keys); err != nil {
		return "", fmt.Errorf("session %s did not accept the input: %w", name, err)
	}
	deadline := time.Now().Add(workOpenWait)
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("the key went into %s, but there was no time to see the calls go — look at the session", name)
		case <-time.After(workStepWait):
		}
		screen, seen := t.screen(ctx)
		if !seen {
			return "", fmt.Errorf("the key went into %s, and its screen can no longer be read: whether the calls "+
				"went to the background is unknown", name)
		}
		if _, left, _ := backgroundKeys(screen, t.kind()); left == 0 {
			return fmt.Sprintf("%s moved to the background in %s: the turn goes on, and the work runs on beside it",
				plural(shown, "call", "calls"), name), nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the key went into %s, but claude still shows a call in the foreground after %s: "+
				"it was not moved", name, workOpenWait)
		}
	}
}

// backgroundKeys reads claude's hints to run a call in the background off the
// screen: how many there are, and the key they name as it is typed. Every
// hint names the same key; one the panel cannot type is refused rather than
// guessed at.
func backgroundKeys(screen, kind string) (string, int, error) {
	var keys string
	shown := 0
	for _, line := range strings.Split(screen, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, backgroundHintOpen) || !strings.HasSuffix(trimmed, backgroundHintEnd) {
			continue
		}
		chord := strings.TrimSuffix(strings.TrimPrefix(trimmed, backgroundHintOpen), backgroundHintEnd)
		typed, err := chordKeys(chord, kind)
		if err != nil {
			return "", 0, err
		}
		keys = typed
		shown++
	}
	return keys, shown, nil
}

// chordKeys turns a chord as claude writes it — "ctrl+b", "ctrl+x ctrl+b" —
// into what the terminal is sent. In tmux claude doubles every ctrl+b and may
// say "(twice)": the second press is what tmux passes through, and the panel
// writes past tmux, so it sends the key once.
func chordKeys(chord, kind string) (string, error) {
	var out strings.Builder
	fields := strings.Fields(chord)
	for i := 0; i < len(fields); i++ {
		key := fields[i]
		if key == backgroundTwice {
			continue
		}
		if kind == "tmux" && key == tmuxPrefixKey && i+1 < len(fields) && fields[i+1] == tmuxPrefixKey {
			i++
		}
		letter, ok := strings.CutPrefix(key, "ctrl+")
		if !ok || len(letter) != 1 || letter[0] < 'a' || letter[0] > 'z' {
			return "", fmt.Errorf("claude names the key to run a call in the background as %q, which the panel "+
				"does not press — use the session's own screen", chord)
		}
		out.WriteByte(letter[0] - 'a' + 1)
	}
	if out.Len() == 0 {
		return "", fmt.Errorf("claude names no key to run a call in the background (%q)", chord)
	}
	return out.String(), nil
}
