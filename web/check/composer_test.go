package check

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestComposerEnterDependsOnTheLayout(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)
	if !strings.Contains(body, "class=\"composer\"") {
		t.Fatalf("%s: there is no composer at all — nothing to write into the session with", chatFile)
	}
	composer := jsBlock(t, chatFile, body, "function Composer(")
	for _, handler := range []string{"onKeyPress", "onKeyUp"} {
		if strings.Contains(composer, handler) {
			t.Errorf("%s: %s showed up — sending by key is parsed only by %s on key down, "+
				"otherwise Enter cuts a reply off in the middle again", chatFile, handler, "onKeyDown")
		}
	}
	if strings.Contains(composer, `"Enter"`) {
		t.Errorf("%s: the composer parses Enter itself — whether a send is asked for lives in asksSend, "+
			"and there must be no second copy of it", chatFile)
	}
	asks := jsBlock(t, chatFile, body, "function asksSend(")
	for _, want := range []struct{ token, why string }{
		{"wide", "the layout is not asked about — one answer for two keyboards, and one of them gets what is not its own"},
		{"e.shiftKey", "Shift+Enter is not separated from Enter — on a monitor there is nothing left to break a line with"},
		{"e.ctrlKey", "from the narrow layout there is nothing to send with but the button"},
		{"e.metaKey", "on a Mac the same key is called Cmd, and without it the shortcut does not work there"},
	} {
		if !strings.Contains(asks, want.token) {
			t.Errorf("%s: asksSend does not ask about %s — %s", chatFile, want.token, want.why)
		}
	}
}

func TestComposerDraftIsPerConversation(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	screen := screenSrc(t, chatFile)

	composer := jsBlock(t, chatFile, screen, "export function Composer(")
	if !strings.Contains(composer, "useDraft(id)") {
		t.Fatalf("%s: the composer field does not hold the chat draft — what was typed goes away with the screen again", chatFile)
	}
	if !strings.Contains(screen, "<${Composer} name=${name} id=${id}") {
		t.Errorf("%s: the screen does not tell the composer which chat this is — the draft has nowhere to get a key", chatFile)
	}

	read := jsBlock(t, chatFile, screen, "function saved(")
	if !strings.Contains(read, "drafts()[id]") {
		t.Errorf("%s: the draft is read by something other than the chat — a foreign text lands in the field", chatFile)
	}
	draft := jsBlock(t, chatFile, screen, "function useDraft(")
	for _, c := range []struct{ what, key string }{
		{"saving", "all[id] = "},
		{"deleting", "delete all[id]"},
	} {
		if !strings.Contains(draft, c.key) {
			t.Errorf("%s: draft %s does not go by chat — what was typed for one session is dropped into another", chatFile, c.what)
		}
	}

	for _, c := range []struct{ what, block string }{
		{"reading", jsBlock(t, chatFile, screen, "function drafts(")},
		{"writing", draft},
	} {
		if !strings.Contains(c.block, "try {") {
			t.Errorf("%s: draft %s is not covered by try — in a private window the whole screen falls over", chatFile, c.what)
		}
	}

	if !strings.Contains(draft, "keep(") {
		t.Errorf("%s: writing a draft does not clean out the old ones — the panel has no other reason to visit the storage", chatFile)
	}
	kept := jsBlock(t, chatFile, screen, "function keep(")
	for _, want := range []string{"DRAFT_TTL", "DRAFT_MAX"} {
		if !strings.Contains(kept, want) {
			t.Errorf("%s: draft cleanup does not look at %s — the phone storage is shared by the whole site", chatFile, want)
		}
	}
}

