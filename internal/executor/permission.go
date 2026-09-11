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

const permAnchor = "Do you want to proceed?"

const permFooter = "Esc to cancel"

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
		return &action.Permission{Unknown: true}
	}
	return nil
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

	at := -1
	for i, line := range lines {
		if line == permAnchor {
			at = i
		}
	}
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
		if strings.Contains(line, permFooter) {
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
