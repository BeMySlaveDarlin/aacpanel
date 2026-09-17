package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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

// permRuleRe is a rule across the screen: a frame line with no edge and no corner,
// which is how the console opens a dialog.
var permRuleRe = regexp.MustCompile(`^[\s─━═╌╍┄┅]*$`)

// permEdges are the runes an edge of a box or a rule is drawn with. A bar standing
// alone is not among them: that is a blank line of a barred command.
const permEdges = "─━═╌╍┄┅╭╮╰╯┌┐└┘┏┓┗┛╔╗╚╝"

var permLasting = []string{"always", "switch to", "don't ask", "do not ask"}

// permHookRe is the heading of a note a hook wrote: the console names the hook
// as event:tool, and the tool is what the dialog is about.
var permHookRe = regexp.MustCompile(`^Hook (\S+) requires confirmation for this \S+`)

// permSourceTagRe is the tag the console hangs on the last line of a hook's note,
// naming where the hook is configured.
var permSourceTagRe = regexp.MustCompile(`\s*\[[^\]]+\]$`)

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
	rules := make([]bool, 0, 64)
	bars := make([]int, 0, 64)
	widths := make([]int, 0, 64)
	for _, raw := range strings.Split(screen, "\n") {
		spaced := strings.Map(unbox, raw)
		drawn := strings.TrimSpace(raw) != ""
		frames = append(frames, drawn && permFrameRe.MatchString(raw) && strings.ContainsAny(raw, permEdges))
		rules = append(rules, drawn && permRuleRe.MatchString(raw))
		bars = append(bars, barColumn(raw))
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

	d := action.Permission{}
	d.Tool, d.Action, d.Note, d.Cut = actionBlock(lines, indents, frames, rules, bars, at)
	d.Options, d.Partial = optionBlock(lines, indents, widths, width, at)
	if len(d.Options) == 0 {
		return action.Permission{}, false
	}
	d.Fingerprint = permFingerprint(d)
	d.Tail = permSaid(d)
	return d, true
}

// dialogTop returns the line the dialog opens with, and whether the opening is off
// the screen altogether.
//
// The console opens a dialog with a rule across the screen, and the rule is looked
// for first, anywhere above the question: a dialog is as tall as the command it asks
// about, and a hook's words under the command make it taller still. A box edge is
// taken next, for a dialog drawn inside a box. With neither on the screen the dialog
// is unframed, and it opens after a blank line, at a line standing in the column of
// the question — the heading and the question share the edge of the dialog, and
// everything between them stands deeper, so a blank line between two parts of the
// dialog is not taken for its edge. A row the console wrapped out of a long line
// stands in the first column and is no edge either, which is why the column is the
// question's and not the first. With no edge at all the dialog is taller than the
// screen: its opening has scrolled off, and every line on the screen belongs to it.
func dialogTop(lines []string, indents []int, frames, rules []bool, at int) (top int, cut bool) {
	for i := at - 1; i >= 0; i-- {
		if rules[i] {
			return i, false
		}
	}
	for i := at - 1; i >= 0; i-- {
		if frames[i] {
			return i, false
		}
	}
	edge := indents[at]
	for i := at - 2; i >= 0; i-- {
		if lines[i] == "" && lines[i+1] != "" && indents[i+1] == edge {
			return i, false
		}
	}
	if len(lines) > 1 && lines[0] != "" && indents[0] == edge && lines[1] != "" && indents[1] > edge {
		return -1, false
	}
	return -1, true
}

// barColumn returns the column a line's bar stands in, for a line barred down its
// left side, and -1 for any other line. A line with an edge on both sides is a box,
// not a bar.
func barColumn(raw string) int {
	line := strings.TrimSpace(raw)
	if !strings.HasPrefix(line, "│") || strings.HasSuffix(line, "│") {
		return -1
	}
	return len([]rune(raw)) - len([]rune(strings.TrimLeft(raw, " ")))
}

