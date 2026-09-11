package check

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestTerminalReachesWhoeverServerAllows(t *testing.T) {
	chat := screenSrc(t, "src/screens/chat.js")
	if !strings.Contains(chat, "term.ok && Boolean(live)") {
		t.Error("showing the terminal is not tied at once to the server answer and to a live session: " +
			"without the first it promises what does not exist, without the second it reaches into an archived chat")
	}

	const termFile = "src/css/term.css"
	raw, err := os.ReadFile(webPath(termFile))
	if err != nil {
		t.Fatalf("terminal styles: %v", err)
	}
	if strings.Contains(cssWithoutComments(string(raw)), "min-width: 1100px") {
		t.Error(termFile + ": the terminal styles are locked inside the wide-screen media query — " +
			"on a phone it arrives without a single rule")
	}

	for _, name := range []string{".viewsw", ".viewbtn", ".winbtn"} {
		if !strings.Contains(cssBlockFile(t, "src/css/chat.css", name), "display") {
			t.Errorf("the rules for %s are not in the chat styles — the button is needed by both headers, "+
				"and in the desktop file the phone will not see them", name)
		}
	}
}

func TestLiveChatOpensInTerminal(t *testing.T) {
	src := screenSrc(t, "src/screens/chat.js")

	if !strings.Contains(src, "canTerm = term.ok && Boolean(live)") {
		t.Error("terminal availability is computed from something other than the server answer and " +
			"a live session: without the first the panel promises what does not exist, without the " +
			"second it tries to attach to an archived chat")
	}
	if !strings.Contains(src, "useViewPick(canTerm, wide)") {
		t.Error("the view is computed from something other than terminal availability and width: " +
			"a default written into state gets stuck on the feed — the server answer arrives after the first frame")
	}
	if !strings.Contains(src, `wide ? "term" : "feed"`) {
		t.Error("the default view does not depend on width: on a phone a forty-column terminal " +
			"opens, on a monitor the feed instead of the session at work")
	}
}

func TestTerminalIsItsOwnComposer(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	src := srcFiles(t)[chatFile]
	if src == "" {
		t.Fatalf("%s not found", chatFile)
	}
	body := stripComments(src)

	if n := strings.Count(body, `live && view !== "term" && html`); n != 2 {
		t.Errorf("%s: %d of the two blocks are drawn under the feed view — the composer and the deck "+
			"have to leave together: the first becomes a second input field into the same session, "+
			"the second repeats with counters what the TUI lists by name", chatFile, n)
	}
	if strings.Contains(body, "chatterm") {
		t.Errorf("%s: something is drawn under the terminal again — the chatterm class exists "+
			"for that alone", chatFile)
	}
}

func TestTerminalFallbacksMatchTokens(t *testing.T) {
	const (
		jsFile  = "src/screens/chat/term.js"
		cssFile = "src/css/term.css"
	)
	js := srcFiles(t)[jsFile]
	if js == "" {
		t.Fatalf("%s not found", jsFile)
	}
	raw, err := os.ReadFile(webPath(cssFile))
	if err != nil {
		t.Fatalf("terminal styles: %v", err)
	}
	css := cssWithoutComments(string(raw))

	at := strings.Index(css, ".termwrap {")
	if at < 0 {
		t.Fatalf("%s: there is no .termwrap block at all — nothing to check", cssFile)
	}
	end := strings.Index(css[at:], "\n}")
	if end < 0 {
		t.Fatalf("%s: the .termwrap block is not closed", cssFile)
	}
	block := css[at : at+end]

	declared := map[string]string{}
	for _, m := range regexp.MustCompile(`(--term[a-z-]*)\s*:\s*([^;]+);`).FindAllStringSubmatch(block, -1) {
		declared[m[1]] = strings.TrimSpace(m[2])
	}
	if len(declared) < 16 {
		t.Fatalf("%s: .termwrap declares %d terminal tokens — sixteen ANSI ones have to be there",
			cssFile, len(declared))
	}

	used := map[string]string{}
	for _, m := range regexp.MustCompile(`\["[a-zA-Z]+",\s*"(--term[a-z-]*)",\s*"([^"]+)"\]`).FindAllStringSubmatch(js, -1) {
		used[m[1]] = m[2]
	}
	for _, m := range regexp.MustCompile(`pick\("(--term[a-z-]*)",\s*"([^"]+)"\)`).FindAllStringSubmatch(js, -1) {
		used[m[1]] = m[2]
	}
	if len(used) == 0 {
		t.Fatalf("%s: no palette fallbacks found — the check is looking in the wrong place", jsFile)
	}

	for token, fallback := range used {
		value, ok := declared[token]
		if !ok {
			t.Errorf("%s: takes %s, which is not in %s — the color will always be the fallback",
				jsFile, token, cssFile)
			continue
		}
		if value != fallback {
			t.Errorf("%s: fallback %s = %q while the token = %q — the substitution gives another color",
				jsFile, token, fallback, value)
		}
	}
	for token := range declared {
		if _, ok := used[token]; !ok {
			t.Errorf("%s: nobody reads token %s — the palette is declared wider than the emulator asks for",
				cssFile, token)
		}
	}
}

