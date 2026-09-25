package check

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

const (
	gateFile     = "src/actions/gate.js"
	registryFile = "src/actions/registry.js"
)

var gateExports = map[string]bool{
	"GateHost":  true,
	"useAction": true,
}

var ceremonies = map[string]ceremony{
	"src/auth.js": {
		paths:   []string{"/auth/passkey/", "/auth/token", "/auth/methods", "/logout", "/login"},
		methods: []string{"POST"},
	},
	"src/screens/chat/term.js": {
		paths:   []string{"/api/term/input", "/api/term/size", "/api/term", "/dist/term.js"},
		methods: []string{"POST"},
	},
	"src/screens/chat/input.js": {
		paths:   []string{"/api/term/input"},
		methods: []string{"POST"},
	},
	"src/push.js": {
		paths:   []string{"/api/push/key", "/api/push/subscription"},
		methods: []string{"POST"},
	},
	"src/renewal.js": {
		paths:   []string{"/api/push/key", "/api/push/subscription"},
		methods: []string{"POST"},
	},
	"src/data/usage.js": {
		paths:   []string{"/api/usage/"},
		methods: []string{"POST"},
	},
	// A brief belongs to the person, not to the host: saving what they typed,
	// marking that it went and putting the document away are all done to the
	// panel's own shelf, and none of them runs anything on the machine. The
	// gate stands in front of execution, and removal asks on the screen
	// instead — the button turns into the question before it does anything.
	"src/data/briefs.js": {
		paths:   []string{"/api/briefs"},
		methods: []string{"PUT", "POST", "DELETE"},
	},
	// A reading of a branch is kept on the same shelf and for the same reason:
	// a note written on a line, taken back or put away changes what the panel
	// holds and runs nothing on the machine. Handing the reading to the
	// session is execution, and that goes through the gate.
	"src/screens/repo/notes.js": {
		paths:   []string{"/api/reviews"},
		methods: []string{"PUT", "POST", "DELETE"},
	},
	// A question aside changes nothing: claude answers from what the
	// conversation holds and writes neither the question nor the answer into
	// it, and the executor takes it as a question, not as an action, keeping
	// no entry of it. It goes as a POST only because the question and the side
	// chat so far do not fit a line of a query.
	"src/screens/chat/sidechat.js": {
		paths:   []string{"/api/session/btw"},
		methods: []string{"POST"},
	},
}

type ceremony struct {
	paths   []string
	methods []string
}

func TestActionsSentOnlyByGate(t *testing.T) {
	post := regexp.MustCompile(`method:\s*"(POST|PUT|PATCH|DELETE)"`)
	urls := regexp.MustCompile("[\"`](/[a-zA-Z][^\"`\\s]*)[\"`]")

	for path, body := range srcFiles(t) {
		if path == gateFile {
			continue
		}

		if strings.Contains(body, "/api/actions") && post.MatchString(body) {
			t.Errorf("%s sends a changing request to /api/actions — execution lives only in %s", path, gateFile)
			continue
		}
		if !post.MatchString(body) {
			continue
		}

		rule, ok := ceremonies[path]
		if !ok {
			t.Errorf("%s sends a changing request past the gate — execution lives only in %s", path, gateFile)
			continue
		}

		for _, m := range urls.FindAllStringSubmatch(body, -1) {
			if !hasPrefix(m[1], rule.paths) {
				t.Errorf("%s goes to %q — of the ceremonies only %v are allowed", path, m[1], rule.paths)
			}
		}
		for _, m := range post.FindAllStringSubmatch(body, -1) {
			if !contains(m[1], rule.methods) {
				t.Errorf("%s sends %s past the gate — of the ceremonies only %v is allowed", path, m[1], rule.methods)
			}
		}
	}
}

