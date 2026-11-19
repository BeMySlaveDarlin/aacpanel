package check

import (
	"slices"
	"strings"
	"testing"
)

func TestQuickCommandsSendPhrasesAndKeepActionsBehindATap(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			type request struct {
				Kind, Target string
				Params       struct {
					Text, MessageID, Project, Session, Until string
					Alone, Group                             bool
				}
			}
			var got struct {
				Elapsed                            float64
				Error, Draft, Keyboard, All, Codex string
				Initial                            struct {
					Selected bool
					Phrases  []string
				}
				Tap, Drag, Restart, Report, Pause, Custom request
				TapOnly                                   map[string]string
				Five, Three, Inherited                    []string
			}
			screen.run(t, "quickcommands.html", &got)
			t.Logf("fixture: %.2f s", got.Elapsed)
			if got.Error != "" {
				t.Fatal(got.Error)
			}
			if got.Initial.Selected || got.Keyboard != "commands" {
				t.Errorf("tap selected a sector: %v; keyboard stood on %q", got.Initial.Selected, got.Keyboard)
			}
			if !slices.Equal(got.Initial.Phrases, []string{"Allcommands", "Status", "Go on", "AskUserQuestion", "Tidy up the checklist", "That's all for today"}) || got.Draft != "Keep this draft" {
				t.Errorf("the ring has %v, the composer keeps %q", got.Initial.Phrases, got.Draft)
			}
			for _, sent := range []struct {
				request request
				text    string
			}{{got.Tap, "Go on"}, {got.Drag, "Tidy up the checklist"}, {got.Custom, "Fifth custom"},
				{got.Report, "Write your report and close this session: to your parent if a session opened you, otherwise to the person in the conversation."}} {
				if sent.request.Kind != "session.send" || sent.request.Target != "lab" || sent.request.Params.Text != sent.text || sent.request.Params.MessageID == "" {
					t.Errorf("phrase %q went as %+v", sent.text, sent.request)
				}
			}
			for _, key := range []string{"restart", "report"} {
				if !strings.Contains(got.TapOnly[key], "lifting here does nothing") {
					t.Errorf("release on %s says %q", key, got.TapOnly[key])
				}
			}
			if got.Restart.Kind != "session.restart" || got.Restart.Target != "lab" {
				t.Errorf("restart went as %+v", got.Restart)
			}
			p := got.Pause
			if p.Kind != "project.pause" || p.Target != "lab" || p.Params.Project != "proj" || p.Params.Session != "lab" || !p.Params.Alone || p.Params.Group || p.Params.Until == "" {
				t.Errorf("the session-only pause went as %+v", p)
			}
			if got.All != "Commands" || got.Codex != "Commands" {
				t.Errorf("all commands: Claude %q, Codex %q", got.All, got.Codex)
			}
			for _, list := range []struct {
				got, want []string
			}{{got.Five, []string{"Allcommands", "Fourth custom", "Second custom", "First custom", "Third custom", "Fifth custom"}},
				{got.Three, []string{"Allcommands", "Second custom", "First custom", "Third custom"}},
				{got.Inherited, []string{"Allcommands", "Inherited two", "Inherited one", "Inherited three"}}} {
				if !slices.Equal(list.got, list.want) {
					t.Errorf("quick phrases %q, want %q", list.got, list.want)
				}
			}
		})
	}
}
