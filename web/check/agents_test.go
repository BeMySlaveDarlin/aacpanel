package check

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestSubagentFeedOnlyReads(t *testing.T) {
	const path = "src/screens/chat/subchat.js"
	files := srcFiles(t)
	body := files[path]
	if body == "" {
		t.Fatalf("%s not found — the test is useless", path)
	}
	clean := withoutComments(body)

	for _, mark := range []string{"<${Composer}", "<${Term}", "deliver(", "session.send"} {
		if strings.Contains(clean, mark) {
			t.Errorf("%s: %s showed up in the agent feed — there is nowhere to write to a subagent, "+
				"and a field whose reply goes elsewhere lies about the addressee", path, mark)
		}
	}

	if got := strings.Count(clean, "id=${id}"); got < 3 {
		t.Errorf("%s: the feed id reaches only %d of the three places (feed, calls, "+
			"details) — the rest will read a position in the parent transcript", path, got)
	}
}

func TestAgentListShowsWhatHarnessKnows(t *testing.T) {
	src := screenSrc(t, "src/screens/chat.js")
	if !strings.Contains(src, "agent.last || agent.reportedAt") {
		t.Error("the threshold is counted from the time of the report, not from the last activity: " +
			"an agent that kept working after reporting is hidden too early")
	}
	if !strings.Contains(src, "AGENT_FADE_MS") {
		t.Error("there is no display threshold at all — the list becomes a sheet over the whole day again")
	}
	row := funcBody(t, src, "function AgentRow(")
	for _, want := range []string{"agent.model", "agent.color", "agent.tokens"} {
		if !strings.Contains(row, want) {
			t.Errorf("the agent row does not show %s — that is harness data, not a guess by the panel", want)
		}
	}
}

func TestAgentContextReachesTheFeedHeader(t *testing.T) {
	src := screenSrc(t, "src/screens/chat.js")
	open := funcBody(t, src, "function openAgent(")
	if !strings.Contains(open, "tokens: agent.tokens") {
		t.Error("opening an agent drops its context — the feed header has nothing to show")
	}
	files := srcFiles(t)
	if !strings.Contains(files["src/screens/chat/subchat.js"], "contextSay(agent)") {
		t.Error("the feed header does not name the context of the agent")
	}
	if !strings.Contains(src, "{ ...sub, ...fresh }") {
		t.Error("the open agent is a snapshot from the moment of the tap: " +
			"its context in the header stands still while the agent works")
	}
}

func TestReportedAgentNeverBorrowsStartTime(t *testing.T) {
	src := screenSrc(t, "src/screens/chat.js")
	row := funcBody(t, src, "function AgentRow(")
	if strings.Contains(row, "reportedAt || agent.at") {
		t.Error("the agent row puts the start time in place of the report time")
	}
	if !strings.Contains(row, "time unknown") {
		t.Error("a report with no time has no honest label: " +
			"the row has to say the time is missing rather than name someone else's")
	}
	if !strings.Contains(row, "silent for ${since(") {
		t.Error("the row of a silent agent names the moment of the report instead of the length of the silence")
	}
}

func TestAgentCountMatchesAgentList(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)
	if !strings.Contains(body, "function splitAgents(") {
		t.Fatalf("%s: no splitAgents — the counter and the list split agents each in their own way again", chatFile)
	}

	for _, head := range []string{"function Work(", "function WorkList(", "function AgentRow("} {
		block := withoutComments(jsBlock(t, chatFile, body, head))
		if strings.Contains(block, `"reported"`) {
			t.Errorf("%s: %s knows about \"reported\" on its own — only splitAgents may decide "+
				"the state of an agent, otherwise the counter drifts from the list", chatFile, head)
		}
	}
	for _, head := range []string{"function Work(", "function WorkList("} {
		if !strings.Contains(jsBlock(t, chatFile, body, head), "splitAgents(") {
			t.Errorf("%s: %s does not ask splitAgents — the number in the row and the list in the sheet "+
				"count agents differently", chatFile, head)
		}
	}

	list := jsBlock(t, chatFile, body, "function WorkList(")
	for _, want := range []string{"live.map(", "said.map("} {
		if !strings.Contains(list, want) {
			t.Errorf("%s: WorkList does not show %q — half the agents disappear "+
				"from the list", chatFile, want)
		}
	}
	if strings.Index(list, "live.map(") > strings.Index(list, "said.map(") {
		t.Errorf("%s: the ones that reported stand above the ones at work — the list reads top down, "+
			"and what the session is waiting for has to come first", chatFile)
	}

	work := jsBlock(t, chatFile, body, "function Work(")
	if strings.Contains(work, "agents.length > 0 && html") {
		t.Errorf("%s: the agent counter is drawn only when the session has had agents — it stands "+
			"in the row whatever it holds and says by going dim that it holds nothing", chatFile)
	}
	label := jsBlock(t, chatFile, body, "function agentLabel(")
	if !strings.Contains(label, `"subagents: none"`) || !strings.Contains(label, "none working") {
		t.Errorf("%s: a session that never had an agent and one whose agents have all reported are "+
			"named by the counter in the same words — the list under it says two different things",
			chatFile)
	}
	if !strings.Contains(work, "live.length > 0 && html") {
		t.Errorf("%s: the number on the agent counter is not tied to the working ones — a 0 in the row "+
			"reads as two at work", chatFile)
	}
}

