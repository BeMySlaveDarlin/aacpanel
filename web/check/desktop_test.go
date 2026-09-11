package check

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDesktopSharesContourRules(t *testing.T) {
	files := srcFiles(t)
	src, ok := files["src/desktop/sessions.js"]
	if !ok {
		t.Fatal("src/desktop/sessions.js not found — the test is looking in the wrong place")
	}

	for _, want := range []struct{ call, harm string }{
		{"pageNames(map, limits)", "the desktop will again name contours after the collector instead of the map labels"},
		{"contoursOf(map, all)", "sessions of a renamed contour will land on a foreign page"},
	} {
		if !strings.Contains(src, want.call) {
			t.Errorf("the desktop does not call %s — %s", want.call, want.harm)
		}
	}

	if !strings.Contains(src, "snapshot.profileMap") {
		t.Error("the desktop takes the map from somewhere other than the snapshot — /api/profiles has another shape, and the join falls apart silently")
	}
}

func TestDesktopRulesStayInsideMediaQuery(t *testing.T) {
	names, err := filepath.Glob(filepath.Join(webDir, "src", "css", "desktop*.css"))
	if err != nil || len(names) == 0 {
		t.Fatalf("desktop styles not found: %v", err)
	}
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("desktop styles: %v", err)
		}
		if outside := rulesOutsideMedia(string(raw)); outside != "" {
			t.Errorf("%s: a rule outside the media query — it changes the phone: %s", filepath.Base(name), outside)
		}
	}

	for _, name := range []string{".deskshell", ".dkbody", ".dkleft", ".dkcenter", ".dkright", ".dktop"} {
		if strings.Count(cssSrc(t), name) == 0 {
			t.Errorf("shell class %s is gone from the styles — the desktop layout is built out of something else", name)
		}
	}
}

func rulesOutsideMedia(css string) string {
	css = cssWithoutComments(css)
	depth, media := 0, 0
	line := 1
	for i := 0; i < len(css); i++ {
		if css[i] == '\n' {
			line++
			continue
		}
		if css[i] == '{' {
			depth++
			if depth == 1 {
				head := strings.TrimSpace(css[max(0, strings.LastIndex(css[:i], "}")+1):i])
				if strings.HasPrefix(head, "@media") {
					media = depth
					continue
				}
				return "line " + strconv.Itoa(line) + ": " + firstLine(head)
			}
			continue
		}
		if css[i] == '}' {
			if depth == media {
				media = 0
			}
			depth--
		}
	}
	return ""
}

func TestDesktopHidesOnlyWhereThereIsAPointer(t *testing.T) {
	names, err := filepath.Glob(filepath.Join(webDir, "src", "css", "desktop*.css"))
	if err != nil || len(names) == 0 {
		t.Fatalf("desktop styles not found: %v", err)
	}
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("desktop styles: %v", err)
		}
		for _, sel := range hiddenOutsidePointerMedia(string(raw)) {
			t.Errorf("%s: %q hides until hover outside the pointer axis — where there is no pointer "+
				"(a phone, a remote desktop) what is hidden cannot be reached at all", filepath.Base(name), sel)
		}
	}
}

func hiddenOutsidePointerMedia(css string) []string {
	type rule struct {
		sel     string
		body    string
		pointer bool
	}
	css = cssWithoutComments(css)
	var stack []string
	var rules []rule
	start := 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			head := strings.TrimSpace(css[start:i])
			stack = append(stack, head)
			if !strings.HasPrefix(head, "@") {
				body := css[i+1:]
				if cut := strings.Index(body, "}"); cut >= 0 {
					body = body[:cut]
				}
				rules = append(rules, rule{sel: head, body: body, pointer: pointerAware(stack)})
			}
			start = i + 1
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			start = i + 1
		case ';':
			start = i + 1
		}
	}

	revealed := map[string]bool{}
	for _, r := range rules {
		if !strings.Contains(r.sel, ":hover") {
			continue
		}
		if !strings.Contains(r.body, "opacity: 1") && !strings.Contains(r.body, "visibility: visible") {
			continue
		}
		fields := strings.Fields(r.sel)
		last := fields[len(fields)-1]
		if at := strings.Index(last, ":"); at > 0 {
			last = last[:at]
		}
		revealed[strings.TrimSuffix(last, ",")] = true
	}

	var bad []string
	for _, r := range rules {
		if strings.Contains(r.sel, ":hover") || r.pointer {
			continue
		}
		if strings.Contains(r.sel, "::") {
			continue
		}
		if !strings.Contains(r.body, "opacity: 0") && !strings.Contains(r.body, "visibility: hidden") {
			continue
		}
		for name := range revealed {
			if strings.Contains(r.sel, name) {
				bad = append(bad, firstLine(r.sel))
				break
			}
		}
	}
	return bad
}

func pointerAware(stack []string) bool {
	for _, head := range stack {
		if strings.HasPrefix(head, "@media") && strings.Contains(head, "hover") {
			return true
		}
	}
	return false
}

