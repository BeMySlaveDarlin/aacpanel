package check

import (
	"os"
	"strings"
	"testing"
)

// agentWord is the word that opens the quiet line of a live session's model,
// as a fixture reads it: what it says, whose it is, its tip and its hue.
type agentWord struct {
	Text   string `json:"text"`
	Agent  string `json:"agent"`
	Tip    string `json:"tip"`
	Colour string `json:"colour"`
}

// The words Claude and Codex are painted in the colours of the agents' own
// marks wherever they say who runs a session: Anthropic's terracotta and the
// middle blue of codex's badge on the dark theme the fixtures run in, a
// darker pair on the sky. Only the words: a bar or a ring of an agent keeps
// the colours of the panel.
const (
	claudeWordHue    = "rgb(217, 119, 87)"
	codexWordHue     = "rgb(122, 157, 255)"
	claudeWordSkyHue = "rgb(193, 95, 60)"
	codexWordSkyHue  = "rgb(57, 65, 255)"
)

// A codex thread stands on the lists beside claude sessions, told from them
// by the word that opens the line of its model: Codex, in the colour of
// codex's mark, where theirs says Claude in claude's; it lives on the daemon, not on the stream. The
// panel offers it exactly what the host does for codex: words and files into
// its turn, named in the queue of a busy thread so they can be taken back, how
// it thinks and what it may do, a stop of the turn, the answer to what it
// asks, and a close. Everything claude's — its pickers, the commands, the
// shell, the terminal, the move, the window, Remote Control, the name, the
// restart — is not there, and the screen does not ask the host about any of it. The close says what it
// does to codex wherever it is offered: the desktop row, the phone's sheet and
// the session panel, and the sheet of the gate behind each of them, which has
// no kill under it — the host does not kill codex.
func TestACodexThreadIsOfferedOnlyWhatTheHostDoesForIt(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		DeskMarks   []string    `json:"deskMarks"`
		DeskFacts   []string    `json:"deskFacts"`
		DeskAgent   agentWord   `json:"deskAgent"`
		DeskActs    []rowAct    `json:"deskActs"`
		DeskSay     string      `json:"deskSay"`
		ClaudeMarks []string    `json:"claudeMarks"`
		ClaudeAgent agentWord   `json:"claudeAgent"`
		ClaudeActs  int         `json:"claudeActs"`
		PhoneTags   []string    `json:"phoneTags"`
		PhoneState  string      `json:"phoneState"`
		PhoneSince  string      `json:"phoneSince"`
		PhoneAgents []agentWord `json:"phoneAgents"`
		SheetSub    string      `json:"sheetSub"`
		SheetAgent  agentWord   `json:"sheetAgent"`
		Sheet       []struct {
			Text string `json:"text"`
			Note string `json:"note"`
			Off  bool   `json:"off"`
		} `json:"sheet"`
		HeadMarks   []string        `json:"headMarks"`
		HeadSub     string          `json:"headSub"`
		HeadAgent   agentWord       `json:"headAgent"`
		Tabs        []string        `json:"tabs"`
		Place       string          `json:"place"`
		ClaudeTools map[string]bool `json:"claudeTools"`
		StopLabel   string          `json:"stopLabel"`
		StopOff     *bool           `json:"stopOff"`
		SlashList   bool            `json:"slashList"`
		SlashLabel  string          `json:"slashLabel"`
		BangLabel   string          `json:"bangLabel"`
		PasteToast  string          `json:"pasteToast"`
		Clipped     bool            `json:"clipped"`
		Band        []string        `json:"band"`
		Placeholder string          `json:"placeholder"`
		FileSent    []struct {
			Target string   `json:"target"`
			Text   string   `json:"text"`
			Files  []string `json:"files"`
		} `json:"fileSent"`
		Send []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"send"`
		Queued   bool `json:"queued"`
		TakeBack bool `json:"takeBack"`
		Unqueued []struct {
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"unqueued"`
		Gone         bool     `json:"gone"`
		Panel        []string `json:"panel"`
		PanelStopOff *bool    `json:"panelStopOff"`
		StopSent     []string `json:"stopSent"`
		PanelClose   *struct {
			Note string `json:"note"`
			Off  bool   `json:"off"`
		} `json:"panelClose"`
		PanelCloseSheet *gateSheet `json:"panelCloseSheet"`
		DeskCloseSheet  *gateSheet `json:"deskCloseSheet"`
		PhoneCloseSheet *gateSheet `json:"phoneCloseSheet"`
		Asked           []string   `json:"asked"`
	}
	runWideFixture(t, "codexsessions.html", &got)

	const name = "codex-5afc361b"
	plainSend := "send to session " + name

	// Who runs it opens the line of its model, on both lists and in the
	// header; no mark beside the name says it a second time.
	codexWord := func(where string, w agentWord) {
		t.Helper()
		if w.Text != "Codex" || w.Agent != "codex" {
			t.Errorf("%s opens with %+v, expected the word Codex, painted as codex's", where, w)
		}
	}
	codexWord("the line under the desktop row", got.DeskAgent)
	if !strings.Contains(got.DeskAgent.Tip, "codex thread") {
		t.Errorf("the word Codex on the desktop row has the tip %q, expected what the panel does to a codex thread", got.DeskAgent.Tip)
	}
	if len(got.DeskFacts) < 2 || got.DeskFacts[1] != "gpt-6-astra · Extra" {
		t.Errorf("the desktop row of the codex thread runs on %v, expected Codex and then its model", got.DeskFacts)
	}
	if len(got.DeskMarks) != 0 {
		t.Errorf("the desktop row of the codex thread is marked %v beside the name: who runs it opens the line of its model", got.DeskMarks)
	}
	if got.ClaudeAgent.Text != "Claude" || got.ClaudeAgent.Agent != "claude" || got.ClaudeAgent.Tip != "" {
		t.Errorf("the claude row beside it opens with %+v, expected the word Claude with no tip", got.ClaudeAgent)
	}
	if got.DeskAgent.Colour != codexWordHue || got.ClaudeAgent.Colour != claudeWordHue {
		t.Errorf("Codex is painted %q and Claude %q on the desktop rows, expected %s and %s, the colours of their marks",
			got.DeskAgent.Colour, got.ClaudeAgent.Colour, codexWordHue, claudeWordHue)
	}
	if len(got.ClaudeMarks) != 0 || got.ClaudeActs != 1 {
		t.Errorf("the claude row beside it carries marks %v and %d actions: it is unmarked and closable",
			got.ClaudeMarks, got.ClaudeActs)
	}
	if len(got.DeskActs) != 1 || got.DeskActs[0].Label != "close session "+name {
		t.Errorf("the desktop row of the codex thread offers %+v, expected its close alone: a codex thread is not restarted", got.DeskActs)
	} else {
		saysCodexClose(t, "the tip of the close on the desktop row", got.DeskActs[0].Tip)
	}
	if !strings.Contains(got.DeskSay, "waiting") {
		t.Errorf("the desktop row of a waiting codex thread says %q", got.DeskSay)
	}
	if strings.Join(got.PhoneTags, ",") != "stream,daemon" {
		t.Errorf("the phone marks the rows %v, expected the claude session on the stream and the codex thread on the daemon", got.PhoneTags)
	}
	if !strings.Contains(got.PhoneState, "waiting") {
		t.Errorf("the phone row of a waiting codex thread says %q", got.PhoneState)
	}
	if got.PhoneSince != "Codex · gpt-6-astra" {
		t.Errorf("under the state of the codex thread on the phone stands %q, expected who runs it and on which model", got.PhoneSince)
	}
	if len(got.PhoneAgents) != 2 {
		t.Fatalf("the phone shows %d rows with a line under the state: %+v", len(got.PhoneAgents), got.PhoneAgents)
	}
	codexWord("the line under the state on the phone", got.PhoneAgents[1])
	if got.PhoneAgents[0].Text != "Claude" || got.PhoneAgents[0].Agent != "claude" {
		t.Errorf("the claude row on the phone opens with %+v, expected the word Claude", got.PhoneAgents[0])
	}
	if got.PhoneAgents[1].Colour != codexWordHue || got.PhoneAgents[0].Colour != claudeWordHue {
		t.Errorf("Codex is painted %q and Claude %q on the phone rows, expected %s and %s, the colours of their marks",
			got.PhoneAgents[1].Colour, got.PhoneAgents[0].Colour, codexWordHue, claudeWordHue)
	}
	codexWord("the line under the name in the header", got.HeadAgent)
	if !strings.HasPrefix(got.HeadSub, "Codex · gpt-6-astra · answering") {
		t.Errorf("the line under the name in the header reads %q, expected who runs it, its model and how it stands", got.HeadSub)
	}
	if len(got.HeadMarks) != 0 {
		t.Errorf("the header of the conversation carries the marks %v beside the name", got.HeadMarks)
	}

	// The phone's sheet: open it, answer it, stop its turn, close it —
	// nothing else.
	var sheet []string
	for _, l := range got.Sheet {
		sheet = append(sheet, l.Text)
		if l.Off {
			t.Errorf("the sheet line %q is off for a waiting codex thread", l.Text)
		}
		if l.Text == "Close" {
			saysCodexClose(t, "the close on the phone's sheet", l.Note)
		}
	}
	if strings.Join(sheet, ",") != "Open the conversation,Answer what it asks,Stop the turn,Close" {
		t.Errorf("the phone's sheet of a waiting codex thread offers %v", sheet)
	}
	codexWord("the line under the name in the sheet", got.SheetAgent)
	if !strings.HasPrefix(got.SheetSub, "Codex · gpt-6-astra ·") || !strings.HasSuffix(got.SheetSub, "· daemon") {
		t.Errorf("the sheet says %q under the name, expected who runs it on which model, and where it lives last", got.SheetSub)
	}

	// The conversation: the feed and its files, no terminal, no move.
	if strings.Join(got.Tabs, ",") != "Feed,Files" {
		t.Errorf("the conversation of a codex thread is watched with %v, expected the feed and the files alone", got.Tabs)
	}
	if !strings.Contains(got.Place, "daemon") {
		t.Errorf("the session button says %q — the thread lives on the daemon", got.Place)
	}
	for tool, shown := range got.ClaudeTools {
		if shown {
			t.Errorf("the conversation of a codex thread shows claude's %s", tool)
		}
	}

	// The composer: codex's band, the stop of a busy turn, and a field that
	// says a message goes after the turn.
	if strings.Join(got.Band, ",") != "clip,think,perm,thread" {
		t.Errorf("the band under the field of a codex thread holds %v, expected the paperclip, how it thinks, what it may do and the thread", got.Band)
	}
	if got.Placeholder != "Goes after the turn" {
		t.Errorf("the field of a busy codex thread says %q, expected that a message goes after the turn", got.Placeholder)
	}
	if got.StopOff == nil || *got.StopOff || !strings.Contains(got.StopLabel, "stop the work of session "+name) {
		t.Errorf("an empty composer of a busy codex thread offers the stop %q (off %v)", got.StopLabel, got.StopOff)
	}
	if got.SlashList || got.SlashLabel != plainSend {
		t.Errorf("a slash typed to codex opens the list of commands (%v) or sends as %q", got.SlashList, got.SlashLabel)
	}
	if got.BangLabel != plainSend {
		t.Errorf("a leading ! typed to codex goes out as %q, expected a plain message", got.BangLabel)
	}
	if !got.Clipped || got.PasteToast != "" {
		t.Errorf("a pasted file is attached %v, and the toast says %q", got.Clipped, got.PasteToast)
	}
	if len(got.FileSent) != 1 || got.FileSent[0].Target != name || got.FileSent[0].Text != "here it is" ||
		strings.Join(got.FileSent[0].Files, ",") != "shot.png" {
		t.Errorf("the file went out as %+v, expected session.file with the words and shot.png", got.FileSent)
	}
	if len(got.Send) != 1 || got.Send[0].Target != name || got.Send[0].Params["text"] != "and now say it in one word" {
		t.Fatalf("the message went out as %+v", got.Send)
	}
	id, _ := got.Send[0].Params["messageId"].(string)
	if len(id) != 36 || len(got.Send[0].Params) != 2 {
		t.Errorf("the message to codex carries %v, expected its words and the name it is taken back by", got.Send[0].Params)
	}
	if !got.Queued || !got.TakeBack {
		t.Errorf("the message sent stands in the feed %v, with the buttons to take it back %v", got.Queued, got.TakeBack)
	}
	if len(got.Unqueued) != 1 || got.Unqueued[0].Target != name || got.Unqueued[0].Params["messageId"] != id || !got.Gone {
		t.Errorf("taking it back sent %+v and left the row %v, expected session.unqueue by the name it went with", got.Unqueued, !got.Gone)
	}

	// The session panel: where it lives, its id, the stop of its turn and the
	// close.
	if strings.Join(got.Panel, ",") != "With codex,MCP servers,Skills,Copy the session ID,Stop the turn,Close the session" {
		t.Errorf("the session panel of a codex thread lists %v", got.Panel)
	}
	if got.PanelStopOff == nil || *got.PanelStopOff || strings.Join(got.StopSent, ",") != name {
		t.Errorf("the stop of a busy codex turn is off %v and sent to %v", got.PanelStopOff, got.StopSent)
	}
	if got.PanelClose == nil || got.PanelClose.Off {
		t.Errorf("the session panel of a codex thread closes it with %+v, expected a close that presses", got.PanelClose)
	} else {
		saysCodexClose(t, "the close in the session panel", got.PanelClose.Note)
	}

	// Each close asks on the sheet of the gate, in codex's words and with no
	// kill under it, and the press sends the close of the thread.
	for where, sheet := range map[string]*gateSheet{
		"the session panel": got.PanelCloseSheet, "the desktop row": got.DeskCloseSheet,
		"the phone's sheet": got.PhoneCloseSheet,
	} {
		checkCodexCloseSheet(t, where, name, sheet)
	}

	for _, path := range got.Asked {
		for _, never := range []string{"/api/session/switch", "/api/session/window", "/api/session/commands",
			"/api/session/status", "/api/session/btw"} {
			if path == never {
				t.Errorf("the screen asked %s about a codex thread: the host does none of it for codex", path)
			}
		}
	}
}

