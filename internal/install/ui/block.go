package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Outcome is where a key left a block.
type Outcome int

const (
	Open      Outcome = iota // still asking
	Submitted                // every question answered, Submit taken
	Back                     // Esc on the first tab: the caller steps back
)

// Block is a set of questions of one theme, drawn the way Claude Code draws
// AskUserQuestion: a tab a question, a Submit tab with the answers read back,
// and a digit that picks at once. A block of one question has no tabs and no
// Submit — answering it is submitting it.
type Block struct {
	Title     string // heads the answers in the feed
	Questions []Question
	Answers   []Answer

	// EscHint says what Esc does on the first tab: "go back" between blocks,
	// "cancel" under a frame.
	EscHint string
	// Bare drops the rule and the tab row: the block stands under a frame and
	// the frame is its heading.
	Bare bool
	// Refresh runs after every answer. It hides questions an answer made moot
	// and rewords those that name an earlier answer, so the block holds only
	// what is still worth asking.
	Refresh func(*Block)

	tab    int // among the visible questions; one past the last is Submit
	sub    int // the cursor on the Submit tab
	cursor []int
	field  []Field
	err    []string
}

// NewBlock opens a block on its first question.
func NewBlock(title string, qs ...Question) *Block {
	b := &Block{Title: title, Questions: qs, EscHint: "go back"}
	b.Answers = make([]Answer, len(qs))
	b.cursor = make([]int, len(qs))
	b.field = make([]Field, len(qs))
	b.err = make([]string, len(qs))
	for i, q := range qs {
		b.Answers[i] = q.start()
		b.cursor[i] = q.firstRow()
	}
	return b
}

func (q Question) firstRow() int {
	switch q.Kind {
	case Single:
		return q.Default
	case Multi:
		for i, o := range q.Options {
			if !o.Locked {
				return i
			}
		}
	}
	return 0
}

// rows is how many lines the cursor walks: the options, the own line, Skip.
func (q Question) rows() int {
	switch q.Kind {
	case Text, Secret:
		return 1
	}
	n := len(q.Options)
	if q.Own != "" {
		n++
	}
	if q.Kind == Multi && q.Skip != "" {
		n++
	}
	return n
}

func (q Question) ownRow() int {
	switch {
	case q.Kind == Text || q.Kind == Secret:
		return 0
	case q.Own != "":
		return len(q.Options)
	}
	return -1
}

func (q Question) skipRow() int {
	if q.Kind != Multi || q.Skip == "" {
		return -1
	}
	if q.Own != "" {
		return len(q.Options) + 1
	}
	return len(q.Options)
}

func (b *Block) visible() []int {
	var out []int
	for i, q := range b.Questions {
		if !q.Hidden {
			out = append(out, i)
		}
	}
	return out
}

func (b *Block) tabbed() bool { return len(b.visible()) > 1 }

// last is the furthest tab: Submit when there are tabs.
func (b *Block) last() int {
	if b.tabbed() {
		return len(b.visible())
	}
	return 0
}

// Current is the question in focus, or -1 on the Submit tab.
func (b *Block) Current() int {
	vis := b.visible()
	if b.tab < len(vis) {
		return vis[b.tab]
	}
	return -1
}

// OnSubmit tells whether the Submit tab is in focus.
func (b *Block) OnSubmit() bool { return b.tabbed() && b.Current() < 0 }

// Reopen puts the focus on the last tab — the way back into a block from the
// one after it.
func (b *Block) Reopen() { b.tab = b.last() }

func (b *Block) inField(i int) bool {
	return b.cursor[i] == b.Questions[i].ownRow()
}

// Paste types a paste from the terminal into the field in focus.
func (b *Block) Paste(s string) {
	if i := b.Current(); i >= 0 && b.inField(i) {
		b.field[i].Insert(s)
	}
}