func TestDesktopKeysNeverStealTypedText(t *testing.T) {
	shell := srcFiles(t)["src/desktop/shell.js"]
	if shell == "" {
		t.Fatal("src/desktop/shell.js not found")
	}
	body := stripComments(shell)
	if !strings.Contains(body, `addEventListener("keydown"`) {
		t.Fatal("the shell has no key handler — the digits on session rows label something that does not happen")
	}
	focus := srcFiles(t)["src/ui/focus.js"]
	if focus == "" {
		t.Fatal("src/ui/focus.js not found — the question of whether the person is typing in a field has no owner")
	}
	for _, want := range []string{"activeElement", "isContentEditable", "TEXTAREA", "INPUT"} {
		if !strings.Contains(stripComments(focus), want) {
			t.Errorf("the typing check does not ask about %s — a digit will switch the session in the middle of a typed reply", want)
		}
	}
	if !strings.Contains(body, `from "../ui/focus.js"`) {
		t.Error("the shell does not take the typing check from the shared place — a second copy will drift from the first silently")
	}
	for _, want := range []string{"e.altKey", "e.ctrlKey", "e.metaKey"} {
		if !strings.Contains(body, want) {
			t.Errorf("the keyboard does not let %s through — the shortcut goes to the panel instead of the browser", want)
		}
	}
	if !strings.Contains(body, "onOrder=") {
		t.Error("the shell does not take the session order from the column — the key and the label on the row will disagree")
	}
	keys := body[strings.Index(body, `const onKey = `):]
	if cut := strings.Index(keys, `addEventListener("keydown"`); cut > 0 {
		keys = keys[:cut]
	}
	if !strings.Contains(keys, "typing()") {
		t.Error("the handler does not ask whether the person is typing in a field — a digit will switch the session in the middle of a typed reply")
	}
	if !strings.Contains(keys, "terminalOnScreen()") {
		t.Error("the handler does not ask whether the terminal is on screen: there a digit is an answer " +
			"to the session question and Escape interrupts the work, and neither may be intercepted")
	}
	if strings.Contains(keys, "snapshot") {
		t.Error("the key handler walks the snapshot itself — that is a second session order alongside the drawn one")
	}
}

func TestDesktopChatKeepsItsOwnColumn(t *testing.T) {
	shell := srcFiles(t)["src/desktop/shell.js"]
	if shell == "" {
		t.Fatal("src/desktop/shell.js not found")
	}
	at := strings.Index(shell, "<${Chat}")
	if at < 0 {
		t.Fatal("the desktop shell has no chat at all")
	}
	before := shell[max(0, at-400):at]
	if !strings.Contains(before, `class="dkcenter dkchat"`) {
		t.Error("the chat is not wrapped in its own column — its roots become children of the grid and the feed moves into the session column")
	}
	if !strings.Contains(withoutComments(cssSrc(t)), "--chat-measure:") {
		t.Error("the chat side margin is gone — the text of the feed and the composer lands against the window edges")
	}
	if strings.Contains(cssBlock(t, cssSrc(t), ".dkchat"), "max-width") {
		t.Error("the chat has a second line measure on top of --chat-measure — replies collapse into a column of letters")
	}
}

func TestDesktopChatHeadIsTheSectionHead(t *testing.T) {
	src := screenSrc(t, "src/screens/chat.js")

	if !strings.Contains(src, "<${DeskHead}") {
		t.Fatal("on a wide screen the chat draws the layer header: " +
			"the back arrow leads into an empty center and the session path is not there at all")
	}
	if !strings.Contains(src, "dkheadpath") || !strings.Contains(src, "cwd") {
		t.Error("the chat header does not name the directory: it does not say which project the chat runs in")
	}

	css := cssSrc(t)
	for _, name := range []string{".dkhead", ".dkheadtop", ".dkheadbot", ".dkfact", ".dkdot", ".viewsw", ".viewbtn"} {
		if !strings.Contains(css, name+" {") {
			t.Errorf("class %s is gone from the styles — the chat header is built out of what "+
				"does not exist and arrives unstyled", name)
		}
		if !strings.Contains(src, strings.TrimPrefix(name, ".")) {
			t.Errorf("the chat header no longer uses %s — that is a second header "+
				"resembling the first, and they drift apart on the first edit of the neighbouring file", name)
		}
	}
}

func TestChatHeadRulesNeverTouchThePhone(t *testing.T) {
	const chatCSS = "src/css/chat.css"
	raw, err := os.ReadFile(webPath(chatCSS))
	if err != nil {
		t.Fatalf("chat styles: %v", err)
	}
	css := cssWithoutComments(string(raw))

	at := strings.Index(css, "@media (min-width: 1100px) {")
	if at < 0 {
		t.Fatalf("%s: there is no wide-screen media query at all — nothing to check", chatCSS)
	}
	before, after := css[:at], css[at:]

	for _, sel := range []string{".dkhead", ".dkchat", ".dkcenter", ".dkfact"} {
		if strings.Contains(before, sel) {
			t.Errorf("%s: %s is declared outside the media query — a desktop header rule changes the phone",
				chatCSS, sel)
		}
	}
	for _, sel := range []string{".dkchatname", ".dkfact"} {
		if !strings.Contains(after, sel) {
			t.Errorf("%s: %s is gone from the styles — the wide-screen header is built without it", chatCSS, sel)
		}
	}
}

func TestBackButtonIsQuietOnWideScreens(t *testing.T) {
	const layers = "src/css/layers.css"
	raw, err := os.ReadFile(webPath(layers))
	if err != nil {
		t.Fatalf("layer styles: %v", err)
	}
	css := cssWithoutComments(string(raw))

	at := strings.Index(css, "@media (min-width: 1100px) {")
	if at < 0 {
		t.Fatalf("%s: there is no wide-screen rule at all — the back button stays a slab "+
			"among the flat headers of the panel", layers)
	}
	if !strings.Contains(cssBlock(t, css[:at], ".pback"), "background: var(--glass)") {
		t.Errorf("%s: on the phone the back button lost its backdrop — the tap target "+
			"is no longer visible where a finger aims for it", layers)
	}
	quiet := cssBlock(t, css[at:], ".pback")
	for _, want := range []string{"background: none", "box-shadow: none"} {
		if !strings.Contains(quiet, want) {
			t.Errorf("%s: on a wide screen the back button does not lose %q — it again reads "+
				"as the only button of its kind in the panel", layers, want)
		}
	}
}