// On a phone the conversation of a codex thread has the name alone in its
// heading, and the line under it opens with Codex and the model it runs; a
// composer with codex's own band under it — the paperclip, how it thinks and
// what it may do — and nothing of claude's,
// and in the header's tools nothing but where it lives, its id, the stop
// of its turn and its close, which says and asks what a close does to codex. Waiting on a command, it shows the card with the answers codex
// gave, every one of them and in its order, and a press sends the number of
// the answer with the request it belongs to.
func TestACodexThreadOnAPhoneTakesTextAndAnswersItsPermission(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		HeadExtra   []string  `json:"headExtra"`
		Agent       agentWord `json:"agent"`
		Sub         string    `json:"sub"`
		Strip       bool      `json:"strip"`
		ClaudeTools []string  `json:"claudeTools"`
		Band        []string  `json:"band"`
		Placeholder string    `json:"placeholder"`
		Tools       []string  `json:"tools"`
		StopOff     *bool     `json:"stopOff"`
		StopWhy     string    `json:"stopWhy"`
		Close       *struct {
			Note string `json:"note"`
			Off  bool   `json:"off"`
		} `json:"close"`
		CloseSheet *gateSheet `json:"closeSheet"`
		Permit     struct {
			Tool    string `json:"tool"`
			Action  string `json:"action"`
			Options []struct {
				N       string `json:"n"`
				Text    string `json:"text"`
				Lasting bool   `json:"lasting"`
			} `json:"options"`
		} `json:"permit"`
		ComposerUnder   bool     `json:"composerUnder"`
		BusyPlaceholder string   `json:"busyPlaceholder"`
		Attach          []string `json:"attach"`
		PermitSent      []struct {
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"permitSent"`
	}
	runFixture(t, "codexchat.html", &got)

	if len(got.HeadExtra) != 0 {
		t.Errorf("the heading carries %v beside the dot and the name", got.HeadExtra)
	}
	if got.Agent.Text != "Codex" || got.Agent.Agent != "codex" || !strings.HasPrefix(got.Sub, "Codex · gpt-6-astra ·") {
		t.Errorf("the line under the name reads %q and opens with %+v, expected Codex, painted as codex's, and its model",
			got.Sub, got.Agent)
	}
	if !got.Strip || strings.Join(got.Band, ",") != "clip,think,perm,thread" || len(got.ClaudeTools) != 0 {
		t.Errorf("the composer of a codex thread has the band %v of %v and claude's tools %v, expected the paperclip, "+
			"how it thinks and what it may do", got.Strip, got.Band, got.ClaudeTools)
	}
	if got.Placeholder != "Write to codex-5afc361b" {
		t.Errorf("the field says %q", got.Placeholder)
	}
	if got.BusyPlaceholder != "Goes after the turn, behind 2 queued" {
		t.Errorf("the field of a busy thread with two queued says %q", got.BusyPlaceholder)
	}
	if strings.Join(got.Attach, ",") != "Camera,Photos,Files" {
		t.Errorf("the paperclip of a codex thread opens %v, expected where files come from, as claude's does", got.Attach)
	}
	if strings.Join(got.Tools, ",") != "Find in the conversation,Files of the project,With codex,MCP servers,Skills,Copy the session ID,Stop the turn,Close the session" {
		t.Errorf("the tools of a codex thread list %v", got.Tools)
	}
	if got.Close == nil || got.Close.Off {
		t.Errorf("the tools of a codex thread close it with %+v, expected a close that presses", got.Close)
	} else {
		saysCodexClose(t, "the close in the tools on a phone", got.Close.Note)
	}
	checkCodexCloseSheet(t, "the tools on a phone", "codex-5afc361b", got.CloseSheet)
	if got.StopOff == nil || !*got.StopOff || got.StopWhy != "codex is not running a turn" {
		t.Errorf("the stop of an idle thread is off %v and says %q", got.StopOff, got.StopWhy)
	}

	want := []string{
		"Yes",
		"Yes, and don't ask again for commands that start with `touch late.txt`",
		"No, and stop: tell Codex what to do differently",
	}
	if len(got.Permit.Options) != len(want) {
		t.Fatalf("the card shows %d answers, codex gave %d: %+v", len(got.Permit.Options), len(want), got.Permit.Options)
	}
	for i, o := range got.Permit.Options {
		if o.Text != want[i] || o.N != []string{"1", "2", "3"}[i] {
			t.Errorf("answer %d of the card is %s %q, expected %d %q", i+1, o.N, o.Text, i+1, want[i])
		}
	}
	if !got.Permit.Options[1].Lasting || got.Permit.Options[0].Lasting || got.Permit.Options[2].Lasting {
		t.Errorf("the answer marked from now on: %+v — only the one that stops codex asking", got.Permit.Options)
	}
	if got.Permit.Tool != "Bash" || got.Permit.Action != "touch late.txt" {
		t.Errorf("the card asks about %q with %q", got.Permit.Tool, got.Permit.Action)
	}
	if got.ComposerUnder {
		t.Errorf("the composer stands under the card of a waiting thread")
	}
	if len(got.PermitSent) != 1 || got.PermitSent[0].Target != "codex-5afc361b" ||
		got.PermitSent[0].Params["option"] != float64(2) || got.PermitSent[0].Params["dialog"] != "req-7" {
		t.Errorf("the second answer went out as %+v, expected option 2 of request req-7", got.PermitSent)
	}
}

// rowAct is an action in the corner of a desktop row: what it is called and
// its tip.
type rowAct struct {
	Label string `json:"label"`
	Tip   string `json:"tip"`
}

// gateSheet is the sheet the gate put up for a press, as a fixture reads it:
// what it says follows, its own button, the harsher actions under it, and
// what its own button sent.
type gateSheet struct {
	Trouble string   `json:"trouble"`
	Title   string   `json:"title"`
	Effect  string   `json:"effect"`
	OK      string   `json:"ok"`
	Harsher []string `json:"harsher"`
	Sent    []struct {
		Kind   string `json:"kind"`
		Target string `json:"target"`
	} `json:"sent"`
}

// saysCodexClose checks that a line beside a close of a codex thread tells
// what a close does to codex rather than to claude.
func saysCodexClose(t *testing.T, where, note string) {
	t.Helper()
	for _, want := range []string{"turn at work breaks off", "lets the thread go", "codex resume", "tmux"} {
		if !strings.Contains(note, want) {
			t.Errorf("%s says %q, expected what a close does to codex: %q is missing", where, note, want)
		}
	}
	for _, claude := range []string{"archive", "history", "process"} {
		if strings.Contains(note, claude) {
			t.Errorf("%s says %q: %q is what a close does to claude", where, note, claude)
		}
	}
}

// checkCodexCloseSheet checks the sheet of the gate behind a close of a codex
// thread: codex's words, its own button for a plain close, no kill under it,
// and the close of the thread sent by the press.
func checkCodexCloseSheet(t *testing.T, where, name string, sheet *gateSheet) {
	t.Helper()
	if sheet == nil || sheet.Trouble != "" {
		t.Errorf("the close in %s put up no sheet: %+v", where, sheet)
		return
	}
	for _, want := range []string{"turn codex is running breaks off", "lets the thread go", "codex resume", "tmux"} {
		if !strings.Contains(sheet.Effect, want) {
			t.Errorf("the sheet of the close in %s says %q: %q is missing", where, sheet.Effect, want)
		}
	}
	if strings.Contains(sheet.Effect, "transcript") {
		t.Errorf("the sheet of the close in %s promises to wait for a transcript: %q", where, sheet.Effect)
	}
	if sheet.OK != "Close" {
		t.Errorf("the sheet of the close in %s presses %q, expected Close", where, sheet.OK)
	}
	if len(sheet.Harsher) != 0 {
		t.Errorf("the sheet of the close in %s offers %v under it: the host does not kill codex", where, sheet.Harsher)
	}
	if len(sheet.Sent) != 1 || sheet.Sent[0].Kind != "session.close" || sheet.Sent[0].Target != name {
		t.Errorf("the close in %s sent %+v, expected session.close of %s", where, sheet.Sent, name)
	}
}
