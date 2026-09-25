package check

import (
	"reflect"
	"strings"
	"testing"
)

type pickPhoneShot struct {
	Head          []string `json:"head"`
	ModelTitle    string   `json:"modelTitle"`
	Main          []string `json:"main"`
	MainDesc      []string `json:"mainDesc"`
	Checked       []string `json:"checked"`
	EffortRow     string   `json:"effortRow"`
	Group         []string `json:"group"`
	Other         []string `json:"other"`
	ConfirmSheet  bool     `json:"confirmSheet"`
	AfterPick     []string `json:"afterPick"`
	EffortTitle   string   `json:"effortTitle"`
	Stops         []string `json:"stops"`
	StopOn        string   `json:"stopOn"`
	ModeTitle     string   `json:"modeTitle"`
	Modes         []string `json:"modes"`
	ModeOn        []string `json:"modeOn"`
	ModeIcons     int      `json:"modeIcons"`
	SendReady     bool     `json:"sendReady"`
	FromComposer  string   `json:"fromComposer"`
	ComposerAfter string   `json:"composerAfter"`
	Sent          []string `json:"sent"`
	TermMain      []string `json:"termMain"`
	TermOn        []string `json:"termOn"`
	TermModesOff  bool     `json:"termModesOff"`
	TermNote      string   `json:"termNote"`
	Loud          []string `json:"loud"`
	Bare          []string `json:"bare"`
	AttachMode    string   `json:"attachMode"`
	AttachOpens   string   `json:"attachOpens"`
}

// The model list is the one claude gives, a row per model: its default is an
// entry of its own that stands for a model another entry names already. Under
// it the effort, and then the models of the catalogue no alias reaches.
func TestThePhonePicksAModelFromTheListClaudeGives(t *testing.T) {
	var got pickPhoneShot
	runFixture(t, "pickphone.html", &got)

	if !reflect.DeepEqual(got.Head, []string{"Auto", "Opus 5.5", "Extra"}) {
		t.Errorf("the line inside the composer offers %v", got.Head)
	}
	if got.ModelTitle != "Select model" {
		t.Errorf("the model list opens as %q", got.ModelTitle)
	}
	if !reflect.DeepEqual(got.Main, []string{"Opus 5.5", "Fable 5.1", "Sonnet 5", "Haiku 4.5"}) {
		t.Errorf("the main models are %v", got.Main)
	}
	if len(got.MainDesc) != 4 || got.MainDesc[0] != "Best for everyday, complex tasks" {
		t.Errorf("the models are described as %v", got.MainDesc)
	}
	if !reflect.DeepEqual(got.Checked, []string{"Opus 5.5"}) {
		t.Errorf("the model the session runs is marked as %v", got.Checked)
	}
	if got.EffortRow != "Extra" {
		t.Errorf("the effort row says %q", got.EffortRow)
	}
	if !reflect.DeepEqual(got.Group, []string{"Other models"}) ||
		!reflect.DeepEqual(got.Other, []string{"Opus 5", "Fable 5", "Opus 4.8", "Opus 4.7", "Sonnet 4.6", "Opus 4.6"}) {
		t.Errorf("the other models are %v under %v", got.Other, got.Group)
	}
}

// A row tapped in the list is the choice: no sheet asks again, the setting
// goes to the host as one change, and the mark moves to it.
func TestAPickIsOneChangeWithoutASecondQuestion(t *testing.T) {
	var got pickPhoneShot
	runFixture(t, "pickphone.html", &got)

	if got.ConfirmSheet {
		t.Error("a confirmation sheet stood in front of a row just tapped")
	}
	if !reflect.DeepEqual(got.AfterPick, []string{"Sonnet 5"}) {
		t.Errorf("after the pick the mark stands on %v", got.AfterPick)
	}
	want := []string{
		`session.set:{"model":"sonnet"}`,
		`session.set:{"model":"claude-opus-4-8"}`,
		`session.set:{"effort":"max"}`,
		`session.set:{"mode":"plan"}`,
	}
	if !reflect.DeepEqual(got.Sent, want) {
		t.Errorf("the host got %v, want %v", got.Sent, want)
	}
}

// The effort is a scale of the levels the model takes; the mode list is the
// four modes with their words, and /model alone opens the list rather than
// waiting for an argument.
func TestTheEffortTheModeAndTheBareCommand(t *testing.T) {
	var got pickPhoneShot
	runFixture(t, "pickphone.html", &got)

	if got.EffortTitle != "Effort" || got.StopOn != "Extra" ||
		!reflect.DeepEqual(got.Stops, []string{"Low", "Medium", "High", "Extra", "Max"}) {
		t.Errorf("the effort opens as %q at %q with stops %v", got.EffortTitle, got.StopOn, got.Stops)
	}
	if got.ModeTitle != "Select mode" || !reflect.DeepEqual(got.Modes, []string{"Manual", "Accept edits", "Plan", "Auto"}) ||
		!reflect.DeepEqual(got.ModeOn, []string{"Auto"}) || got.ModeIcons != 4 {
		t.Errorf("the mode list is %q %v, marked %v, %d icons", got.ModeTitle, got.Modes, got.ModeOn, got.ModeIcons)
	}
	if !got.SendReady || got.FromComposer != "Select model" || got.ComposerAfter != "" {
		t.Errorf("/model alone: send ready %v, opened %q, composer left %q", got.SendReady, got.FromComposer, got.ComposerAfter)
	}
	if got.AttachMode != "Permission Auto" || got.AttachOpens != "Select mode" {
		t.Errorf("the attach sheet names the mode as %q and opens %q", got.AttachMode, got.AttachOpens)
	}
}

