package check

import (
	"strings"
	"testing"
)

// The button after the paperclip in the composer's strip lists what the panel
// does itself, read from the registry the composer reads, and each row does
// what typing it does.
func TestTheCommandsButtonDoesWhatTypingDoes(t *testing.T) {
	var got struct {
		DeckOverflow  int      `json:"deckOverflow"`
		InStrip       bool     `json:"inStrip"`
		InDeck        bool     `json:"inDeck"`
		Order         []string `json:"order"`
		Gaps          []int    `json:"gaps"`
		StripOverflow int      `json:"stripOverflow"`
		CmdsIcon      bool     `json:"cmdsIcon"`
		PageOverflow  int      `json:"pageOverflow"`
		Title         string   `json:"title"`
		Groups        []string `json:"groups"`
		Lines         []string `json:"lines"`
		ModelNote     string   `json:"modelNote"`
		HooksTitle    string   `json:"hooksTitle"`
		Picker        string   `json:"picker"`
		Btw           string   `json:"btw"`
		SheetAfterBtw bool     `json:"sheetAfterBtw"`
		Confirm       string   `json:"confirm"`
		SentBefore    int      `json:"sentBefore"`
		Sent          []string `json:"sent"`
		SheetAfter    bool     `json:"sheetAfterSend"`
		ConsoleLines  []string `json:"consoleLines"`
		OldHostOff    bool     `json:"oldHostOff"`
		OldHostNote   string   `json:"oldHostNote"`
		Error         string   `json:"error"`
	}
	runFixture(t, "commandsheet.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if !got.InStrip || got.InDeck {
		t.Errorf("the commands button stands in the strip of the composer: %v, in the row under it: %v — "+
			"it belongs after the paperclip", got.InStrip, got.InDeck)
	}
	if strings.Join(got.Order, ",") != "file,commands,model,effort,mode" {
		t.Errorf("the strip reads %v: the paperclip, the commands, then the model, the effort and the mode", got.Order)
	}
	for i, gap := range got.Gaps {
		if gap < 6 {
			t.Errorf("the buttons %d and %d of the strip stand %d px apart: too close for a thumb to tell", i, i+1, gap)
		}
	}
	if got.StripOverflow > 0 {
		t.Errorf("the strip overflows the phone by %d px", got.StripOverflow)
	}
	if !got.CmdsIcon {
		t.Errorf("the commands button is not an icon: a slash in a row of words reads poorly")
	}
	if got.DeckOverflow > 0 || got.PageOverflow > 0 {
		t.Errorf("the row under the composer overflows the phone by %d px (the page by %d)", got.DeckOverflow, got.PageOverflow)
	}
	if got.Title != "Commands" || strings.Join(got.Groups, ",") != "Screens,How the session runs,The conversation" {
		t.Errorf("the list is %q with groups %v", got.Title, got.Groups)
	}
	for _, want := range []string{"/mcp", "/status", "/hooks", "/memory", "/skills", "/agents", "/config",
		"/model", "/effort", "Mode", "/btw", "/context", "/usage", "/compact"} {
		if !strings.Contains(" "+strings.Join(got.Lines, " ")+" ", " "+want+" ") {
			t.Errorf("the list has no %s: %v", want, got.Lines)
		}
	}
	if !strings.Contains(got.ModelNote, "Opus 5.5") {
		t.Errorf("the model row does not say what the session runs: %q", got.ModelNote)
	}
	if got.HooksTitle != "Hooks" {
		t.Errorf("/hooks opened %q", got.HooksTitle)
	}
	if got.Picker == "" {
		t.Error("/model opened no picker")
	}
	if got.Btw != "/btw why is the build slow" || got.SheetAfterBtw {
		t.Errorf("/btw left the composer as %q (sheet still open: %v)", got.Btw, got.SheetAfterBtw)
	}
	if !strings.Contains(got.Confirm, "/context") || got.SentBefore != 0 {
		t.Errorf("/context went out without the confirmation: %q, %d sent before it", got.Confirm, got.SentBefore)
	}
	if len(got.Sent) != 1 || got.Sent[0] != `session.command:{"command":"context","arg":""}` || got.SheetAfter {
		t.Errorf("the confirmed /context sent %v (sheet still open: %v)", got.Sent, got.SheetAfter)
	}
	console := " " + strings.Join(got.ConsoleLines, " ") + " "
	for _, gone := range []string{" /hooks ", " /mcp ", " /btw "} {
		if strings.Contains(console, gone) {
			t.Errorf("a console session is offered %s: %v", strings.TrimSpace(gone), got.ConsoleLines)
		}
	}
	if !strings.Contains(console, " /status ") || !strings.Contains(console, " /context ") {
		t.Errorf("a console session lost what works there: %v", got.ConsoleLines)
	}
	if !got.OldHostOff || got.OldHostNote == "" || got.OldHostNote == "Context usage" {
		t.Errorf("an executor that cannot send commands left /context pressable (%v) with the note %q",
			!got.OldHostOff, got.OldHostNote)
	}
}