func TestGateKeepsSendPrivate(t *testing.T) {
	body := srcFiles(t)[gateFile]
	if body == "" {
		t.Fatalf("%s not found", gateFile)
	}

	exported := regexp.MustCompile(`(?m)^export\s+(?:async\s+)?(?:function|const|let|var|class)\s+(\w+)`)
	for _, m := range exported.FindAllStringSubmatch(body, -1) {
		if !gateExports[m[1]] {
			t.Errorf("%s exports %q — only %v may go outside", gateFile, m[1], keys(gateExports))
		}
	}

	if strings.Contains(body, "export { send") || strings.Contains(body, "export {send") {
		t.Errorf("%s exports send — sending has to stay private", gateFile)
	}

	if n := strings.Count(body, "send("); n != 3 {
		t.Errorf("%s: expected one declaration of send and two calls, found %d occurrences", gateFile, n)
	}
}

var instantActions = map[string]bool{
	"alert.ack":      true,
	"session.send":   true,
	"session.answer": true,
	"session.stop":   true,
	"session.file":   true,
	"session.permit": true,
	// Esc is pressed only into a session that is holding a screen of its own,
	// and the executor reads the screen before the key: on a free composer it
	// presses nothing. It takes a session out of a dialog it put up, which is
	// no more than writing into it — and a second sheet in front of a button
	// the person pressed to recover from a refusal is a sheet in the way.
	"session.escape": true,
	// Taking back a message the person sent is no more than not sending it:
	// the message is the person's own, and it goes back into the composer.
	"session.unqueue": true,
	// A model, an effort or a mode picked from the list the session offers:
	// the list is where the person chose, and a sheet asking "really?" over a
	// row they have just tapped is a second press for the same thing. What is
	// dangerous about a mode stays out of the list — the two that stop a
	// session asking at all are not offered there.
	"session.set": true,
	// Reconnecting, enabling or disabling one MCP server is pressed on the
	// server's own card, and undone by the button beside it.
	"session.mcp":     true,
	"profile.reorder": true,
	"group.reorder":   true,
	"project.reorder": true,
	"disk.hide":       true,
	"disk.show":       true,
}

var instantExecActions = map[string]bool{
	"session.send":    true,
	"session.answer":  true,
	"session.stop":    true,
	"session.permit":  true,
	"session.file":    true,
	"session.escape":  true,
	"session.unqueue": true,
	"session.set":     true,
	"session.mcp":     true,
}

func TestInstantActionsStayHarmless(t *testing.T) {
	registry := srcFiles(t)[registryFile]
	if registry == "" {
		t.Fatalf("%s not found", registryFile)
	}

	ids := regexp.MustCompile(`(?m)^\s{4}"([a-z]+\.[a-zA-Z]+)":`).FindAllStringSubmatch(registry, -1)
	for _, m := range ids {
		id := m[1]
		block := after(registry, `"`+id+`":`)
		instant := strings.Contains(block, "instant: true")

		if instant && !instantActions[id] {
			t.Errorf("%q is declared instant but is not in the list of harmless ones — %v", id, keys(instantActions))
		}
		if instant && !strings.Contains(block, "send:") && !instantExecActions[id] {
			t.Errorf("%q is instant but goes to the executor: it has no address of its own", id)
		}
		if instantExecActions[id] && strings.Contains(block, "send:") {
			t.Errorf("%q is listed as going to the executor but has an address of its own — the list drifted from the code", id)
		}
		if !instant && instantActions[id] {
			t.Errorf("%q is listed as instant but does not turn the sheet off in the registry — the list drifted from the code", id)
		}
	}
}

func TestScreensImportOnlyPublicGateAPI(t *testing.T) {
	imports := regexp.MustCompile(`import\s*\{([^}]*)\}\s*from\s*"[^"]*actions/gate\.js"`)

	for path, body := range srcFiles(t) {
		if path == gateFile {
			continue
		}
		for _, m := range imports.FindAllStringSubmatch(body, -1) {
			for _, name := range strings.Split(m[1], ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				if !gateExports[name] {
					t.Errorf("%s imports %q from the gate — only %v is available outside", path, name, keys(gateExports))
				}
			}
		}
	}
}

