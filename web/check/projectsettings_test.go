package check

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"aacpanel/internal/schema"
)

// schemaAnswer serves what GET /api/profiles/schema answers, from the schema
// the service is built with, and what the models take as the host says it.
func schemaAnswer(traits map[string]any) map[string]http.Handler {
	return map[string]http.Handler{
		"/api/profiles/schema": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"params":  schema.Params(),
				"retired": schema.RetiredKeys(),
				"layers":  []string{schema.LayerClaude, schema.LayerPanel, schema.LayerAccount, schema.LayerContour, schema.LayerProject},
				"traits":  traits,
			})
		}),
	}
}

// A model that takes no effort and no Auto strikes them out: the project's
// own effort holds Save with the way out, the account's Auto is said to start
// as Manual and holds nothing.
func TestTheProjectSettingsPageStrikesWhatTheModelDoesNotTake(t *testing.T) {
	var got struct {
		Struck     []string `json:"struck"`
		Blocked    string   `json:"blocked"`
		Exit       string   `json:"exit"`
		AutoStruck string   `json:"autoStruck"`
		AutoSays   string   `json:"autoSays"`
	}
	serve := schemaAnswer(map[string]any{
		"claude-haiku-4-5-20251001": map[string]any{"effort": false, "autoMode": false},
	})
	serve["/mode"] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("haiku")) })
	runFixtureServing(t, "projectsettings.html", phoneScreen, phonePointer, serve, &got)

	if len(got.Struck) != 5 {
		t.Errorf("a model with no effort strikes %v, meant every effort", got.Struck)
	}
	if got.Blocked != "Haiku has no effort — High would not start" || got.Exit != "Remove Effort" {
		t.Errorf("the project's own effort on such a model reads %q with the way out %q", got.Blocked, got.Exit)
	}
	if got.AutoStruck != "1" || got.AutoSays != "Haiku has no Auto: Auto from the account would start as Manual" {
		t.Errorf("the account's Auto on such a model: struck %q, said %q", got.AutoStruck, got.AutoSays)
	}
}