func TestTerminalScrollRulesReachThePhone(t *testing.T) {
	const cssFile = "src/css/term.css"
	block := cssBlockFile(t, cssFile, ".termscreen")
	if block == "" {
		t.Fatalf("%s: there is no .termscreen block — the swipe has nothing to stand on", cssFile)
	}
	if !strings.Contains(block, "touch-action: none") {
		t.Errorf("%s: .termscreen has no touch-action: none — a finger drags the page, not the screen", cssFile)
	}

	raw, err := os.ReadFile(webPath(cssFile))
	if err != nil {
		t.Fatalf("%s: %v", cssFile, err)
	}
	css := cssWithoutComments(string(raw))
	at := strings.Index(css, ".termscreen {")
	if media := strings.LastIndex(css[:at], "@media"); media >= 0 &&
		strings.Count(css[media:at], "{") > strings.Count(css[media:at], "}") {
		t.Errorf("%s: the .termscreen rule sits inside a media query — the phone will not see it", cssFile)
	}

	const jsFile = "src/screens/chat/term.js"
	js := stripComments(srcFiles(t)[jsFile])
	if js == "" {
		t.Fatalf("%s not found", jsFile)
	}
	for _, want := range []string{`"touchmove"`, `new WheelEvent("wheel"`, `mouseTrackingMode`} {
		if !strings.Contains(js, want) {
			t.Errorf("%s: no %s — the swipe does not reach tmux as a wheel", jsFile, want)
		}
	}
	if strings.Contains(js, "scrollLines(") {
		t.Errorf("%s: the swipe scrolls the emulator buffer (scrollLines) — it is empty, the screen lives in tmux", jsFile)
	}
}

func TestTerminalTakesFocusAfterItIsShown(t *testing.T) {
	src := srcFiles(t)["src/screens/chat/term.js"]
	if src == "" {
		t.Fatal("src/screens/chat/term.js not found")
	}
	body := stripComments(src)

	off := cssBlockFile(t, "src/css/term.css", ".termwrap.off .termscreen")
	if !strings.Contains(off, "display: none") {
		t.Error("the screen of a terminal that is not shown is no longer hidden: the price of moving " +
			"the focus into an effect was exactly this rule, and without it the invariant guards nothing")
	}

	at := strings.Index(body, `es.addEventListener("ready"`)
	if at < 0 {
		t.Fatal("the terminal has no ready handler — the id for input arrives with the first event, and without it the terminal does not write")
	}
	ready := body[at:]
	if cut := strings.Index(ready, `es.onmessage`); cut > 0 {
		ready = ready[:cut]
	}
	if strings.Contains(ready, "focus(") {
		t.Error("the focus is set from the ready handler: the state has not reached the DOM yet, " +
			"the screen is under display:none, and focus() silently does nothing")
	}

	at = strings.Index(body, `if (state.kind !== "live") return;`)
	if at < 0 {
		t.Fatal("the terminal focus is not tied to the stream state: then it sits again where " +
			"the screen is not visible yet")
	}
	effect := body[at:]
	if cut := strings.Index(effect, "}, [state.kind]);"); cut > 0 {
		effect = effect[:cut]
	} else {
		t.Fatal("the focus effect does not depend on the stream state — it fires once and at the wrong moment")
	}
	if !strings.Contains(effect, "mayFocus()") {
		t.Error("the terminal takes the focus without asking: on a phone that is a keyboard over half " +
			"the screen, on a wide one a cursor yanked out of a field where a person is already typing")
	}
	if !strings.Contains(effect, "focus()") {
		t.Error("the effect focuses nothing — the cursor stays on the button that opened the chat")
	}
}

