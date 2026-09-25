package check

import (
	"reflect"
	"strings"
	"testing"
)

type pickScopeShot struct {
	Stops          []string `json:"stops"`
	UltraDashed    bool     `json:"ultraDashed"`
	Foot           string   `json:"foot"`
	Scope          []string `json:"scope"`
	ScopeOn        []string `json:"scopeOn"`
	NotesAtSession []string `json:"notesAtSession"`
	UltraToast     string   `json:"ultraToast"`
	ScopeOnAfter   []string `json:"scopeOnAfter"`
	UltraOff       bool     `json:"ultraOff"`
	UltraTitle     string   `json:"ultraTitle"`
	DefaultNote    string   `json:"defaultNote"`
	MaxToast       string   `json:"maxToast"`
	HighToast      string   `json:"highToast"`
	ModelScopeOn   []string `json:"modelScopeOn"`
	ModelToast     string   `json:"modelToast"`
	SonnetStops    []string `json:"sonnetStops"`
	SonnetFoot     int      `json:"sonnetFoot"`
	UltraChip      string   `json:"ultraChip"`
	UltraOn        string   `json:"ultraOn"`
	UltraHead      string   `json:"ultraHead"`
	UltraFoot      string   `json:"ultraFoot"`
	TermScopes     int      `json:"termScopes"`
	TermNote       string   `json:"termNote"`
	TermStops      []string `json:"termStops"`
	TermToast      string   `json:"termToast"`
	TermModelNote  string   `json:"termModelNote"`
	Sent           []string `json:"sent"`
	Subs           []string `json:"subs"`
}

func pickScopeFixture(t *testing.T) pickScopeShot {
	t.Helper()
	var got pickScopeShot
	runFixture(t, "pickscope.html", &got)
	return got
}

// The scale of a model that takes xhigh ends in ultracode: not a sixth level
// of thinking but xhigh with workflows, so it stands past the levels behind a
// dashed line and says what it is under it.
func TestTheEffortEndsInUltracodeBehindALine(t *testing.T) {
	got := pickScopeFixture(t)
	if !reflect.DeepEqual(got.Stops, []string{"Low", "Medium", "High", "Extra", "Max", "Ultracode"}) {
		t.Errorf("the scale of a model that takes xhigh has the stops %v", got.Stops)
	}
	if !got.UltraDashed {
		t.Error("ultracode is not set apart from the levels by a dashed line")
	}
	if got.Foot != "xhigh + workflows" {
		t.Errorf("under ultracode the scale says %q", got.Foot)
	}
	if !reflect.DeepEqual(got.SonnetStops, []string{"Low", "Medium", "High"}) || got.SonnetFoot != 0 {
		t.Errorf("a model without xhigh offers %v, with %d lines about ultracode", got.SonnetStops, got.SonnetFoot)
	}
	if got.UltraChip != "Ultracode" || got.UltraOn != "Ultracode" || got.UltraHead != "Ultracode" ||
		got.UltraFoot != "Ultracode · xhigh + workflows" {
		t.Errorf("a session in ultracode reads %q on the strip, %q under the knob, %q at the head, %q under the scale",
			got.UltraChip, got.UltraOn, got.UltraHead, got.UltraFoot)
	}
}

// On the stream a pick is for the session unless the person says otherwise;
// the choice stays across the model and the effort, and ultracode, which is
// never a default, is off while the pick would be saved as one.
func TestAPickOnTheStreamGoesWhereThePersonSays(t *testing.T) {
	got := pickScopeFixture(t)
	if !reflect.DeepEqual(got.Scope, []string{"This session", "Default"}) || !reflect.DeepEqual(got.ScopeOn, []string{"This session"}) {
		t.Errorf("where a pick goes offers %v with %v chosen", got.Scope, got.ScopeOn)
	}
	if len(got.NotesAtSession) != 0 {
		t.Errorf("a pick for the session carries notes: %v", got.NotesAtSession)
	}
	if !reflect.DeepEqual(got.ScopeOnAfter, []string{"Default"}) || !reflect.DeepEqual(got.ModelScopeOn, []string{"Default"}) {
		t.Errorf("the default chosen stands as %v on the effort and %v on the models", got.ScopeOnAfter, got.ModelScopeOn)
	}
	if !got.UltraOff || got.UltraTitle != "ultracode is set per session" {
		t.Errorf("ultracode saved as a default: off %v, says %q", got.UltraOff, got.UltraTitle)
	}
	if !strings.Contains(got.DefaultNote, "max") || !strings.Contains(got.DefaultNote, "Ultracode") {
		t.Errorf("the default says nothing of what claude keeps for the session alone: %q", got.DefaultNote)
	}
	want := []string{
		`session.set:{"effort":"ultracode","scope":"session"}`,
		`session.set:{"effort":"max","scope":"default"}`,
		`session.set:{"effort":"high","scope":"default"}`,
		`session.set:{"model":"sonnet","scope":"default"}`,
		`session.set:{"effort":"high"}`,
	}
	if !reflect.DeepEqual(got.Sent, want) {
		t.Errorf("the host got %v, want %v", got.Sent, want)
	}
}

