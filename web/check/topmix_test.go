package check

import (
	"strings"
	"testing"
)

type topRowShown struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Sub   string `json:"sub"`
	Proc  bool   `json:"proc"`
	Bar   string `json:"bar"`
	Width string `json:"width"`
	Title string `json:"title"`
}

type topMixShown struct {
	DiskRows          []topRowShown `json:"diskRows"`
	DiskFoot          string        `json:"diskFoot"`
	AskedProcsForDisk bool          `json:"askedProcsForDisk"`
	CPURows           []topRowShown `json:"cpuRows"`
	CPUFoot           string        `json:"cpuFoot"`
	ProcTagShown      bool          `json:"procTagShown"`
	BrokenRows        []topRowShown `json:"brokenRows"`
	BrokenWarn        string        `json:"brokenWarn"`
	MarkCut           bool          `json:"markCut"`
	MarkWidth         int           `json:"markWidth"`
	NameCut           bool          `json:"nameCut"`
}

func findRow(rows []topRowShown, name string) (topRowShown, bool) {
	for _, row := range rows {
		if strings.Contains(row.Name, name) {
			return row, true
		}
	}
	return topRowShown{}, false
}

// The question a host metric answers is "who is eating the machine", and the
// answer is not only the containers: most of what runs on a host runs beside
// them. The list holds both — the containers the history recorded and the
// processes the machine has right now.
func TestTheTopOfAHostMetricHoldsProcessesBesideContainers(t *testing.T) {
	var got topMixShown
	runFixture(t, "topmix.html", &got)

	proc, ok := findRow(got.CPURows, "burn.py")
	if !ok {
		names := make([]string, 0, len(got.CPURows))
		for _, row := range got.CPURows {
			names = append(names, row.Name)
		}
		t.Fatalf("the process eating the processor is not in the list, it holds only %v", names)
	}
	if _, ok := findRow(got.CPURows, "warden-db"); !ok {
		t.Error("the containers of the history left the list when the processes joined it")
	}
	if !proc.Proc {
		t.Error("a process is not marked as one: a row measured this second reads as an hour of history")
	}
	if !got.ProcTagShown {
		t.Error("the mark of a process is in the markup but not on the screen")
	}
	if !strings.Contains(proc.Sub, "pid 4242") || !strings.Contains(proc.Sub, "aziz") {
		t.Errorf("a process row says %q: without the pid and the user there is nothing to go and look at", proc.Sub)
	}
	if !strings.Contains(proc.Title, "/usr/bin/python3") {
		t.Errorf("the full command is not kept anywhere: the row carries %q", proc.Title)
	}

	// The list is sorted by the same column for both kinds, so a process eating
	// 91.5% stands above a container whose peak was 40.2%.
	if got.CPURows[0].Name == "" || !strings.Contains(got.CPURows[0].Name, "burn.py") {
		t.Errorf("the list opens with %q: the biggest consumer is not at the top", got.CPURows[0].Name)
	}
	if proc.Value != "92%" {
		t.Errorf("the process shows %q of the processor, and the fixture gave it 91.5", proc.Value)
	}
	if !strings.Contains(proc.Bar, "proc") {
		t.Errorf("the bar of a process carries %q: the two kinds of row are drawn the same", proc.Bar)
	}

	// A mixed list has to say what its numbers are, because one row is an
	// average over an hour and the one under it is this second.
	if !strings.Contains(got.CPUFoot, "right now") {
		t.Errorf("the list explains itself as %q: nothing says the processes are not history", got.CPUFoot)
	}
}

// Disk growth is written per container only: there is nothing per process to
// show, so the screen does not go and ask the host for it.
func TestTheDiskTopStaysWithTheContainers(t *testing.T) {
	var got topMixShown
	runFixture(t, "topmix.html", &got)

	if got.AskedProcsForDisk {
		t.Error("the disk list asked the host for processes: it has no per-process figure to draw")
	}
	if len(got.DiskRows) != 1 || got.DiskRows[0].Proc {
		t.Errorf("the disk list holds %d rows and the first is a process=%v", len(got.DiskRows), got.DiskRows[0].Proc)
	}
	if got.DiskRows[0].Value != "+1.0 MB" {
		t.Errorf("the disk row shows %q: the growth over the period is what it used to show", got.DiskRows[0].Value)
	}
}

// The history and the host are two different sources. When the history is not
// answering, the processes are still there — and they are the half of the
// answer that is about right now.
func TestProcessesSurviveAHistoryThatIsNotAnswering(t *testing.T) {
	var got topMixShown
	runFixture(t, "topmix.html", &got)

	if len(got.BrokenRows) == 0 {
		t.Fatal("a fallen history took the processes with it: the card is empty on a machine that is busy")
	}
	if _, ok := findRow(got.BrokenRows, "burn.py"); !ok {
		t.Error("the processes are not in the list when the history is down")
	}
	// A command is long and a phone is narrow: what gives way is the name, not
	// the mark that says which kind of row this is.
	if got.MarkCut || got.MarkWidth < 40 {
		t.Errorf("the mark of a process is cut down to %d px beside a long command: the row eats its own label", got.MarkWidth)
	}
	if !got.NameCut {
		t.Error("the long command in the fixture is not being cut at all: the check no longer covers a row too narrow for its name")
	}
	if got.BrokenWarn == "" {
		t.Error("nothing says the history is missing: the list looks complete while half of it is gone")
	}
	for _, row := range got.BrokenRows {
		if !row.Proc {
			t.Errorf("a row that is not a process (%q) came from a history that answered 503", row.Name)
		}
	}
}
