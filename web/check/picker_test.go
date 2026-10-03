package check

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

type pickPhoneShot struct {
	Head           []string `json:"head"`
	StripTall      float64  `json:"stripTall"`
	RootPx         float64  `json:"rootPx"`
	ModelTitle     string   `json:"modelTitle"`
	Main           []string `json:"main"`
	MainDesc       []string `json:"mainDesc"`
	Checked        []string `json:"checked"`
	EffortRow      string   `json:"effortRow"`
	Group          []string `json:"group"`
	Other          []string `json:"other"`
	ConfirmSheet   bool     `json:"confirmSheet"`
	AfterPick      []string `json:"afterPick"`
	EffortTitle    string   `json:"effortTitle"`
	Stops          []string `json:"stops"`
	StopOn         string   `json:"stopOn"`
	ModeTitle      string   `json:"modeTitle"`
	Modes          []string `json:"modes"`
	ModeOn         []string `json:"modeOn"`
	ModeIcons      int      `json:"modeIcons"`
	SendReady      bool     `json:"sendReady"`
	FromComposer   string   `json:"fromComposer"`
	ComposerAfter  string   `json:"composerAfter"`
	Sent           []string `json:"sent"`
	TermMain       []string `json:"termMain"`
	TermOn         []string `json:"termOn"`
	TermModesOff   bool     `json:"termModesOff"`
	TermNote       string   `json:"termNote"`
	Loud           []string `json:"loud"`
	Bare           []string `json:"bare"`
	AttachTitle    string   `json:"attachTitle"`
	BandPainted    bool     `json:"bandPainted"`
	WordsSmaller   bool     `json:"wordsSmaller"`
	EmptySendClear bool     `json:"emptySendClear"`
	SendFilled     bool     `json:"sendFilled"`
	AttachRows     int      `json:"attachRows"`
}