var entailed = regexp.MustCompile(`entails:\s*"([a-z]+\.[a-zA-Z]+)"`)

func TestActionRegistryMatchesSpec(t *testing.T) {
	file := srcFiles(t)[registryFile]
	if file == "" {
		t.Fatalf("%s not found", registryFile)
	}
	body := actionsBlock(t, file)

	entails := map[string]bool{}
	for _, m := range entailed.FindAllStringSubmatch(body, -1) {
		entails[m[1]] = true
	}
	required := make([]string, 0, len(action.Kinds))
	for _, k := range action.Kinds {
		if entails[string(k)] {
			continue
		}
		required = append(required, string(k))
	}
	for _, id := range required {
		if !strings.Contains(body, `"`+id+`"`) {
			t.Errorf("the registry has no action %q — it is in action.Kinds", id)
		}
	}
	if len(entails) == 0 {
		t.Error("not a single `entails` is left in the registry — the check guards the wrong place")
	}

	effects := regexp.MustCompile(`effect:\s*"([^"]*)"`).FindAllStringSubmatch(body, -1)
	if len(effects) < len(required) {
		t.Errorf("consequences described: %d, there must be at least %d actions", len(effects), len(required))
	}
	for _, m := range effects {
		text := strings.TrimSpace(m[1])
		if text == "" {
			t.Error("an empty consequence text — there is nothing to read before the action")
		}
		if strings.Contains(strings.ToLower(text), "are you sure") {
			t.Errorf("the text %q asks are you sure instead of naming the consequence", text)
		}
	}
}

func TestSessionKillNeedsSecondConfirm(t *testing.T) {
	registry := srcFiles(t)[registryFile]

	kill := after(registry, `"session.kill":`)
	if kill == "" {
		t.Fatal("the registry has no session.kill")
	}
	if !strings.Contains(kill, "second:") {
		t.Error("session.kill has no second confirmation — kill -9 goes out on the first press")
	}

	gate := srcFiles(t)[gateFile]
	if !strings.Contains(gate, "action.second") {
		t.Errorf("%s does not check second — the second confirmation will not fire", gateFile)
	}
}

func TestEveryActionButtonChecksHostKnowsIt(t *testing.T) {
	files := srcFiles(t)
	call := regexp.MustCompile(`run\("([a-z]+\.[a-zA-Z]+)"`)
	check := regexp.MustCompile(`knows\(exec, "([a-z]+\.[a-zA-Z]+)"\)`)

	onHost := map[string]bool{}
	for _, k := range action.Kinds {
		onHost[string(k)] = true
	}

	for path, body := range files {
		if path == registryFile || path == gateFile {
			continue
		}
		called := map[string]bool{}
		for _, m := range call.FindAllStringSubmatch(body, -1) {
			if onHost[m[1]] {
				called[m[1]] = true
			}
		}
		if len(called) == 0 {
			continue
		}
		checked := map[string]bool{}
		for _, m := range check.FindAllStringSubmatch(body, -1) {
			checked[m[1]] = true
		}
		for kind := range called {
			if !checked[kind] {
				t.Errorf("%s: the button calls %s, but knows(exec, %q) is checked nowhere — "+
					"on a host with an old aacpanel-exec it silently does nothing",
					path, kind, kind)
			}
		}
	}
}

