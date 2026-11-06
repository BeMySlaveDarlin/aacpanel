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
