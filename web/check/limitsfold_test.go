package check

import (
	"strings"
	"testing"
)

type contourTaskScene struct {
	Columns       []struct{ Vendor, Window, Percent, Label, Tone string }
	Actions       []string
	Projects      []string
	BeforeConfirm int
	Sent          []struct {
		Kind, Target string
		Params       struct{ Project int }
	}
	Profile bool
	Tabs    struct {
		Initial, Unfiled, Quiet, Deferred, OpenDeferred, Closed  taskBoardState
		Search, SearchClosed, EmptyUnfiled, SearchUnfiled, Empty taskBoardState
		TaskBack, ReportBack, Reopened, WithoutUnfiled           taskBoardState
	}
	Unfiled                 string
	QuietBefore             bool
	QuietAfter              string
	ClosedToday             string
	Week                    string
	Searched                bool
	Task                    struct{ Body, People, Events string }
	TaskBack                bool
	Report                  string
	ReportRows, ReportPages int
	Yesterday               string
	PeriodCalls             []struct{ From, Until, Offset string }
	Summary                 struct {
		FirstButton, Text, Markdown, Time, Copied, Again, Failure, Cached string
		BeforeConfirm                                                     int
		Busy, PeriodCleared                                               bool
		Posts                                                             []struct {
			Contour     int
			From, Until string
		}
	}
	ScreenBack                         bool
	LongCollapsed, LongFirst, LongMore int
	Empty                              string
	EmptyBack                          bool
	StaleColumns                       int
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
	Forecasts []struct {
		Name         string
		Full, Folded limitToolbarState
		Windows      []limitDetailState
	}
	ChipForecast []limitDetailState
	Details      struct {
		ExpectedResetAt, ExpectedResetLocal                    string
		Full, Week, Other, Folded, Stale, Codex, NoReset       limitPopoverState
		ToggledClosed, OutsideClosed, EscapeClosed, InsideKept bool
		FoldClosed, UnfoldClosed                               bool
	}
}

type taskBoardState struct {
	Tabs, Selected, Headings, Rows, Folds []string
	Unfiled, Hint                         string
	Periods, Panels                       int
	Linked                                bool
}

type limitPopoverState struct {
	Count                                   int
	Words, Label, Role, ResetAt, ResetLocal string
	Compact                                 bool
	Windows                                 []limitDetailState
	Readings                                []struct {
		Label, Tag, Type, Popup, Expanded string
		Focusable                         bool
	}
}

type limitDetailState struct {
	Label, Words, Forecast, Tone, Projection string
	Ghost                                    bool
}

type limitToolbarState struct {
	Tucked, Stuck bool
	Words         string
	Vendors       []string
	Frames        []struct {
		Vendor   string
		Accounts int
	}
	Readings []struct {
		Vendor, Percent, Label, Title, Tone, Share string
		Old, Clock, Column, Bar, Mini, Ghost       bool
		Window, Cap, Projection                    string
	}
	Actions []struct {
		Label, Title, Words             string
		Available, Icon, FramedByVendor bool
	}
}

func TestContourToolbarShowsLimitColumnsAndStartsMappedSessions(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	if len(got.Columns) != 3 {
		t.Fatalf("columns: %+v", got.Columns)
	}
	// A column names its window in its label and shows the time to its reset.
	for i, want := range []string{"claude:Claude, 5 hours:1h", "claude:Claude, 7 days:5d", "codex:Codex, 7 days:5d"} {
		column := got.Columns[i]
		parts := strings.Split(want, ":")
		if column.Vendor != parts[0] || !strings.HasPrefix(column.Label, parts[1]+",") || column.Window != parts[2] ||
			column.Percent != []string{"99%", "88%", "44%"}[i] || !strings.Contains(column.Tone, []string{"crit", "crit", "ok"}[i]) {
			t.Errorf("column %d: %+v, want %s", i, column, want)
		}
	}
	if !strings.Contains(got.Columns[0].Label, "99.5%") {
		t.Errorf("column details: %+v", got.Columns)
	}
	if strings.Join(got.Actions, "|") != "Tasks|Profile|New session" {
		t.Errorf("toolbar actions: %v", got.Actions)
	}
	if got.StaleColumns != 3 || !strings.Contains(got.StaleNote, "the agent is silent") {
		t.Errorf("silent collector loses its stale marks: %d, %s", got.StaleColumns, got.StaleNote)
	}
	if !got.Profile || len(got.Projects) != 2 || !strings.HasPrefix(got.Projects[0], "shop") || got.Projects[1] != "api" {
		t.Errorf("mapped picker/profile: %+v", got)
	}
	if got.BeforeConfirm != 0 || len(got.Sent) != 1 || got.Sent[0].Kind != "session.open" || got.Sent[0].Target != "shop" || got.Sent[0].Params.Project != 7 {
		t.Errorf("session open bypassed the gate or lost its project: %+v", got.Sent)
	}
}