func TestEveryExecActionReachableFromUI(t *testing.T) {
	files := srcFiles(t)
	registry := files[registryFile]
	if registry == "" {
		t.Fatalf("%s not found", registryFile)
	}

	mentioned := func(id string) bool {
		for path, body := range files {
			if path == registryFile || path == gateFile {
				continue
			}
			if strings.Contains(body, `"`+id+`"`) {
				return true
			}
		}
		return false
	}

	escalate := regexp.MustCompile(`escalate:\s*"([a-z]+\.[a-zA-Z]+)"`)
	viaEscalate := map[string]string{}
	viaService := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s{4}"([a-z]+\.[a-zA-Z]+)":`).FindAllStringSubmatch(actionsBlock(t, registry), -1) {
		own := after(registry, `"`+m[1]+`":`)
		if next := escalate.FindStringSubmatch(own); next != nil && mentioned(m[1]) {
			viaEscalate[next[1]] = m[1]
		}
		if next := entailed.FindStringSubmatch(own); next != nil && mentioned(m[1]) {
			viaService[next[1]] = m[1]
		}
	}

	for _, k := range action.Kinds {
		id := string(k)
		if mentioned(id) {
			continue
		}
		if from, ok := viaEscalate[id]; ok {
			t.Logf("%q is pressed not by a button but by escalation from %q — that is allowed", id, from)
			continue
		}
		if from, ok := viaService[id]; ok && mentioned(from) {
			t.Logf("%q is asked by the service itself when a person presses %q — that is allowed", id, from)
			continue
		}
		t.Errorf("the executor can do action %q, but there is nothing to press it with: not a single mention in the screens", id)
	}
}

func TestCommandListMatchesRegistry(t *testing.T) {
	body := commandsBlock(t, srcFiles(t)[registryFile])

	entry := regexp.MustCompile(`(?m)^\s{4}([a-z]+):\s*\{`)
	onScreen := map[string]bool{}
	for _, m := range entry.FindAllStringSubmatch(body, -1) {
		onScreen[m[1]] = true
	}
	for name := range action.Commands {
		if !onScreen[name] {
			t.Errorf("the protocol accepts command %q, but the panel does not know about it — there is nothing to press it with", name)
		}
	}
	for name := range onScreen {
		if _, ok := action.Commands[name]; !ok {
			t.Errorf("the panel shows command %q, which is not in action.Commands — the server will reject it", name)
		}
	}

	seen := map[string][]string{}
	at := ""
	for _, line := range strings.Split(body, "\n") {
		if m := entry.FindStringSubmatch(line); m != nil {
			at = m[1]
			continue
		}
		open := strings.Index(line, "args: [")
		if at == "" || open < 0 {
			continue
		}
		shut := strings.LastIndex(line, "]")
		if shut < open {
			continue
		}
		var list []string
		for _, raw := range strings.Split(line[open+len("args: ["):shut], ",") {
			raw = strings.Trim(strings.TrimSpace(raw), `"`)
			if raw != "" {
				list = append(list, raw)
			}
		}
		seen[at] = list
	}
	for name, want := range action.Commands {
		got := seen[name]
		if len(got) != len(want) {
			t.Errorf("command %q has the options %v in the panel and %v in the protocol", name, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("command %q, option %d: %q in the panel, %q in the protocol", name, i+1, got[i], want[i])
			}
		}
	}
}

func commandsBlock(t *testing.T, body string) string {
	t.Helper()
	const head = "export const COMMANDS = {"
	at := strings.Index(body, head)
	if at < 0 {
		t.Fatalf("%s: there is no COMMANDS dictionary — there is nothing to describe the slash commands with", registryFile)
	}
	rest := body[at+len(head):]
	end := strings.Index(rest, "\n};")
	if end < 0 {
		t.Fatalf("%s: the COMMANDS dictionary is not closed", registryFile)
	}
	return rest[:end]
}

