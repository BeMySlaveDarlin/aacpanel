package check

import (
	"strings"
	"testing"
)

func TestFinalizeThresholdSurvivesFormCleanup(t *testing.T) {
	var got struct {
		CleanSet        map[string]any `json:"cleanSet"`
		CleanUnset      map[string]any `json:"cleanUnset"`
		MergedInherited map[string]any `json:"mergedInherited"`
		MergedOwn       map[string]any `json:"mergedOwn"`
		Summary         string         `json:"summary"`
		Parsed          []any          `json:"parsed"`
	}
	launchNode(t, launchBundle(t, false), `
import { clean, merged, summary, parseFinalizeAt } from BUNDLE;
process.stdout.write(JSON.stringify({
    cleanSet: clean({ finalizeAt: 80 }),
    cleanUnset: clean({ model: "opus", finalizeAt: undefined }),
    mergedInherited: merged({ finalizeAt: 80 }, { model: "opus" }),
    mergedOwn: merged({ finalizeAt: 80 }, { finalizeAt: 60 }),
    summary: summary({ model: "opus", finalizeAt: 80 }),
    parsed: [parseFinalizeAt("80"), parseFinalizeAt(" 7 "), parseFinalizeAt(""), parseFinalizeAt("80.5"), parseFinalizeAt("abc")],
}));
`, &got)

	// The launcher reads a number; a string "80" would be skipped with a warning.
	if value, ok := got.CleanSet["finalizeAt"]; !ok || value != float64(80) {
		t.Errorf("the threshold did not reach the database as the number 80: %v (%T)", value, value)
	}
	if _, ok := got.CleanUnset["finalizeAt"]; ok {
		t.Errorf("an unchecked threshold reached the database: %v", got.CleanUnset)
	}
	if got.MergedInherited["finalizeAt"] != float64(80) {
		t.Errorf("a silent project did not inherit the profile threshold: %v", got.MergedInherited)
	}
	if got.MergedOwn["finalizeAt"] != float64(60) {
		t.Errorf("the project's own threshold lost to the profile's: %v", got.MergedOwn)
	}
	if !strings.Contains(got.Summary, "finalize at 80%") {
		t.Errorf("the collapsed card does not show the threshold: %q", got.Summary)
	}
	want := []float64{80, 7, 0, 0, 0}
	for i, w := range want {
		if i >= len(got.Parsed) || got.Parsed[i] != w {
			t.Errorf("the field read back as %v, expected %v", got.Parsed, want)
			break
		}
	}
}

func TestFinalizeThresholdRendersOnBothSides(t *testing.T) {
	var got struct {
		FieldsOff       string `json:"fieldsOff"`
		FieldsInherited string `json:"fieldsInherited"`
		FieldsOn        string `json:"fieldsOn"`
		FieldsJunk      string `json:"fieldsJunk"`
		ViewInherited   string `json:"viewInherited"`
		ViewOff         string `json:"viewOff"`
	}
	launchNode(t, launchBundle(t, true), renderPrelude+`
const none = () => {};
process.stdout.write(JSON.stringify({
    fieldsOff: text(LaunchFields({ value: {}, onChange: none, inherited: {} })),
    fieldsInherited: text(LaunchFields({ value: {}, onChange: none, inherited: { finalizeAt: 70 } })),
    fieldsOn: text(LaunchFields({ value: { finalizeAt: 65 }, onChange: none, inherited: {} })),
    fieldsJunk: text(LaunchFields({ value: { finalizeAt: 150 }, onChange: none, inherited: {} })),
    viewInherited: text(LaunchView({ launch: {}, profile: { finalizeAt: 80 } })),
    viewOff: text(LaunchView({ launch: {}, profile: {} })),
}));
`, &got)

	field := func(rendered string) string {
		_, after, ok := strings.Cut(rendered, "type=number")
		if !ok {
			return ""
		}
		before, _, _ := strings.Cut(after, "%")
		return before
	}

	for _, want := range []string{"finalize when the context fills up", "checked=false", "unchecked — the session is never told to finalize"} {
		if !strings.Contains(got.FieldsOff, want) {
			t.Errorf("the form without a threshold did not say %q:\n%s", want, got.FieldsOff)
		}
	}
	if pct := field(got.FieldsOff); !strings.Contains(pct, "disabled=true") || !strings.Contains(pct, "value=80") {
		t.Errorf("the unchecked field is not a disabled 80: %q", pct)
	}

	if !strings.Contains(got.FieldsInherited, "unchecked — as in the profile: 70%") {
		t.Errorf("the project form hides the profile threshold:\n%s", got.FieldsInherited)
	}
	if pct := field(got.FieldsInherited); !strings.Contains(pct, "disabled=true") || !strings.Contains(pct, "value=70") {
		t.Errorf("the unchecked field does not show the profile's 70: %q", pct)
	}

	if !strings.Contains(got.FieldsOn, "checked=true") || !strings.Contains(got.FieldsOn, "context guard hook") {
		t.Errorf("a set threshold is shown without the switch on or without the hook caveat:\n%s", got.FieldsOn)
	}
	if pct := field(got.FieldsOn); !strings.Contains(pct, "disabled=false") || !strings.Contains(pct, "value=65") {
		t.Errorf("the checked field is not an editable 65: %q", pct)
	}

	if !strings.Contains(got.FieldsJunk, "outside 1–99") || !strings.Contains(got.FieldsJunk, "pfhelp warn") {
		t.Errorf("a threshold the launcher will skip is shown without a warning:\n%s", got.FieldsJunk)
	}

	for _, want := range []string{"finalize at", "80%", "from the profile"} {
		if !strings.Contains(got.ViewInherited, want) {
			t.Errorf("an inherited threshold is shown without %q:\n%s", want, got.ViewInherited)
		}
	}
	if !strings.Contains(got.ViewOff, "finalize at") {
		t.Errorf("the card without a threshold does not name the row at all:\n%s", got.ViewOff)
	}
}
