package check

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// cssRule is one declaration block with the place it was written.
type cssRule struct {
	file string
	sel  string
	body string
	line int
	at   int
}

// cssRules reads every declaration block of every stylesheet. At-rules keep
// their own braces, so a rule inside a media query is returned like any other.
func cssRules(t *testing.T) []cssRule {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(webDir, "src", "css", "*.css"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no stylesheets found — the test is useless: %v", err)
	}
	sort.Strings(names)

	var out []cssRule
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		css := cssWithoutComments(string(raw))
		depth, start, line := 0, 0, 1
		for i := 0; i < len(css); i++ {
			switch css[i] {
			case '\n':
				line++
			case '{':
				head := strings.TrimSpace(css[start:i])
				depth++
				if !strings.HasPrefix(head, "@") {
					body := css[i+1:]
					if cut := strings.IndexAny(body, "{}"); cut >= 0 {
						body = body[:cut]
					}
					out = append(out, cssRule{
						file: filepath.Base(name), sel: firstLine(head), body: body, line: line, at: i,
					})
				}
				start = i + 1
			case '}':
				depth--
				start = i + 1
			case ';':
				start = i + 1
			}
		}
	}
	return out
}

func (r cssRule) has(prop string) bool { return strings.Contains(r.body, prop) }

func (r cssRule) where() string { return r.file + ":" + strconv.Itoa(r.line) + " " + r.sel }

// TestEllipsisAlwaysHasSomethingToClip guards the mistake that leaves no trace:
// text-overflow draws nothing at all unless the same element also hides its
// overflow, so the text simply runs out of the box and nobody sees a reason.
func TestEllipsisAlwaysHasSomethingToClip(t *testing.T) {
	clipped := map[string]bool{}
	for _, r := range cssRules(t) {
		if r.has("overflow: hidden") || r.has("overflow-x: hidden") {
			clipped[r.file+"|"+r.sel] = true
		}
	}
	for _, r := range cssRules(t) {
		if !r.has("text-overflow: ellipsis") {
			continue
		}
		if r.has("overflow: hidden") || r.has("overflow-x: hidden") || clipped[r.file+"|"+r.sel] {
			continue
		}
		t.Errorf("%s: text-overflow without a clip — the ellipsis is never drawn "+
			"and the text runs past the edge instead", r.where())
	}
}

// TestNothingBreaksInTheMiddleOfAWord keeps paths and names off break-all,
// which splits at whatever character the line ends on even when a slash or a
// hyphen one step away offered a place to break.
func TestNothingBreaksInTheMiddleOfAWord(t *testing.T) {
	for _, r := range cssRules(t) {
		if r.has("word-break: break-all") {
			t.Errorf("%s: word-break: break-all cuts through the middle of a word — "+
				"overflow-wrap: anywhere breaks at a slash or a hyphen first", r.where())
		}
		if r.has("word-break: break-word") {
			t.Errorf("%s: word-break: break-word is the retired spelling of "+
				"overflow-wrap: anywhere — one name for the same thing keeps the styles readable", r.where())
		}
	}
}

// namesInLists are the rows where the text is a name, a path or a file: what
// tells one row from the next lives at the end of it, so a line shortened from
// the right leaves rows that cannot be told apart.
var namesInLists = []struct {
	file, sel, holds string
}{
	{"feed.css", ".mfname", "the name of an attached file"},
	{"profiles.css", ".pfgname", "the name of a project group"},
	{"sessions.css", ".srow .nm", "the name of a session, behind the group prefix"},
	{"calls.css", ".cnname", "the name of a tool call"},
}

func TestNamesInListsAreNotShortened(t *testing.T) {
	rules := cssRules(t)
	for _, want := range namesInLists {
		found := false
		for _, r := range rules {
			if r.file != want.file || r.sel != want.sel {
				continue
			}
			found = true
			if r.has("text-overflow: ellipsis") {
				t.Errorf("%s holds %s and is shortened with an ellipsis — the tail is "+
					"exactly the part that names the row", r.where(), want.holds)
			}
			if r.has("white-space: nowrap") {
				t.Errorf("%s holds %s and refuses to wrap — a long one is drawn over "+
					"its neighbour on the row", r.where(), want.holds)
			}
			if !r.has("overflow-wrap: anywhere") {
				t.Errorf("%s holds %s but names no place to break — an unbroken name "+
					"pushes the row wider than the screen", r.where(), want.holds)
			}
		}
		if !found {
			t.Errorf("%s: rule %s is gone — the row that carries %s is styled somewhere else now",
				want.file, want.sel, want.holds)
		}
	}
}

// TestTheStatusBarNeverClipsItsChips: the bar carries words and buttons side by
// side. text-overflow can shorten a word but never a button, so a bar that hid
// its overflow would saw the address chip through its own border.
func TestTheStatusBarNeverClipsItsChips(t *testing.T) {
	rules := cssRules(t)
	var bar, status *cssRule
	for i, r := range rules {
		if r.file != "header.css" {
			continue
		}
		switch r.sel {
		case ".statusbar":
			bar = &rules[i]
		case ".statusbar .status":
			status = &rules[i]
		}
	}
	if bar == nil || status == nil {
		t.Fatal("header.css has no .statusbar or no .statusbar .status — the header is built out of something else")
	}
	for _, r := range []*cssRule{bar, status} {
		if r.has("overflow: hidden") {
			t.Errorf("%s hides its overflow — the address chip is cut through its border "+
				"instead of moving to the next line", r.where())
		}
		if !r.has("flex-wrap: wrap") {
			t.Errorf("%s does not wrap — when the title and the icons leave it too little room "+
				"there is nowhere for the chips to go", r.where())
		}
	}
}

// TestTheWrapFixesReachThePhone: these files are written phone first and the
// wide screen only adds to them at the end. A rule parked inside the wide-screen
// query would leave the phone exactly as it was.
func TestTheWrapFixesReachThePhone(t *testing.T) {
	for _, want := range namesInLists {
		raw, err := os.ReadFile(webPath("src/css/" + want.file))
		if err != nil {
			t.Fatalf("%s: %v", want.file, err)
		}
		css := cssWithoutComments(string(raw))
		wide := strings.Index(css, "@media (min-width: 1100px)")
		if wide < 0 {
			continue
		}
		if at := strings.Index(css, want.sel+" {"); at > wide {
			t.Errorf("%s: %s is declared inside the wide-screen query — the phone, "+
				"where the row is narrow, keeps the old rule", want.file, want.sel)
		}
	}
}