func TestFileLimitMatchesProtocol(t *testing.T) {
	body := srcFiles(t)["src/screens/chat/tools.js"]
	if body == "" {
		t.Fatal("src/screens/chat/tools.js not found — the file and command buttons live there")
	}
	megabytes := map[string]int{"FILE_MAX": action.FileMax, "PACK_MAX": action.FilesBytesMax}
	for name, want := range megabytes {
		m := regexp.MustCompile(`const ` + name + ` = (\d+) \* 1024 \* 1024;`).FindStringSubmatch(body)
		if m == nil {
			t.Errorf("tools.js has no %s — the panel does not know its own ceiling", name)
			continue
		}
		mb, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		if int64(mb)<<20 != int64(want) {
			t.Errorf("the panel holds %s at %d MB while the protocol says %d MB", name, mb, want>>20)
		}
	}
	m := regexp.MustCompile(`FILES_MAX = (\d+);`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("tools.js has no FILES_MAX — the panel does not know how many files leave at once")
	}
	count, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if count != action.FilesMax {
		t.Errorf("the panel takes %d files at once while the protocol says %d", count, action.FilesMax)
	}
}

func TestSendCarriesHTTPStatusToCaller(t *testing.T) {
	body := srcFiles(t)[gateFile]
	if body == "" {
		t.Fatalf("%s not found", gateFile)
	}
	block := jsBlock(t, gateFile, body, "async function send(")

	fails := regexp.MustCompile(`return \{ ok: false,[^}]*\}`).FindAllString(block, -1)
	if len(fails) == 0 {
		t.Fatal("not a single failure found in send() — the test guards the wrong place")
	}
	for _, ret := range fails {
		if strings.Contains(ret, "the network is unavailable") {
			continue
		}
		if !strings.Contains(ret, "status:") {
			t.Errorf("%s: failure %q carries no status — the response code is lost in the gate, "+
				"and the caller cannot tell 409 from the other failures", gateFile, ret)
		}
	}
}

func TestJournalClaimOnlyForJournaledActions(t *testing.T) {
	registry := srcFiles(t)[registryFile]
	if registry == "" {
		t.Fatalf("%s not found", registryFile)
	}
	body := actionsBlock(t, registry)

	onHost := map[string]bool{}
	for _, k := range action.Kinds {
		onHost[string(k)] = true
	}

	ids := regexp.MustCompile(`(?m)^\s{4}"([a-z]+\.[a-zA-Z]+)":`).FindAllStringSubmatch(body, -1)
	if len(ids) == 0 {
		t.Fatal("not a single action found in the registry — the test guards the wrong place")
	}
	for _, m := range ids {
		id := m[1]
		block := withoutComments(after(registry, `"`+id+`":`))
		hasOwnSend := strings.Contains(block, "send:")
		declaredUnjournaled := strings.Contains(block, "journaled: false")

		if hasOwnSend && !declaredUnjournaled {
			t.Errorf("%q goes by an address of its own (send:) but is not marked journaled: false — "+
				"the gate will promise a journal entry that never happens", id)
		}
		if !hasOwnSend && declaredUnjournaled {
			t.Errorf("%q is marked journaled: false but has no address of its own — then it goes "+
				"through /api/actions and really is written to the journal, and the flag lies", id)
		}
		if !hasOwnSend && !onHost[id] {
			t.Errorf("%q is not listed in action.Kinds and has no address of its own — "+
				"it is unclear who executes it at all", id)
		}
	}

	gate := withoutComments(srcFiles(t)[gateFile])
	if !strings.Contains(gate, "action.journaled") {
		t.Errorf("%s does not check action.journaled — the line about the journal cannot know "+
			"that a particular action does not write there", gateFile)
	}
}

