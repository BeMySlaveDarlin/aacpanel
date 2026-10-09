package check

import (
	"os"
	"strings"
	"testing"
)

// A codex thread stands on the lists beside claude sessions with a mark of
// its own, and the panel offers it exactly what the host does for codex:
// text into its turn, a stop of the turn, and the answer to what it asks.
// Everything claude's — the pickers, the commands, the shell, the files, the
// terminal, the move, the window, Remote Control, the name, the end — is not
// there, and the screen does not ask the host about any of it.
func TestACodexThreadIsOfferedOnlyWhatTheHostDoesForIt(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		DeskMarks   []string `json:"deskMarks"`
		DeskMarkTip string   `json:"deskMarkTip"`
		DeskActs    int      `json:"deskActs"`
		DeskSay     string   `json:"deskSay"`
		ClaudeMarks []string `json:"claudeMarks"`
		ClaudeActs  int      `json:"claudeActs"`
		PhoneTags   []string `json:"phoneTags"`
		PhoneState  string   `json:"phoneState"`
		SheetSub    string   `json:"sheetSub"`
		Sheet       []struct {
			Text string `json:"text"`
			Off  bool   `json:"off"`
		} `json:"sheet"`
		HeadMark    string          `json:"headMark"`
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
		Send        []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"send"`
		Queued       bool     `json:"queued"`
		TakeBack     bool     `json:"takeBack"`
		Panel        []string `json:"panel"`
		PanelStopOff *bool    `json:"panelStopOff"`
		StopSent     []string `json:"stopSent"`
		Asked        []string `json:"asked"`
	}
	runWideFixture(t, "codexsessions.html", &got)

	const name = "codex-5afc361b"
	plainSend := "send to session " + name

	// The mark, on both lists and in the header.
	if strings.Join(got.DeskMarks, ",") != "codex" || !strings.Contains(got.DeskMarkTip, "codex thread") {
		t.Errorf("the desktop row of the codex thread is marked %v (tip %q), expected codex alone", got.DeskMarks, got.DeskMarkTip)
	}
	if len(got.ClaudeMarks) != 0 || got.ClaudeActs != 1 {
		t.Errorf("the claude row beside it carries marks %v and %d actions: it is drawn as before, unmarked and closable",
			got.ClaudeMarks, got.ClaudeActs)
	}
	if got.DeskActs != 0 {
		t.Errorf("the desktop row of the codex thread offers %d actions: it is neither closed nor restarted from the panel", got.DeskActs)
	}
	if !strings.Contains(got.DeskSay, "waiting") {
		t.Errorf("the desktop row of a waiting codex thread says %q", got.DeskSay)
	}
	if strings.Join(got.PhoneTags, ",") != "stream,codex" {
		t.Errorf("the phone marks the rows %v, expected the claude session on the stream and the codex thread as codex", got.PhoneTags)
	}
	if !strings.Contains(got.PhoneState, "waiting") {
		t.Errorf("the phone row of a waiting codex thread says %q", got.PhoneState)
	}
	if got.HeadMark != "codex" {
		t.Errorf("the header of the conversation marks it %q, expected codex", got.HeadMark)
	}

	// The phone's sheet: open it, answer it, stop its turn — nothing else.
	var sheet []string
	for _, l := range got.Sheet {
		sheet = append(sheet, l.Text)
		if l.Off {
			t.Errorf("the sheet line %q is off for a waiting codex thread", l.Text)
		}
	}
	if strings.Join(sheet, ",") != "Open the conversation,Answer what it asks,Stop the turn" {
		t.Errorf("the phone's sheet of a waiting codex thread offers %v", sheet)
	}
	if !strings.HasPrefix(got.SheetSub, "codex · gpt-6-astra") {
		t.Errorf("the sheet says %q under the name, expected where it lives and what it runs", got.SheetSub)
	}

	// The conversation: the feed and its files, no terminal, no move.
	if strings.Join(got.Tabs, ",") != "Feed,Files" {
		t.Errorf("the conversation of a codex thread is watched with %v, expected the feed and the files alone", got.Tabs)
	}
	if !strings.Contains(got.Place, "codex") {
		t.Errorf("the session button says %q — the thread lives with codex", got.Place)
	}
	for tool, shown := range got.ClaudeTools {
		if shown {
			t.Errorf("the conversation of a codex thread shows claude's %s", tool)
		}
	}

	// The composer: the stop of a busy turn, and text alone.
	if got.StopOff == nil || *got.StopOff || !strings.Contains(got.StopLabel, "stop the work of session "+name) {
		t.Errorf("an empty composer of a busy codex thread offers the stop %q (off %v)", got.StopLabel, got.StopOff)
	}
	if got.SlashList || got.SlashLabel != plainSend {
		t.Errorf("a slash typed to codex opens the list of commands (%v) or sends as %q", got.SlashList, got.SlashLabel)
	}
	if got.BangLabel != plainSend {
		t.Errorf("a leading ! typed to codex goes out as %q, expected a plain message", got.BangLabel)
	}
	if got.Clipped || !strings.Contains(got.PasteToast, "takes text only") {
		t.Errorf("a pasted file is attached (%v) or the toast says %q", got.Clipped, got.PasteToast)
	}
	if len(got.Send) != 1 || got.Send[0].Target != name || got.Send[0].Params["text"] != "and now say it in one word" {
		t.Fatalf("the message went out as %+v", got.Send)
	}
	if _, ok := got.Send[0].Params["messageId"]; ok || len(got.Send[0].Params) != 1 {
		t.Errorf("the message to codex carries %v: text alone goes to codex, with nothing to take it back by", got.Send[0].Params)
	}
	if !got.Queued || got.TakeBack {
		t.Errorf("the message sent stands in the feed %v, with the buttons to take it back %v", got.Queued, got.TakeBack)
	}

	// The session panel: where it lives, its id, the stop of its turn.
	if strings.Join(got.Panel, ",") != "With codex,Copy the session ID,Stop the turn" {
		t.Errorf("the session panel of a codex thread lists %v", got.Panel)
	}
	if got.PanelStopOff == nil || *got.PanelStopOff || strings.Join(got.StopSent, ",") != name {
		t.Errorf("the stop of a busy codex turn is off %v and sent to %v", got.PanelStopOff, got.StopSent)
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

// On a phone the conversation of a codex thread carries its mark beside the
// name, a composer of text alone with no band of claude's settings under it,
// and in the header's tools nothing but where it lives, its id and the stop
// of its turn. Waiting on a command, it shows the card with the answers codex
// gave, every one of them and in its order, and a press sends the number of
// the answer with the request it belongs to.
func TestACodexThreadOnAPhoneTakesTextAndAnswersItsPermission(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		Mark        string   `json:"mark"`
		Strip       bool     `json:"strip"`
		ClaudeTools []string `json:"claudeTools"`
		Placeholder string   `json:"placeholder"`
		Tools       []string `json:"tools"`
		StopOff     *bool    `json:"stopOff"`
		StopWhy     string   `json:"stopWhy"`
		Permit      struct {
			Tool    string `json:"tool"`
			Action  string `json:"action"`
			Options []struct {
				N       string `json:"n"`
				Text    string `json:"text"`
				Lasting bool   `json:"lasting"`
			} `json:"options"`
		} `json:"permit"`
		ComposerUnder bool `json:"composerUnder"`
		PermitSent    []struct {
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"permitSent"`
	}
	runFixture(t, "codexchat.html", &got)

	if got.Mark != "codex" {
		t.Errorf("the header marks the thread %q, expected codex", got.Mark)
	}
	if got.Strip || len(got.ClaudeTools) != 0 {
		t.Errorf("the composer of a codex thread has the band of settings %v and claude's tools %v", got.Strip, got.ClaudeTools)
	}
	if got.Placeholder != "Write to codex-5afc361b" {
		t.Errorf("the field says %q", got.Placeholder)
	}
	if strings.Join(got.Tools, ",") != "Find in the conversation,Files of the project,With codex,Copy the session ID,Stop the turn" {
		t.Errorf("the tools of a codex thread list %v", got.Tools)
	}
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
