package check

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestFeedsWireCodeCopy(t *testing.T) {
	files := srcFiles(t)

	for _, feed := range []string{"src/screens/chat.js", "src/screens/chat/subchat.js"} {
		src, ok := files[feed]
		if !ok {
			t.Fatalf("%s not found — the test is looking in the wrong place", feed)
		}
		if !strings.Contains(src, "codecopy.fromClick(event, toast)") {
			t.Errorf("%s: the feed does not call code copying — the button in the block is drawn but dead", feed)
		}
	}

	block := cssBlockFile(t, "src/css/markdown.css", ".mdbar")
	if !strings.Contains(block, "display: flex") {
		t.Error(".mdbar has no display: flex — the copy button slides under the block language")
	}
}

func TestFeedListensToWake(t *testing.T) {
	files := srcFiles(t)
	src, ok := files["src/screens/chat/feedwindow.js"]
	if !ok {
		t.Fatal("src/screens/chat/feedwindow.js not found — the test is looking in the wrong place")
	}
	for _, want := range []struct{ code, harm string }{
		{`addEventListener("visibilitychange"`, "the feed will not learn the tab came back and stays with the state it had before sleep"},
		{"wakeNeeded(document.visibilityState", "the reconnect decision is made past the shared function"},
		{`removeEventListener("visibilitychange"`, "the listener outlives the chat and keeps raising the stream of a closed feed"},
	} {
		if !strings.Contains(src, want.code) {
			t.Errorf("the feed has no %s — %s", want.code, want.harm)
		}
	}
}

func TestChatFeedCSSInvariants(t *testing.T) {
	css := cssSrc(t)

	cases := []struct {
		selector string
		rule     string
		harm     string
	}{
		{
			selector: ".chatfeed",
			rule:     "overflow-x: hidden",
			harm:     "the feed slides sideways as a whole again instead of breaking the word in place",
		},
		{
			selector: ".msg",
			rule:     "overflow-wrap: anywhere",
			harm:     "a long word (two links glued together without a space, say) stops breaking in place and pushes the feed into horizontal scrolling",
		},
		{
			selector: ".mdcode pre",
			rule:     "overflow-x: auto",
			harm:     "code inside a message loses its own scrolling and drags the whole feed sideways",
		},
		{
			selector: ".mdtable",
			rule:     "overflow-x: auto",
			harm:     "a table inside a message loses its own scrolling and drags the whole feed sideways",
		},
	}

	for _, c := range cases {
		block := cssBlock(t, css, c.selector)
		if !strings.Contains(block, c.rule) {
			t.Errorf("%s: %s has no %q — %s", cssFile, c.selector, c.rule, c.harm)
		}
	}
}

func TestOwnReplyKeepsLineBreaks(t *testing.T) {
	const mdFile = "src/md.js"
	md := srcFiles(t)[mdFile]
	if md == "" {
		t.Fatalf("no %s — the markup parser moved, the test is useless", mdFile)
	}
	body := jsBlock(t, mdFile, md, "export function render(")
	if !strings.Contains(body, "breaks") {
		t.Errorf("%s: render() cannot do hard line breaks — a person reply glues back into one paragraph", mdFile)
	}
	if !strings.Contains(paragraphLine(t, mdFile, body), "breaks") {
		t.Errorf("%s: the paragraph is built past the break flag — the lines glue back together with a space", mdFile)
	}
	if !strings.Contains(md, "<br") {
		t.Errorf("%s: a line break has nothing to become in the markup (there is no <br> at all)", mdFile)
	}

	const chatFile = "src/screens/chat.js"
	screen := screenSrc(t, chatFile)
	row := jsBlock(t, chatFile, screen, "export function Row(")
	if !regexp.MustCompile(`const mine = item\.role === "me"`).MatchString(row) {
		t.Fatalf("%s: the feed row does not tell a person reply apart — there is nobody to ask hard breaks for", chatFile)
	}
	if !regexp.MustCompile(`render\(\s*item\.text\s*,\s*\{\s*breaks:\s*mine\s*\}`).MatchString(row) {
		t.Errorf("%s: the reply is rendered without hard breaks — three paragraphs become one again", chatFile)
	}
	mail := jsBlock(t, chatFile, screen, "function Mail(")
	if strings.Contains(mail, "breaks") {
		t.Errorf("%s: the mail asks for hard breaks — the rule about a person reply spread onto foreign markup", chatFile)
	}

	css := cssSrc(t)
	flat := regexp.MustCompile(`white-space:\s*pre\s*[;}]`)
	for _, selector := range []string{".msg", ".msg.me"} {
		if flat.MatchString(cssBlock(t, css, selector)) {
			t.Errorf("%s: %s carries white-space: pre — a long word stops breaking and the feed slides sideways as a whole again", cssFile, selector)
		}
	}
}

