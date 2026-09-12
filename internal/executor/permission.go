package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"aacpanel/internal/action"
)

// permAnchor is the question of the commonest dialog, kept as a fallback for a
// screen whose list of options cannot be found. The question itself is no longer
// what the dialog is recognised by: the client asks it in a dozen wordings — about
// an edit to a named file, about fetching a page, about a connection, about
// carrying on — and a list of them would be one release behind for ever.
const permAnchor = "Do you want to proceed?"

// permFooter closes the dialog. Matched without regard to case: the client writes
// it both ways.
const permFooter = "esc to cancel"

var permOptionRe = regexp.MustCompile(`^[❯>*]?\s*(\d{1,2})\.\s+(\S.*)$`)

func unbox(r rune) rune {
	if strings.ContainsRune("─━│┃╭╮╰╯┌┐└┘┏┓┗┛═╔╗╚╝╌╍┄┅", r) {
		return ' '
	}
	return r
}

var permFrameRe = regexp.MustCompile(`^[\s─━│┃╭╮╰╯┌┐└┘┏┓┗┛═╔╗╚╝╌╍┄┅]*$`)

var permLasting = []string{"always", "switch to", "don't ask", "do not ask"}

func permissionFor(screen string) *action.Permission {
	if d, ok := parsePermission(screen); ok {
		return &d
	}
	if dialogOnScreen(screen) {
		return &action.Permission{Unknown: true, Raw: rawDialog(screen)}
	}
	return nil
}

// permRawUp and permRawDown bound the window taken around the anchor: the dialog is
// a couple of lines of subject and a short list, and everything beyond that is the
// conversation, which is nobody's business here.
const permRawUp = 8

const permRawDown = 24

// rawDialog returns the dialog lines as they stand on the screen, for a screen the
// parser did not understand. It walks out from the anchor to the line that opens the
// dialog and to the one that closes it; with either boundary missing it returns
// nothing, because a window with no edge takes in the conversation above along with
// whatever was said there.
func rawDialog(screen string) []string {
	rows := strings.Split(screen, "\n")
	text := make([]string, len(rows))
	frame := make([]bool, len(rows))
	for i, raw := range rows {
		text[i] = strings.TrimRight(strings.Map(unbox, raw), " ")
		frame[i] = permFrameRe.MatchString(raw) && strings.TrimSpace(raw) != ""
	}

	trimmed := make([]string, len(text))
	for i, line := range text {
		trimmed[i] = strings.TrimSpace(line)
	}
	at := questionAt(trimmed)
	if at < 0 {
		return nil
	}

	top := -1
	for i := at - 1; i >= 0 && at-i <= permRawUp; i-- {
		if frame[i] || strings.TrimSpace(text[i]) == "" {
			top = i
			break
		}
	}
	bottom := -1
	for i := at + 1; i < len(text) && i-at <= permRawDown; i++ {
		if frame[i] || hasFooter(text[i]) {
			bottom = i
			break
		}
	}
	if top < 0 || bottom < 0 {
		return nil
	}

	out := text[top : bottom+1]
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return nil
	}

	// The frame the box was drawn with has become blank columns on the left, and on a
	// phone they are width taken away from the text.
	indent := 0
	for i, line := range out {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if n := len(line) - len(strings.TrimLeft(line, " ")); i == 0 || n < indent {
			indent = n
		}
	}
	flush := make([]string, len(out))
	for i, line := range out {
		if len(line) < indent {
			line = ""
		} else {
			line = line[indent:]
		}
		flush[i] = line
	}
	return flush
}

const permTail = 25

var permCursorRe = regexp.MustCompile(`^›\s+\S`)

var permHints = []string{
	"esc to cancel",
	"esc to close",
	"esc to go back",
	"enter to confirm",
	"enter to select",
	"enter to submit",
}

const permCorners = "╭╮╰╯┌┐└┘┏┓┗┛╔╗╚╝"