func TestLimitColumnsOpenBothWindowsAndSwitchVendors(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	details := got.Details
	for i, pop := range []limitPopoverState{details.Full, details.Week, details.Other, details.Folded} {
		name := []string{"Claude limits", "Codex limits", "Claude limits", "Claude limits"}[i]
		if pop.Count != 1 || pop.Label != name || pop.Role != "dialog" {
			t.Errorf("details %d: %+v", i, pop)
		}
		for _, want := range []string{name, "resets in ", "Updated 1 min ago"} {
			if !strings.Contains(pop.Words, want) {
				t.Errorf("details %d lost %q: %s", i, want, pop.Words)
			}
		}
		if len(pop.Readings) != 3 {
			t.Fatalf("details %d lost a column: %+v", i, pop.Readings)
		}
		for _, button := range pop.Readings {
			expanded := "false"
			if strings.HasPrefix(button.Label, []string{"Claude, 5 hours", "Codex, 7 days", "Claude, 5 hours", "Claude, 5 hours"}[i]) {
				expanded = "true"
			}
			if button.Tag != "BUTTON" || button.Type != "button" || button.Popup != "dialog" || !button.Focusable || button.Expanded != expanded {
				t.Errorf("details %d control: %+v, want expanded=%s", i, button, expanded)
			}
		}
		if i == 1 {
			if len(pop.Windows) != 1 || pop.Windows[0].Label != "Codex, 7 days" || !strings.Contains(pop.Windows[0].Words, "44% used") {
				t.Errorf("codex details: %+v", pop.Windows)
			}
		} else if len(pop.Windows) != 2 || pop.Windows[0].Label != "Claude, 5 hours" || pop.Windows[1].Label != "Claude, 7 days" || !strings.Contains(pop.Windows[0].Words, "99.5% used") || !strings.Contains(pop.Windows[1].Words, "88% used") {
			t.Errorf("both Claude windows: %+v", pop.Windows)
		}
	}
	if details.Full.ResetAt != details.ExpectedResetAt || details.Full.ResetLocal == "" || !strings.Contains(details.Full.ResetLocal, details.ExpectedResetLocal) || !strings.Contains(details.Full.Words, "resets in 1 h") {
		t.Errorf("local reset and countdown: %+v", details.Full)
	}
	if strings.Contains(details.Full.Words, "last known numbers") || strings.Contains(details.Week.Words, "limit is reached") {
		t.Errorf("fresh limits gained stale or reached notices: %s; %s", details.Full.Words, details.Week.Words)
	}
	if !details.Folded.Compact {
		t.Errorf("folded column did not open its details: %+v", details.Folded)
	}
}

func TestLimitDetailsCloseOnToggleOutsideEscapeAndFolding(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	details := got.Details
	if !details.ToggledClosed || !details.OutsideClosed || !details.EscapeClosed || !details.FoldClosed || !details.UnfoldClosed || !details.InsideKept {
		t.Errorf("details dismissal: toggle=%v outside=%v escape=%v fold=%v unfold=%v inside=%v", details.ToggledClosed, details.OutsideClosed, details.EscapeClosed, details.FoldClosed, details.UnfoldClosed, details.InsideKept)
	}
}