const agentChatFile = "../agent/chat/cards.py"

func TestEveryAskRoundOutcomeIsNamed(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)
	raw, err := os.ReadFile(webPath(agentChatFile))
	if err != nil {
		t.Fatalf("%s not found: %v", agentChatFile, err)
	}

	round := pyBlock(t, agentChatFile, string(raw), "def ask_round(")
	found := regexp.MustCompile(`status = "([a-z]+)"`).FindAllStringSubmatch(round, -1)
	if len(found) == 0 {
		t.Fatalf("no round status found in %s — the test is useless, check the anchor", agentChatFile)
	}

	names := jsBlock(t, chatFile, body, "const ROUND_NAMES = {")
	for _, one := range found {
		if !regexp.MustCompile(`(?m)^\s+` + one[1] + `:\s*"`).MatchString(names) {
			t.Errorf("round status %q is not put into words in ROUND_NAMES (%s): the card "+
				"shows a round with no answer at all and does not say why there are none", one[1], chatFile)
		}
	}
}

func TestAnsweredAskRoundReplacesHangingCard(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)

	hidden := regexp.MustCompile(`const HIDDEN = new Set\(\[([^\]]*)\]\)`).FindStringSubmatch(body)
	if hidden == nil {
		t.Fatalf("%s has no HIDDEN — nothing to check, check the anchor", chatFile)
	}
	if strings.Contains(hidden[1], `"asked"`) {
		t.Errorf("%s: the asked role got into HIDDEN — the question card goes dark and nothing "+
			"shows up in its place: the round disappears from the feed entirely", chatFile)
	}

	if !regexp.MustCompile(`state\.work\.ask\s*&&\s*!closed\(`).MatchString(withoutComments(body)) {
		t.Errorf("%s: the card of a hanging question is drawn without a check for a closed round — "+
			"it stays on screen next to its own result and calls for a second answer", chatFile)
	}

	guard := jsBlock(t, chatFile, body, "function closed(")
	for _, want := range []string{`role === "asked"`, "item.use === use"} {
		if !strings.Contains(guard, want) {
			t.Errorf("%s: closed() does not check %s — the question that goes dark is not the one "+
				"that was answered", chatFile, want)
		}
	}
}

func TestAskAnswerClosesSheetAndKeepsRefusalOnCard(t *testing.T) {
	const askFile = "src/screens/ask.js"
	body := screenSrc(t, askFile)
	block := withoutComments(jsUntil(t, askFile, body, "const send = async (all, words) => {", "\n    };"))

	closeAt := strings.Index(block, "setOpen(false)")
	runAt := strings.Index(block, "await run(")
	if closeAt < 0 || runAt < 0 {
		t.Fatalf("%s: sending the answer has neither the sheet closing nor the request itself — "+
			"the test is useless, check the anchor", askFile)
	}
	if closeAt > runAt {
		t.Errorf("%s: the question sheet closes after the executor answers — on a busy session "+
			"that is a couple of seconds of dimmed buttons and the words Sending the answer…", askFile)
	}

	if strings.Contains(block, "setTimeout(") {
		t.Errorf("%s: sending locks the card by timer again — the sheet closes on the press, "+
			"there is nothing left to lock", askFile)
	}

	fail := jsUntil(t, askFile, block, "if (!result.ok) {", "\n        }")
	for _, want := range []struct{ call, why string }{
		{"setFail(", "the refusal leaves with the toast and the question keeps hanging"},
		{"setOpen(true)", "the refusal lands on a closed sheet, that is, nowhere"},
	} {
		if !strings.Contains(fail, want.call) {
			t.Errorf("%s: a failed send does not call %s — %s", askFile, want.call, want.why)
		}
	}

	wait := jsUntil(t, askFile, body, `<span class="waittext">`, "</span>")
	if !strings.Contains(wait, "sending ?") {
		t.Errorf("%s: the waiting line does not tell an answer on its way from a session waiting — "+
			"a closed sheet reads as a press that did not work", askFile)
	}
	if strings.Contains(body, "setSent(") {
		t.Errorf("%s: the answer sent line is back — after a successful answer the chat screen "+
			"takes the card away, and there is nobody to show it to", askFile)
	}
}

