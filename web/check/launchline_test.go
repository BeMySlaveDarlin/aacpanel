package check

import (
	"strings"
	"testing"
)

// The launch line folds the stream's plumbing into one word, marks the
// project's own values apart from the contour's and from the command's own
// words, and says what the holder does past the handshake — the first message
// by what it is, not its words.
func TestTheLaunchLineReadsAsTheCommand(t *testing.T) {
	var got struct {
		Stream       []string `json:"stream"`
		Tmux         []string `json:"tmux"`
		StreamLayers []string `json:"streamLayers"`
		TmuxLayers   []string `json:"tmuxLayers"`
		Then         string   `json:"then"`
		TmuxThen     bool     `json:"tmuxThen"`
		OnStream     []bool   `json:"onStream"`
		Labels       []string `json:"labels"`
		Drift        []string `json:"drift"`
	}
	runFixture(t, "launchline.html", &got)
	if strings.Join(got.Stream, " ") != "claude ⟨stream⟩ -n person --effort high" {
		t.Errorf("the stream line reads %q", strings.Join(got.Stream, " "))
	}
	if strings.Join(got.Tmux, " ") != "claude -n aacpanel --remote-control aacpanel" {
		t.Errorf("the tmux line reads %q", strings.Join(got.Tmux, " "))
	}
	if s, m := strings.Join(got.StreamLayers, ","), strings.Join(got.TmuxLayers, ","); s != ",project,,,contour,contour" || m != ",,,project,project" {
		t.Errorf("the words of the stream line come from %q and of the tmux line from %q — meant "+
			"the project's own, the contour's and the command's own told apart", s, m)
	}
	if got.Then != "then: remote control on · the first message" || got.TmuxThen {
		t.Errorf("then reads %q (tmux has one: %v)", got.Then, got.TmuxThen)
	}
	if strings.Join(got.Labels, "|") != "stream|no RC|cap 70%|no first message|sonnet|xhigh|codex xhigh|codex gpt-6|tmux|codex tmux" {
		t.Errorf("a project's own values read %v", got.Labels)
	}
	want := "person lives in tmux — the project says the stream|api runs Fable — the project says Opus"
	if strings.Join(got.Drift, "|") != want {
		t.Errorf("drift reads %q, meant %q — and a model the launch does not name is the session's own pick", strings.Join(got.Drift, "|"), want)
	}
	if got.OnStream[0] != true || got.OnStream[1] || got.OnStream[2] {
		t.Errorf("onStream says %v", got.OnStream)
	}
}
