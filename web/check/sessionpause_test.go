package check

import (
	"strings"
	"testing"
	"time"
)

func TestSessionPauseAndRestartChoices(t *testing.T) {
	for _, screen := range []struct {
		name, file string
		run        func(*testing.T, string, any)
	}{
		{"phone", "sessionstops.html", runFixture}, {"desk", "sessiongroups.html", runWideFixture},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Pause struct {
					State, Alarm, Header, Chosen                      string
					Feed, ProjectRecipients, GroupRecipients, Options []string
					Letters                                           int
					Priority                                          [][]string
					PauseSent, Restart                                []struct {
						Kind, Target string
						Params       map[string]any
					}
					NoConfirmation, Menu, Empty, PauseFits, RestartFits bool
				}
			}
			screen.run(t, screen.file, &got)
			p := got.Pause
			if !strings.Contains(p.State, "paused · until") || !strings.Contains(p.Header, "paused · until") || p.Alarm == "" {
				t.Errorf("pause state or alarm absent: %+v", p)
			}
			if len(p.Feed) != 3 || !strings.HasPrefix(p.Feed[0], "Pause until") || p.Feed[1] != "Pause lifted" || p.Feed[2] != "Pause over" || p.Letters != 0 {
				t.Errorf("pause letters lost their cards: %v", p.Feed)
			}
			for i, want := range []string{"busy", "wait", "bg", "idle", "busy"} {
				head := want
				if head == "wait" {
					head = "waiting"
				}
				if len(p.Priority) != 5 || strings.Join(p.Priority[i], ",") != want+","+head {
					t.Fatalf("pause overrode activity: %v", p.Priority)
				}
			}
			if strings.Join(p.ProjectRecipients, ",") != "lab · paused" || strings.Join(p.GroupRecipients, ",") != "lab · paused,peer,quiet" {
				t.Errorf("pause scope: %v / %v", p.ProjectRecipients, p.GroupRecipients)
			}
			if len(p.PauseSent) != 2 || !p.NoConfirmation {
				t.Fatalf("pause actions: %+v", p.PauseSent)
			}
			first, second := p.PauseSent[0], p.PauseSent[1]
			until, _ := first.Params["until"].(string)
			at, err := time.Parse(time.RFC3339, until)
			if first.Kind != "project.pause" || first.Params["project"] != float64(501) || first.Params["group"] != true || err != nil || time.Until(at) < 25*time.Minute || time.Until(at) > 31*time.Minute {
				t.Errorf("pause request: %+v, %v", first, err)
			}
			if second.Kind != "project.pause" || second.Params["group"] != false || second.Params["resume"] != true || second.Params["until"] != nil {
				t.Errorf("resume request: %+v", second)
			}
			if !p.PauseFits || !p.RestartFits {
				t.Errorf("the sheet hides its confirmation: pause %v, restart %v", p.PauseFits, p.RestartFits)
			}
			if len(p.Options) != 3 || !strings.HasPrefix(p.Options[0], "Clean restart") || !strings.Contains(p.Options[0], "no first message") || !strings.HasPrefix(p.Options[1], "Restart and continue") || !strings.HasPrefix(p.Options[2], "Restart as auto-restart") {
				t.Errorf("restart choices: %v", p.Options)
			}
			if len(p.Restart) != 3 {
				t.Fatalf("restart actions: %+v", p.Restart)
			}
			for i, key := range []string{"clean", "resume", "prepare"} {
				r := p.Restart[i]
				if r.Kind != "session.restart" || r.Target != "lab" || len(r.Params) != 1 || r.Params[key] != true {
					t.Errorf("restart %s: %+v", key, r)
				}
			}
			if screen.name == "desk" && !p.Menu || screen.name == "phone" && !p.Empty {
				t.Errorf("pause entry availability: %+v", p)
			}
		})
	}
}
