package check

import (
	"strings"
	"testing"
)

type contourTaskScene struct {
	Rings         []struct{ Vendor, Window, Percent, Label, Tone string }
	Actions       []string
	Projects      []string
	BeforeConfirm int
	Sent          []struct {
		Kind, Target string
		Params       struct{ Project int }
	}
	Profile                            bool
	Headings                           []string
	Unfiled                            string
	QuietBefore                        bool
	QuietAfter                         string
	ClosedToday                        string
	Week                               string
	Searched                           bool
	Task                               struct{ Body, People, Events string }
	TaskBack                           bool
	Report                             string
	ReportRows, ReportPages            int
	Yesterday                          string
	PeriodCalls                        []struct{ From, Until, Offset string }
	ScreenBack                         bool
	LongCollapsed, LongFirst, LongMore int
	Empty                              string
	EmptyBack                          bool
	StaleRings                         int
	StaleNote                          string
	FoldedBeforeConfirm                int
	FoldedProfile, FoldedTasks         bool
	FoldedSent                         []struct {
		Kind, Target string
		Params       struct{ Project int }
	}
	Full, TuckedNew, TuckedProfile, TuckedTasks limitToolbarState
	StaleFull, StaleFold                        limitToolbarState
	Folding                                     []limitToolbarState
	SingleVendor                                []struct {
		Vendor       string
		Full, Folded limitToolbarState
	}
}

type limitToolbarState struct {
	Tucked, Stuck bool
	Words         string
	Vendors       []string
	Frames        []struct {
		Vendor  string
		Windows int
	}
	Readings []struct {
		Vendor, Percent, Label, Title, Tone, Share string
		Old, Clock, Gauge, Bar                     bool
	}
	Actions []struct {
		Label, Title, Words             string
		Available, Icon, FramedByVendor bool
	}
}

func TestContourToolbarShowsLimitWindowsAndStartsMappedSessions(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	if len(got.Rings) != 3 {
		t.Fatalf("rings: %+v", got.Rings)
	}
	for i, want := range []string{"claude:1h/5h", "claude:5d/7d", "codex:5d/7d"} {
		ring := got.Rings[i]
		if ring.Vendor+":"+ring.Window != want || ring.Label == "" {
			t.Errorf("ring %d: %+v, want %s", i, ring, want)
		}
	}
	for i, want := range []string{"99%", "88%", "44%"} {
		if got.Rings[i].Percent != want {
			t.Errorf("ring %d says %q, want %q", i, got.Rings[i].Percent, want)
		}
	}
	if !strings.Contains(got.Rings[0].Label, "99.5%") || !strings.Contains(got.Rings[0].Tone, "crit") || !strings.Contains(got.Rings[2].Tone, "ok") {
		t.Errorf("ring details and tones: %+v", got.Rings)
	}
	if strings.Join(got.Actions, "|") != "Tasks|Profile|New session" {
		t.Errorf("toolbar actions: %v", got.Actions)
	}
	if got.StaleRings != 3 || !strings.Contains(got.StaleNote, "the agent is silent") {
		t.Errorf("silent collector loses its stale marks: %d, %s", got.StaleRings, got.StaleNote)
	}
	if !got.Profile || len(got.Projects) != 2 || !strings.HasPrefix(got.Projects[0], "shop") || got.Projects[1] != "api" {
		t.Errorf("mapped picker/profile: %+v", got)
	}
	if got.BeforeConfirm != 0 || len(got.Sent) != 1 || got.Sent[0].Kind != "session.open" || got.Sent[0].Target != "shop" || got.Sent[0].Params.Project != 7 {
		t.Errorf("session open bypassed the gate or lost its project: %+v", got.Sent)
	}
}

func TestTheLimitsFoldOverTheListScrolledPastThem(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	if len(got.Folding) != 5 {
		t.Fatalf("scroll stops: %+v", got.Folding)
	}
	for i, want := range []bool{false, false, true, true, false} {
		stop := got.Folding[i]
		if stop.Tucked != want {
			t.Errorf("scroll stop %d: tucked=%v, want %v", i, stop.Tucked, want)
		}
		if stop.Stuck != (i > 0 && i < 4) {
			t.Errorf("scroll stop %d: stuck=%v", i, stop.Stuck)
		}
		if len(stop.Readings) != 3 {
			t.Fatalf("scroll stop %d lost a window: %+v", i, stop)
		}
		for j, percent := range []string{"99%", "88%", "44%"} {
			reading := stop.Readings[j]
			if reading.Percent != percent || reading.Share != []string{"99.5", "88", "44"}[j] || !strings.Contains(reading.Tone, []string{"crit", "crit", "ok"}[j]) {
				t.Errorf("stop %d window %d: %+v", i, j, reading)
			}
			if reading.Bar != want || reading.Gauge == want || reading.Label == "" || reading.Title != reading.Label {
				t.Errorf("stop %d lost the reading's form or meaning: %+v", i, reading)
			}
		}
		if want && (len(stop.Vendors) != 0 || strings.Contains(strings.ToLower(stop.Words), "claude") || strings.Contains(strings.ToLower(stop.Words), "codex")) {
			t.Errorf("folded row contains vendor words: %+v", stop)
		}
	}
	for _, state := range []limitToolbarState{got.TuckedNew, got.TuckedProfile, got.TuckedTasks} {
		if !state.Tucked || len(state.Actions) != 3 {
			t.Fatalf("folded controls: %+v", state)
		}
		for i, action := range state.Actions {
			if !action.Available || !action.Icon || action.Words != "" || action.FramedByVendor || action.Label != []string{"Tasks", "Profile", "New session"}[i] || action.Title != action.Label {
				t.Errorf("folded action: %+v", action)
			}
		}
	}
	if !got.FoldedProfile || !got.FoldedTasks || got.FoldedBeforeConfirm != 0 || len(got.FoldedSent) != 1 || got.FoldedSent[0].Kind != "session.open" || got.FoldedSent[0].Target != "shop" || got.FoldedSent[0].Params.Project != 7 {
		t.Errorf("folded actions did not complete their paths: %+v", got)
	}
}

