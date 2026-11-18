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
	}{{"phone", "sessionstops.html", runFixture}, {"desk", "sessiongroups.html", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			type request struct {
				Kind, Target string
				Params       map[string]any
			}
			type item struct {
				Key, Label, Why string
				Off             bool
			}
			type choiceState struct {
				Disabled, Off bool
				Value         string
				Picked        []string
			}
			var got struct {
				Pause struct {
					State, Header, Toast, Together, Friday, Move, NoEndWords, DistantMark, KeyboardDay string
					Feed, Scope, GroupScope, Mornings, Options                                         []string
					Priority                                                                           [][]string
					Sent                                                                               map[string]request
					Initial                                                                            struct {
						Disabled      bool
						Toggle, Words string
					}
					Interaction struct {
						FocusClosed, PassClosed, ReleaseOpened, RepeatClosed, HoverClosed, ClickOpened, ContinueClosed, KeyboardFocusOff, DragRepeat bool
						OtherPlate, ReturnPlate, LivePlate, DayHint                                                                                  string
						DialRepeat, NoEndRepeat, DayBefore, DayPicked, DayHover, DayCarry, DayRepeat, DayLimit                                       choiceState
						ScopeBefore, ScopeHover, ScopeClick, ScopeKept                                                                               []string
					}
					TwoHours, Hover struct{ Plate, Button string }
					Release         struct {
						Requests int
						Button   string
					}
					ForeignRelease                                                            int
					Days                                                                      []item
					DayOff, NoOpened, NoConfirmation, ProjectMenu, SessionMenu, SleepingAlone bool
					OverLimit, Back                                                           struct {
						Disabled bool
						Value    string
					}
					WithinLimit string
					Sleep       struct {
						Plate, Mark, Toggle, ReadOnly string
						Controls                      []item
					}
					Indefinite struct{ Mark, Plate string }
					Project    struct {
						Toggle  bool
						Sectors []item
					}
					Restart []request
					Letters int
					Edges   struct{ Asleep, Awake string }
					Expect  struct{ TwoHours, Friday, Six string }
				}
			}
			screen.run(t, screen.file, &got)
			p := got.Pause
			if p.State != "sleeping until Fri 10:00" || p.Header != "sleeping until Fri 20 Nov, 10:00 · in 1 d 18 h" {
				t.Errorf("the shared words of the end: card %q, conversation %q", p.State, p.Header)
			}
			if strings.Join(p.Feed, "|") != "Pause until Fri 20 Nov, 10:00|Pause lifted|Pause over|Pause with no end" || p.NoEndWords != "until lifted" || p.Letters != 0 {
				t.Errorf("the feed's pause words: %v, no end %q, %d drawn as letters", p.Feed, p.NoEndWords, p.Letters)
			}
			// A sleeping session holds its context without spending it: its
			// bar is grey, an awake one's keeps the colour of its fill.
			if !strings.Contains(p.Edges.Asleep, "scsleep") || p.Edges.Awake == "" || strings.Contains(p.Edges.Awake, "scsleep") {
				t.Errorf("the context bar of a sleeping card is %q, of an awake one %q: want grey only while asleep", p.Edges.Asleep, p.Edges.Awake)
			}
			for i, want := range []string{"busy,busy", "wait,waiting", "bg,bg", "idle,idle", "busy,busy"} {
				if len(p.Priority) != 5 || strings.Join(p.Priority[i], ",") != want {
					t.Fatalf("pause overrode activity: %v", p.Priority)
				}
			}
			if strings.Join(p.Scope, ",") != "evirma-api,review,tests" || strings.Join(p.GroupScope, ",") != "evirma-api,review,tests,grandchild,evirma-billing" {
				t.Errorf("the one-level session and map-group scope: %v / %v", p.Scope, p.GroupScope)
			}
			if !p.Initial.Disabled || p.Initial.Toggle != "false" || p.Initial.Words != "Pick when it ends" || p.Together != "true" || !p.NoOpened {
				t.Errorf("the initial bar or opened toggle: %+v, together %s, absent without opened %v", p.Initial, p.Together, p.NoOpened)
			}
			if !strings.HasPrefix(p.TwoHours.Plate, "until 17:20 · in 2 h") || p.TwoHours.Button != "Pause evirma-apiuntil 17:20" {
				t.Errorf("two hours readback: %+v", p.TwoHours)
			}
			if p.Hover.Plate != p.TwoHours.Plate || p.Hover.Button != p.TwoHours.Button {
				t.Errorf("hover changed the selection: %+v", p.Hover)
			}
			i := p.Interaction
			if !i.FocusClosed || !i.PassClosed || !i.HoverClosed || !i.ReleaseOpened || !i.RepeatClosed || !i.ClickOpened || !i.ContinueClosed {
				t.Errorf("pause activation needs a press, and a repeated press closes it: %+v", i)
			}
			if !strings.HasPrefix(i.OtherPlate, "Restart the session") || i.ReturnPlate != p.TwoHours.Plate {
				t.Errorf("the sector's own hint and the committed end: %q / %q", i.OtherPlate, i.ReturnPlate)
			}
			for name, state := range map[string]choiceState{"hour": i.DialRepeat, "no end": i.NoEndRepeat} {
				if !state.Disabled || state.Value != "No value selected" {
					t.Errorf("pressing the selected %s did not clear it: %+v", name, state)
				}
			}
			if !strings.HasPrefix(i.LivePlate, "until 17:20 · in 2 h") || !i.DragRepeat {
				t.Errorf("a drag must read live and choose even the same end: %q, %v", i.LivePlate, i.DragRepeat)
			}
			if !i.DayBefore.Off || len(i.DayBefore.Picked) != 0 || !i.KeyboardFocusOff {
				t.Errorf("hover or focus chose a day: %+v, keyboard off %v", i.DayBefore, i.KeyboardFocusOff)
			}
			if i.DayPicked.Off || strings.Join(i.DayPicked.Picked, ",") != "day-2" || strings.Join(i.DayHover.Picked, ",") != "day-2" || !strings.HasPrefix(i.DayHover.Value, "until Fri 20 Nov, 10:00") || !strings.HasPrefix(i.DayHint, "Sat 21") {
				t.Errorf("the clicked day did not keep its mark and hour: %+v / %+v", i.DayPicked, i.DayHover)
			}
			if !strings.HasPrefix(i.DayCarry.Value, "until Sat 21 Nov, 10:00") || i.DayCarry.Disabled || strings.Join(i.DayCarry.Picked, ",") != "day-3" {
				t.Errorf("changing the day lost its hour: %+v", i.DayCarry)
			}
			if !i.DayRepeat.Off || !i.DayRepeat.Disabled || len(i.DayRepeat.Picked) != 0 || i.DayRepeat.Value != "No value selected" {
				t.Errorf("pressing the selected day did not clear it and its hour: %+v", i.DayRepeat)
			}
			if !i.DayLimit.Disabled || i.DayLimit.Off || i.DayLimit.Value != "No value selected" || strings.Join(i.DayLimit.Picked, ",") != "day-7" {
				t.Errorf("moving to the final day retained an unavailable hour: %+v", i.DayLimit)
			}
			if strings.Join(i.ScopeBefore, ",") != "project" || strings.Join(i.ScopeHover, ",") != "project" || strings.Join(i.ScopeClick, ",") != "group" || strings.Join(i.ScopeKept, ",") != "group" {
				t.Errorf("the project scope needs a click and keeps its mark: %+v / %+v / %+v / %+v", i.ScopeBefore, i.ScopeHover, i.ScopeClick, i.ScopeKept)
			}
			if p.Release.Requests != 2 || !strings.Contains(p.Release.Button, "17:20") || p.ForeignRelease != 2 {
				t.Errorf("a release sent a request: crown %+v, bar %d", p.Release, p.ForeignRelease)
			}
			if len(p.Days) != 8 || p.Days[0].Label != "Thu 19" || p.Days[6].Label != "Wed 25" || !p.Days[7].Off || p.Days[7].Why != "over 7 d" || !p.DayOff {
				t.Errorf("the days ring and unselected hours: %+v, hours off %v", p.Days, p.DayOff)
			}
			if !strings.HasPrefix(p.Friday, "until Fri 20 Nov, 10:00 · in 1 d 18 h") || !p.OverLimit.Disabled || p.OverLimit.Value != "No value selected" || !strings.Contains(p.WithinLimit, "Wed 15:00") {
				t.Errorf("the other day or seven-day limit: %q, %+v, %q", p.Friday, p.OverLimit, p.WithinLimit)
			}
			if !strings.HasPrefix(p.KeyboardDay, "until Fri 20 Nov, 7:00") {
				t.Errorf("the keyboard's day and hour: %q", p.KeyboardDay)
			}
			if !p.Back.Disabled || p.Back.Value != "No value selected" {
				t.Errorf("Back retained a selected end: %+v", p.Back)
			}
			if p.DistantMark != "until Fri 10:00" || p.Sleep.Mark != "until 18:00" || !strings.HasPrefix(p.Sleep.Plate, "Sleeps until 18:00 · 2 h 40 min left") ||
				!strings.Contains(p.Sleep.Plate, "review and tests sleep with it") || p.Sleep.Toggle != "true" || p.Sleep.ReadOnly != "until 18:00" {
				t.Errorf("the sleeping session's viewing dial: %+v", p.Sleep)
			}
			for _, control := range p.Sleep.Controls {
				allowed := control.Key == "pause" || control.Key == "close"
				if allowed && control.Off || !allowed && (!control.Off || control.Why != "sleeping") {
					t.Errorf("sleeping control: %+v", control)
				}
			}
			if len(p.Sleep.Controls) == 0 || !strings.HasPrefix(p.Move, "Move the end of evirma-api + 2") || p.Indefinite.Mark != "until lifted" || !strings.HasPrefix(p.Indefinite.Plate, "Sleeps until lifted") {
				t.Errorf("sleeping pause controls: move %q, indefinite %+v", p.Move, p.Indefinite)
			}
			if p.Project.Toggle || len(p.Project.Sectors) != 3 || p.Project.Sectors[0].Key != "project" || p.Project.Sectors[1].Key != "group" || p.Project.Sectors[2].Key != "continue" {
				t.Errorf("the project scope ring: %+v", p.Project)
			}
			sessionRequest := func(name string, alone bool, end string, noEnd, resume bool) {
				t.Helper()
				r, ok := p.Sent[name]
				if !ok || r.Kind != "project.pause" || r.Target != "evirma" || r.Params["project"] != float64(501) || r.Params["session"] != "evirma-api" || r.Params["alone"] != alone || r.Params["group"] != nil ||
					(r.Params["noEnd"] == true) != noEnd || (r.Params["resume"] == true) != resume {
					t.Errorf("%s scope: %+v", name, r)
				}
				if end != "" {
					value, _ := r.Params["until"].(string)
					if strings.HasSuffix(value, "Z") {
						t.Errorf("%s lost the local offset: %s", name, value)
					}
				}
				if end != "" && !sameMoment(r.Params["until"], end) || end == "" && r.Params["until"] != nil {
					t.Errorf("%s end: %+v, want %s", name, r, end)
				}
			}
			sessionRequest("alone", true, p.Expect.TwoHours, false, false)
			sessionRequest("undo", true, "", false, true)
			sessionRequest("together", false, p.Expect.TwoHours, false, false)
			sessionRequest("noEnd", true, "", true, false)
			sessionRequest("friday", true, p.Expect.Friday, false, false)
			sessionRequest("resume", false, "", false, true)
			sessionRequest("move", false, p.Expect.Six, false, false)
			sessionRequest("resumeAlone", true, "", false, true)
			solo := p.Sent["soloResume"]
			if !p.SleepingAlone || solo.Kind != "project.pause" || solo.Params["session"] != "evirma-billing" || solo.Params["alone"] != true || solo.Params["resume"] != true {
				t.Errorf("the sleeping session without opened sessions: %v, %+v", p.SleepingAlone, solo)
			}
			for _, name := range []string{"project", "group"} {
				r := p.Sent[name]
				if r.Kind != "project.pause" || r.Params["project"] != float64(501) || r.Params["group"] != (name == "group") || r.Params["session"] != nil || !sameMoment(r.Params["until"], p.Expect.TwoHours) {
					t.Errorf("%s scope: %+v", name, r)
				}
			}
			if !strings.Contains(p.Toast, "Pause requested") || !strings.Contains(p.Toast, "Undo") || !p.NoConfirmation {
				t.Errorf("pause confirmation or Undo: %q, %v", p.Toast, p.NoConfirmation)
			}
			if len(p.Mornings) != 3 {
				t.Fatalf("morning samples: %v", p.Mornings)
			}
			for i, delta := range []time.Duration{-8*time.Hour - 20*time.Minute, 15*time.Hour + 40*time.Minute, 15*time.Hour + 40*time.Minute} {
				// Compare against the fixture's local-clock sample, so the
				// run is independent of the machine's timezone.
				two, _ := time.Parse(time.RFC3339, p.Expect.TwoHours)
				if !sameMoment(p.Mornings[i], two.Add(delta).Format(time.RFC3339)) {
					t.Errorf("morning %d: %s", i, p.Mornings[i])
				}
			}
			if len(p.Options) != 3 || !strings.HasPrefix(p.Options[0], "Clean restart") || !strings.Contains(p.Options[0], "no first message") || !strings.HasPrefix(p.Options[1], "Restart and continue") || !strings.HasPrefix(p.Options[2], "Restart as auto-restart") {
				t.Errorf("restart choices: %v", p.Options)
			}
			if len(p.Restart) != 3 {
				t.Fatalf("restart actions: %+v", p.Restart)
			}
			for i, key := range []string{"clean", "resume", "prepare"} {
				r := p.Restart[i]
				if r.Kind != "session.restart" || r.Target != "evirma-billing" || len(r.Params) != 1 || r.Params[key] != true {
					t.Errorf("restart %s: %+v", key, r)
				}
			}
			if screen.name == "desk" {
				if !p.ProjectMenu || !p.SessionMenu {
					t.Errorf("desktop menu routes: project %v, session %v", p.ProjectMenu, p.SessionMenu)
				}
				r := p.Sent["keyboard"]
				end, err := time.Parse(time.RFC3339, p.Expect.TwoHours)
				if err != nil {
					t.Fatal(err)
				}
				sessionRequest("keyboard", true, end.Add(10*time.Minute).Format(time.RFC3339), false, false)
				if r.Params["resume"] != nil {
					t.Errorf("Enter continued instead of moving the end: %+v", r)
				}
			}
		})
	}
}

// sameMoment compares RFC 3339 ends with the offset of the person's clock.
func sameMoment(until any, want string) bool {
	text, _ := until.(string)
	got, err := time.Parse(time.RFC3339, text)
	meant, errMeant := time.Parse(time.RFC3339, want)
	return err == nil && errMeant == nil && got.Equal(meant)
}