func TestLimitDetailsShowStaleAgesReachedAndUnknownResets(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	details := got.Details
	for i, pop := range []limitPopoverState{details.Stale, details.Codex, details.NoReset} {
		for _, want := range []string{[]string{"Claude limits", "Codex limits", "Claude limits"}[i], []string{"45.5% used", "100% used", "12.3% used"}[i], []string{"Updated 2 h ago", "Updated 3 h ago", "Updated 2 h ago"}[i], "last known numbers"} {
			if pop.Count != 1 || !strings.Contains(pop.Words, want) {
				t.Errorf("stale details %d lost %q: %+v", i, want, pop)
			}
		}
	}
	if !strings.Contains(details.Codex.Words, "codex says the limit is reached") {
		t.Errorf("reached codex limit: %s", details.Codex.Words)
	}
	if details.NoReset.ResetAt != "" || details.NoReset.ResetLocal != "" || strings.Contains(details.NoReset.Words, "resets") {
		t.Errorf("unknown reset invented a time: %+v", details.NoReset)
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
			if reading.Bar || !reading.Column || reading.Mini != want || reading.Label == "" || reading.Title != reading.Label || (reading.Window == "") != want {
				t.Errorf("stop %d lost the reading's form or meaning: %+v", i, reading)
			}
		}
		if want && (strings.Join(stop.Vendors, "|") != "claude|codex" || strings.Contains(strings.ToLower(stop.Words), "claude") || strings.Contains(strings.ToLower(stop.Words), "codex")) {
			t.Errorf("folded row lost its vendor icons or contains vendor words: %+v", stop)
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
	if len(got.Full.Frames) != 2 || got.Full.Frames[0].Vendor != "claude" || got.Full.Frames[0].Accounts != 2 || got.Full.Frames[1].Vendor != "codex" || got.Full.Frames[1].Accounts != 1 {
		t.Fatalf("full frames: %+v", got.Full.Frames)
	}
	for _, state := range []limitToolbarState{got.StaleFull, got.StaleFold} {
		if len(state.Readings) != 3 {
			t.Fatalf("stale windows: %+v", state)
		}
		// One clock per vendor, on its last column, so two windows do not
		// stack two marks over each other.
		for i, reading := range state.Readings {
			if !reading.Old || reading.Clock != (i != 0) || !strings.Contains(reading.Label, "last known numbers") {
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
		if len(single.Full.Frames) != 1 || single.Full.Frames[0].Vendor != single.Vendor || len(single.Folded.Frames) != 1 || strings.Join(single.Folded.Vendors, "|") != single.Vendor {
			t.Errorf("single vendor groups: %+v", single)
		}
		if len(single.Full.Readings) != len(single.Folded.Readings) {
			t.Errorf("single vendor lost a window: %+v", single)
		}
		if single.Vendor == "claude" {
			if len(single.Full.Readings) != 2 || single.Full.Readings[0].Percent != "59%" || !strings.Contains(single.Full.Readings[0].Tone, "ok") ||
				single.Full.Readings[1].Percent != "60%" || single.Full.Readings[1].Window != "4d" || !strings.HasPrefix(single.Full.Readings[1].Label, "Claude, 7 days,") || !strings.Contains(single.Full.Readings[1].Tone, "warn") {
				t.Errorf("threshold boundary: %+v", single)
			}
		} else if len(single.Full.Readings) != 1 || !strings.Contains(single.Full.Readings[0].Tone, "crit") || !single.Folded.Readings[0].Old || !single.Folded.Readings[0].Clock {
			t.Errorf("codex threshold or stale sign: %+v", single)
		}
	}
}

func TestLimitColumnsShowEveryWindowWithItsForecast(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	if len(got.Forecasts) != 6 {
		t.Fatalf("forecast cases: %+v", got.Forecasts)
	}
	// Each case: the columns as time to reset, share, tone, cap, ghost and
	// projection.
	type col struct {
		left, pct, tone, cap string
		ghost                bool
		projection           string
	}
	cases := []struct {
		name string
		cols []col
	}{
		{"out", []col{{"3h", "42%", "crit", "▲", true, "100"}, {"5d", "88%", "ok", "", true, "92"}}},
		{"tight", []col{{"3h", "64%", "warn", "", true, "96"}, {"5d", "90%", "ok", "", true, "92"}}},
		{"tie", []col{{"3h", "70%", "ok", "", true, "80"}, {"5d", "85%", "ok", "", true, "90"}}},
		{"threshold", []col{{"3h", "59%", "ok", "", false, "59"}, {"5d", "60%", "warn", "", false, "60"}}},
		{"edge", []col{{"3h", "72%", "warn", "", true, "100"}, {"5d", "60%", "ok", "", true, "90"}}},
		{"day", []col{{"12h", "8%", "ok", "", true, "22"}}},
	}
	for i, scene := range got.Forecasts {
		want := cases[i]
		if scene.Name != want.name {
			t.Errorf("case %d: %s", i, scene.Name)
		}
		for j, state := range []limitToolbarState{scene.Full, scene.Folded} {
			if len(state.Readings) != len(want.cols) || state.Tucked != (j == 1) {
				t.Fatalf("%s columns: %+v", scene.Name, state)
			}
			for k, column := range state.Readings {
				w := want.cols[k]
				// Folded, a column keeps its share and the fill behind it alone.
				window, capMark := w.left, w.cap
				if j == 1 {
					window, capMark = "", ""
				}
				if column.Percent != w.pct || column.Window != window || !strings.Contains(column.Tone, w.tone) || !column.Column || column.Bar || column.Mini != (j == 1) {
					t.Errorf("%s state %d column %d: %+v, want %+v", scene.Name, j, k, column, w)
				}
				if column.Cap != capMark || column.Ghost != w.ghost || column.Projection != w.projection {
					t.Errorf("%s forecast marks of column %d: %+v, want %+v", scene.Name, k, column, w)
				}
			}
		}
		expected := []string{"runs out in ~2 h 15 min", "tight: ~96% at reset", "lasts until reset", "", "tight: ~100% at reset", "lasts until reset"}[i]
		if len(scene.Windows) != 2 && !(i == 5 && len(scene.Windows) == 1) {
			t.Fatalf("%s popup windows: %+v", scene.Name, scene.Windows)
		}
		for j, window := range scene.Windows {
			wantForecast := "lasts until reset"
			if j == 0 {
				wantForecast = expected
			}
			if i == 3 {
				wantForecast = ""
			}
			wantTone := "ok"
			if j == 0 && i == 0 {
				wantTone = "crit"
			}
			if (j == 0 && (i == 1 || i == 4)) || (j == 1 && i == 3) {
				wantTone = "warn"
			}
			if !strings.Contains(window.Tone, wantTone) || window.Forecast != wantForecast || window.Ghost != (i != 3) || !strings.Contains(window.Words, "% used") || !strings.Contains(window.Words, "resets in") {
				t.Errorf("%s popup window %d: %+v, want forecast %q", scene.Name, j, window, wantForecast)
			}
		}
	}
	if len(got.ChipForecast) != 2 || got.ChipForecast[0].Forecast != "runs out in ~2 h 15 min" || got.ChipForecast[1].Forecast != "lasts until reset" || !got.ChipForecast[0].Ghost || !got.ChipForecast[1].Ghost {
		t.Errorf("desktop chip forecast details: %+v", got.ChipForecast)
	}
}

func TestContourTasksGroupWorkAndOpenDetailsAndPeriodReports(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
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
	if got.ReportRows != 2 || got.ReportPages != 2 || !strings.Contains(got.Report, "Changed") || !strings.Contains(got.Yesterday, "LMS-42") || len(got.PeriodCalls) != 7 || !strings.Contains(got.Week, "SER-1063") {
		t.Errorf("period report: %+v", got)
	}
	if !strings.Contains(got.ClosedToday, "Completed inventory check") {
		t.Errorf("closed period: %s", got.ClosedToday)
	}
	if !got.TaskBack || !got.ScreenBack || !got.EmptyBack {
		t.Errorf("back failed to close a layer: %+v", got)
	}
	if got.Summary.FirstButton != "Summarize" || got.Summary.BeforeConfirm != 0 || !got.Summary.Busy || len(got.Summary.Posts) != 2 {
		t.Errorf("summary confirmation and progress: %+v", got.Summary)
	}
	if got.Summary.Markdown != "verified" || got.Summary.Cached != "verified" || !strings.Contains(got.Summary.Text, "sonnet") || got.Summary.Time != "2025-01-01T10:00:00Z" || got.Summary.Copied != "Inventory was **verified** by work-ser1063." {
		t.Errorf("summary content, metadata, copy and cache: %+v", got.Summary)
	}
	if got.Summary.Again != "Summarize again" || !strings.Contains(got.Summary.Failure, "The model is unavailable.") || !got.Summary.PeriodCleared {
		t.Errorf("summary retry, errors or period switch: %+v", got.Summary)
	}
	for _, post := range got.Summary.Posts {
		if post.Contour != 4 || post.From != got.PeriodCalls[0].From || post.Until != got.PeriodCalls[0].Until {
			t.Errorf("summary requested a different contour or period: %+v", post)
		}
	}
}

func TestContourTaskTabsShowOneSectionAndKeepItUnderSheets(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	tabs := got.Tabs
	if strings.Join(tabs.Initial.Tabs, "|") != "In work 1|No task 1|Quiet 1|Deferred 1|Closed 3" {
		t.Errorf("task tabs: %v", tabs.Initial.Tabs)
	}
	for i, board := range []taskBoardState{tabs.Initial, tabs.Unfiled, tabs.Quiet, tabs.Deferred, tabs.Closed} {
		want := []string{"In work 1", "No task 1", "Quiet 1", "Deferred 1", "Closed 3"}[i]
		if board.Panels != 1 || !board.Linked || strings.Join(board.Selected, "|") != want || strings.Join(board.Headings, "|") != want {
			t.Errorf("section %d: %+v, want only %s", i, board, want)
		}
		rows, folds, periods := 0, 0, 0
		if i == 0 {
			rows = 1
		}
		if i == 2 || i == 3 {
			folds = 1
		}
		if i == 4 {
			periods = 4
			folds = len(board.Folds)
			if folds == 0 {
				t.Error("closed tasks lost their month groups")
			}
		}
		if len(board.Rows) != rows || len(board.Folds) != folds || board.Periods != periods || (board.Unfiled != "") != (i == 1) {
			t.Errorf("section %s includes another section's content: %+v", want, board)
		}
	}
	if len(tabs.OpenDeferred.Rows) != 1 || !strings.Contains(tabs.OpenDeferred.Rows[0], "Deferred API work") {
		t.Errorf("deferred project did not unfold: %+v", tabs.OpenDeferred)
	}
	for _, board := range []taskBoardState{tabs.TaskBack, tabs.ReportBack} {
		if strings.Join(board.Selected, "|") != "Closed 3" || board.Periods != 4 {
			t.Errorf("closing a sheet reset the selected section: %+v", board)
		}
	}
	if strings.Join(tabs.Reopened.Selected, "|") != "In work 1" {
		t.Errorf("reopening the task layer kept the previous selection: %+v", tabs.Reopened)
	}
	if strings.Join(tabs.WithoutUnfiled.Tabs, "|") != "In work 1|Quiet 1|Deferred 1|Closed 3" {
		t.Errorf("a registry without unfiled work gained a No task tab: %+v", tabs.WithoutUnfiled)
	}
}

func TestContourTaskSearchCountsEverySectionAndExplainsAnEmptyTab(t *testing.T) {
	var got contourTaskScene
	runFixture(t, "limitsfold.html", &got)
	tabs := got.Tabs
	if strings.Join(tabs.Search.Tabs, "|") != "In work 1|No task 0|Quiet 0|Deferred 0|Closed 1" {
		t.Errorf("search did not update all tab counts: %+v", tabs.Search)
	}
	if strings.Join(tabs.Search.Selected, "|") != "Quiet 0" || !strings.Contains(tabs.Search.Hint, "No matches in Quiet") || len(tabs.Search.Rows) != 0 || len(tabs.Search.Folds) != 0 || tabs.Search.Periods != 0 {
		t.Errorf("an empty selected section switched tabs or lost its explanation: %+v", tabs.Search)
	}
	if strings.Join(tabs.SearchClosed.Selected, "|") != "Closed 1" || tabs.SearchClosed.Periods != 4 {
		t.Errorf("search lost matching work in another section: %+v", tabs.SearchClosed)
	}
	if strings.Join(tabs.EmptyUnfiled.Selected, "|") != "No task 0" || !strings.Contains(tabs.EmptyUnfiled.Hint, "No matches in No task") {
		t.Errorf("an empty No task tab cannot explain the search: %+v", tabs.EmptyUnfiled)
	}
	if strings.Join(tabs.SearchUnfiled.Tabs, "|") != "In work 0|No task 1|Quiet 0|Deferred 0|Closed 0" || !strings.Contains(tabs.SearchUnfiled.Unfiled, "SER-1064") {
		t.Errorf("search lost an unfiled session's name: %+v", tabs.SearchUnfiled)
	}
	if strings.Join(tabs.Empty.Tabs, "|") != "In work 0|No task 0|Quiet 0|Deferred 0|Closed 0" || strings.Join(tabs.Empty.Selected, "|") != "No task 0" || !strings.Contains(tabs.Empty.Hint, "No matches in No task") {
		t.Errorf("a search with no results replaced the selected section: %+v", tabs.Empty)
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