// Update takes a key and says where it left the block.
func (b *Block) Update(k tea.KeyPressMsg) Outcome {
	i := b.Current()
	if len(b.visible()) == 0 {
		return Submitted
	}
	if i < 0 {
		return b.updateSubmit(k)
	}
	q := &b.Questions[i]
	b.err[i] = ""
	field := b.inField(i)
	typed := field && b.field[i].Value() != ""

	switch {
	case key.Matches(k, Keys.Back):
		if b.tab > 0 {
			b.tab--
			return Open
		}
		return Back
	case key.Matches(k, Keys.Next), key.Matches(k, Keys.Right) && !typed:
		b.tab = min(b.tab+1, b.last())
		return Open
	case key.Matches(k, Keys.Prev), key.Matches(k, Keys.Left) && !typed:
		b.tab = max(b.tab-1, 0)
		return Open
	case key.Matches(k, Keys.Up):
		b.cursor[i] = max(0, b.cursor[i]-1)
		return Open
	case key.Matches(k, Keys.Down):
		b.cursor[i] = min(q.rows()-1, b.cursor[i]+1)
		return Open
	case key.Matches(k, Keys.Pick):
		return b.pick(i)
	}

	if field {
		b.field[i].Key(k)
		return Open
	}
	switch q.Kind {
	case Single:
		// A digit picks at once, as in Claude Code; the digit of the own line
		// only moves there, since there is nothing typed to pick yet.
		if n := digit(k); n > 0 && n <= q.rows() {
			b.cursor[i] = n - 1
			if n-1 < len(q.Options) {
				return b.pick(i)
			}
		}
	case Multi:
		if key.Matches(k, Keys.Toggle) {
			b.toggle(i)
		}
	}
	return Open
}

func (b *Block) toggle(i int) {
	q, a, c := b.Questions[i], &b.Answers[i], b.cursor[i]
	if c >= len(q.Options) {
		return
	}
	if q.Options[c].Locked {
		b.err[i] = q.Options[c].Label + " is always part of the install."
		return
	}
	a.Picks[c] = !a.Picks[c]
}

func (b *Block) pick(i int) Outcome {
	q, a, c := &b.Questions[i], &b.Answers[i], b.cursor[i]
	typed := strings.TrimSpace(b.field[i].Value())
	if q.Kind == Secret {
		typed = b.field[i].Value()
	}

	switch q.Kind {
	case Single:
		if c == q.ownRow() {
			if !b.accept(i, typed) {
				return Open
			}
			a.Choice, a.Text = len(q.Options), typed
		} else {
			a.Choice = c
		}
	case Multi:
		if c == q.ownRow() && typed != "" {
			if b.accept(i, typed) {
				q.Options = append(q.Options, Option{Label: typed, Detail: "added by you"})
				a.Picks = append(a.Picks, true)
				b.field[i].Set("")
			}
			return Open
		}
		if c == q.skipRow() {
			for j := range a.Picks {
				a.Picks[j] = q.Options[j].Locked
			}
		}
	case Text, Secret:
		if !b.accept(i, typed) {
			return Open
		}
		a.Text = typed
	}
	a.Given = true
	if b.Refresh != nil {
		b.Refresh(b)
	}
	return b.advance(i)
}

// accept runs a typed value past the question's check; a refusal stays on
// screen under the field until the next key.
func (b *Block) accept(i int, v string) bool {
	if v == "" {
		b.err[i] = "Type an answer first."
		if b.Questions[i].Kind == Single {
			b.err[i] = "Type an answer, or pick one of the options above."
		}
		return false
	}
	if check := b.Questions[i].Check; check != nil {
		if err := check(v); err != nil {
			b.err[i] = err.Error()
			return false
		}
	}
	return true
}

func (b *Block) advance(answered int) Outcome {
	if !b.tabbed() {
		return Submitted
	}
	vis := b.visible()
	b.tab = len(vis)
	for pos, i := range vis {
		if i == answered {
			b.tab = pos + 1
		}
	}
	return Open
}