// The settings page of a project draws a row per launch parameter of the
// schema — the common ones, then claude's under its open tab; a change goes
// into the draft and the one bar counts it; the command
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
		ApplyHead           string         `json:"applyHead"`
		ApplyNow            []string       `json:"applyNow"`
		ApplyLater          []string       `json:"applyLater"`
		Applied             map[string]any `json:"applied"`
		Moved               map[string]any `json:"moved"`
		PathSays            string         `json:"pathSays"`
		PathSheet           string         `json:"pathSheet"`
		GestureAsks         bool           `json:"gestureAsks"`
		ClosedByGesture     int            `json:"closedByGesture"`
		Leave               string         `json:"leave"`
		ClosedBeforeDiscard int            `json:"closedBeforeDiscard"`
		ClosedAfterDiscard  int            `json:"closedAfterDiscard"`
		Previews            int            `json:"previews"`
	}
	runFixtureServing(t, "projectsettings.html", phoneScreen, phonePointer, schemaAnswer(map[string]any{}), &got)

	want := []string{"Agent", "First message", "Model", "Effort", "Permissions", "Remote Control", "Panel tools",
		"Context cap", "Auto restart", "Message after a restart", "Environment", "Extra arguments"}
	if strings.Join(got.Rows, "|") != strings.Join(want, "|") {
		t.Errorf("the rows are %v, meant %v", got.Rows, want)
	}
	if got.ConsoleInherited != "1" || got.RcOwn != "0" || got.EffortInherited != "1" {
		t.Errorf("own and inherited are not told apart: tmux %q, RC %q, effort %q", got.ConsoleInherited, got.RcOwn, got.EffortInherited)
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
		t.Errorf("permissions take a change %q in tmux and %q on the stream", got.PermLiveTmux, got.PermLiveStream)
	}
	if got.BarAfterTwo != "2 changes" || len(got.FeedSays) == 0 {
		t.Errorf("after the stream: %q, the card says %v", got.BarAfterTwo, got.FeedSays)
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
	if got.ApplyHead != "2 sessions run now" || strings.Join(got.ApplyNow, "|") != "Effort Max|Move to the stream|Effort Max|Permissions Auto" {
		t.Errorf("after Save the running session is offered %q %v", got.ApplyHead, got.ApplyNow)
	}
	if strings.Join(got.ApplyLater, "|") != "Effort Max: in tmux claude also keeps it as the account's default for new sessions|Permissions Auto — at the next start" {
		t.Errorf("what the session takes later is said as %v", got.ApplyLater)
	}
	if p, _ := got.Applied["params"].(map[string]any); got.Applied["kind"] != "session.set" || got.Applied["target"] != "helios" || p["effort"] != "max" {
		t.Errorf("applying the effort sent %v", got.Applied)
	}
	if p, _ := got.Moved["params"].(map[string]any); got.Moved["kind"] != "session.switch" || p["to"] != "stream" {
		t.Errorf("the move sent %v", got.Moved)
	}
	if !strings.Contains(got.PathSays, "stay in the archive") || !strings.Contains(got.PathSheet, "stay in the archive") ||
		!strings.Contains(got.PathSheet, `moves to group "side"`) {
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

// A project whose contour starts codex opens on the Codex tab: the common
// keys stand above both tabs, the codex keys under their own, and the line in
// place of the command says what New starts codex with. What a
// phone does not choose is not offered — never asking, the sandbox off — and
// one the map stores is said. The model is picked from the daemon's list, and
// the efforts offered are the chosen model's. The command of claude is under
// its own tab and no codex change marks a word of it. Picking Claude Code
// opens its tab, and Save sends what changed key by key.
func TestTheProjectSettingsPageLaysTheAgentsOutInTabs(t *testing.T) {
	var got struct {
		Tabs           []string       `json:"tabs"`
		OpenAtStart    string         `json:"openAtStart"`
		Rows           []string       `json:"rows"`
		Soon           string         `json:"soon"`
		LineInCodex    bool           `json:"lineInCodex"`
		AgentFrom      string         `json:"agentFrom"`
		Approvals      []string       `json:"approvals"`
		ApprovalHeld   string         `json:"approvalHeld"`
		Sandboxes      []string       `json:"sandboxes"`
		PlacesOfCodex  []string       `json:"placesOfCodex"`
		EffortsBefore  []string       `json:"effortsBefore"`
		Models         []string       `json:"models"`
		ModelsSaid     string         `json:"modelsSaid"`
		ModelNow       string         `json:"modelNow"`
		EffortsOfModel []string       `json:"effortsOfModel"`
		ApprovalAfter  string         `json:"approvalAfter"`
		ClaudeRows     []string       `json:"claudeRows"`
		ClaudeDrafted  []string       `json:"claudeDrafted"`
		CodexAsks      int            `json:"codexAsks"`
		Followed       string         `json:"followed"`
		Patch          map[string]any `json:"patch"`
		ApplyLater     []string       `json:"applyLater"`
	}
	serve := schemaAnswer(map[string]any{})
	serve["/mode"] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("codex")) })
	runFixtureServing(t, "projectsettings.html", phoneScreen, phonePointer, serve, &got)

	if strings.Join(got.Tabs, "|") != "Claude|Codex" || got.OpenAtStart != "Codex" {
		t.Errorf("the tabs are %v with %q open, meant Codex open — the agent the contour starts", got.Tabs, got.OpenAtStart)
	}
	if want := "Agent|First message|Where it lives|Model|Effort|Approvals|Sandbox"; strings.Join(got.Rows, "|") != want {
		t.Errorf("the rows are %v, meant %s", got.Rows, want)
	}
	if got.Soon != "New starts a thread in the codex daemon of the contour, with what is chosen below; "+
		"config.toml of the contour's codex home decides the rest" || got.LineInCodex {
		t.Errorf("the Codex tab says %q and shows the command of claude: %v", got.Soon, got.LineInCodex)
	}
	if got.AgentFrom != "Codex — the contour" {
		t.Errorf("the agent from the contour reads %q", got.AgentFrom)
	}
	if strings.Join(got.Approvals, "|") != "Untrusted|On request" || strings.Join(got.Sandboxes, "|") != "Read only|Workspace" {
		t.Errorf("approvals %v and sandboxes %v are offered — never asking and the sandbox off are not", got.Approvals, got.Sandboxes)
	}
	if !strings.HasPrefix(got.ApprovalHeld, "never — kept as the map stores it") {
		t.Errorf("the stored never reads %q", got.ApprovalHeld)
	}
	if strings.Join(got.PlacesOfCodex, "|") != "Daemon|tmux" || len(got.EffortsBefore) != 6 {
		t.Errorf("codex lives in %v and offers the efforts %v before a model is chosen", got.PlacesOfCodex, got.EffortsBefore)
	}
	if strings.Join(got.Models, "|") != "GPT-5.5 Codex|GPT-5.5|Leave it to codex" || got.ModelsSaid != "the models the codex daemon lists" {
		t.Errorf("the list of models is %v, said %q", got.Models, got.ModelsSaid)
	}
	if got.ModelNow != "GPT-5.5" || strings.Join(got.EffortsOfModel, "|") != "Low|High" {
		t.Errorf("the model reads %q and offers %v, meant GPT-5.5 offering Low and High", got.ModelNow, got.EffortsOfModel)
	}
	if got.ApprovalAfter != "what config.toml of the contour's codex home says" {
		t.Errorf("with the stored never removed the approvals read %q", got.ApprovalAfter)
	}
	if len(got.ClaudeRows) != 12 || got.ClaudeRows[2] != "Model" || got.ClaudeRows[11] != "Extra arguments" {
		t.Errorf("the Claude tab's rows are %v", got.ClaudeRows)
	}
	if len(got.ClaudeDrafted) != 0 {
		t.Errorf("codex changes marked words of the claude command: %v", got.ClaudeDrafted)
	}
	if got.CodexAsks != 1 {
		t.Errorf("the models of codex were asked %d times, meant once the tab opened", got.CodexAsks)
	}
	if got.Followed != "Claude" {
		t.Errorf("after picking Claude Code the %q tab is open", got.Followed)
	}
	set, _ := got.Patch["launchSet"].(map[string]any)
	unset, _ := got.Patch["launchUnset"].([]any)
	if len(got.Patch) != 2 || len(set) != 3 || set["codexModel"] != "gpt-5.5" || set["codexEffort"] != "high" ||
		set["agent"] != "claude" || len(unset) != 1 || unset[0] != "codexApproval" {
		t.Errorf("Save sent %v, meant the model, the effort and the agent set and the stored approval removed", got.Patch)
	}
	for _, line := range got.ApplyLater {
		if !strings.HasPrefix(line, "Agent Claude Code") {
			t.Errorf("a running claude session is offered %q — a key of codex reaches none of it", line)
		}
	}
	if len(got.ApplyLater) == 0 {
		t.Error("the running sessions were told nothing of the agent")
	}
}
