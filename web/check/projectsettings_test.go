package check

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"aacpanel/internal/schema"
)

// schemaAnswer serves what GET /api/profiles/schema answers, from the schema
// the service is built with.
func schemaAnswer() map[string]http.Handler {
	return map[string]http.Handler{
		"/api/profiles/schema": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"params":  schema.Params(),
				"retired": schema.RetiredKeys(),
				"layers":  []string{schema.LayerClaude, schema.LayerPanel, schema.LayerAccount, schema.LayerContour, schema.LayerProject},
				"traits":  map[string]any{},
			})
		}),
	}
}

// The settings page of a project draws a row per launch parameter of the
// schema; a change goes into the draft and the one bar counts it; the command
// is asked of the service again with the draft and the changed words are
// marked; the feed turns the permissions live; a draft the launch refuses
// holds Save with one press out; a plain Save goes without a sheet and sends
// only what changed; a new directory and a new group get their sheet, which
// names both; leaving a draft asks.
func TestTheProjectSettingsPageKeepsADraft(t *testing.T) {
	var got struct {
		Rows                []string       `json:"rows"`
		ConsoleInherited    string         `json:"consoleInherited"`
		Account             []string       `json:"account"`
		Hints               []string       `json:"hints"`
		RcOwn               string         `json:"rcOwn"`
		EffortInherited     string         `json:"effortInherited"`
		EffortFrom          string         `json:"effortFrom"`
		BarAtStart          bool           `json:"barAtStart"`
		BackIsNoChange      bool           `json:"backIsNoChange"`
		BarAfterOne         string         `json:"barAfterOne"`
		EffortRowDraft      string         `json:"effortRowDraft"`
		DraftedWords        []string       `json:"draftedWords"`
		PermLiveTmux        string         `json:"permLiveTmux"`
		BarAfterTwo         string         `json:"barAfterTwo"`
		PermLiveStream      string         `json:"permLiveStream"`
		FeedSays            []string       `json:"feedSays"`
		Blocked             string         `json:"blocked"`
		SaveDisabled        bool           `json:"saveDisabled"`
		Exit                string         `json:"exit"`
		AfterExit           string         `json:"afterExit"`
		PinRemoved          string         `json:"pinRemoved"`
		Patch               map[string]any `json:"patch"`
		SheetOnPlainSave    bool           `json:"sheetOnPlainSave"`
		Done                []bool         `json:"done"`
		BarAfterSave        bool           `json:"barAfterSave"`
		PathSays            string         `json:"pathSays"`
		PathSheet           string         `json:"pathSheet"`
		GestureAsks         bool           `json:"gestureAsks"`
		ClosedByGesture     int            `json:"closedByGesture"`
		Leave               string         `json:"leave"`
		ClosedBeforeDiscard int            `json:"closedBeforeDiscard"`
		ClosedAfterDiscard  int            `json:"closedAfterDiscard"`
		Previews            int            `json:"previews"`
	}
	runFixtureServing(t, "projectsettings.html", phoneScreen, phonePointer, schemaAnswer(), &got)

	want := []string{"Model", "Effort", "Permissions", "Remote Control", "First message", "Context cap",
		"Auto restart", "Message after a restart", "Environment", "Extra arguments"}
	if strings.Join(got.Rows, "|") != strings.Join(want, "|") {
		t.Errorf("the rows are %v, meant %v", got.Rows, want)
	}
	if got.ConsoleInherited != "1" || got.RcOwn != "0" || got.EffortInherited != "1" {
		t.Errorf("own and inherited are not told apart: console %q, RC %q, effort %q", got.ConsoleInherited, got.RcOwn, got.EffortInherited)
	}
	if got.EffortFrom != "Extra — the account" {
		t.Errorf("an effort from the account reads %q", got.EffortFrom)
	}
	if len(got.Account) != 2 || got.Account[1] != "from the account, not in the command: opus[1m] · xhigh" {
		t.Errorf("the account lines read %v", got.Account)
	}
	if len(got.Hints) != 1 || !strings.Contains(got.Hints[0], "Permissions Auto here is the same as the account") {
		t.Errorf("a pin repeating the account is not offered for removal: %v", got.Hints)
	}
	if got.BarAtStart {
		t.Error("the bar stands with nothing in the draft")
	}
	if !got.BackIsNoChange {
		t.Error("a value put back as it was still counts as a change")
	}
	if got.BarAfterOne != "1 change" || got.EffortRowDraft != "1" {
		t.Errorf("one change reads %q, the row marked %q", got.BarAfterOne, got.EffortRowDraft)
	}
	if strings.Join(got.DraftedWords, " ") != "--effort max" {
		t.Errorf("the changed words of the command are %v, meant --effort max", got.DraftedWords)
	}
	if got.PermLiveTmux != "next start" || got.PermLiveStream != "now" {
		t.Errorf("permissions take a change %q in the console and %q in the feed", got.PermLiveTmux, got.PermLiveStream)
	}
	if got.BarAfterTwo != "2 changes" || len(got.FeedSays) == 0 {
		t.Errorf("after the feed: %q, the card says %v", got.BarAfterTwo, got.FeedSays)
	}
	if !strings.Contains(got.Blocked, "-p: the panel decides where the session lives") || !got.SaveDisabled || got.Exit != "Undo the change" {
		t.Errorf("a refused draft reads %q, Save disabled %v, the way out %q", got.Blocked, got.SaveDisabled, got.Exit)
	}
	if got.AfterExit != "2 changes" {
		t.Errorf("after the way out the bar reads %q", got.AfterExit)
	}
	set, _ := got.Patch["launchSet"].(map[string]any)
	unset, _ := got.Patch["launchUnset"].([]any)
	if len(got.Patch) != 2 || set["effort"] != "max" || set["transport"] != "stream" || len(set) != 2 ||
		len(unset) != 1 || unset[0] != "permissionMode" {
		t.Errorf("Save sent %v, meant the two launch keys set and the pin removed — nothing else", got.Patch)
	}
	if got.PinRemoved != "Auto — the account" {
		t.Errorf("with the pin removed the permissions read %q, meant what the account gives", got.PinRemoved)
	}
	if got.SheetOnPlainSave || len(got.Done) != 1 || !got.Done[0] || got.BarAfterSave {
		t.Errorf("a plain Save: sheet %v, done %v, bar left %v", got.SheetOnPlainSave, got.Done, got.BarAfterSave)
	}
	if !strings.Contains(got.PathSays, "stay in the archive") || !strings.Contains(got.PathSheet, "stay in the archive") ||
		!strings.Contains(got.PathSheet, `moves to group "pets"`) {
		t.Errorf("a new directory and group say %q under the field and %q on their sheet", got.PathSays, got.PathSheet)
	}
	if got.Leave != "2 changes not saved" || got.ClosedBeforeDiscard != 0 || got.ClosedAfterDiscard != 1 {
		t.Errorf("leaving a draft: %q, closed %d before Discard and %d after", got.Leave, got.ClosedBeforeDiscard, got.ClosedAfterDiscard)
	}
	if !got.GestureAsks || got.ClosedByGesture != 0 {
		t.Errorf("the back gesture over a draft: asked %v, closed the page %d times — the draft is thrown away unasked", got.GestureAsks, got.ClosedByGesture)
	}
	if got.Previews == 0 {
		t.Error("the command was never asked of the service for the draft")
	}
}
