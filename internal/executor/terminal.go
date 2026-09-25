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
	// How a message is typed: a run of small writes with a pause between
	// them. One large write is taken by the console for a paste — it folds
	// the text into a chip and marks it in the transcript as pasted content,
	// and the words of a person then reach the session as data rather than as
	// what they said. These two numbers are what was measured against a live
	// session: at this size and this pace a message of a thousand characters
	// over thirteen lines arrives as typed, and one write of the same text
	// arrives folded.
	typeChunk = 80
	typePause = 50 * time.Millisecond
	markRunes = 24
	tailLines = 3
	tailRunes = 64
)

var promptMarks = []string{"❯", ">"}

var listHints = []string{"to select", "enter to view"}

// What a row of a list carries after the prompt mark.
var listMarks = []string{"◯", "●", "○", "◉"}

// What the composer shows in place of the text it took in: a picture, or a
// paste it folded up. Either one standing there means the message is in the
// composer.
var attachChips = []string{"[Image#", "[Pastedtext#"}

// opensAMode says whether the composer would read the line as a key rather
// than as text, so that typing it in would change what arrives. A leading bang
// hands the rest to a shell, a leading hash files it away as a memory, and a
// leading slash is a command whose palette swallows whatever follows it.
// Pasted, each of these is text like any other — and a slash command pasted
// whole is still run as the command it is.
func opensAMode(text string) bool {
	body := strings.TrimSpace(text)
	for _, mark := range []string{"!", "#", "/"} {
		if strings.HasPrefix(body, mark) {
			return true
		}
	}
	return false
}

// leavesAListOpen says whether the message ends in an unfinished @name. Typed
// in, it leaves the list of files open with its first row under the Enter that
// was meant to send the message — so a space is typed after it, which closes
// the list and is trimmed off the message anyway. A space, and not Esc: Esc
// reaches a session that is working as an interruption of its work.
func leavesAListOpen(text string) bool {
	body := strings.TrimRight(text, " \t")
	last := body
	if at := strings.LastIndexAny(body, " \t\n"); at >= 0 {
		last = body[at+1:]
	}
	return strings.HasPrefix(last, "@") && len(last) > 1
}