func dialogOnScreen(screen string) bool {
	lines := strings.Split(screen, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	start := end - permTail
	if start < 0 {
		start = 0
	}
	for _, raw := range lines[start:end] {
		if permFrameRe.MatchString(raw) && strings.ContainsAny(raw, permCorners) {
			return true
		}
		line := strings.TrimSpace(strings.Map(unbox, raw))
		if permCursorRe.MatchString(line) {
			return true
		}
		low := strings.ToLower(line)
		for _, hint := range permHints {
			if strings.Contains(low, hint) {
				return true
			}
		}
	}
	return false
}

func hasFooter(line string) bool {
	return strings.Contains(strings.ToLower(line), permFooter)
}

// questionAt returns the line the dialog asks with, on a screen of trimmed lines.
//
// The dialog is found by its shape rather than by its wording: the footer closes
// it, the numbered list stands above the footer, and the question is the line above
// the list. That holds whatever the client asks about — an edit, a fetch, a
// connection — and it steps over a question quoted in the conversation, which has no
// list under it.
//
// With no list to be found it falls back to the commonest wording, so a screen that
// used to be read still is.
func questionAt(lines []string) int {
	const scan = 24

	footer := -1
	for i, line := range lines {
		if hasFooter(line) {
			footer = i
		}
	}
	if footer >= 0 {
		// The first option, not the nearest one: the options wrap, and a wrapped
		// line is no option at all.
		first := -1
		for i := footer - 1; i >= 0 && footer-i <= scan; i-- {
			if m := permOptionRe.FindStringSubmatch(lines[i]); m != nil && m[1] == "1" {
				first = i
				break
			}
		}
		for i := first - 1; i >= 0; i-- {
			if lines[i] != "" {
				return i
			}
		}
	}

	at := -1
	for i, line := range lines {
		if line == permAnchor {
			at = i
		}
	}
	return at
}

func parsePermission(screen string) (action.Permission, bool) {
	lines := make([]string, 0, 64)
	indents := make([]int, 0, 64)
	frames := make([]bool, 0, 64)
	widths := make([]int, 0, 64)
	for _, raw := range strings.Split(screen, "\n") {
		spaced := strings.Map(unbox, raw)
		frames = append(frames, permFrameRe.MatchString(raw) && strings.TrimSpace(raw) != "")
		lines = append(lines, strings.TrimSpace(spaced))
		indents = append(indents, len(spaced)-len(strings.TrimLeft(spaced, " ")))
		widths = append(widths, len([]rune(raw)))
	}
	width := 0
	for _, w := range widths {
		if w > width {
			width = w
		}
	}

	at := questionAt(lines)
	if at < 0 {
		return action.Permission{}, false
	}

	d := action.Permission{Action: actionBlock(lines, indents, frames, at)}
	if len(d.Action) > 0 {
		d.Tool, d.Action = d.Action[0], d.Action[1:]
	}
	d.Options, d.Partial = optionBlock(lines, indents, widths, width, at)
	if len(d.Options) == 0 {
		return action.Permission{}, false
	}
	d.Fingerprint = permFingerprint(d)
	return d, true
}

func actionBlock(lines []string, indents []int, frames []bool, at int) []string {
	const maxAction = 8

	top := -1
	for i := at - 1; i >= 0 && at-i <= maxAction; i-- {
		if frames[i] {
			top = i
			break
		}
	}
	if top < 0 {
		for i := at - 1; i >= 0 && at-i <= maxAction; i-- {
			if lines[i] == "" {
				top = i
				break
			}
		}
	}

	out := []string{}
	for i := top + 1; i < at; i++ {
		if lines[i] == "" {
			continue
		}
		if strings.HasPrefix(lines[i], "Tip:") {
			for i+1 < at && (lines[i+1] == "" || indents[i+1] > indents[i]) {
				if lines[i+1] == "" {
					break
				}
				i++
			}
			continue
		}
		out = append(out, lines[i])
	}
	if len(out) > maxAction {
		out = out[len(out)-maxAction:]
	}
	return out
}

func optionBlock(lines []string, indents, widths []int, width, at int) ([]action.PermOption, bool) {
	const maxScan = 24
	out := []action.PermOption{}
	partial := false
	seen := map[int]bool{}
	wrapAt := -1
	for i := at + 1; i < len(lines) && i <= at+maxScan; i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		if hasFooter(line) {
			return out, partial || !contiguous(seen)
		}
		if m := permOptionRe.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil || seen[n] {
				partial = true
				continue
			}
			seen[n] = true
			text := strings.TrimSpace(m[2])
			out = append(out, action.PermOption{N: n, Text: text, Lasting: lasting(text)})
			wrapAt = indents[i] + len(m[1]) + 2
			continue
		}
		if wrapAt >= 0 && indents[i] >= wrapAt {
			glue := " "
			if widths[i-1] >= width {
				glue = ""
			}
			last := &out[len(out)-1]
			last.Text = strings.TrimSpace(last.Text + glue + line)
			last.Lasting = lasting(last.Text)
			continue
		}
		partial = true
	}
	return out, true
}