func (b *Block) updateSubmit(k tea.KeyPressMsg) Outcome {
	switch {
	case key.Matches(k, Keys.Back), key.Matches(k, Keys.Prev), key.Matches(k, Keys.Left):
		b.tab--
	case key.Matches(k, Keys.Up):
		b.sub = 0
	case key.Matches(k, Keys.Down):
		b.sub = 1
	case key.Matches(k, Keys.Pick):
		if b.sub == 0 {
			return Submitted
		}
		b.tab, b.sub = 0, 0
	default:
		switch digit(k) {
		case 1:
			return Submitted
		case 2:
			b.tab, b.sub = 0, 0
		}
	}
	return Open
}

// Replace puts q in place of the question at i, its answer and cursor back
// at the start: an earlier answer changed what it offers.
func (b *Block) Replace(i int, q Question) {
	b.Questions[i] = q
	b.Answers[i] = q.start()
	b.cursor[i] = q.firstRow()
	b.field[i] = Field{}
	b.err[i] = ""
}

// Find is the question with the id, for Refresh to hide or reword it.
func (b *Block) Find(id string) *Question {
	for i := range b.Questions {
		if b.Questions[i].ID == id {
			return &b.Questions[i]
		}
	}
	return nil
}

// Get is a question and its answer by id; ok is false for a question the
// block does not have or has hidden.
func (b *Block) Get(id string) (q Question, a Answer, ok bool) {
	for i := range b.Questions {
		if b.Questions[i].ID == id && !b.Questions[i].Hidden {
			return b.Questions[i], b.Answers[i], true
		}
	}
	return Question{}, Answer{}, false
}

// Value is the stored value of an answer, empty for a question not asked.
func (b *Block) Value(id string) string {
	q, a, ok := b.Get(id)
	if !ok {
		return ""
	}
	return q.Value(a)
}

// Summary is the entry the feed keeps of a submitted block: every question
// and its answer, a secret as SecretShown.
func (b *Block) Summary(t Theme, width int) string {
	var lines []string
	for _, i := range b.visible() {
		q := b.Questions[i]
		lines = append(lines, "· "+q.Prompt+" → "+t.Accent.Render(q.Show(b.Answers[i])))
	}
	return t.Entry(Asked, b.Title, lines, width)
}

// View draws the block. height, when above zero, is the room it may take;
// a list of options taller than that scrolls with the cursor.
func (b *Block) View(t Theme, width, height int) string {
	var head, foot []string
	if !b.Bare {
		head = append(head, t.Dim.Render(strings.Repeat("─", width)))
		if tabs := b.tabRow(t, width); tabs != "" {
			head = append(head, tabs, "")
		}
	}

	i := b.Current()
	if i < 0 {
		return strings.Join(append(head, b.submitView(t, width)...), "\n")
	}
	q := b.Questions[i]
	head = append(head, indent(t.Strong.Render(q.Prompt), " ", " ", width)...)
	if q.Note != "" {
		head = append(head, indent(t.Dim.Render(q.Note), " ", " ", width)...)
	}
	head = append(head, "")

	if b.err[i] != "" {
		foot = append(foot, indent(t.Fail.Render("✗ "+b.err[i]), " ", "   ", width)...)
	}
	foot = append(foot, "")
	foot = append(foot, indent(t.Dim.Render(b.hint(q)), " ", " ", width)...)

	room := 0
	if height > 0 {
		room = max(3, height-len(head)-len(foot))
	}
	rows := b.rowsView(t, i, width)
	body := window(t, rows, b.cursor[i], room)
	return strings.Join(append(append(head, body...), foot...), "\n")
}

func (b *Block) hint(q Question) string {
	esc := "Esc to go back"
	if b.tab == 0 && b.EscHint != "" {
		esc = "Esc to " + b.EscHint
	}
	nav := "Tab/Arrow keys to navigate"
	if !b.tabbed() {
		nav = "↑/↓ to navigate"
	}
	switch q.Kind {
	case Multi:
		if !b.tabbed() {
			nav = "↑/↓ to move"
		}
		return "Space to toggle · Enter to accept · " + nav + " · " + esc
	case Text, Secret:
		if !b.tabbed() {
			return "Enter to confirm · " + esc
		}
		return "Enter to confirm · Tab to navigate · " + esc
	}
	return "Enter to select · " + nav + " · " + esc
}