const waitsFile = "src/ui/waits.js"

func TestWaitReasonsComeFromClaude(t *testing.T) {
	files := srcFiles(t)
	src, ok := files[waitsFile]
	if !ok {
		t.Fatalf("the dictionary of waiting reasons is not found (%s) — the test is useless", waitsFile)
	}

	want := []string{"dialog open", "input needed", "sandbox request", "goal proposal", "worker request"}
	for _, reason := range want {
		if !strings.Contains(src, `"`+reason+`"`) {
			t.Errorf("reason %q is not put into words in %s", reason, waitsFile)
		}
	}

	keys := regexp.MustCompile(`(?m)^\s*"([^"]+)":`).FindAllStringSubmatch(src, -1)
	if len(keys) != len(want) {
		t.Errorf("the dictionary holds %d reasons while claude has %d: %v", len(keys), len(want), keys)
	}

	all := map[string]string{}
	for path, body := range files {
		all[path] = body
	}
	literal := regexp.MustCompile(`waitingFor\s*[=!]==?\s*"`)
	for _, path := range sortedKeys(all) {
		if path == waitsFile {
			continue
		}
		if literal.MatchString(all[path]) {
			t.Errorf("%s compares the waiting reason against a literal, past %s", path, waitsFile)
		}
	}
}