func TestLocalReplyMatchesThroughAttachmentChip(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	screen := screenSrc(t, chatFile)

	if n := strings.Count(withoutComments(screen), "[Image #"); n != 1 {
		t.Errorf("%s: %d places know the attachment note instead of one — the parsing has to be shared", chatFile, n)
	}

	same := jsBlock(t, chatFile, screen, "export function sameReply(")
	if n := strings.Count(same, "bare("); n < 2 {
		t.Errorf("%s: sameReply() cleans one side out of two — what arrived and what was sent are compared as they are", chatFile)
	}

	if !strings.Contains(screen, "sameReply(item.text, local)") {
		t.Errorf("%s: the shown copy is cleared by something other than the shared parsing — the harness note will again disagree with what was typed", chatFile)
	}
	for _, was := range []string{"text === l.text", "text.includes(l.file)"} {
		if strings.Contains(screen, was) {
			t.Errorf("%s: clearing the copy compares the texts itself (%s) — a reply with an attachment never matches that way", chatFile, was)
		}
	}

	composer := jsBlock(t, chatFile, screen, "export function Composer(")
	if !strings.Contains(composer, "sent: body") {
		t.Errorf("%s: the composer does not remember what was sent apart from what is shown — there is nothing to compare the copy against", chatFile)
	}
}

func TestChatRunSurvivesHiddenItems(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)

	list := regexp.MustCompile(`const HIDDEN = new Set\(\[([^\]]*)\]\)`).FindStringSubmatch(body)
	if list == nil {
		t.Fatalf("%s has no shared HIDDEN list of invisible roles: without it everyone decides on their own again where the run ends", chatFile)
	}
	roles := regexp.MustCompile(`"[a-z]+"`).FindAllString(list[1], -1)
	if len(roles) == 0 {
		t.Fatalf("the HIDDEN list in %s is empty: background command completions and artifact links will break the row again", chatFile)
	}

	for _, head := range []string{"function rows(", "function tail(", "function weld("} {
		block := jsBlock(t, chatFile, body, head)
		if !strings.Contains(block, "HIDDEN.has(") {
			t.Errorf("%s: %s does not ask HIDDEN — an invisible item between two calls breaks the row of chips again", chatFile, head)
		}
	}

	for _, head := range []string{"function tail(", "function weld("} {
		block := jsBlock(t, chatFile, body, head)
		for _, role := range roles {
			if strings.Contains(block, role) {
				t.Errorf("%s: %s knows the role %s on its own — there has to be one list of invisible roles, HIDDEN", chatFile, head, role)
			}
		}
	}

	for _, want := range []string{"const feed = weld(state.items)", "rows(feed)", "runCalls(feed,"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: no %q — the row of chips and the call list under it count the run from different feeds", chatFile, want)
		}
	}
}

func TestWorkChipsOpenTheirOwnSection(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)
	work := jsBlock(t, chatFile, body, "function Work(") +
		jsBlock(t, chatFile, body, "function WorkRefs(")

	// What the row draws is what matters here; counting over a list to put a
	// number on a chip is the row doing its job. So the markup is what is
	// looked at, not the arithmetic above it.
	for _, name := range []string{"function Work(", "function WorkRefs("} {
		block := jsBlock(t, chatFile, body, name)
		if at := strings.Index(block, "return html"); at >= 0 {
			block = block[at:]
		}
		if strings.Contains(block, ".map(") {
			t.Errorf("%s: the row under the composer draws the list inside itself again — it "+
				"expands in place and pushes the chat off the screen", chatFile)
		}
	}

	for _, kind := range []string{"tasks", "agents", "arts", "briefs"} {
		call := `onOpen({ kind: "` + kind + `" })`
		if n := strings.Count(work, call); n != 1 {
			t.Errorf("%s: the row under the composer holds %d occurrences of %q — every counter "+
				"has a section of its own, otherwise a tap on agents opens the shared list", chatFile, n, call)
		}
		if !strings.Contains(body, `"`+kind+`"`) {
			t.Errorf("%s: section %q opens from nowhere", chatFile, kind)
		}
	}

	for _, want := range []string{
		"const WORK_LISTS = new Set([",
		"WORK_LISTS.has(look.kind)",
		"<${WorkList}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: no %q — the sheet will not tell a work list from the details", chatFile, want)
		}
	}

	lists := regexp.MustCompile(`const WORK_LISTS = new Set\(\[([^\]]*)\]\)`).FindStringSubmatch(body)
	if lists == nil {
		t.Fatalf("%s: the WORK_LISTS section list does not parse", chatFile)
	}
	names := jsBlock(t, chatFile, body, "const LOOK_NAMES = {")
	for _, quoted := range regexp.MustCompile(`"[a-z]+"`).FindAllString(lists[1], -1) {
		kind := strings.Trim(quoted, `"`)
		if !strings.Contains(names, kind+":") {
			t.Errorf("%s: section %q has no label in LOOK_NAMES — the sheet opens unnamed",
				chatFile, kind)
		}
	}
}