// The model list is the one claude gives, a row per model: its default is an
// entry of its own that stands for a model another entry names already. Under
// it the effort, and then the models of the catalogue no alias reaches.
func TestThePhonePicksAModelFromTheListClaudeGives(t *testing.T) {
	var got pickPhoneShot
	runFixture(t, "pickphone.html", &got)

	if !reflect.DeepEqual(got.Head, []string{"Opus 5.5· Extra", "Auto"}) {
		t.Errorf("the line inside the composer offers %v", got.Head)
	}
	// The line stands at 0.85 of the 2.5rem a button of the field takes.
	if got.RootPx == 0 || math.Abs(got.StripTall/(2.5*got.RootPx)-0.85) > 0.02 {
		t.Errorf("the line with the model is %.1f px tall at a root of %.1f px — it stands at 0.85 of 2.5rem", got.StripTall, got.RootPx)
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
// goes to the host as one change, and the mark moves to it. On the stream a
// model or an effort says it is for this session unless told otherwise.
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
		`session.set:{"model":"sonnet","scope":"session"}`,
		`session.set:{"model":"claude-opus-4-8","scope":"session"}`,
		`session.set:{"effort":"max","scope":"session"}`,
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
		!reflect.DeepEqual(got.Stops, []string{"Low", "Medium", "High", "Extra", "Max", "Ultracode"}) {
		t.Errorf("the effort opens as %q at %q with stops %v", got.EffortTitle, got.StopOn, got.Stops)
	}
	if got.ModeTitle != "Select mode" || !reflect.DeepEqual(got.Modes, []string{"Manual", "Accept edits", "Plan", "Auto"}) ||
		!reflect.DeepEqual(got.ModeOn, []string{"Auto"}) || got.ModeIcons != 4 {
		t.Errorf("the mode list is %q %v, marked %v, %d icons", got.ModeTitle, got.Modes, got.ModeOn, got.ModeIcons)
	}
	if !got.SendReady || got.FromComposer != "Select model" || got.ComposerAfter != "" {
		t.Errorf("/model alone: send ready %v, opened %q, composer left %q", got.SendReady, got.FromComposer, got.ComposerAfter)
	}
	if got.AttachTitle != "Attach" || got.AttachRows != 0 {
		t.Errorf("the attach sheet %q carries %d rows besides the files — the mode has its own word in the strip",
			got.AttachTitle, got.AttachRows)
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

// The strip under the field is a band of its own with quieter words, and the
// send button a filled square that gives up its fill when there is nothing to
// send: the settings do not read as more of the field, and the press that
// sends does not read as a glyph on it.
func TestTheComposerStripStandsApartFromTheField(t *testing.T) {
	var got pickPhoneShot
	runFixture(t, "pickphone.html", &got)
	if !got.BandPainted || !got.WordsSmaller {
		t.Errorf("the strip is painted %v, its words smaller than the field's %v", got.BandPainted, got.WordsSmaller)
	}
	if !got.SendFilled || !got.EmptySendClear {
		t.Errorf("the send button is filled with words %v, clear and off when empty %v", got.SendFilled, got.EmptySendClear)
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
	if !reflect.DeepEqual(got.Bare, []string{"Opus 5.5· Extra off", "Auto off"}) {
		t.Errorf("without a way to change them the strip reads %v", got.Bare)
	}
}

type pickDeskShot struct {
	Chips           []string `json:"chips"`
	InComposer      int      `json:"inComposer"`
	ModelMenu       []string `json:"modelMenu"`
	Numbers         []string `json:"numbers"`
	Marked          []string `json:"marked"`
	EffortRow       []string `json:"effortRow"`
	More            []string `json:"more"`
	SubsMore        []string `json:"subsMore"`
	SubsEffort      []string `json:"subsEffort"`
	ExpandedEffort  []string `json:"expandedEffort"`
	EffortHead      string   `json:"effortHead"`
	EffortStops     []string `json:"effortStops"`
	EffortOn        string   `json:"effortOn"`
	EffortBeside    bool     `json:"effortBeside"`
	InsideKeeps     int      `json:"insideKeeps"`
	SubsBack        []string `json:"subsBack"`
	ExpandedBack    []string `json:"expandedBack"`
	AfterKey        int      `json:"afterKey"`
	ModeMenu        []string `json:"modeMenu"`
	ModeOn          []string `json:"modeOn"`
	AfterModeEscape int      `json:"afterModeEscape"`
	AfterEscape     int      `json:"afterEscape"`
	AfterAway       int      `json:"afterAway"`
	AfterEffort     int      `json:"afterEffort"`
	ChipsAfter      []string `json:"chipsAfter"`
	Sent            []string `json:"sent"`
	Confirm         bool     `json:"confirm"`
	Bare            []string `json:"bare"`
}

// On a wide screen two words sit inside the composer, the model with its
// effort and the mode, and open menus over it: the models numbered, the older
// ones and the effort behind rows of the model's menu, the mode that most
// sessions run in first. A digit picks a model even with the effort open
// beside the menu, Esc and a press elsewhere close everything, and a pick goes
// out as one change with no sheet in front of it, the words following it.
func TestTheDesktopPicksFromMenusOverTheComposer(t *testing.T) {
	var got pickDeskShot
	runWideFixture(t, "pickdesk.html", &got)

	if !reflect.DeepEqual(got.Chips, []string{"Opus 5.5· Extra", "Auto"}) || got.InComposer != 1 {
		t.Errorf("the strip inside the composer reads %v (strips there: %d) — the model with its effort, then the mode",
			got.Chips, got.InComposer)
	}
	if !reflect.DeepEqual(got.Bare, []string{"Opus 5.5", "Auto"}) {
		t.Errorf("a session that says no effort reads %v", got.Bare)
	}
	if !reflect.DeepEqual(got.ModelMenu, []string{"Opus 5.5", "Fable 5.1", "Sonnet 5", "Haiku 4.5", "More models", "Effort"}) ||
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
	if got.AfterKey != 0 || got.AfterModeEscape != 0 || got.AfterEscape != 0 || got.AfterAway != 0 || got.AfterEffort != 0 {
		t.Errorf("menus left open: after a digit %d, Esc on the modes %d, Esc on the effort %d, a press elsewhere %d, an effort %d",
			got.AfterKey, got.AfterModeEscape, got.AfterEscape, got.AfterAway, got.AfterEffort)
	}
	want := []string{`session.set:{"model":"sonnet","scope":"session"}`, `session.set:{"effort":"high","scope":"session"}`,
		`session.set:{"mode":"acceptEdits"}`}
	if !reflect.DeepEqual(got.Sent, want) || got.Confirm {
		t.Errorf("the host got %v (a sheet in front: %v), want %v", got.Sent, got.Confirm, want)
	}
	if !reflect.DeepEqual(got.ChipsAfter, []string{"Sonnet 5· High", "Accept edits"}) {
		t.Errorf("after the picks the strip reads %v", got.ChipsAfter)
	}
}

// The effort has no word of its own on a wide screen: it is a row of the
// model's menu that says the current effort and opens the scale beside the
// menu, in the place of the older models. One list stands beside the menu at a
// time, and a press inside it is a press inside the menu.
func TestTheDesktopEffortOpensBesideTheModelMenu(t *testing.T) {
	var got pickDeskShot
	runWideFixture(t, "pickdesk.html", &got)

	if !reflect.DeepEqual(got.EffortRow, []string{"Extra"}) {
		t.Errorf("the effort row of the model menu says %v", got.EffortRow)
	}
	if !reflect.DeepEqual(got.SubsMore, []string{"more models"}) || !reflect.DeepEqual(got.SubsEffort, []string{"effort"}) ||
		!reflect.DeepEqual(got.SubsBack, []string{"more models"}) {
		t.Errorf("beside the menu: after More models %v, after Effort %v, after More models again %v — one list at a time",
			got.SubsMore, got.SubsEffort, got.SubsBack)
	}
	if !reflect.DeepEqual(got.ExpandedEffort, []string{"Effort"}) || !reflect.DeepEqual(got.ExpandedBack, []string{"More models"}) {
		t.Errorf("the rows say they are open: %v with the effort, %v with the older models", got.ExpandedEffort, got.ExpandedBack)
	}
	if !got.EffortBeside {
		t.Error("the effort does not open beside the model menu, bottom to bottom")
	}
	if got.EffortHead != "EffortExtra" || got.EffortOn != "Extra" ||
		!reflect.DeepEqual(got.EffortStops, []string{"Low", "Medium", "High", "Extra", "Max", "Ultracode"}) {
		t.Errorf("the effort beside the menu is headed %q, at %q, with the stops %v", got.EffortHead, got.EffortOn, got.EffortStops)
	}
	if got.InsideKeeps != 1 {
		t.Error("a press inside the effort beside the menu closed it")
	}
}