func TestGapsUnderComposerComeFromOneNumber(t *testing.T) {
	css := cssSrc(t)

	page := cssBlock(t, css, ".body")
	if !strings.Contains(page, "--body-gap:") || !strings.Contains(page, "gap: var(--body-gap)") {
		t.Errorf("%s: the shared page gap is set by a literal — the rule under the composer "+
			"repeats it with a number of its own, and the two values drift apart silently: %s", cssFile, page)
	}

	under := cssBlock(t, css, ".body:has(> .deck)")
	if !strings.Contains(under, "padding-bottom: var(--deck-gap)") {
		t.Errorf("%s: the bottom margin of the chat page does not come from the shared number: %s", cssFile, under)
	}

	deck := cssBlock(t, css, ".deck")
	if !strings.Contains(deck, "var(--deck-gap)") {
		t.Errorf("%s: the gap up to the button row is counted with a number of its own, not the "+
			"same one as the gap below it: %s", cssFile, deck)
	}
	if !strings.Contains(deck, "var(--body-gap)") {
		t.Errorf("%s: the gap up to the button row does not subtract what the page already gave "+
			"as the shared gap — the sum is not the number written here: %s", cssFile, deck)
	}

	if !strings.Contains(deck, "justify-content: flex-end") {
		t.Errorf("%s: the row under the composer no longer fills from the right edge — the "+
			"chips end up on the side the thumb does not reach, and the left, which is being "+
			"kept free for buttons of its own, is taken: %s", cssFile, deck)
	}

	chips := cssBlock(t, css, ".deck > .wchips")
	if strings.Contains(chips, "flex: 1") {
		t.Errorf("%s: the chip group stretches across the width again, which pushes it off "+
			"the right edge and leaves a gap in the middle of the row: %s", cssFile, chips)
	}

	// The selector is matched as a substring, and ".wchips" alone finds the rule
	// scoped to the deck first; the newline pins it to the shared one.
	all := cssBlock(t, css, "\n.wchips")
	if !strings.Contains(all, "flex-wrap: nowrap") {
		t.Errorf("%s: the chips wrap onto a second line instead of the group taking the width "+
			"it needs to the left, and a wrapped row aligned right comes out ragged: %s", cssFile, all)
	}

	right := cssBlock(t, css, ".deckright")
	if strings.Contains(right, "margin-left: auto") {
		t.Errorf("%s: the right-hand group still pushes itself off with a margin, which drives "+
			"the chips back to the left edge: %s", cssFile, right)
	}
	if strings.Contains(right, "min-width") {
		t.Errorf("%s: the right-hand group holds a width of its own — whatever is not a button "+
			"in it stands between the last chip and the right edge as a gap: %s", cssFile, right)
	}
}

// The row under the composer holds the same four buttons whatever the session is
// doing. A chip that arrives with the first task moves the neighbours out from
// under a thumb already on its way, and a group that leaves when it is empty
// takes the row away from the edge it is read from.
func TestWorkRowKeepsEveryButtonWithNothingToShow(t *testing.T) {
	const workFile = "src/screens/chat/work.js"
	src := screenSrc(t, workFile)

	left := jsBlock(t, workFile, src, "export function Work(")
	if strings.Contains(left, "return null") {
		t.Errorf("%s: the left-hand group leaves the row when the session holds nothing — half "+
			"the buttons of the row go with it", workFile)
	}
	right := jsBlock(t, workFile, src, "export function WorkRefs(")

	for _, chip := range []struct{ block, kind, what string }{
		{left, "tasks", "background work"},
		{left, "agents", "the subagents"},
		{right, "plan", "the plan"},
		{right, "arts", "the artifacts"},
	} {
		tag := chipTag(t, workFile, chip.block, chip.kind)
		if gate := chipGate(chip.block, chip.kind); gate != "" {
			t.Errorf("%s: the button of %s is drawn only when there is something behind it — the "+
				"row changes its length under the thumb: %s", workFile, chip.what, gate)
		}
		if !strings.Contains(tag, "idle") {
			t.Errorf("%s: the button of %s looks the same full and empty — a row of four live "+
				"counters says the session holds work it does not: %s", workFile, chip.what, tag)
		}
		if strings.Contains(tag, "disabled") {
			t.Errorf("%s: the button of %s refuses the tap when it is empty — a dead button is "+
				"read as a broken panel, while the list it opens says out loud there is nothing: %s",
				workFile, chip.what, tag)
		}
	}
}