func TestChatFeedStaysAtBottomWhenItShrinks(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	src := screenSrc(t, chatFile)

	at := strings.Index(src, "new ResizeObserver(() => {")
	if at < 0 {
		t.Fatalf("%s: nobody watches the feed height — a growing composer and the phone "+
			"keyboard push the last entry under the edge again", chatFile)
	}
	tail := src[at:]
	end := strings.Index(tail, "\n    });")
	if end < 0 {
		t.Fatalf("%s: the end of the effect around the feed height observer is not visible — "+
			"there is nothing to check", chatFile)
	}
	effect := tail[:end]

	watch := regexp.MustCompile(`new ResizeObserver\(\(\) => \{([^}]*)\}\)`).FindStringSubmatch(effect)
	if watch == nil {
		t.Fatalf("%s: the feed height observer has an empty body: %s", chatFile, effect)
	}
	if !strings.Contains(watch[1], "stickRef.current") {
		t.Errorf("%s: the feed returns to the bottom without asking where the person stands — "+
			"someone reading the middle of the chat is yanked down by typing a reply: %s", chatFile, watch[1])
	}
	if !strings.Contains(watch[1], "scrollTop = box.scrollHeight") {
		t.Errorf("%s: shrinking the feed answers with nothing: %s", chatFile, watch[1])
	}
	if !strings.Contains(effect, "ro.observe(box)") {
		t.Errorf("%s: the height observer is created but attached to nothing", chatFile)
	}
	if !strings.Contains(src, "watchRef.current.disconnect()") {
		t.Errorf("%s: the observer of the previous box is never let go — it watches a node "+
			"nobody can see any more, while the box on the screen is watched by nobody", chatFile)
	}
	if !strings.Contains(src, "if (watchRef.current) watchRef.current.disconnect();\n    }, []);") {
		t.Errorf("%s: the feed observer is never disconnected — it outlives its screen", chatFile)
	}
}

func TestChatDropsPreviousSessionState(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	src := screenSrc(t, chatFile)

	const marker = "setFiles([]);"
	if !strings.Contains(src, marker) {
		t.Fatalf("%s: attachments are not reset on a session switch — a file from the previous "+
			"session goes into the new one together with the reply", chatFile)
	}
	if !strings.Contains(src, "setLocal([]);") {
		t.Errorf("%s: unconfirmed replies of the previous session stay in the feed of the new one", chatFile)
	}

	tail := src[strings.Index(src, marker):]
	deps := strings.Index(tail, "}, [name, id]);")
	if deps < 0 || deps > 600 {
		t.Errorf("%s: the reset is not tied to a change of [name, id] — on the desktop it never fires at all", chatFile)
	}
}

func TestChatViewOutlivesTheSessionSwitch(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	src := srcFiles(t)[chatFile]
	if src == "" {
		t.Fatalf("%s not found — the test is useless", chatFile)
	}

	if !strings.Contains(src, "useViewPick(") {
		t.Errorf("%s: the layout is not taken from the device setting — the choice is lost "+
			"on the very first move to a neighbouring chat", chatFile)
	}

	end := strings.Index(src, "}, [name, id]);")
	if end < 0 {
		t.Fatalf("%s: there is no session switch effect — the test is useless", chatFile)
	}
	head := strings.LastIndex(src[:end], "useEffect(() => {")
	if head < 0 {
		t.Fatalf("%s: the reset on a session switch has no beginning", chatFile)
	}
	body := withoutComments(src[head:end])
	for _, bad := range []string{"pickView", "saveView", "setView", "setPick"} {
		if strings.Contains(body, bad) {
			t.Errorf("%s: the reset on a session switch touches the layout (%s) — the feed choice "+
				"lasts until the first move to a neighbouring chat", chatFile, bad)
		}
	}

	// Both headers draw the same tools, built once: the phone and the desktop
	// cannot call different view switches.
	if n := strings.Count(src, "<${ViewToggle}"); n != 1 {
		t.Errorf("%s: the view toggle is drawn in %d places — the phone and the desktop drift apart silently",
			chatFile, n)
	}
	for _, tool := range []string{"<${WindowToggle}", "<${RemoteToggle}"} {
		if n := strings.Count(src, tool); n != 1 {
			t.Errorf("%s: %s is drawn in %d places — the phone and the desktop drift apart silently", chatFile, tool, n)
		}
	}
	if !strings.Contains(src, "tools=${tools}") || !strings.Contains(src, "${viewPair}") ||
		!strings.Contains(src, "${hostTools}") {
		t.Errorf("%s: the two headers do not share the tools of the conversation", chatFile)
	}
}