// The note after a pick says where it went, the way claude keeps it.
func TestTheNoteAfterAPickSaysWhereItWent(t *testing.T) {
	got := pickScopeFixture(t)
	cases := []struct{ name, got, want string }{
		{"ultracode", got.UltraToast, "from the next request, for this session only"},
		{"max as a default", got.MaxToast, "from the next request; claude keeps max for this session only"},
		{"high as a default", got.HighToast, "from the next request, and as the default for new sessions"},
		{"a model as a default", got.ModelToast, "from the next request, and as the default for new sessions"},
		{"a pick in a terminal", got.TermToast, "from the next request, and as the default for new sessions"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: the note says %q, want %q", c.name, c.got, c.want)
		}
	}
	want := []string{
		"from the next request, for this session only",
		"from the next request",
		"from the next request, and as the default for new sessions",
		"from the next request; claude keeps max for this session only",
		"from the next request, and as the default for new sessions",
		"from the next request, and as the default for new sessions",
		"from the next request; claude keeps max for this session only",
		"from the next request",
		"from the next request",
	}
	if !reflect.DeepEqual(got.Subs, want) {
		t.Errorf("the notes are %q, want %q", got.Subs, want)
	}
}

// A terminal saves whatever is typed there as the default: it offers no
// choice, says so on the models and the effort alike, and its pick carries no
// word about where it goes.
func TestATerminalSaysItsPickIsTheDefault(t *testing.T) {
	got := pickScopeFixture(t)
	const note = "In a terminal the pick is saved as the default for new sessions."
	if got.TermScopes != 0 || got.TermNote != note || got.TermModelNote != note {
		t.Errorf("a terminal: %d choices, the effort says %q, the models %q", got.TermScopes, got.TermNote, got.TermModelNote)
	}
	if !reflect.DeepEqual(got.TermStops, []string{"Low", "Medium", "High", "Extra", "Max", "Ultracode"}) {
		t.Errorf("a terminal's scale is %v", got.TermStops)
	}
}

type pickScopeDeskShot struct {
	Stops        []string `json:"stops"`
	Scope        []string `json:"scope"`
	StillOpen    int      `json:"stillOpen"`
	UltraOff     bool     `json:"ultraOff"`
	ModelScopeOn []string `json:"modelScopeOn"`
	TermScopes   int      `json:"termScopes"`
	TermNote     string   `json:"termNote"`
	Sent         []string `json:"sent"`
}

// On a wide screen the menus of the effort and the model carry the same
// choice: it does not close the menu, it stays for the next menu, and a digit
// that picks a model takes along the choice as it stands at the press. A
// terminal says its pick is the default.
func TestTheDesktopMenusSayWhereAPickGoes(t *testing.T) {
	var got pickScopeDeskShot
	runWideFixture(t, "pickscopedesk.html", &got)
	if !reflect.DeepEqual(got.Stops, []string{"Low", "Medium", "High", "Extra", "Max", "Ultracode"}) {
		t.Errorf("the effort menu has the stops %v", got.Stops)
	}
	if !reflect.DeepEqual(got.Scope, []string{"This session", "Default"}) || got.StillOpen != 1 || !got.UltraOff {
		t.Errorf("the effort menu offers %v, stays open after the choice %d, ultracode off as a default %v",
			got.Scope, got.StillOpen, got.UltraOff)
	}
	if !reflect.DeepEqual(got.ModelScopeOn, []string{"Default"}) {
		t.Errorf("the model menu forgot the choice: %v", got.ModelScopeOn)
	}
	if got.TermScopes != 0 || got.TermNote != "In a terminal the pick is saved as the default for new sessions." {
		t.Errorf("a terminal's model menu: %d choices, says %q", got.TermScopes, got.TermNote)
	}
	want := []string{
		`session.set:{"effort":"high","scope":"default"}`,
		`session.set:{"model":"sonnet","scope":"default"}`,
		`session.set:{"model":"haiku","scope":"session"}`,
		`session.set:{"model":"fable"}`,
	}
	if !reflect.DeepEqual(got.Sent, want) {
		t.Errorf("the host got %v, want %v", got.Sent, want)
	}
}