// tabRow is the row of tabs: ☒ answered, ■ in focus and not yet answered,
// ☐ ahead, ✔ Submit. A row wider than the terminal shrinks to the tab in
// focus and its place among the rest.
func (b *Block) tabRow(t Theme, width int) string {
	vis := b.visible()
	if len(vis) == 0 {
		return ""
	}
	if len(vis) == 1 {
		q := b.Questions[vis[0]]
		if q.Tab == "" {
			return ""
		}
		return " " + t.Focus.Render(" ■ "+q.Tab+" ")
	}
	var tabs []string
	for pos, i := range vis {
		glyph := "☐"
		switch {
		case b.Answers[i].Given:
			glyph = "☒"
		case pos == b.tab:
			glyph = "■"
		}
		tabs = append(tabs, glyph+" "+b.Questions[i].Tab)
	}
	tabs = append(tabs, "✔ Submit")

	var full strings.Builder
	full.WriteString(" ← ")
	plain := " ← "
	for pos, tab := range tabs {
		cell := " " + tab + " "
		plain += cell
		if pos == b.tab {
			cell = t.Focus.Render(cell)
		}
		full.WriteString(cell)
	}
	plain += " →"
	full.WriteString(" →")
	if Width(plain) <= width {
		return full.String()
	}
	where := fmt.Sprintf(" · %d of %d", b.tab+1, len(tabs))
	return " ← " + t.Focus.Render(" "+fit(tabs[b.tab], width-Width(where)-10)+" ") + t.Dim.Render(where) + " →"
}

func (b *Block) submitView(t Theme, width int) []string {
	vis := b.visible()
	tabW := 0
	for _, i := range vis {
		tabW = max(tabW, Width(b.Questions[i].Tab))
	}
	out := []string{" " + t.Strong.Render("Review your answers"), ""}
	for _, i := range vis {
		q := b.Questions[i]
		lead := "  ● " + pad(q.Tab, tabW) + "  → "
		out = append(out, indent(t.Accent.Render(q.Show(b.Answers[i])), lead, strings.Repeat(" ", Width(lead)), width)...)
	}
	out = append(out, "", " "+t.Strong.Render("Ready to submit your answers?"), "")
	for n, label := range []string{"Submit answers", "Change an answer"} {
		cur := "  "
		if n == b.sub {
			cur = t.Accent.Render("❯") + " "
			label = t.Accent.Render(label)
		}
		out = append(out, " "+cur+fmt.Sprintf("%d. ", n+1)+label)
	}
	return append(out, "", " "+t.Dim.Render("Enter to select · Esc to go back"))
}