func TestFieldConflictActionsMatchFormsScreen(t *testing.T) {
	registry := srcFiles(t)[registryFile]
	if registry == "" {
		t.Fatalf("%s not found", registryFile)
	}
	body := actionsBlock(t, registry)
	forms := srcFiles(t)["src/screens/profiles/forms.js"]
	if forms == "" {
		t.Fatal("src/screens/profiles/forms.js not found")
	}

	titles := regexp.MustCompile(`(?m)^\s{4}"([a-z]+\.[a-zA-Z]+)":\s*\[`).FindAllStringSubmatch(forms, -1)
	hasForm := map[string]bool{}
	for _, m := range titles {
		hasForm[m[1]] = true
	}
	if len(hasForm) == 0 {
		t.Fatal("not a single form (TITLES) found in forms.js — the test guards the wrong place")
	}

	ids := regexp.MustCompile(`(?m)^\s{4}"([a-z]+\.[a-zA-Z]+)":`).FindAllStringSubmatch(body, -1)
	for _, m := range ids {
		id := m[1]
		block := withoutComments(after(registry, `"`+id+`":`))
		declared := strings.Contains(block, "fieldConflict: true")
		if declared && !hasForm[id] {
			t.Errorf("%q is marked fieldConflict: true but has no form (and no field) — "+
				"the toast on 409 is suppressed and there is nowhere to show the refusal", id)
		}
		if hasForm[id] && !declared {
			t.Errorf("%q has a form with a field but is not marked fieldConflict: true — "+
				"a 409 on the name will keep coming as a toast alone, and on a short form "+
				"that covers the field pixel for pixel rather than merely repeating it", id)
		}
	}

	gate := withoutComments(srcFiles(t)[gateFile])
	if !strings.Contains(gate, "action.fieldConflict") {
		t.Errorf("%s does not check action.fieldConflict — the toast cannot know that the action "+
			"already shows the refusal at the field", gateFile)
	}
}

func TestJournalFlagMatchesOwnAddress(t *testing.T) {
	src := srcFiles(t)[registryFile]
	if src == "" {
		t.Fatalf("%s not found", registryFile)
	}

	for _, m := range regexp.MustCompile(`(?m)^    "([a-z]+\.[a-zA-Z]+)":`).FindAllStringSubmatch(src, -1) {
		name := m[1]
		block := actionBlock(src, name)
		ownAddress := regexp.MustCompile(`(?m)^\s+send:`).MatchString(block)
		marked := regexp.MustCompile(`(?m)^\s+journaled: false`).MatchString(block)
		switch {
		case ownAddress && !marked:
			t.Errorf("%s goes by an address of its own but promises a journal: there will be no entry", name)
		case !ownAddress && marked:
			t.Errorf("%s goes through /api/actions and is journaled, yet it is marked as not written", name)
		}
	}
}

func TestDiskPickerOnlyFillsTheForm(t *testing.T) {
	files := srcFiles(t)
	picker, ok := files["src/screens/profiles/disk.js"]
	if !ok {
		t.Fatal("there is no directory list from the disk: src/screens/profiles/disk.js")
	}
	for _, banned := range []string{"useAction", "fetch(", "run("} {
		if strings.Contains(picker, banned) {
			t.Errorf("the list from the disk calls %s itself — sending has to go through the form and the gate", banned)
		}
	}
	forms := files["src/screens/profiles/forms.js"]
	if !strings.Contains(forms, "DiskPicker") || !strings.Contains(forms, "Pick a project") {
		t.Errorf("the project form does not open the list from the disk — there is no Pick a project button")
	}
	if n := strings.Count(forms, "await run("); n != 1 {
		t.Errorf("%d sends from the form, expected one — in useSave", n)
	}
}

func TestGateSheetClosesOnPressNotOnAnswer(t *testing.T) {
	body := srcFiles(t)[gateFile]
	if body == "" {
		t.Fatalf("%s not found", gateFile)
	}
	block := withoutComments(jsUntil(t, gateFile, body, "const confirm = useCallback(", "\n    }, ["))

	closeAt := strings.Index(block, "setPending(null)")
	sendAt := strings.Index(block, "await send(")
	if closeAt < 0 {
		t.Fatalf("%s: the confirmation does not close the sheet at all — nothing to check", gateFile)
	}
	if sendAt < 0 {
		t.Fatalf("%s: the confirmation has no send — the test is useless, check the anchor", gateFile)
	}
	if closeAt > sendAt {
		t.Errorf("%s: the sheet closes after the host answers — on a session answer that is a couple "+
			"of seconds of a frozen dialog, and a person reads them as a broken panel", gateFile)
	}

	if strings.Contains(withoutComments(body), "disabled=") {
		t.Errorf("%s: the sheet buttons have a dimmed state again — the sheet closes on the press, "+
			"there is nothing to wait for on it", gateFile)
	}

	if !strings.Contains(block, "resolve(result)") {
		t.Errorf("%s: the confirmation does not give the caller the real answer — a closed sheet "+
			"stops being cosmetic and becomes a claim that it worked", gateFile)
	}
}