func TestLimitFramesKeepVendorsAndStaleReadingsDistinct(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	if len(got.Full.Frames) != 2 || got.Full.Frames[0].Vendor != "claude" || got.Full.Frames[0].Windows != 2 || got.Full.Frames[1].Vendor != "codex" || got.Full.Frames[1].Windows != 1 {
		t.Fatalf("full frames: %+v", got.Full.Frames)
	}
	for _, state := range []limitToolbarState{got.StaleFull, got.StaleFold} {
		if len(state.Readings) != 3 {
			t.Fatalf("stale windows: %+v", state)
		}
		for _, reading := range state.Readings {
			if !reading.Old || !reading.Clock || !strings.Contains(reading.Label, "last known numbers") {
				t.Errorf("stale reading lost its sign: %+v", reading)
			}
		}
	}
	if got.StaleFull.Tucked || !got.StaleFold.Tucked {
		t.Errorf("stale forms: full=%v folded=%v", got.StaleFull.Tucked, got.StaleFold.Tucked)
	}
	if len(got.SingleVendor) != 2 {
		t.Fatalf("single vendor cases: %+v", got.SingleVendor)
	}
	for _, single := range got.SingleVendor {
		if len(single.Full.Frames) != 1 || single.Full.Frames[0].Vendor != single.Vendor || len(single.Folded.Frames) != 0 || len(single.Folded.Vendors) != 0 {
			t.Errorf("single vendor groups: %+v", single)
		}
		if len(single.Full.Readings) != len(single.Folded.Readings) {
			t.Errorf("single vendor lost a window: %+v", single)
		}
		if single.Vendor == "claude" {
			if len(single.Full.Readings) != 2 || !strings.Contains(single.Full.Readings[0].Tone, "ok") || !strings.Contains(single.Full.Readings[1].Tone, "warn") {
				t.Errorf("threshold boundary: %+v", single)
			}
		} else if len(single.Full.Readings) != 1 || !strings.Contains(single.Full.Readings[0].Tone, "crit") || !single.Folded.Readings[0].Old || !single.Folded.Readings[0].Clock {
			t.Errorf("codex threshold or stale sign: %+v", single)
		}
	}
}

func TestContourTasksGroupWorkAndOpenDetailsAndPeriodReports(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	if strings.Join(got.Headings, "|") != "In work 1|No task 1|Quiet 1|Deferred 1|Closed 3" {
		t.Errorf("task groups: %v", got.Headings)
	}
	if !strings.Contains(got.Unfiled, "SER-1064") || !strings.Contains(got.Unfiled, "work-ser1064") || !got.QuietBefore || !strings.Contains(got.QuietAfter, "Review the quiet importer") {
		t.Errorf("unfiled/backlog: %+v", got)
	}
	for _, want := range []string{"Description", "stock", "Verify", "Read the logs"} {
		if !strings.Contains(got.Task.Body, want) {
			t.Errorf("Markdown body lost %q: %s", want, got.Task.Body)
		}
	}
	if !strings.Contains(got.Task.People, "2 conversations") || !strings.Contains(got.Task.Events, "Changed") || !strings.Contains(got.Task.Events, "work-ser1063") {
		t.Errorf("participants/events: %+v", got.Task)
	}
	if got.ReportRows != 2 || got.ReportPages != 2 || !strings.Contains(got.Report, "Changed") || !strings.Contains(got.Yesterday, "LMS-42") || len(got.PeriodCalls) != 5 || !strings.Contains(got.Week, "SER-1063") {
		t.Errorf("period report: %+v", got)
	}
	if !strings.Contains(got.ClosedToday, "Completed inventory check") {
		t.Errorf("closed period: %s", got.ClosedToday)
	}
	if !got.TaskBack || !got.ScreenBack || !got.EmptyBack {
		t.Errorf("back failed to close a layer: %+v", got)
	}
}

func TestContourTasksKeepLongBacklogsFoldedAndExplainAnEmptyRegistry(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	if got.LongCollapsed != 1 || got.LongFirst != 40 || got.LongMore != 80 || !got.Searched {
		t.Errorf("long backlog rendered all tasks or lost its next page: %d, %d, %d", got.LongCollapsed, got.LongFirst, got.LongMore)
	}
	if !strings.Contains(got.Empty, "No tasks in Acme yet") {
		t.Errorf("empty registry: %s", got.Empty)
	}
}
