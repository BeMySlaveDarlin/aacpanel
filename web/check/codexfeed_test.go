package check

import (
	"reflect"
	"strings"
	"testing"
)

// What codex alone writes stands in its feed as cards, not as answers: a
// question of plan mode with the note beside its pick, and one put away as
// refused; the plan read whole; a review begun as a line and ended with its
// verdict and a row a finding — the answer codex writes after it saying the
// same is folded into it, and an answer that says something else stays; a goal
// with how it stands and its spend; and the line of a turn codex began for its
// goal.
func TestACodexFeedDrawsWhatCodexAloneWrites(t *testing.T) {
	type finding struct {
		Tag     string `json:"tag"`
		Title   string `json:"title"`
		At      string `json:"at"`
		Tone    string `json:"tone"`
		Clamped bool   `json:"clamped"`
	}
	var got struct {
		AI    []string `json:"ai"`
		Asked []struct {
			Off    bool   `json:"off"`
			Why    string `json:"why"`
			Answer string `json:"answer"`
			Note   string `json:"note"`
		} `json:"asked"`
		Plan []struct {
			Label string `json:"label"`
			Items int    `json:"items"`
			Text  string `json:"text"`
		} `json:"plan"`
		Starts  []string `json:"starts"`
		Reviews []struct {
			Tone     string    `json:"tone"`
			Verdict  string    `json:"verdict"`
			Text     string    `json:"text"`
			Findings []finding `json:"findings"`
		} `json:"reviews"`
		Goals []struct {
			Word  string `json:"word"`
			Title string `json:"title"`
			Meta  string `json:"meta"`
			Bar   string `json:"bar"`
		} `json:"goals"`
		Notes  []string `json:"notes"`
		Opened struct {
			Expanded string `json:"expanded"`
			Clamped  bool   `json:"clamped"`
		} `json:"opened"`
	}
	runFixture(t, "codexfeed.html", &got)

	wantAI := []string{"The plan is above.", "Nothing to fix in the committed change."}
	if !reflect.DeepEqual(got.AI, wantAI) {
		t.Errorf("the answers of the feed are %q, want %q: a row of codex's own fell through to an answer, "+
			"or the answer that repeats a review was not folded — or one that does not was", got.AI, wantAI)
	}

	if len(got.Asked) != 2 {
		t.Fatalf("the feed has %d cards of questions", len(got.Asked))
	}
	if a := got.Asked[0]; a.Off || a.Answer != "Spaces (Recommended)" || a.Note != "keep it short" {
		t.Errorf("the answered question reads %+v: the note beside the pick is not on it", a)
	}
	if a := got.Asked[1]; !a.Off || a.Why != "rejected" || a.Answer != "" {
		t.Errorf("the question put away reads %+v", a)
	}

	if len(got.Plan) != 1 || got.Plan[0].Label != "plan" || got.Plan[0].Items != 2 ||
		!strings.Contains(got.Plan[0].Text, ".editorconfig") {
		t.Errorf("the plan is drawn as %+v", got.Plan)
	}

	wantStarts := []string{"Review current changes", "Review changes against no-such-branch"}
	if !reflect.DeepEqual(got.Starts, wantStarts) {
		t.Errorf("the starts of the reviews read %q", got.Starts)
	}
	if len(got.Reviews) != 2 {
		t.Fatalf("the feed has %d ends of reviews", len(got.Reviews))
	}
	bad, good := got.Reviews[0], got.Reviews[1]
	if bad.Tone != "v-crit" || !strings.HasPrefix(bad.Verdict, "patch is incorrect") || !strings.Contains(bad.Verdict, "99% sure") {
		t.Errorf("the failed review says %q in %q", bad.Verdict, bad.Tone)
	}
	if !strings.Contains(bad.Text, "regresses the existing add function") {
		t.Errorf("the explanation of the review reads %q", bad.Text)
	}
	wantFinding := []finding{{Tag: "P1", Title: "Preserve addition semantics in add", At: "calc.py:2", Tone: "s-crit", Clamped: true}}
	if !reflect.DeepEqual(bad.Findings, wantFinding) {
		t.Errorf("the findings read %+v, want %+v", bad.Findings, wantFinding)
	}
	if good.Tone != "v-ok" || len(good.Findings) != 0 {
		t.Errorf("the review that found nothing reads %+v", good)
	}
	if got.Opened.Expanded != "true" || got.Opened.Clamped {
		t.Errorf("a finding tapped stays shut: %+v", got.Opened)
	}

	if len(got.Goals) != 2 {
		t.Fatalf("the feed has %d goals", len(got.Goals))
	}
	run, spent := got.Goals[0], got.Goals[1]
	if run.Word != "running" || run.Title != "Make CI green on main" || run.Meta != "41k of 200k tokens" || run.Bar != "21%" {
		t.Errorf("the running goal reads %+v", run)
	}
	if spent.Word != "budget spent" || spent.Meta != "203k of 200k tokens | 12m 34s" || spent.Bar != "100%" {
		t.Errorf("the goal out of its budget reads %+v", spent)
	}

	if !reflect.DeepEqual(got.Notes, []string{"codex goes on with its goal"}) {
		t.Errorf("the lines of the feed read %q", got.Notes)
	}
}