func TestSessionActionsWaitForTheSnapshot(t *testing.T) {
	files := srcFiles(t)
	catch := stripComments(files["src/catchup.js"])
	if catch == "" {
		t.Fatal("src/catchup.js not found: waiting for the snapshot has no owner")
	}
	for _, want := range []string{"CATCH_UP_MS", "CATCH_UP_LIMIT", "export function useCatchUp", "export function settled"} {
		if !strings.Contains(catch, want) {
			t.Errorf("the shared wait has no %s", want)
		}
	}
	if !strings.Contains(catch, "if (settled(task, names, at, ids)") {
		t.Error("the wait is cleared by something other than a flag: then it is cleared by timeout, " +
			"that is, the panel simply waits a minute and shows what was there")
	}
	if !strings.Contains(catch, "Date.now() - task.since > CATCH_UP_LIMIT") {
		t.Error("the wait has no ceiling: the executor answers ok even when the console died " +
			"at startup — the panel will poll the snapshot forever")
	}
	at := strings.Index(catch, "setInterval(")
	if at < 0 {
		t.Fatal("the wait does not hurry the snapshot: the ordinary poll runs every fifteen seconds, " +
			"and the action looks undone")
	}
	loop := catch[at:]
	if cut := strings.Index(loop, "}, CATCH_UP_MS);"); cut > 0 {
		loop = loop[:cut]
	} else {
		t.Fatal("the step of the catch-up poll is set by something other than the shared number")
	}
	if !strings.Contains(loop, "refresh()") {
		t.Error("the wait timer does not go for the snapshot: it ticks idly, and the action " +
			"is still confirmed by the next poll every fifteen seconds")
	}

	app := stripComments(files["src/app.js"])
	if !strings.Contains(app, "useCatchUp(snapshot, refresh)") {
		t.Error("the wait is started somewhere other than next to the snapshot: the screen that " +
			"holds it carries the pending row away when it unmounts")
	}
	if !strings.Contains(app, "onRefresh: refresh, wait,") {
		t.Error("the wait does not reach the shells: the layouts will start their own, and the deadlines drift apart")
	}
	mobile := stripComments(files["src/mobile/shell.js"])
	if strings.Count(mobile, "wait=${wait}") < 2 {
		t.Error("on a phone the wait does not reach the sessions screen: the chain has two links — " +
			"the shell gives it to the section, the section to the screen — and it breaks one link at a time")
	}
	if !strings.Contains(mobile, "wait=${wait} faults=") {
		t.Error("the sessions screen on a phone does not get the wait — the pending row on the card stays empty")
	}
	if !strings.Contains(stripComments(files["src/desktop/shell.js"]), "wait=${wait}") {
		t.Error("the desktop does not take the wait from above")
	}

	if !strings.Contains(stripComments(files["src/desktop/sessions.js"]), `wait.of("close", s.session)`) {
		t.Error("the row on the monitor does not ask whether it is being closed: the close stays " +
			"unanswered until the next poll, and the person presses close a second time")
	}

	panels := stripComments(files["src/desktop/panels.js"])
	if strings.Contains(panels, "onDone=${onClose}") {
		t.Error("a successful session start still means close the panel: nobody hurries the snapshot")
	}
	if !strings.Contains(panels, "onOpened=${onOpened}") {
		t.Error("the projects panel does not tell the shell that the console came up: the panel stays " +
			"open over the list that already holds the answer to the press")
	}
}