func TestAnsweredHidesStaleWaiting(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	files := srcFiles(t)
	body := withoutComments(files[chatFile])
	if body == "" {
		t.Fatalf("%s not found", chatFile)
	}

	if !regexp.MustCompile(`live\.status === "waiting" && !holding\b`).MatchString(body) {
		t.Errorf("%s: the permission card is drawn from status === \"waiting\" alone — "+
			"after an answer from the panel a lagging snapshot brings it back for 5–10 seconds", chatFile)
	}
	if !strings.Contains(body, "const holding = lagging(answer, live)") {
		t.Errorf("%s: the flag for a lagging snapshot is computed by something other than lagging() "+
			"from answered.js — a second opinion on the same thing drifts from the first on the first edit", chatFile)
	}

	if !regexp.MustCompile(`!closed\(state\.items, state\.work\.ask\.toolUseId\)\s*&& !hidesAsk\(answer, state\.work\.ask\.toolUseId\)`).MatchString(body) {
		t.Errorf("%s: the question card is not checked against the answer mark by toolUseId — "+
			"the next feed poll brings the answered question back until the agent has seen tool_result", chatFile)
	}

	for _, want := range []string{
		`onAnswered=${(use) => mark(answered(live, use))}`,
		`onAnswered=${() => mark(answered(live, ""))}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: no %s — one of the two answers sets no mark, and its snapshot "+
				"lags just as before", chatFile, want)
		}
	}
	for _, c := range []struct{ file, head, tail string }{
		{"src/screens/ask.js", "const send = async (all, words) => {", "\n    };"},
		{"src/screens/ask.js", "const drop = async () => {", "\n    };"},
		{"src/screens/chat/permit.js", "const press = async (option) => {", "\n    };"},
		{"src/screens/chat/permit.js", "const escape = async () => {", "\n    };"},
	} {
		block := withoutComments(jsUntil(t, c.file, files[c.file], c.head, c.tail))
		runAt := strings.Index(block, "await run(")
		at := strings.Index(block, "onAnswered(")
		if runAt < 0 || at < 0 || at < runAt {
			t.Errorf("%s: %s does not call onAnswered after the host answers — the chat screen never "+
				"learns that an answer was given, and the card stands until the snapshot", c.file, firstLine(c.head))
		}
		if fail := strings.Index(block, "if (!result.ok) {"); fail >= 0 && at < fail {
			t.Errorf("%s: %s sets the mark before the refusal is handled — a refused answer "+
				"would hide a card that was never answered", c.file, firstLine(c.head))
		}
	}

	if !regexp.MustCompile(`const next = settle\(answer, live\);\s*if \(next !== answer\) mark\(next\);`).MatchString(body) {
		t.Errorf("%s: the snapshot is not checked against the mark (settle) — after busy a new dialog "+
			"with the same reason stays hidden until the ceiling", chatFile)
	}
	if !regexp.MustCompile(`\[name, liveStatus, liveWait, liveStatusAt\]`).MatchString(body) ||
		!strings.Contains(body, "live.statusUpdatedAt") {
		t.Errorf("%s: checking the snapshot against the mark does not depend on the session and statusUpdatedAt — "+
			"a new dialog with the same reason stays hidden until the ceiling", chatFile)
	}
	if !strings.Contains(body, "setTimeout(() => mark(null)") {
		t.Errorf("%s: there is no ceiling — an answer that never arrived hides the dialog forever", chatFile)
	}
	if !strings.Contains(body, "const answer = recall(name)") || !strings.Contains(body, "remember(name, next)") {
		t.Errorf("%s: the mark is not kept in the store of answered.js by session — it dies with the screen "+
			"on navigation, and on return the composer locks and the answered card comes back "+
			"until the snapshot catches up", chatFile)
	}
	if strings.Contains(body, "setAnswer") {
		t.Errorf("%s: the mark is held in screen state past the store — a second copy drifts from "+
			"the first on the first edit, and on a session switch one of them is stale", chatFile)
	}

	if !strings.Contains(body, "hold=${holding}") {
		t.Errorf("%s: the composer does not know that by the snapshot the session is still in a dialog — "+
			"the dialog eats the reply, not the composer", chatFile)
	}
	if !strings.Contains(body, "deliver(run, name, row.hold)") {
		t.Errorf("%s: a held reply does not leave through deliver() — that is, either it does not "+
			"leave at all or it leaves as a second copy of the action pick", chatFile)
	}
	if strings.Contains(body, `run("session.`) {
		t.Errorf("%s: the chat screen sends the session action itself, past deliver() from the composer", chatFile)
	}
	if !strings.Contains(body, `l.state !== "held"`) {
		t.Errorf("%s: a reply waiting to be sent is cleared by a text match with someone else's — "+
			"what was typed is lost", chatFile)
	}

	const composerFile = "src/screens/chat/composer.js"
	send := withoutComments(jsUntil(t, composerFile, files[composerFile], "const send = async () => {", "\n    };"))
	holdAt := strings.Index(send, "if (hold) {")
	deliverAt := strings.Index(send, "await deliver(")
	if holdAt < 0 || deliverAt < 0 || holdAt > deliverAt {
		t.Fatalf("%s: sending has no hold branch before deliver — the reply goes into the dialog", composerFile)
	}
	held := send[holdAt:deliverAt]
	for _, want := range []string{`state: "held"`, "hold: { text: body, files: pack }", "return;"} {
		if !strings.Contains(held, want) {
			t.Errorf("%s: the hold branch does not contain %s — the reply either never appears in the feed "+
				"or leaves twice", composerFile, want)
		}
	}
}

func TestSilentThoughtSaysWhyItIsSilent(t *testing.T) {
	src := stripComments(srcFiles(t)["src/screens/chat/calls.js"])
	if src == "" {
		t.Fatal("src/screens/chat/calls.js not found")
	}

	at := strings.Index(src, `<div class="callnode still"`)
	if at < 0 {
		t.Fatal("there is no thinking node at all — then there is nothing to explain a pause in the chat with")
	}
	quiet := src[at:]
	if cut := strings.Index(quiet, "</div>"); cut > 0 {
		quiet = quiet[:cut]
	}

	key := "key=${" + "`" + "think-"
	if n := strings.Count(src, key); n != 1 {
		t.Errorf("%d thinking nodes in the timeline, and there is only one", n)
	}
	if !strings.Contains(quiet, key) {
		t.Error("the thinking node became expandable — and there is nothing to expand under it: " +
			"the text of a thought rides as a feed row, and only silent blocks reach the timeline")
	}
	if !strings.Contains(quiet, "text not recorded") {
		t.Error("the node says nothing about the reason: a person taps the nodes, nothing happens, " +
			"and the panel looks broken")
	}
	if !strings.Contains(quiet, "shortTokens(call.tokens)") {
		t.Error("the node no longer names the size of the thinking")
	}
}