// rowsView is every row of the question as lines, one slice a row, so that a
// window over them never cuts a row in half.
func (b *Block) rowsView(t Theme, i, width int) [][]string {
	q, a, c := b.Questions[i], b.Answers[i], b.cursor[i]
	f := &b.field[i]
	cursor := func(row int) string {
		if row == c {
			return t.Accent.Render("❯") + " "
		}
		return "  "
	}

	var rows [][]string
	switch q.Kind {
	case Text, Secret:
		lead := " " + cursor(0)
		return [][]string{{lead + f.View(t, q.Kind == Secret, q.Placeholder, width-Width(lead)-1)}}

	case Single:
		nw := len(fmt.Sprint(q.rows()))
		for n, o := range q.Options {
			num := fmt.Sprintf("%*d. ", nw, n+1)
			lead := " " + cursor(n) + num
			label := o.Label
			if n == c {
				label = t.Accent.Render(label)
			}
			if a.Given && a.Choice == n {
				label += " " + t.OK.Render("✔")
			}
			row := indent(label, lead, strings.Repeat(" ", Width(lead)), width)
			if o.Detail != "" {
				row = append(row, indent(t.Dim.Render(o.Detail), strings.Repeat(" ", Width(lead)), strings.Repeat(" ", Width(lead)), width)...)
			}
			rows = append(rows, row)
		}
		if q.Own != "" {
			n := len(q.Options)
			lead := " " + cursor(n) + fmt.Sprintf("%*d. ", nw, n+1)
			rows = append(rows, []string{lead + b.ownView(t, i, width-Width(lead)-1)})
		}

	case Multi:
		labelW := 0
		for _, o := range q.Options {
			labelW = max(labelW, Width(o.Label))
		}
		labelW = min(labelW, 22)
		detailAt := 1 + 2 + 4 + labelW + 2
		inline := width-detailAt >= 20
		group := ""
		for n, o := range q.Options {
			var row []string
			if o.Group != "" && o.Group != group {
				group = o.Group
				row = append(row, "   "+t.Strong.Render(group))
			}
			box := "[ ]"
			if n < len(a.Picks) && a.Picks[n] {
				box = "[" + t.Accent.Render("✔") + "]"
				if o.Locked {
					box = t.Dim.Render("[✔]")
				}
			}
			label := o.Label
			if n == c {
				label = t.Accent.Render(label)
			}
			lead := " " + cursor(n) + box + " "
			switch {
			case o.Detail == "":
				row = append(row, lead+label)
			case inline && Width(o.Label) <= labelW:
				gap := strings.Repeat(" ", detailAt)
				detail := indent(t.Dim.Render(o.Detail), gap, gap, width)
				row = append(row, lead+pad(label, labelW)+"  "+strings.TrimLeft(detail[0], " "))
				row = append(row, detail[1:]...)
			default:
				row = append(row, lead+label)
				gap := strings.Repeat(" ", 8)
				row = append(row, indent(t.Dim.Render(o.Detail), gap, gap, width)...)
			}
			rows = append(rows, row)
		}
		if q.Own != "" {
			n := len(q.Options)
			lead := " " + cursor(n)
			rows = append(rows, []string{lead + b.ownView(t, i, width-Width(lead)-1)})
		}
		if q.Skip != "" {
			n := q.skipRow()
			label := t.Dim.Render(q.Skip)
			if n == c {
				label = t.Accent.Render(q.Skip)
			}
			rows = append(rows, []string{" " + cursor(n) + label})
		}
	}
	return rows
}

// ownView is the own line: its label while empty and out of focus, the
// typed text otherwise, with the caret when in focus.
func (b *Block) ownView(t Theme, i, width int) string {
	q, f := b.Questions[i], &b.field[i]
	if b.inField(i) {
		return f.View(t, false, q.Own, width)
	}
	if f.Value() != "" {
		return fit(f.Value(), width)
	}
	return t.Dim.Render(q.Own)
}

// window is the rows that fit into room lines, around the row of the cursor;
// what is cut off above and below is counted. room under one fits all.
func window(t Theme, rows [][]string, cursor, room int) []string {
	total := 0
	for _, r := range rows {
		total += len(r)
	}
	flat := func(from, to int) []string {
		var out []string
		for _, r := range rows[from:to] {
			out = append(out, r...)
		}
		return out
	}
	if room < 1 || total <= room || len(rows) == 0 {
		return flat(0, len(rows))
	}
	cursor = min(max(cursor, 0), len(rows)-1)
	from, to := cursor, cursor+1
	used := len(rows[cursor])
	budget := room - 2 // the two lines that count what is cut off
	for grew := true; grew; {
		grew = false
		if to < len(rows) && used+len(rows[to]) <= budget {
			used += len(rows[to])
			to++
			grew = true
		}
		if from > 0 && used+len(rows[from-1]) <= budget {
			from--
			used += len(rows[from])
			grew = true
		}
	}
	out := flat(from, to)
	if from > 0 {
		out = append([]string{t.Dim.Render(fmt.Sprintf("   ↑ %d more", from))}, out...)
	}
	if to < len(rows) {
		out = append(out, t.Dim.Render(fmt.Sprintf("   ↓ %d more", len(rows)-to)))
	}
	return out
}
