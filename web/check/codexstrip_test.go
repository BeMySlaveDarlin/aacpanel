package check

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// codexBand is the band under the field of a codex thread, as a fixture
// reads it.
type codexBand struct {
	Think    string   `json:"think"`
	Perm     string   `json:"perm"`
	PermOff  *bool    `json:"permOff"`
	PermWhy  string   `json:"permWhy"`
	ThinkOff *bool    `json:"thinkOff"`
	Clip     bool     `json:"clip"`
	Claude   []string `json:"claude"`
}

// On a phone the band of a codex thread says how it thinks — the model and
// the effort, or the plan and the effort while it plans, since the header
// names the model — and what it may do in a short word, beside the paperclip
// and with nothing of claude's. The word of what it may do presses only for
// one of the three modes the panel sets: settings of the thread's own and a
// thread the panel only reads stand as they are, saying why. Behind the words
// are the sheets: the plan, the models of the catalogue and the efforts the
// model takes, with no ultracode; the three modes and the sandbox. Each press
// sends one setting, and a model sent alone shows the effort it will run at.
func TestTheBandOfACodexThreadSaysHowItThinksAndWhatItMayDo(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type row struct {
		Name string `json:"name"`
		On   bool   `json:"on"`
		Off  bool   `json:"off"`
	}
	type thinkSheet struct {
		Title  string   `json:"title"`
		Plan   string   `json:"plan"`
		Models []row    `json:"models"`
		Effort string   `json:"effort"`
		Stops  []string `json:"stops"`
		StopOn string   `json:"stopOn"`
		Ultra  bool     `json:"ultra"`
	}
	var got struct {
		Ask, Plan, ReadOnly, Auto, Custom, Unknown codexBand
		AskedBeforeOpen                            int `json:"askedBeforeOpen"`
		UnknownPlan                                struct {
			Off     bool   `json:"off"`
			Sub     string `json:"sub"`
			Checked string `json:"checked"`
		} `json:"unknownPlan"`
		Think      thinkSheet       `json:"think"`
		AfterModel thinkSheet       `json:"afterModel"`
		PlanSent   []map[string]any `json:"planSent"`
		ModelSent  []map[string]any `json:"modelSent"`
		EffortSent []map[string]any `json:"effortSent"`
		ChipAfter  string           `json:"chipAfter"`
		Perm       struct {
			Title   string   `json:"title"`
			Rows    []row    `json:"rows"`
			Sandbox []string `json:"sandbox"`
		} `json:"perm"`
		ModeSent     []map[string]any `json:"modeSent"`
		PermAfter    string           `json:"permAfter"`
		Targets      []string         `json:"targets"`
		CustomOpened bool             `json:"customOpened"`
	}
	runFixture(t, "codexstrip.html", &got)

	pressable := func(where string, b codexBand, think, perm string) {
		t.Helper()
		if b.Think != think || b.Perm != perm {
			t.Errorf("%s: the band says %q and %q, expected %q and %q", where, b.Think, b.Perm, think, perm)
		}
		if b.ThinkOff == nil || *b.ThinkOff || b.PermOff == nil || *b.PermOff {
			t.Errorf("%s: the words of the band are off (%v, %v)", where, b.ThinkOff, b.PermOff)
		}
		if !b.Clip || len(b.Claude) != 0 {
			t.Errorf("%s: the band has the paperclip %v and claude's words %v", where, b.Clip, b.Claude)
		}
	}
	pressable("ask", got.Ask, "gpt-6-astra · High", "Ask")
	pressable("planning", got.Plan, "Plan · High", "Ask")
	pressable("read only", got.ReadOnly, "gpt-6-astra · High", "Read")
	pressable("approve for me", got.Auto, "gpt-6-astra · High", "Auto")

	still := func(where string, b codexBand, word, why string) {
		t.Helper()
		if b.Perm != word || b.PermOff == nil || !*b.PermOff || !strings.Contains(b.PermWhy, why) {
			t.Errorf("%s: what codex may do reads %q (off %v, %q), expected %q standing still and saying %q",
				where, b.Perm, b.PermOff, b.PermWhy, word, why)
		}
		if b.Think != "gpt-6-astra · High" || b.ThinkOff == nil || *b.ThinkOff {
			t.Errorf("%s: how codex thinks reads %q (off %v), expected the model and the effort, pressable", where, b.Think, b.ThinkOff)
		}
	}
	still("settings of its own", got.Custom, "Custom", "permissions of its own")
	still("a thread the panel only reads", got.Unknown, "Unknown", "the panel only reads this thread")
	if got.CustomOpened {
		t.Error("the word of settings of the thread's own opens a list to pick from")
	}
	if got.AskedBeforeOpen != 0 {
		t.Errorf("the catalogue of codex was asked %d times before a list was opened", got.AskedBeforeOpen)
	}
	if !got.UnknownPlan.Off || got.UnknownPlan.Checked != "false" || !strings.Contains(got.UnknownPlan.Sub, "not known") {
		t.Errorf("the plan of a thread the panel only reads: %+v, expected a switch standing still that says it is not known", got.UnknownPlan)
	}

	if got.Think.Title != "How codex thinks" || got.Think.Plan != "false" {
		t.Errorf("the sheet of how codex thinks is %q with the plan %q", got.Think.Title, got.Think.Plan)
	}
	wantModels := []row{{Name: "gpt-6-astra", On: true}, {Name: "gpt-6-astra-mini"}}
	if !reflect.DeepEqual(got.Think.Models, wantModels) {
		t.Errorf("the sheet lists the models %+v, expected the catalogue with the thread's own picked", got.Think.Models)
	}
	if strings.Join(got.Think.Stops, ",") != "Low,Medium,High,Extra" || got.Think.StopOn != "High" || got.Think.Ultra {
		t.Errorf("the scale has %v at %q (ultracode %v), expected the efforts the model takes at High and no ultracode",
			got.Think.Stops, got.Think.StopOn, got.Think.Ultra)
	}
	one := func(what string, sent []map[string]any, want map[string]any) {
		t.Helper()
		if len(sent) != 1 || !reflect.DeepEqual(sent[0], want) {
			t.Errorf("%s sent %v, expected one setting %v", what, sent, want)
		}
	}
	one("the plan switch", got.PlanSent, map[string]any{"plan": true})
	one("the press on a model", got.ModelSent, map[string]any{"model": "gpt-6-astra-mini"})
	if strings.Join(got.AfterModel.Stops, ",") != "Minimal,Low,Medium" || got.AfterModel.StopOn != "Medium" ||
		got.AfterModel.Effort != "Effort · Medium" || got.AfterModel.Plan != "true" {
		t.Errorf("after the model the sheet shows %+v, expected its efforts at its own default, since it does not take High", got.AfterModel)
	}
	one("the press on an effort", got.EffortSent, map[string]any{"effort": "minimal"})
	if got.ChipAfter != "Plan · Minimal" {
		t.Errorf("after the picks the band says %q", got.ChipAfter)
	}

	if got.Perm.Title != "What codex may do" {
		t.Errorf("the sheet of the modes is titled %q", got.Perm.Title)
	}
	wantModes := []row{{Name: "Read only"}, {Name: "Ask", On: true}, {Name: "Approve for me"}}
	if !reflect.DeepEqual(got.Perm.Rows, wantModes) {
		t.Errorf("the sheet offers the modes %+v, expected the three the panel sets, the thread's own picked", got.Perm.Rows)
	}
	if strings.Join(got.Perm.Sandbox, "|") != "writes /srv/shop|beyond it codex asks you|full access only when a thread starts" {
		t.Errorf("the sandbox of the thread reads %v", got.Perm.Sandbox)
	}
	one("the press on a mode", got.ModeSent, map[string]any{"mode": "auto"})
	if got.PermAfter != "Auto" {
		t.Errorf("after the pick the band says %q", got.PermAfter)
	}
	if strings.Join(got.Targets, ",") != "codex-5afc361b" {
		t.Errorf("the settings went to %v", got.Targets)
	}
}
