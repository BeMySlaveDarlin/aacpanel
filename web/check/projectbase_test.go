package check

import (
	"regexp"
	"strings"
	"testing"
)

// The base branch is a setting of the project, and the project form is where
// its settings live. A field the form never draws cannot be set at all, and a
// field the form draws but leaves out of the fields it sends is worse: it
// looks saved and is not.
func TestTheProjectFormCarriesTheBaseBranch(t *testing.T) {
	const form = "src/screens/profiles/forms.js"
	src := stripComments(srcFiles(t)[form])
	if src == "" {
		t.Fatalf("%s not found — the test is looking in the wrong place", form)
	}

	project := src[strings.Index(src, "function ProjectForm"):]
	if !strings.Contains(project, "Base branch") {
		t.Errorf("%s: the project form has no field for the base branch — the setting exists in the map and nowhere on the screen", form)
	}
	if !regexp.MustCompile(`base:\s*base\.trim\(\)`).MatchString(project) {
		t.Errorf("%s: the branch is typed into the form but never joins the fields that are sent — it reads as saved and is not", form)
	}

	// The default belongs in the placeholder, not in the value: a field filled
	// in for the person is later read as an answer they gave.
	placeholder := regexp.MustCompile(`placeholder="main"[\s\S]{0,120}?value=\$\{base\}`)
	if !placeholder.MatchString(project) {
		t.Errorf("%s: main is not offered as the placeholder of the branch field — either it is missing or it was written into the value", form)
	}
	if regexp.MustCompile(`useState\(was\.base \|\| "main"\)`).MatchString(project) {
		t.Errorf("%s: the form starts with main already typed in, so the map records a choice nobody made", form)
	}
}