func TestTerminalKeepsTheKeyboardWhileItIsOnScreen(t *testing.T) {
	files := srcFiles(t)

	focus := stripComments(files["src/ui/focus.js"])
	if !strings.Contains(focus, `querySelector(".termwrap:not(.off)")`) {
		t.Error("a terminal on screen is recognised by something other than the live bridge: a detached " +
			"one keeps its node, and the panel would hand the keys to what is not on screen")
	}

	term := stripComments(files["src/screens/chat/term.js"])
	at := strings.Index(term, "const keepFocus =")
	if at < 0 {
		t.Fatal("the terminal does not take back a lost focus: a key will not be intercepted " +
			"and will not be typed either — the press disappears silently")
	}
	keep := term[at:]
	if cut := strings.Index(keep, "screen.addEventListener"); cut > 0 {
		keep = keep[:cut]
	}
	if !strings.Contains(keep, "event.relatedTarget") {
		t.Error("the focus comes back from anywhere: someone who clicked into the search field " +
			"loses it right back to the terminal")
	}
	if !strings.Contains(keep, "mayFocus()") {
		t.Error("the focus comes back on a phone too: the keyboard slides out after a tap on the header")
	}
	if !strings.Contains(term, "unfocus()") {
		t.Error("the focus listener is never removed — it outlives the unmounting of the terminal")
	}
}

