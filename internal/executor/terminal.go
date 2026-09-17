package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

type term interface {
	kind() string
	attempts() int
	send(ctx context.Context, payload string) error
	screen(ctx context.Context) (string, bool)
}

func termFor(ctx context.Context, pid int) (term, error) {
	pane, tmuxErr := tmuxPaneFor(ctx, pid)
	if tmuxErr == nil {
		return pane, nil
	}

	tab, konsoleErr := konsoleTabFor(ctx, pid)
	if konsoleErr == nil {
		return tab, nil
	}

	return nil, fmt.Errorf(
		"the session terminal was not found: it is not in tmux (%v), and it is not a konsole "+
			"window either (%v). The panel types into sessions it started itself — those live in tmux",
		tmuxErr, konsoleErr)
}

func sessionsPossible() bool {
	_, err := exec.LookPath(tmuxBin())
	return err == nil
}

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
	enterKey   = "\r"
	clearLine  = "\x15"
	escKey     = "\x1b"
	pastePoll  = 90 * time.Millisecond
	arriveWait = 3 * time.Second
	sendWait   = 5 * time.Second
	markRunes  = 24
)

var promptMarks = []string{"❯", ">"}

var listHints = []string{"to select", "enter to view"}

// What a row of a list carries after the prompt mark.
var listMarks = []string{"◯", "●", "○", "◉"}

var attachChips = []string{"[Image#", "[Pastedtext#"}

func pasteAndSend(ctx context.Context, t term, text string, tail *transcriptTail) (bool, error) {
	if screen, seen := t.screen(ctx); seen {
		if busy, ready := composerReady(screen); !ready {
			return false, fmt.Errorf(
				"nothing was typed: %s. Press Esc in the session to get back to its composer, then send again", busy)
		}
	}
	if err := t.send(ctx, clearLine+pasteStart+text+pasteEnd+enterKey); err != nil {
		return false, err
	}
	mark := composerMark(text)
	arrived := false
	blind := false
	astray := 0
	deadline := time.Now().Add(arriveWait)
	for {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("the text was typed, but there was no time to confirm it — it stayed in the composer")
		case <-time.After(pastePoll):
		}

		if tail.saw(mark) {
			return true, nil
		}
		if blind {
			if time.Now().After(deadline) {
				return false, nil
			}
			continue
		}

		screen, seen := t.screen(ctx)
		if !seen {
			if err := t.send(ctx, enterKey); err != nil {
				return false, err
			}
			if !tail.watching() {
				return false, nil
			}
			blind = true
			continue
		}
		if busy, ready := composerReady(screen); !ready {
			astray++
			if astray > 1 {
				return false, fmt.Errorf(
					"the session left its composer while the message was going in: %s — "+
						"whether it arrived is unknown. Press Esc there and send it again", busy)
			}
			continue
		}
		astray = 0
		state, _ := composerStateOf(screen, mark)
		if state.held {
			if !arrived {
				arrived = true
				deadline = time.Now().Add(sendWait)
			}
			if err := t.send(ctx, enterKey); err != nil {
				return false, err
			}
		} else if arrived || state.onScreen {
			return true, nil
		}
		if time.Now().After(deadline) {
			break
		}
	}
	if arrived {
		return false, fmt.Errorf("what was typed stayed in the composer: Enter did not send it within %s", sendWait)
	}
	if tail.watching() {
		return false, fmt.Errorf(
			"the message is neither on the session screen nor in its conversation after %s — "+
				"whether it arrived is unknown", arriveWait)
	}
	return false, fmt.Errorf("the session screen never showed the message within %s — whether it arrived is unknown", arriveWait)
}

type composer struct {
	held     bool
	onScreen bool
}

func composerStateOf(screen, mark string) (composer, bool) {
	body, ok := composerText(screen)
	if !ok {
		return composer{}, false
	}
	var st composer
	packed := squeeze(body)
	if mark != "" && strings.Contains(packed, mark) {
		st.held = true
	}
	for _, chip := range attachChips {
		if strings.Contains(packed, chip) {
			st.held = true
		}
	}
	st.onScreen = st.held || (mark != "" && strings.Contains(squeeze(screen), mark))
	return st, true
}

func composerMark(text string) string {
	var out []rune
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		out = append(out, r)
		if len(out) == markRunes {
			break
		}
	}
	return string(out)
}

func composerText(screen string) (string, bool) {
	body, _, ok := composerAt(screen)
	return body, ok
}

// composerAt finds the composer and says where it sits.
//
// Normally it is boxed: a rule above it, a rule below it, the chips of the
// session under that. But the screen is only as tall as the window, and what
// stands above the composer — a long answer, a dialog of the session — pushes
// the lower rule off the bottom. Then the composer is the last thing on the
// screen: a rule, and the prompt under it. Reading only the boxed shape made a
// session on a phone-sized window look like a session showing a screen of its
// own, and nothing could be typed into it at all.
//
// The second return says the composer runs to the bottom of the screen, which
// is what tells the caller there is nothing below it to check.
func composerAt(screen string) (string, bool, bool) {
	lines := strings.Split(screen, "\n")
	end := lastRule(lines, len(lines)-1)
	if end < 0 {
		return "", false, false
	}
	if start := lastRule(lines, end-1); start >= 0 {
		if body := lines[start+1 : end]; promptStarts(body) {
			return strings.Join(body, "\n"), false, true
		}
	}
	tail := lines[end+1:]
	if promptStarts(tail) && !listRow(tail) {
		return strings.Join(tail, "\n"), true, true
	}
	return "", false, false
}

// listRow reports that the prompt mark belongs to a row of a list rather than
// to the composer: a subagent tray marks the row under the cursor with the same
// character the composer starts with, and the circle after it is the telling
// part.
func listRow(lines []string) bool {
	for _, line := range lines {
		rest := strings.TrimSpace(line)
		if rest == "" {
			continue
		}
		for _, mark := range promptMarks {
			rest = strings.TrimPrefix(rest, mark)
		}
		rest = strings.TrimSpace(rest)
		for _, mark := range listMarks {
			if strings.HasPrefix(rest, mark) {
				return true
			}
		}
		return false
	}
	return false
}

func promptStarts(body []string) bool {
	for _, line := range body {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return cursorAt(line) != ""
	}
	return false
}

func cursorAt(line string) string {
	rest := strings.TrimLeft(line, " \t")
	for _, mark := range promptMarks {
		if !strings.HasPrefix(rest, mark) {
			continue
		}
		tail := []rune(rest[len(mark):])
		if len(tail) == 0 || unicode.IsSpace(tail[0]) {
			return mark
		}
	}
	return ""
}

func composerReady(screen string) (string, bool) {
	_, toBottom, ok := composerAt(screen)
	if !ok {
		return "the session is showing a screen of its own, not its composer", false
	}
	if toBottom {
		// The composer is the last thing on the screen: there is no room under
		// it for a list to have taken the keyboard.
		return "", true
	}
	lines := strings.Split(screen, "\n")
	for _, line := range lines[lastRule(lines, len(lines)-1)+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if cursorAt(line) != "" {
			return fmt.Sprintf("a list below the composer has the keyboard: %q", trimmed), false
		}
		low := strings.ToLower(trimmed)
		for _, hint := range listHints {
			if strings.Contains(low, hint) {
				return fmt.Sprintf("a list below the composer has the keyboard: %q", trimmed), false
			}
		}
	}
	return "", true
}

func lastRule(lines []string, from int) int {
	for i := from; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "──") {
			return i
		}
	}
	return -1
}

func squeeze(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