// A terminal lists no models of its own: the aliases it takes are named after
// the catalogue. Its mode is switched on its own screen, and the list says so
// rather than offering a row that would do nothing.
func TestATerminalPicksFromTheCatalogueAndNotItsMode(t *testing.T) {
	var got pickPhoneShot
	runFixture(t, "pickphone.html", &got)

	if !reflect.DeepEqual(got.TermMain, []string{"Opus 5.5", "Fable 5.1", "Sonnet 5", "Haiku 4.5"}) {
		t.Errorf("a terminal offers %v", got.TermMain)
	}
	if !reflect.DeepEqual(got.TermOn, []string{"Opus 5.5"}) {
		t.Errorf("a terminal marks %v as its model", got.TermOn)
	}
	if !got.TermModesOff || !strings.Contains(got.TermNote, "shift+tab") {
		t.Errorf("a terminal's modes: off %v, note %q", got.TermModesOff, got.TermNote)
	}
}

// A session that asks about nothing says so in red where its mode shows, and
// an executor that cannot change a setting leaves the words without a way in.
func TestTheComposerStripKeepsItsWordsAndItsWarning(t *testing.T) {
	var got pickPhoneShot
	runFixture(t, "pickphone.html", &got)

	if !reflect.DeepEqual(got.Loud, []string{"Bypass"}) {
		t.Errorf("a session past the questions shows %v in red", got.Loud)
	}
	if !reflect.DeepEqual(got.Bare, []string{"Auto off", "Opus 5.5 off", "Extra off"}) {
		t.Errorf("without a way to change them the strip reads %v", got.Bare)
	}
}

type pickDeskShot struct {
	Chips       []string `json:"chips"`
	InComposer  int      `json:"inComposer"`
	ModelMenu   []string `json:"modelMenu"`
	Numbers     []string `json:"numbers"`
	Marked      []string `json:"marked"`
	More        []string `json:"more"`
	AfterKey    int      `json:"afterKey"`
	ModeMenu    []string `json:"modeMenu"`
	ModeOn      []string `json:"modeOn"`
	AfterEscape int      `json:"afterEscape"`
	AfterAway   int      `json:"afterAway"`
	EffortHead  string   `json:"effortHead"`
	AfterEffort int      `json:"afterEffort"`
	Sent        []string `json:"sent"`
	Confirm     bool     `json:"confirm"`
}

// On a wide screen the three sit inside the composer and open menus over it:
// the models numbered, the older ones beside them, the mode that most sessions
// run in first, the effort a scale. A digit picks, Esc and a press elsewhere
// close, and a pick goes out as one change with no sheet in front of it.
func TestTheDesktopPicksFromMenusOverTheComposer(t *testing.T) {
	var got pickDeskShot
	runWideFixture(t, "pickdesk.html", &got)

	if !reflect.DeepEqual(got.Chips, []string{"Auto", "Opus 5.5", "Extra"}) || got.InComposer != 1 {
		t.Errorf("the strip inside the composer reads %v (strips there: %d)", got.Chips, got.InComposer)
	}
	if !reflect.DeepEqual(got.ModelMenu, []string{"Opus 5.5", "Fable 5.1", "Sonnet 5", "Haiku 4.5", "More models"}) ||
		!reflect.DeepEqual(got.Numbers, []string{"1", "2", "3", "4"}) || !reflect.DeepEqual(got.Marked, []string{"Opus 5.5"}) {
		t.Errorf("the model menu is %v numbered %v, marked %v", got.ModelMenu, got.Numbers, got.Marked)
	}
	if len(got.More) != 6 || got.More[0] != "Opus 5" {
		t.Errorf("more models opens %v", got.More)
	}
	if !reflect.DeepEqual(got.ModeMenu, []string{"Auto", "Manual", "Accept edits", "Plan"}) ||
		!reflect.DeepEqual(got.ModeOn, []string{"Auto"}) {
		t.Errorf("the mode menu is %v, marked %v", got.ModeMenu, got.ModeOn)
	}
	if got.AfterKey != 0 || got.AfterEscape != 0 || got.AfterAway != 0 || got.AfterEffort != 0 {
		t.Errorf("menus left open: after a digit %d, Esc %d, a press elsewhere %d, an effort %d",
			got.AfterKey, got.AfterEscape, got.AfterAway, got.AfterEffort)
	}
	if got.EffortHead != "EffortExtra" {
		t.Errorf("the effort menu is headed %q", got.EffortHead)
	}
	want := []string{`session.set:{"model":"sonnet"}`, `session.set:{"effort":"high"}`, `session.set:{"mode":"acceptEdits"}`}
	if !reflect.DeepEqual(got.Sent, want) || got.Confirm {
		t.Errorf("the host got %v (a sheet in front: %v), want %v", got.Sent, got.Confirm, want)
	}
}
