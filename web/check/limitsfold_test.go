package check

import (
	"strings"
	"testing"
)

type contourTaskScene struct {
	Rings         []struct{ Vendor, Window, Label, Tone string }
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