func contiguous(seen map[int]bool) bool {
	for n := 1; n <= len(seen); n++ {
		if !seen[n] {
			return false
		}
	}
	return true
}

func lasting(text string) bool {
	low := strings.ToLower(strings.TrimRight(text, ". "))
	if low == "yes" || low == "no" {
		return false
	}
	for _, mark := range permLasting {
		if strings.Contains(low, mark) {
			return true
		}
	}
	return true
}

func permFingerprint(d action.Permission) string {
	var b strings.Builder
	b.WriteString(flatten(d.Tool))
	for _, line := range d.Action {
		b.WriteString("\n")
		b.WriteString(flatten(line))
	}
	for _, o := range d.Options {
		b.WriteString(fmt.Sprintf("\n%d. %s", o.N, flatten(o.Text)))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func flatten(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func permSession(target string) (liveSession, error) {
	found, known, err := liveSessionsByName(target)
	if err != nil {
		return liveSession{}, err
	}
	switch {
	case len(found) == 0:
		if len(known) == 0 {
			return liveSession{}, fmt.Errorf("there is no live session %s: there are no live sessions at all right now", target)
		}
		return liveSession{}, fmt.Errorf("there is no live session %s; live ones are: %s", target, strings.Join(known, ", "))
	case len(found) > 1:
		return liveSession{}, fmt.Errorf("there are two sessions named %s right now — it is unclear whose dialog to press in", target)
	}
	return found[0], nil
}

// Permission answers the permission question for the named session.
func (e *Executor) Permission(ctx context.Context, target string) (*action.Permission, error) {
	return e.sessionPermission(ctx, target)
}

func (e *Executor) sessionPermission(ctx context.Context, target string) (*action.Permission, error) {
	s, err := permSession(target)
	if err != nil {
		return nil, err
	}
	t, err := termFor(ctx, s.PID)
	if err != nil {
		return nil, fmt.Errorf("the screen of session %s cannot be read: %w", target, err)
	}
	screen, ok := t.screen(ctx)
	if !ok {
		return nil, fmt.Errorf("the screen of session %s was not captured: the %s terminal is silent", target, t.kind())
	}
	return permissionFor(screen), nil
}

func (e *Executor) sessionPermit(ctx context.Context, target string, p *action.Permit) (string, error) {
	if p == nil {
		return "", fmt.Errorf("it is not said which item to press")
	}
	d, err := e.sessionPermission(ctx, target)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("session %s is asking nothing — the dialog is already closed", target)
	}
	if d.Unknown {
		return "", fmt.Errorf("the screen of session %s holds a dialog the panel does not know — nothing is pressed in it blindly", target)
	}
	if d.Fingerprint != p.Fingerprint {
		return "", fmt.Errorf("the dialog of session %s changed while you were looking — look again", target)
	}
	chosen := -1
	for _, o := range d.Options {
		if o.N == p.Option {
			chosen = o.N
			break
		}
	}
	if chosen < 0 {
		return "", fmt.Errorf("there is no item %d in the dialog of session %s", p.Option, target)
	}

	s, err := permSession(target)
	if err != nil {
		return "", err
	}
	t, err := termFor(ctx, s.PID)
	if err != nil {
		return "", fmt.Errorf("the permission prompt of session %s cannot be answered from the panel: %w", target, err)
	}
	if err := t.send(ctx, strconv.Itoa(chosen)); err != nil {
		return "", fmt.Errorf("the keypress did not go through: %w", err)
	}
	return fmt.Sprintf("item %d pressed in session %s", chosen, target), nil
}