func TestTerminalKeysSendWhatAKeyboardWouldSend(t *testing.T) {
	const jsFile = "src/screens/chat/term.js"
	src := stripComments(srcFiles(t)[jsFile])
	if src == "" {
		t.Fatalf("%s not found", jsFile)
	}

	want := [][2]string{
		{"esc", `\x1b`},
		{"tab", `\t`},
		{"left", `\x1b[D`},
		{"up", `\x1b[A`},
		{"down", `\x1b[B`},
		{"right", `\x1b[C`},
		{"enter", `\r`},
	}
	got := regexp.MustCompile(`\{ id: "(\w+)", label: "[^"]*", bytes: "([^"]*)"`).FindAllStringSubmatch(src, -1)
	if len(got) != len(want) {
		t.Fatalf("%s: %d keys in the row instead of %d — the set is closed and changes by decision, "+
			"not along the way", jsFile, len(got), len(want))
	}
	for i, m := range got {
		if m[1] != want[i][0] || m[2] != want[i][1] {
			t.Errorf("%s: key #%d — %q sends %q, expected %q → %q",
				jsFile, i, m[1], m[2], want[i][0], want[i][1])
		}
	}
	for what, bytes := range map[string]string{"Shift+Tab": `\x1b[Z`, "Ctrl+C": `\x03`} {
		if strings.Contains(src, `"`+bytes+`"`) {
			t.Errorf("%s: %s showed up in the row — it is left out not by oversight "+
				"but by decision: its price is not the price of an arrow", jsFile, what)
		}
	}

	for _, key := range []string{"esc", "tab", "enter"} {
		at := strings.Index(src, `{ id: "`+key+`"`)
		if at < 0 {
			continue
		}
		line := src[at:]
		if end := strings.Index(line, "\n"); end > 0 {
			line = line[:end]
		}
		if strings.Contains(line, "repeat: true") {
			t.Errorf("%s: holding repeats %q — repeated, it does what "+
				"nobody asked for", jsFile, key)
		}
	}
	if n := strings.Count(src, "repeat: true"); n != 4 {
		t.Errorf("%s: repeat is set on %d keys out of the four arrows", jsFile, n)
	}
	if !strings.Contains(src, "REPEAT_AFTER") || strings.Contains(src, "REPEAT_AFTER = 0") {
		t.Errorf("%s: the repeat has no start delay — a single tap becomes two presses", jsFile)
	}
	if !strings.Contains(src, "setTimeout(") || !strings.Contains(src, "setInterval(") {
		t.Errorf("%s: the repeat is built from something other than a delay and a step — holding "+
			"either does not repeat at all or repeats at once", jsFile)
	}
	for _, off := range []string{"onPointerUp=${stop}", "onPointerLeave=${stop}", "onPointerCancel=${stop}"} {
		if !strings.Contains(src, off) {
			t.Errorf("%s: no %s — the key stays pressed after the finger leaves", jsFile, off)
		}
	}
	if !strings.Contains(src, "useEffect(() => stop, [])") {
		t.Errorf("%s: the repeat timers are not cleared on unmount — they outlive "+
			"a closed terminal", jsFile)
	}

	press := arrowFn(t, jsFile, src, "    const press = (key) => (event) => {")
	if !strings.Contains(press, "event.preventDefault()") {
		t.Errorf("%s: pressing a key does not prevent the default — the focus moves onto the button "+
			"and the keyboard collapses", jsFile)
	}
	if !strings.Contains(src, "onPointerDown=${press(key)}") {
		t.Errorf("%s: the key sends on something other than pointerdown — by click the focus has already moved", jsFile)
	}

	if !strings.Contains(src, `${state.kind === "live" && !wide && html`) {
		t.Errorf("%s: the key row is drawn by something other than a live bridge and a narrow screen — "+
			"it shows up either on a monitor or where there is nothing to send to", jsFile)
	}

	const cssPath = "src/css/term.css"
	danger := cssBlockFile(t, cssPath, ".termkey.danger")
	if !strings.Contains(danger, "margin-right: auto") {
		t.Errorf("%s: Esc is not separated from the other keys — a finger landing on it by mistake "+
			"interrupts the work of the session", cssPath)
	}
	if !strings.Contains(danger, "var(--crit)") {
		t.Errorf("%s: Esc is not named by color — until it is pressed it is indistinguishable from Tab", cssPath)
	}
	key := cssBlockFile(t, cssPath, ".termkey")
	if !strings.Contains(key, "min-height: 40px") {
		t.Errorf("%s: the key has no tap target — missing an arrow inside a dialog "+
			"picks the wrong item", cssPath)
	}
	if !strings.Contains(key, "touch-action: manipulation") {
		t.Errorf("%s: a double tap on a key zooms the screen instead of pressing a second time", cssPath)
	}
}

func TestTerminalSaysWhyAFileWontPaste(t *testing.T) {
	const jsFile = "src/screens/chat/term.js"
	src := stripComments(srcFiles(t)[jsFile])
	if src == "" {
		t.Fatalf("%s not found", jsFile)
	}
	if !strings.Contains(src, `el.addEventListener("paste", refuse, true)`) {
		t.Errorf("%s: the paste is caught outside the capture phase — xterm handles it first", jsFile)
	}
	if !strings.Contains(src, `el.removeEventListener("paste", refuse, true)`) {
		t.Errorf("%s: the paste listener is never removed — it outlives a closed terminal", jsFile)
	}
	at := strings.Index(src, "        const refuse = (event) => {")
	if at < 0 {
		t.Fatal(jsFile + ": there is no refusal for pasting a file at all")
	}
	refuse := src[at:]
	if end := strings.Index(refuse, "\n    };"); end > 0 {
		refuse = refuse[:end]
	}
	if !strings.Contains(refuse, "toast(") {
		t.Errorf("%s: a file is not pasted into the terminal silently — silence reads as breakage, "+
			"not as a refusal", jsFile)
	}
	if !strings.Contains(refuse, "event.preventDefault()") || !strings.Contains(refuse, "event.stopPropagation()") {
		t.Errorf("%s: the refusal does not stop the event — after the toast the emulator pastes "+
			"something of its own into the session", jsFile)
	}
}
