package check

import (
	"reflect"
	"strings"
	"testing"
)

func TestCodexCodeBlocksRunThroughTheGateAndSettleTheShellCard(t *testing.T) {
	var got struct {
		Shell struct {
			Runs []struct {
				Preview, Title, Output   string
				Before                   int
				Starting, Running, Ended struct {
					Command, State, Bar string
					Cards               int
				}
				Sent struct {
					Kind, Target string
					Params       struct{ Text, MessageID string }
				}
			}
			Cancelled     struct{ Sends, Cards int }
			Failed, Draft string
		}
	}
	runFixture(t, "codexfeed.html", &got)
	s := got.Shell
	if s.Cancelled.Sends != 0 || s.Cancelled.Cards != 0 || s.Draft != "keep the codex draft" {
		t.Errorf("cancel or draft changed: %+v", s)
	}
	if len(s.Runs) != 2 {
		t.Fatalf("codex shell runs = %+v", s.Runs)
	}
	command := "printf '%s\\n' 'a b' | cat"
	for i, r := range s.Runs {
		if r.Preview != command || r.Title != "Run in shop-checkout?" || r.Before != 0 {
			t.Errorf("codex shell gate = %+v", r)
		}
		if r.Sent.Kind != "session.shell" || r.Sent.Target != "shop-checkout" || r.Sent.Params.Text != command || !uuidShape.MatchString(r.Sent.Params.MessageID) {
			t.Errorf("codex shell action = %+v", r.Sent)
		}
		ended := []string{"exit 0", "exit 7"}[i]
		if r.Starting.State != "starting" || !strings.HasPrefix(r.Running.State, "running") || r.Ended.State != ended || r.Output != []string{"shell output 0", "shell output 7"}[i] {
			t.Errorf("codex shell states = %+v", r)
		}
		for _, state := range []struct {
			Command, State, Bar string
			Cards               int
		}{r.Starting, r.Running, r.Ended} {
			if state.Command != command || !strings.HasPrefix(state.Bar, "!"+command) || !strings.Contains(state.Bar, state.State) || state.Cards != i+1 {
				t.Errorf("codex shell card and bar disagree: %+v", state)
			}
		}
	}
	if !strings.Contains(s.Failed, "the daemon refused the shell command") {
		t.Errorf("codex shell refusal = %q", s.Failed)
	}
}

func TestACodexMessageCopiesThroughTheFeed(t *testing.T) {
	var got struct {
		Copy struct {
			Button bool   `json:"button"`
			Text   string `json:"text"`
			Toast  string `json:"toast"`
		} `json:"copy"`
	}
	runFixture(t, "codexfeed.html", &got)
	if !got.Copy.Button || got.Copy.Text != "Propose a plan for the indentation." || got.Copy.Toast != "Copied" {
		t.Errorf("the person's message in a codex feed did not copy: %+v", got.Copy)
	}
}

// What codex alone writes stands in its feed as cards, not as answers: a
// question of plan mode with the note beside its pick, and one put away as
// refused; the plan read whole; a review begun as a line and ended with its
// verdict and a row a finding — the answer codex writes after it saying the
// same is folded into it, and an answer that says something else stays; a goal
// with how it stands and its spend; and the line of a turn codex began for its
// goal.
func TestACodexFeedDrawsWhatCodexAloneWrites(t *testing.T) {
	type finding struct {
		Tag      string `json:"tag"`
		Title    string `json:"title"`
		At       string `json:"at"`
		Tone     string `json:"tone"`
		Expanded string `json:"expanded"`
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
	wantFinding := []finding{{Tag: "P1", Title: "Preserve addition semantics in add", At: "calc.py:2", Tone: "s-crit", Expanded: "false"}}
	if !reflect.DeepEqual(bad.Findings, wantFinding) {
		t.Errorf("the findings read %+v, want %+v", bad.Findings, wantFinding)
	}
	if good.Tone != "v-ok" || len(good.Findings) != 0 {
		t.Errorf("the review that found nothing reads %+v", good)
	}
	if got.Opened.Expanded != "true" {
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
