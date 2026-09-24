package check

import (
	"strings"
	"testing"
)

// A project is put on the stream from the screen of its launch parameters,
// not by an edit of the database: the choice is a field, the summary names
// it, and the view says where it came from.
func TestWhereTheSessionLivesIsAFieldOfTheLaunch(t *testing.T) {
	var got struct {
		Fields    string `json:"fields"`
		Inherited string `json:"inherited"`
		View      string `json:"view"`
		Summary   string `json:"summary"`
		Tmux      string `json:"tmux"`
	}
	launchNode(t, launchBundle(t, true), renderPrelude+`
import { summary } from BUNDLE;
process.stdout.write(JSON.stringify({
    fields: text(LaunchFields({ value: { transport: "stream" }, onChange: () => {}, inherited: {} })),
    inherited: text(LaunchFields({ value: {}, onChange: () => {}, inherited: { transport: "stream" } })),
    view: text(LaunchView({ launch: {}, profile: { transport: "stream" } })),
    summary: summary({ transport: "stream", model: "haiku" }),
    tmux: summary({ transport: "tmux" }),
}));
`, &got)

	for _, want := range []string{"Where the session lives", "value=stream", "no terminal", "remote control does not reach"} {
		if !strings.Contains(got.Fields, want) {
			t.Errorf("the field does not say %q:\n%s", want, got.Fields)
		}
	}
	if !strings.Contains(got.Inherited, "from the profile: stream") {
		t.Errorf("a project silent on it does not say the profile puts it on the stream:\n%s", got.Inherited)
	}
	if !strings.Contains(got.View, "lives in") || !strings.Contains(got.View, "from the profile") {
		t.Errorf("the view does not show where the session lives and why:\n%s", got.View)
	}
	if !strings.Contains(got.Summary, "on the stream") {
		t.Errorf("the summary does not name the stream: %q", got.Summary)
	}
	if strings.Contains(got.Tmux, "stream") {
		t.Errorf("a terminal session is summed up as a stream one: %q", got.Tmux)
	}
}