// chipTag returns the button of the row that opens the given list.
func chipTag(t *testing.T, file, block, kind string) string {
	t.Helper()
	head := chipHead(block, kind)
	if head < 0 {
		t.Fatalf("%s: no button of the row opens %q — the test guards the wrong place", file, kind)
	}
	end := strings.Index(block[head:], "</button>")
	if end < 0 {
		t.Fatalf("%s: the button that opens %q is not closed", file, kind)
	}
	return block[head : head+end]
}

// chipGate returns what stands between the button before this one and this one:
// a chip put behind a count is preceded by a condition left open.
func chipGate(block, kind string) string {
	head := chipHead(block, kind)
	if head < 0 {
		return ""
	}
	before := block[:head]
	if prev := strings.LastIndex(before, "</button>"); prev >= 0 {
		before = before[prev+len("</button>"):]
	}
	if !strings.Contains(before, "&& html") {
		return ""
	}
	lines := strings.Split(strings.TrimRight(before, " \t\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func chipHead(block, kind string) int {
	at := strings.Index(block, `kind: "`+kind+`"`)
	if at < 0 {
		return -1
	}
	return strings.LastIndex(block[:at], "<button")
}

func TestKeyboardResizesTheWindowNotTheView(t *testing.T) {
	raw, err := os.ReadFile(webPath("app.html"))
	if err != nil {
		t.Fatalf("application shell: %v", err)
	}
	meta := regexp.MustCompile(`<meta name="viewport" content="([^"]*)">`).FindStringSubmatch(string(raw))
	if meta == nil {
		t.Fatal("app.html: there is no viewport declaration at all — the shell renders like an ordinary page")
	}
	if !strings.Contains(meta[1], "interactive-widget=resizes-content") {
		t.Errorf("app.html: the keyboard again shrinks the view instead of the window — the chat "+
			"header goes past the top edge together with the whole shell: %s", meta[1])
	}
}

func TestPanelTakesFocusOnlyWhereThereIsNoKeyboard(t *testing.T) {
	files := srcFiles(t)
	focus := stripComments(files["src/ui/focus.js"])
	if focus == "" {
		t.Fatal("src/ui/focus.js not found")
	}
	if !strings.Contains(focus, `from "./wide.js"`) || !strings.Contains(focus, "matchMedia(WIDE)") {
		t.Error("the focus decision does not ask about width: on a phone the keyboard slides out " +
			"every time a card opens")
	}
	if !strings.Contains(focus, "!typing()") {
		t.Error("the focus is taken from someone already standing in a field: what is typed goes " +
			"elsewhere and the cursor jumps in the middle of a word")
	}

	for _, path := range sortedKeys(files) {
		if path == "src/ui/focus.js" {
			continue
		}
		if strings.Contains(stripComments(files[path]), "isContentEditable") {
			t.Errorf("%s starts a typing check of its own — it drifts from the shared one on the first edit", path)
		}
	}

	for _, path := range []string{"src/screens/chat/composer.js", "src/screens/chat/term.js"} {
		if !strings.Contains(stripComments(files[path]), "mayFocus()") {
			t.Errorf("%s takes the focus without asking permission", path)
		}
	}

	chat := stripComments(files["src/screens/chat.js"])
	if !strings.Contains(chat, "focus=${`${name}|${id || \"\"}|${view}`}") {
		t.Error("the composer does not know the chat or the view changed: the focus is set once " +
			"in the life of the screen, and switching the feed with the terminal does not move it")
	}
}

func TestAttachmentTargetsEachHaveTheirOwnInput(t *testing.T) {
	files := srcFiles(t)
	src := files["src/screens/chat/tools.js"]
	if src == "" {
		t.Fatal("src/screens/chat/tools.js not found")
	}
	body := stripComments(src)

	at := strings.Index(body, "export function PickFile(")
	if at < 0 {
		t.Fatal("the paperclip is gone from the composer")
	}
	pick := body[at:]
	if cut := strings.Index(pick, "export function AttachSheet("); cut > 0 {
		pick = pick[:cut]
	}
	if !strings.Contains(pick, `knows(exec, "session.file")`) {
		t.Error("the paperclip does not ask about session.file")
	}
	if strings.Contains(pick, `<label class=${`+"`"+`iconbtn pickfile`) {
		t.Error("the paperclip is still a label on an input: it opens the system dialog itself, " +
			"and the target sheet ends up on top of a path that never asks it")
	}
	if !strings.Contains(pick, "onClick=${onAsk}") {
		t.Error("the paperclip does not ask to open the target sheet")
	}

	if strings.Contains(stripComments(files["src/screens/chat/composer.js"]), "<${Sheet}") {
		t.Error("the sheet is drawn inside the composer: `position: fixed` inside glass is " +
			"not fixed, and a closed sheet peeks out above the bottom edge")
	}
	if !strings.Contains(stripComments(files["src/screens/chat.js"]), "${live && html`<${AttachSheet}") {
		t.Error("the chat screen does not draw the target sheet for a live session — then someone " +
			"below draws it, and below there is only the composer with its glass")
	}

	for _, want := range []string{`id: "camera"`, `id: "photo"`, `id: "files"`} {
		if !strings.Contains(body, want) {
			t.Errorf("target %s is not in the list", want)
		}
	}
	if !strings.Contains(body, `capture: "environment"`) {
		t.Error("nothing raises the camera: with no `capture` the Camera tile opens the same gallery " +
			"as the one next to it — that is, its label lies")
	}
	if !strings.Contains(body, `capture: "environment", multiple: false`) {
		t.Error("the camera allows a multiple pick: one shot is taken, and `multiple` next to `capture` " +
			"opens the gallery instead of the camera on some phones")
	}
	if !strings.Contains(body, "TARGETS.map(") || !strings.Contains(body, `<input`) {
		t.Error("the inputs are not tied to the tiles — then the target sets nothing")
	}

	take := body[strings.Index(body, "const take = async"):]
	if cut := strings.Index(take, "\n    };"); cut > 0 {
		take = take[:cut]
	}
	if !strings.Contains(take, "onClose()") {
		t.Error("the sheet does not close after a file is picked: it stays hanging over the composer, " +
			"and that reads as press again, though this is not where to press")
	}

	if !strings.Contains(pick, "disabled=${!canSend}") {
		t.Error("the paperclip does not go dim without session.file: the sheet opens and there is nothing to attach")
	}
	if !strings.Contains(body, "if (!canSend) return null;") {
		t.Error("the target sheet is drawn even when the host does not know session.file")
	}

	if !strings.Contains(stripComments(files["src/ui/sheet.js"]), "select, label") {
		t.Error("the sheet takes a tap on a label as the start of a gesture — instead of a dialog " +
			"the tile toggles the sheet height")
	}
}

func TestFileAttachmentsKeepTheirCeiling(t *testing.T) {
	body := screenSrc(t, "src/screens/chat.js")

	atts := jsBlock(t, "src/screens/chat/files.js", body, "export function FileAtts(")
	if !strings.Contains(atts, "list.slice(0, HEAD)") {
		t.Error("src/screens/chat/files.js: the collapsed list shows every file — " +
			"thirty attachments push the chat itself out of the feed again")
	}
	if !strings.Contains(atts, "el.scrollHeight > el.clientHeight") {
		t.Error("src/screens/chat/files.js: the there-is-more mask is set without measuring the block — " +
			"it will hang over a list that fits whole too")
	}

	css := cssSrc(t)
	open := cssBlock(t, css, ".mflist.mfopen")
	for _, rule := range []string{"overflow-y: auto", "max-height: max(", "width: 0", "min-width: 100%"} {
		if !strings.Contains(open, rule) {
			t.Errorf("%s: .mflist.mfopen has no %q — an expanded list either becomes a sheet over "+
				"the whole chat again or drags the whole feed sideways", cssFile, rule)
		}
	}
	if !strings.Contains(open, "150px") {
		t.Errorf("%s: .mflist.mfopen has no floor of 150px — on a short screen "+
			"40%% of the height gives two rows, and expanded stops differing from collapsed", cssFile)
	}
	if mask := cssBlock(t, css, ".mflist.mfmask"); !strings.Contains(mask, "mask-image") {
		t.Errorf("%s: .mflist.mfmask has no mask — a cut-off list reads as breakage "+
			"rather than as an invitation to scroll", cssFile)
	}
}

func TestPastedFileJoinsTheAttachmentShelf(t *testing.T) {
	files := srcFiles(t)
	const composerFile = "src/screens/chat/composer.js"
	const toolsFile = "src/screens/chat/tools.js"
	composer := stripComments(files[composerFile])
	tools := stripComments(files[toolsFile])
	if composer == "" || tools == "" {
		t.Fatal("the sources of the composer or the paperclip are missing")
	}

	if !strings.Contains(composer, "onPaste=${paste}") {
		t.Fatalf("%s: the field has no paste handler — a copied screenshot has nowhere to go",
			composerFile)
	}
	paste := arrowFn(t, composerFile, composer, "    const paste = async (event) => {")
	if !strings.Contains(paste, "intake(picked, pack, toast, clipName)") {
		t.Errorf("%s: the paste goes around the shared road — the ceiling checks and the byte "+
			"reading live in intake, and a second copy of them drifts from the first silently", composerFile)
	}
	for _, mark := range []string{"deliver(", "run(", "onLocal("} {
		if strings.Contains(paste, mark) {
			t.Errorf("%s: the paste calls %s — a reply has to leave by a button, "+
				"not by the motion that attached a file", composerFile, mark)
		}
	}
	for _, ceiling := range []string{"FILE_MAX", "PACK_MAX"} {
		if strings.Contains(composer, ceiling) {
			t.Errorf("%s: ceiling %s is counted a second time — intake is what counts it", composerFile, ceiling)
		}
	}
	intake := jsBlock(t, toolsFile, tools, "export async function intake(")
	for _, ceiling := range []string{"FILES_MAX", "FILE_MAX", "PACK_MAX"} {
		if !strings.Contains(intake, ceiling) {
			t.Errorf("%s: intake does not check %s — the file leaves and silently falls off on the way",
				toolsFile, ceiling)
		}
	}
	if !strings.Contains(tools, "tidy(name || file.name)") {
		t.Errorf("%s: the name from the clipboard is put in past tidy — the reply carries "+
			"what the protocol will not accept", toolsFile)
	}
}

func TestClipboardTextIsNeverSwallowed(t *testing.T) {
	files := srcFiles(t)
	for file, head := range map[string]string{
		"src/screens/chat/composer.js": "    const paste = async (event) => {",
		"src/screens/chat/term.js":     "        const refuse = (event) => {",
	} {
		src := stripComments(files[file])
		if src == "" {
			t.Fatalf("%s not found", file)
		}
		at := strings.Index(src, head)
		if at < 0 {
			t.Fatalf("%s: there is no paste handler (%q)", file, head)
		}
		body := src[at:]
		if end := strings.Index(body, "\n    };"); end > 0 {
			body = body[:end]
		}
		if !strings.Contains(body, `data.getData("text/plain")`) {
			t.Errorf("%s: the paste does not ask about text in the clipboard — an ordinary paste "+
				"stops pasting text", file)
		}
		if !strings.Contains(body, "data.files") {
			t.Errorf("%s: the paste does not look at the clipboard files — there is nothing to take", file)
		}
	}
}
