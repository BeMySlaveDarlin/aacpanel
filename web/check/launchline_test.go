package check

import (
	"strings"
	"testing"
)

// The launch line folds the stream's plumbing into one word, underlines the
// project's own values and dashes the contour's, and says what the holder
// does past the handshake — the first message by what it is, not its words.
func TestTheLaunchLineReadsAsTheCommand(t *testing.T) {
	var got struct {
		Stream   []string `json:"stream"`
		Tmux     []string `json:"tmux"`
		Own      string   `json:"own"`
		Contour  string   `json:"contour"`
		Plain    string   `json:"plain"`
		Then     string   `json:"then"`
		TmuxThen bool     `json:"tmuxThen"`
		OnStream []bool   `json:"onStream"`
		Labels   []string `json:"labels"`
		Drift    []string `json:"drift"`
	}
	runFixture(t, "launchline.html", &got)
	if strings.Join(got.Stream, " ") != "claude ⟨stream⟩ -n person --effort high" {
		t.Errorf("the stream line reads %q", strings.Join(got.Stream, " "))
	}
	if strings.Join(got.Tmux, " ") != "claude -n aacpanel --remote-control aacpanel" {
		t.Errorf("the console line reads %q", strings.Join(got.Tmux, " "))
	}
	if got.Own != "solid" || got.Contour != "dashed" || got.Plain != "none" {
		t.Errorf("own %q, contour %q, the command's own %q — meant solid, dashed, none", got.Own, got.Contour, got.Plain)
	}
	if got.Then != "then: remote control on · the first message" || got.TmuxThen {
		t.Errorf("then reads %q (console has one: %v)", got.Then, got.TmuxThen)
	}
	if strings.Join(got.Labels, "|") != "feed|no RC|cap 70%|no first message|sonnet" {
		t.Errorf("a project's own values read %v", got.Labels)
	}
	want := "person runs in the console — the project says the feed|ai-platform runs Fable — the project says Opus"
	if strings.Join(got.Drift, "|") != want {
		t.Errorf("drift reads %q, meant %q — and a model the launch does not name is the session's own pick", strings.Join(got.Drift, "|"), want)
	}
	if got.OnStream[0] != true || got.OnStream[1] || got.OnStream[2] {
		t.Errorf("onStream says %v", got.OnStream)
	}
}
