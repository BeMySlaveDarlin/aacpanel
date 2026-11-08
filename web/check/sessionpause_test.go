package check

import (
	"fmt"
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
			type choice struct {
				Value   string
				Pressed []string
			}
			var got struct {
				Pause struct {
					Sleep struct {
						States         []string
						Controls, Menu []struct {
							Key, Label, Why string
							Off             bool
						}
						Continue, Close struct {
							Kind, Target string
							Params       map[string]any
						}
					}
					State, Ends, Header, SessionScope, SessionCount, OpenedCount, OpenedResume             string
					Feed, ProjectRecipients, GroupRecipients, Options, SessionRecipients, OpenedRecipients []string
					SessionScopes, NoOpenedScopes                                                          []struct{ Value, Label string }
					AloneResumeAbsent                                                                      bool
					Letters                                                                                int
					Priority, Choices                                                                      [][]string
					PauseSent, Restart, OneSent                                                            []struct {
						Kind, Target string
						Params       map[string]any
					}
					NoConfirmation, Menu          bool
					BeforeNine, ForTwo, AfterNine choice
					Opened                        struct {
						choice
						Ahead float64
					}
					NoEnd struct {
						choice
						Off  bool
						Note string
					}
					Expect struct{ NineToday, NineTomorrow, TwoHours, NineTodayAt, NineTomorrowAt string }
				}
			}
			screen.run(t, screen.file, &got)
			p := got.Pause
			if len(p.Sleep.States) != 2 || !strings.HasPrefix(p.Sleep.States[0], "sleeping until ") || p.Sleep.States[1] != "sleeping" {
				t.Errorf("sleeping states: %v", p.Sleep.States)
			}
			for _, control := range p.Sleep.Controls {
				allowed := control.Key == "pause" || control.Key == "close" || strings.HasPrefix(control.Label, "continue session ") || strings.HasPrefix(control.Label, "close session ")
				if allowed && control.Off || !allowed && (!control.Off || control.Why != "sleeping") {
					t.Errorf("sleeping control: %+v", control)
				}
			}
			if screen.name == "desk" {
				if len(p.Sleep.Menu) == 0 {
					t.Error("sleeping menu is absent")
				}
				for _, item := range p.Sleep.Menu {
					allowed := item.Label == "Continue now" || item.Label == "Close the session"
					if allowed && item.Off || !allowed && (!item.Off || item.Why != "sleeping") {
						t.Errorf("sleeping menu item: %+v", item)
					}
				}
			}
			if c := p.Sleep.Continue; c.Kind != "project.pause" || c.Target != "lab" || c.Params["session"] != "lab" || c.Params["project"] != float64(501) || c.Params["alone"] != true || c.Params["resume"] != true {
				t.Errorf("sleeping Continue: %+v", c)
			}
			if c := p.Sleep.Close; c.Kind != "session.close" || c.Target != "lab" {
				t.Errorf("sleeping Close: %+v", c)
			}
			// The card says paused and when the pause ends beside it; the head
			// of the conversation says both too.
			if !strings.HasPrefix(p.State, "paused until ") || strings.HasPrefix(p.Ends, "→") || !strings.Contains(p.Header, "paused until ") {
				t.Errorf("pause state or its end absent: %+v", p)
			}
			if len(p.Feed) != 4 || !strings.HasPrefix(p.Feed[0], "Pause until") || p.Feed[1] != "Pause lifted" || p.Feed[2] != "Pause over" ||
				p.Feed[3] != "Pause with no end" || p.Letters != 0 {
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
			// A pause reaches the sessions its sessions had the panel open, the
			// helper of lab-docs in the other project among them.
			if strings.Join(p.ProjectRecipients, ",") != "lab · paused,lab-docs,helper · paused" || strings.Join(p.GroupRecipients, ",") != "lab · paused,lab-docs,peer,quiet,helper · paused" {
				t.Errorf("pause scope: %v / %v", p.ProjectRecipients, p.GroupRecipients)
			}
			wantChoices := [][]string{{"For", "1 h", "2 h", "4 h", "6 h", "12 h"}, {"Until the morning", "8:00", "9:00", "10:00"},
				{"Until lunch", "12:00", "13:00", "14:00"}, {"Until the evening", "17:00", "18:00", "19:00"}, {"Until lifted", "No end"}}
			if fmt.Sprint(p.Choices) != fmt.Sprint(wantChoices) {
				t.Errorf("the quick choices: %v", p.Choices)
			}
			// A quick choice puts its moment into the field: the sheet opens an
			// hour ahead with that choice pressed, until 9 is today's nine at
			// half past eight and tomorrow's at half past nine, and two hours
			// from half past nine is half past eleven. No end empties the field
			// and turns it off.
			for _, c := range []struct {
				name string
				got  choice
				want string
				on   string
			}{
				{"before nine", p.BeforeNine, p.Expect.NineToday, "9:00"}, {"after nine", p.AfterNine, p.Expect.NineTomorrow, "9:00"},
				{"two hours", p.ForTwo, p.Expect.TwoHours, "2 h"}, {"no end", p.NoEnd.choice, "", "No end"},
			} {
				if c.got.Value != c.want || strings.Join(c.got.Pressed, ",") != c.on {
					t.Errorf("%s: the field reads %q with %v pressed, want %q with %s", c.name, c.got.Value, c.got.Pressed, c.want, c.on)
				}
			}
			if strings.Join(p.Opened.Pressed, ",") != "1 h" || p.Opened.Ahead < 55 || p.Opened.Ahead > 61 {
				t.Errorf("the sheet opens on %+v", p.Opened)
			}
			if !p.NoEnd.Off || !strings.HasPrefix(p.NoEnd.Note, "No end") {
				t.Errorf("the field of a pause with no end: %+v", p.NoEnd)
			}
			if len(p.PauseSent) != 2 || !p.NoConfirmation {
				t.Fatalf("pause actions: %+v", p.PauseSent)
			}
			first, second := p.PauseSent[0], p.PauseSent[1]
			if first.Kind != "project.pause" || first.Params["project"] != float64(501) || first.Params["group"] != true || !sameMoment(first.Params["until"], p.Expect.NineTodayAt) {
				t.Errorf("pause request: %+v, want until %s", first, p.Expect.NineTodayAt)
			}
			if second.Kind != "project.pause" || second.Params["group"] != false || second.Params["resume"] != true || second.Params["until"] != nil {
				t.Errorf("resume request: %+v", second)
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
			if screen.name == "desk" && !p.Menu {
				t.Errorf("pause entry availability: %+v", p)
			}
			// The card defaults to one session; the sessions it opened belong
			// to a scope of their own, available only when it reaches some.
			if p.SessionScope != "session" || strings.Join(p.SessionRecipients, ",") != "lab-docs" || p.SessionCount != "1 session in this scope:" {
				t.Errorf("the pause of one session opens on %q for %v", p.SessionScope, p.SessionRecipients)
			}
			if strings.Join(p.OpenedRecipients, ",") != "lab-docs,helper · paused" || p.OpenedCount != "2 sessions in this scope:" || p.OpenedResume != "Resume 1" || !p.AloneResumeAbsent {
				t.Errorf("the scope of opened sessions: recipients %v, count %q, resume %q, alone resume absent %v", p.OpenedRecipients, p.OpenedCount, p.OpenedResume, p.AloneResumeAbsent)
			}
			for _, c := range []struct {
				name string
				got  []struct{ Value, Label string }
				want string
			}{
				{"with opened sessions", p.SessionScopes, "session:This session · lab-docs,opened:With the 1 it opened · lab-docs,project:This project · lab,group:Whole group · Services"},
				{"without opened sessions", p.NoOpenedScopes, "session:This session · lab-docs,project:This project · lab,group:Whole group · Services"},
			} {
				var options []string
				for _, o := range c.got {
					options = append(options, o.Value+":"+o.Label)
				}
				if strings.Join(options, ",") != c.want {
					t.Errorf("the scopes %s: %+v", c.name, c.got)
				}
			}
			if len(p.OneSent) != 4 {
				t.Fatalf("the pause of one session sent %+v", p.OneSent)
			}
			if c := p.OneSent[0]; c.Kind != "project.pause" || c.Params["project"] != float64(501) || c.Params["session"] != "lab" ||
				c.Params["alone"] != true || c.Params["resume"] != true || c.Params["group"] != nil {
				t.Errorf("the continue of the paused card sent %+v", c)
			}
			if c := p.OneSent[1]; c.Kind != "project.pause" || c.Params["session"] != "lab-docs" || !sameMoment(c.Params["until"], p.Expect.NineTomorrowAt) ||
				c.Params["alone"] != true || c.Params["group"] != nil || c.Params["noEnd"] != nil {
				t.Errorf("the pause of one session sent %+v, want until %s", c, p.Expect.NineTomorrowAt)
			}
			if c := p.OneSent[2]; c.Kind != "project.pause" || c.Params["session"] != "lab-docs" || c.Params["alone"] != false || c.Params["noEnd"] != true || c.Params["until"] != nil {
				t.Errorf("the pause with no end sent %+v", c)
			}
			if c := p.OneSent[3]; c.Kind != "project.pause" || c.Params["session"] != "lab-docs" || c.Params["alone"] != false || c.Params["resume"] != true || c.Params["group"] != nil || c.Params["until"] != nil {
				t.Errorf("the resume of opened sessions sent %+v", c)
			}
		})
	}
}

// sameMoment says whether the end a request carries, RFC 3339 with the
// person's offset, is the moment the fixture meant.
func sameMoment(until any, want string) bool {
	text, _ := until.(string)
	got, err := time.Parse(time.RFC3339, text)
	meant, errMeant := time.Parse(time.RFC3339, want)
	return err == nil && errMeant == nil && got.Equal(meant)
}