func TestChatStreamIsKeyedByConversationNotSnapshot(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	src := screenSrc(t, chatFile)

	at := strings.Index(src, "new EventSource(`/api/chat/stream")
	if at < 0 {
		t.Fatalf("%s: the feed stream is never opened — the test is useless", chatFile)
	}
	head := strings.LastIndex(src[:at], "useEffect(() => {")
	if head < 0 {
		t.Fatalf("%s: the feed stream is opened outside an effect", chatFile)
	}
	tail := strings.Index(src[at:], "\n    }, [")
	if tail < 0 {
		t.Fatalf("%s: the dependency array of the stream effect is not found", chatFile)
	}
	body := src[head : at+tail]
	rest := src[at+tail+len("\n    }, ["):]
	end := strings.Index(rest, "]")
	if end < 0 {
		t.Fatalf("%s: the dependency array of the stream effect is not closed", chatFile)
	}
	var deps []string
	for _, dep := range strings.Split(rest[:end], ",") {
		if dep = strings.TrimSpace(dep); dep != "" {
			deps = append(deps, dep)
		}
	}

	if contains("live", deps) {
		t.Errorf("%s: the stream effect depends on the live object — it is new on every agent "+
			"snapshot, and the stream will be reopened every fifteen seconds: %v", chatFile, deps)
	}
	for _, want := range []string{"name", "id"} {
		if !contains(want, deps) {
			t.Errorf("%s: the stream effect does not depend on %s — on a chat switch the stream "+
				"stays with the previous one: %v", chatFile, want, deps)
		}
	}
	if hit := regexp.MustCompile(`\blive\.`).FindString(withoutComments(body)); hit != "" {
		t.Errorf("%s: fields of live are read inside the stream effect — with live out of the "+
			"dependencies the closure sees the object as it was when the stream opened", chatFile)
	}
}

func TestChatHeadDotSeparatesBusyFromWaiting(t *testing.T) {
	src := screenSrc(t, "src/screens/chat.js")

	waits := strings.Index(src, "dkwaiting")
	busy := strings.Index(src, "dkbusy")
	if waits < 0 || busy < 0 {
		t.Fatal("the chat header does not tell a busy session from one waiting for an answer — " +
			"the dot does not say whether you are being called to the machine")
	}
	if waits > busy {
		t.Error("busy is checked before waiting: a session with an open question is marked " +
			"`busy` and shown as working — that is the answer inverted")
	}

	css := cssSrc(t)
	for _, name := range []string{".dkdot.dkwaiting", ".dkdot.dkbusy", ".dkdot.dkidle"} {
		if !strings.Contains(css, name) {
			t.Errorf("state %s has no color — every dot turns the same color "+
				"and there is nothing left to tell them apart by", name)
		}
	}
}

func TestChatTaskParamIsNotConversationID(t *testing.T) {
	const front = "src/screens/chat/look.js"
	src := srcFiles(t)[front]
	if src == "" {
		t.Fatalf("%s not found", front)
	}
	if !strings.Contains(stripComments(src), "/api/chat/task?${base}&task=") {
		t.Errorf("%s calls /api/chat/task with no task parameter — the service will not see the task", front)
	}

	raw, err := os.ReadFile(repoPath("cmd/aacpanel/chat.go"))
	if err != nil {
		t.Fatalf("chat handlers: %v", err)
	}
	body := withoutComments(string(raw))
	at := strings.Index(body, "func (s *Server) apiChatTask(")
	if at < 0 {
		t.Fatal("apiChatTask not found")
	}
	fn := body[at:]
	if end := strings.Index(fn, "\nfunc "); end > 0 {
		fn = fn[:end]
	}
	if !strings.Contains(fn, `Query().Get("task")`) {
		t.Errorf("apiChatTask does not read the task parameter")
	}
	if strings.Contains(fn, `Query().Get("id")`) {
		t.Errorf("apiChatTask reads id itself — the same parameter sessionUUID uses for the chat uuid")
	}
}