// actionBlock returns the tool the dialog asks about, the lines under it up to the
// question — the command and its description — and the note the console put between
// them and the question, when the question is not the console's own.
//
// The console bars two things down the left side, and the column tells them apart:
// a command of several lines is barred inside its box, deeper than the question,
// and the note stands at the edge of the dialog, in the column of the question. The
// hint under the note, saying where to change the rule or the hook, is not shown: it
// is not what the human decides by. On a screen the opening has scrolled off, the
// first line is a line of the command and not the tool, and the tool is what the
// note names, or empty.
func actionBlock(lines []string, indents []int, frames, rules []bool, bars []int, at int) (tool string, out, note []string, cut bool) {
	top, cut := dialogTop(lines, indents, frames, rules, at)

	out = []string{}
	for i := top + 1; i < at; i++ {
		if lines[i] == "" {
			continue
		}
		if bars[i] >= 0 && bars[i] <= indents[at] {
			note = append(note, lines[i])
			continue
		}
		if len(note) > 0 {
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
	if len(note) > 0 {
		note[len(note)-1] = permSourceTagRe.ReplaceAllString(note[len(note)-1], "")
	}
	if cut {
		return hookTool(note), out, note, true
	}
	if len(out) > 0 {
		tool, out = out[0], out[1:]
	}
	return tool, out, note, false
}

// hookTool returns the tool a hook's note names, for a screen with the heading
// scrolled off: the console names the hook as event:tool.
func hookTool(note []string) string {
	if len(note) == 0 {
		return ""
	}
	m := permHookRe.FindStringSubmatch(note[0])
	if m == nil {
		return ""
	}
	name := m[1]
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	return name
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

// lasting tells whether an item grants something for good. An unfamiliar item is
// taken for a lasting one: that is the reading which grants nothing by mistake. A
// refusal grants nothing whatever it goes on to say.
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
	first, _, _ := strings.Cut(low, ",")
	if first == "no" || first == "deny" {
		return false
	}
	return true
}

// permFingerprint says which dialog was read, so that a keypress lands in the
// dialog the person was looking at and not in whatever replaced it.
//
// It survives the screen being redrawn, because the screen is redrawn without
// the dialog changing at all: the terminal of the panel attaches to the same
// tmux window and sizes it to the phone, so the same question wraps one way
// while it is read in the conversation and another way while it is read in the
// terminal. A dialog taller than the screen shows a different part of itself at
// every width on top of that.
//
// So the lines are joined back into one run of words — a wrap is not a change —
// and of that run only the tail is taken: the end of a dialog is what stays on
// the screen whatever the width, while the head of a long command is the first
// thing to scroll away. What the tail leaves out is the beginning of a command
// the console itself says is cut off.
const permTailChars = 400

func permFingerprint(d action.Permission) string {
	var b strings.Builder
	b.WriteString(squash(d.Tool))
	b.WriteString("\n")
	b.WriteString(permSaid(d))
	for _, o := range d.Options {
		b.WriteString(fmt.Sprintf("\n%d.%s", o.N, squash(o.Text)))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// squash drops every space from the text.
//
// The console wraps a line wherever the window ends, inside a word as readily
// as between two: a hook named mcp__atlassian__jira_add_comment comes back as
// "jira_add_c omment" at one width and whole at another. Spaces are therefore
// not part of what is compared — what is compared is the run of characters the
// person read.
func squash(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), "")
}

// permSaid is everything the dialog says under its heading, spaces dropped and
// cut to what is worth carrying back with a keypress.
func permSaid(d action.Permission) string {
	said := make([]string, 0, len(d.Action)+len(d.Note))
	said = append(said, d.Action...)
	said = append(said, d.Note...)
	return runeTail(squash(strings.Join(said, " ")), permTailChars)
}

// runeTail is the last n bytes of the text, cut on a rune: half a character is
// not a character.
func runeTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
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

// permSame says whether the keypress belongs to the dialog now on the screen.
//
// The whole dialog is what is compared, and its end alone is enough only when
// the console says the head is off the screen: there the person is answering
// what they can see, and how much of a long command is above the screen changes
// with the width of a window the panel resizes itself every time the terminal
// is opened.
func permSame(d *action.Permission, p *action.Permit) bool {
	if d.Fingerprint == p.Fingerprint {
		return true
	}
	if !d.Cut || p.Tail == "" || d.Tail == "" {
		return false
	}
	// One reading shows more of the command than the other, and neither shows
	// its beginning: what they share is an end, and an end they do not share is
	// another dialog.
	return strings.HasSuffix(d.Tail, p.Tail) || strings.HasSuffix(p.Tail, d.Tail)
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
	if !permSame(d, p) {
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
