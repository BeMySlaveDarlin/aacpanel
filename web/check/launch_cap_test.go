package check

import (
	"strings"
	"testing"
)

// The cap reaches the map as a number, an empty field as no key, and the
// switch as true or nothing; a silent project takes the profile's.
func TestTheContextCapSurvivesFormCleanup(t *testing.T) {
	var got struct {
		CleanSet        map[string]any `json:"cleanSet"`
		CleanUnset      map[string]any `json:"cleanUnset"`
		MergedInherited map[string]any `json:"mergedInherited"`
		MergedOwn       map[string]any `json:"mergedOwn"`
		Summary         string         `json:"summary"`
		Parsed          []any          `json:"parsed"`
	}
	launchNode(t, launchBundle(t, false), `
import { clean, merged, summary, parseCap } from BUNDLE;
process.stdout.write(JSON.stringify({
    cleanSet: clean({ contextCap: 80, autoRestart: true }),
    cleanUnset: clean({ model: "opus", contextCap: undefined, autoRestart: undefined }),
    mergedInherited: merged({ contextCap: 80, autoRestart: true }, { model: "opus" }),
    mergedOwn: merged({ contextCap: 80 }, { contextCap: 60 }),
    summary: summary({ model: "opus", contextCap: 70, autoRestart: true }),
    parsed: [parseCap("80"), parseCap(" 70 "), parseCap(""), parseCap("80.5"), parseCap("abc")],
}));
`, &got)

	if got.CleanSet["contextCap"] != float64(80) || got.CleanSet["autoRestart"] != true {
		t.Errorf("the cap and the switch did not reach the map as 80 and true: %v", got.CleanSet)
	}
	if _, ok := got.CleanUnset["contextCap"]; ok {
		t.Errorf("an empty cap reached the map: %v", got.CleanUnset)
	}
	if _, ok := got.CleanUnset["autoRestart"]; ok {
		t.Errorf("an unchecked switch reached the map: %v", got.CleanUnset)
	}
	if got.MergedInherited["contextCap"] != float64(80) || got.MergedInherited["autoRestart"] != true {
		t.Errorf("a silent project did not inherit the profile's cap: %v", got.MergedInherited)
	}
	if got.MergedOwn["contextCap"] != float64(60) {
		t.Errorf("the project's own cap lost to the profile's: %v", got.MergedOwn)
	}
	if !strings.Contains(got.Summary, "cap 70%") || !strings.Contains(got.Summary, "auto restart") {
		t.Errorf("the collapsed card does not show the cap and the restart: %q", got.Summary)
	}
	if len(got.Parsed) != 5 || got.Parsed[0] != float64(80) || got.Parsed[1] != float64(70) || got.Parsed[2] != nil ||
		got.Parsed[3] != float64(0) || got.Parsed[4] != float64(0) {
		t.Errorf("the field read back as %v, meant [80 70 <nil> 0 0]", got.Parsed)
	}
}

// The form says what the cap does with the switch off and on, where it comes
// from, and warns of one the map will not keep; the card names both rows.
func TestTheContextCapRendersOnBothSides(t *testing.T) {
	var got struct {
		FieldsOff       string `json:"fieldsOff"`
		FieldsInherited string `json:"fieldsInherited"`
		FieldsOn        string `json:"fieldsOn"`
		FieldsJunk      string `json:"fieldsJunk"`
		ViewInherited   string `json:"viewInherited"`
	}
	launchNode(t, launchBundle(t, true), renderPrelude+`
const none = () => {};
process.stdout.write(JSON.stringify({
    fieldsOff: text(LaunchFields({ value: {}, onChange: none, inherited: {} })),
    fieldsInherited: text(LaunchFields({ value: {}, onChange: none, inherited: { contextCap: 70, autoRestart: true } })),
    fieldsOn: text(LaunchFields({ value: { contextCap: 65, autoRestart: true }, onChange: none, inherited: {} })),
    fieldsJunk: text(LaunchFields({ value: { contextCap: 30 }, onChange: none, inherited: {} })),
    viewInherited: text(LaunchView({ launch: {}, profile: { contextCap: 80, autoRestart: true } })),
}));
`, &got)

	for _, want := range []string{"restart past the context cap", "checked=false", "names 80% (the panel's default)", "nothing restarts"} {
		if !strings.Contains(got.FieldsOff, want) {
			t.Errorf("the form without a cap did not say %q:\n%s", want, got.FieldsOff)
		}
	}
	if !strings.Contains(got.FieldsInherited, "past 70% (from the profile) of the window the session wraps up") {
		t.Errorf("the project form hides the profile's cap and restart:\n%s", got.FieldsInherited)
	}
	if !strings.Contains(got.FieldsOn, "checked=true") || !strings.Contains(got.FieldsOn, "past 65% of the window") ||
		!strings.Contains(got.FieldsOn, "context guard hook") {
		t.Errorf("a set cap with the switch on is shown without its effect or the hook caveat:\n%s", got.FieldsOn)
	}
	if !strings.Contains(got.FieldsJunk, "outside 50–95") || !strings.Contains(got.FieldsJunk, "pfhelp warn") {
		t.Errorf("a cap the map will refuse is shown without a warning:\n%s", got.FieldsJunk)
	}
	for _, want := range []string{"context cap", "80%", "auto restart", "from the profile"} {
		if !strings.Contains(got.ViewInherited, want) {
			t.Errorf("an inherited cap is shown without %q:\n%s", want, got.ViewInherited)
		}
	}
}