// keystrokes returns the text as a run of keystrokes: the line breaks a
// console takes for a send are made the ones it takes for a new line. A
// carriage return in the middle of a message would end it there and leave the
// rest standing in the composer as a message of its own.
func keystrokes(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// typeIn writes the message into the composer the way a person would: a
// handful of characters at a time, with a pause the console reads as typing.
func typeIn(ctx context.Context, t term, text string) error {
	runes := []rune(keystrokes(text))
	for at := 0; at < len(runes); at += typeChunk {
		end := at + typeChunk
		if end > len(runes) {
			end = len(runes)
		}
		if err := t.send(ctx, string(runes[at:end])); err != nil {
			return err
		}
		if end == len(runes) {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the message was cut off while it was being typed: %w", ctx.Err())
		case <-time.After(typePause):
		}
	}
	return nil
}

// enterInto puts the message in and asks for it to be sent. What goes in
// between the paste markers is what typing would change: a command, a mode,
// an unfinished name of a file. Everything else is typed, and arrives as the
// words of a person rather than as something they pasted.
func enterInto(ctx context.Context, t term, text string) error {
	if opensAMode(text) {
		return t.send(ctx, clearLine+pasteStart+text+pasteEnd+enterKey)
	}
	if err := t.send(ctx, clearLine); err != nil {
		return err
	}
	if err := typeIn(ctx, t, text); err != nil {
		return err
	}
	if leavesAListOpen(text) {
		if err := t.send(ctx, " "); err != nil {
			return err
		}
	}
	return t.send(ctx, enterKey)
}

// sendWatch lets the one who typed a command take part in the wait for it. A
// command can show it went in by what it changed rather than by its words —
// a command claude runs at once, while it answers, leaves them neither in the
// composer nor in the conversation — and it can open a dialog of its own that
// only the one who typed it knows how to answer.
type sendWatch struct {
	// took reports that what was typed has taken effect.
	took func() bool
	// dialog is shown a screen that left the composer. It says whether it knew
	// the screen and answered it; an error ends the wait.
	dialog func(ctx context.Context, t term, screen string) (bool, error)
}

func (w *sendWatch) tookIt() bool { return w != nil && w.took != nil && w.took() }

func pasteAndSend(ctx context.Context, t term, text string, tail *transcriptTail) (bool, error) {
	return pasteAndSendWith(ctx, t, text, tail, nil)
}

func pasteAndSendWith(ctx context.Context, t term, text string, tail *transcriptTail, w *sendWatch) (bool, error) {
	if screen, seen := t.screen(ctx); seen {
		if busy, ready := composerReady(screen); !ready {
			return false, fmt.Errorf(
				"nothing was typed: %s. Press Esc in the session to get back to its composer, then send again", busy)
		}
	}
	if err := enterInto(ctx, t, text); err != nil {
		return false, err
	}
	mark := composerMark(text)
	arrived := false
	blind := false
	answered := false
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
			if w.tookIt() {
				return true, nil
			}
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
			if !tail.watching() && (w == nil || w.took == nil) {
				return false, nil
			}
			blind = true
			continue
		}
		if busy, ready := composerReady(screen); !ready {
			if w != nil && w.dialog != nil {
				known, err := w.dialog(ctx, t, screen)
				if err != nil {
					return false, err
				}
				if known {
					// The wait is lengthened once, for the answer to take: a
					// dialog that keeps coming back does not keep it going.
					if !answered {
						answered = true
						deadline = time.Now().Add(sendWait)
					}
					astray = 0
					if time.Now().After(deadline) {
						break
					}
					continue
				}
			}
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
		} else if arrived || state.onScreen || w.tookIt() {
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
		return withTail("the session is showing a screen of its own, not its composer", screen), false
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

// composerFree waits for the composer to come back after a key meant to give it
// back. The screen is drawn again in its own time, and reading it in the
// instant the key went in reads the screen that was there before.
//
// It reports what holds the keyboard while something does, and whether the
// screen could be read at all: a screen that cannot be read is not free, it is
// unknown, and the two are answered differently.
func composerFree(ctx context.Context, t term) (busy string, seen bool) {
	deadline := time.Now().Add(freeWait)
	for {
		screen, ok := t.screen(ctx)
		if !ok {
			return "", false
		}
		held, ready := composerReady(screen)
		if ready {
			return "", true
		}
		if time.Now().After(deadline) {
			return held, true
		}
		select {
		case <-ctx.Done():
			return held, true
		case <-time.After(pastePoll):
		}
	}
}

// withTail adds the end of the session screen to the reason it took no input.
// The reason names the fact, these lines name the dialog behind it: without
// them the only way to learn what holds the keyboard is to walk to the machine
// the session runs on.
func withTail(reason, screen string) string {
	tail := screenTail(screen)
	if tail == "" {
		return reason
	}
	return reason + "; it ends with: " + tail
}

// screenTail quotes the last lines of the screen that say something. Rules and
// borders carry no words, and a long line is cut: the reason travels as one
// line into a phone.
func screenTail(screen string) string {
	lines := strings.Split(screen, "\n")
	var tail []string
	for i := len(lines) - 1; i >= 0 && len(tail) < tailLines; i-- {
		line := strings.Trim(lines[i], " \t│┃|")
		if !speaks(line) {
			continue
		}
		tail = append([]string{fmt.Sprintf("%q", cutRunes(line, tailRunes))}, tail...)
	}
	return strings.Join(tail, " / ")
}

// speaks reports that the line carries words rather than the drawing of a box.
func speaks(line string) bool {
	for _, r := range line {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func cutRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
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
