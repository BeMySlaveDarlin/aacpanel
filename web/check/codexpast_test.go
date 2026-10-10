package check

import (
	"os"
	"strings"
	"testing"
)

// A thread of codex in the archive stands beside a conversation of claude and
// reads by the word of its agent in the colour of its agent, as a live thread
// does, with its model and the tokens it spent in place of the words of the
// person and the fill of the context, which the archive of codex does not
// keep. Its resume goes to the daemon of its home — the thread by its id and
// the contour of its home — and the gate says so; it opens the feed of its
// rollout by its id.
func TestAThreadOfCodexInTheArchiveReadsAsCodexResumesAndOpens(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type line struct {
		Word   string `json:"word"`
		Agent  string `json:"agent"`
		Hue    string `json:"hue"`
		About  string `json:"about"`
		When   string `json:"when"`
		Resume bool   `json:"resume"`
	}
	type sent struct {
		Kind   string         `json:"kind"`
		Target string         `json:"target"`
		Params map[string]any `json:"params"`
	}
	var got struct {
		Phone []line `json:"phone"`
		Desk  []struct {
			Word  string `json:"word"`
			Hue   string `json:"hue"`
			Num   string `json:"num"`
			Model string `json:"model"`
		} `json:"desk"`
		Shelf  []string `json:"shelf"`
		Effect string   `json:"effect"`
		Sent   []sent   `json:"sent"`
		Opened []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"opened"`
		Feed  []string `json:"feed"`
		Asked []string `json:"asked"`
	}
	runWideFixture(t, "codexpast.html", &got)

	const thread = "01a120b0-28ab-7600-8dfe-714b9a8a82d7"
	if len(got.Phone) != 2 {
		t.Fatalf("the phone's archive shows %d lines: %+v", len(got.Phone), got.Phone)
	}
	claude, codex := got.Phone[0], got.Phone[1]
	if claude.Word != "" || !strings.Contains(claude.About, "login page") || !strings.Contains(claude.When, "peak 40%") {
		t.Errorf("the conversation of claude reads %+v", claude)
	}
	if codex.Word != "Codex" || codex.Agent != "codex" || codex.Hue != codexWordHue {
		t.Errorf("the thread of codex is not signed by its agent in its colour: %+v, the colour of codex is %s", codex,
			codexWordHue)
	}
	if !strings.Contains(codex.About, "gpt-6-astra") || !strings.Contains(codex.About, "xhigh") ||
		!strings.Contains(codex.When, "29.4m tokens") || strings.Contains(codex.When, "peak") || !codex.Resume {
		t.Errorf("the thread of codex reads %+v: its model, its effort and what it spent were meant", codex)
	}

	if len(got.Desk) != 2 || got.Desk[1].Word != "Codex" || got.Desk[1].Hue != codexWordHue ||
		got.Desk[1].Num != "29.4m" || !strings.Contains(got.Desk[1].Model, "gpt-6-astra") || got.Desk[0].Word != "" {
		t.Errorf("the archive panel of the desktop reads %+v", got.Desk)
	}
	if len(got.Shelf) != 1 || got.Shelf[0] != thread {
		t.Errorf("the shelf of closed conversations holds %v: a thread that spent tokens has something in it", got.Shelf)
	}

	if !strings.Contains(got.Effect, "codex daemon") {
		t.Errorf("the gate says %q before a resume of codex", got.Effect)
	}
	if len(got.Sent) != 2 {
		t.Fatalf("the resumes sent %+v", got.Sent)
	}
	for i, s := range got.Sent {
		if s.Kind != "session.resume" || s.Params["session"] != thread || s.Params["agent"] != "codex" ||
			s.Params["contour"] != "acme" {
			t.Errorf("resume %d went as %+v: the thread, agent codex and the contour of its home were meant", i, s)
		}
	}

	if len(got.Opened) != 1 || got.Opened[0].ID != thread {
		t.Errorf("the line opened %+v", got.Opened)
	}
	if len(got.Feed) != 1 || got.Feed[0] != "The router lives in src/router." {
		t.Errorf("the feed of the thread reads %v", got.Feed)
	}
	if len(got.Asked) == 0 || !strings.Contains(got.Asked[0], "id="+thread) {
		t.Errorf("the feed was asked as %v: by the id of the thread was meant", got.Asked)
	}
}
