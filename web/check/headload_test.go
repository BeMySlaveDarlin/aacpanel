package check

import (
	"strings"
	"testing"
)

type loadShot struct {
	Before struct {
		Names  []string `json:"names"`
		Values []string `json:"values"`
		Tones  []string `json:"tones"`
		Fills  []int    `json:"fills"`
		Bars   int      `json:"bars"`
		Tips   []string `json:"tips"`
	} `json:"before"`
	Opened int `json:"opened"`
	Blank  int `json:"blank"`
}

// The status bar answers "what is this machine doing" without leaving the screen
// you are on: processor, memory and disk, in that order, on one line.
func TestTheStatusBarCarriesTheLoadOfTheHost(t *testing.T) {
	var got loadShot
	runWideFixture(t, "headload.html", &got)

	want := []string{"cpu", "mem", "disk"}
	if len(got.Before.Names) != 3 {
		t.Fatalf("gauges drawn: %v", got.Before.Names)
	}
	for i, name := range want {
		if got.Before.Names[i] != name {
			t.Errorf("gauge %d is %q, the order is cpu, mem, disk", i+1, got.Before.Names[i])
		}
	}
	if got.Before.Values[0] != "74%" || got.Before.Values[1] != "23%" {
		t.Errorf("the numbers do not match the host: %v", got.Before.Values)
	}
}

// The disk shown is the fullest one. A host runs out of one mount at a time,
// and the root disk sitting at a fifth says nothing about the one at 94%.
func TestTheDiskGaugeShowsTheFullestMount(t *testing.T) {
	var got loadShot
	runWideFixture(t, "headload.html", &got)

	if got.Before.Values[2] != "94%" {
		t.Errorf("the disk gauge reads %q while a mount of the host is at 94%%", got.Before.Values[2])
	}
	if !strings.Contains(got.Before.Tips[2], "/mnt/storage") {
		t.Errorf("nothing says which disk is meant: %q", got.Before.Tips[2])
	}
	if got.Before.Tones[2] != "crit" {
		t.Errorf("a disk at 94%% is drawn as %q", got.Before.Tones[2])
	}
	if got.Before.Tones[0] != "warn" {
		t.Errorf("a processor at 74%% is drawn as %q", got.Before.Tones[0])
	}
	// The chip is the number in another form: a fifth used looks a fifth filled.
	if got.Before.Fills[1] < 18 || got.Before.Fills[1] > 28 {
		t.Errorf("the fill does not follow the numbers: %v", got.Before.Fills)
	}
	if got.Before.Bars != 0 {
		t.Errorf("%d gauges still carry a bar of their own beside the number", got.Before.Bars)
	}
}

// The gauges lead into the machine screen, and a snapshot without a host draws
// nothing at all: three zeros would be a lie about a machine nobody asked.
func TestTheLoadLeadsIntoTheMachineAndVanishesWithoutASnapshot(t *testing.T) {
	var got loadShot
	runWideFixture(t, "headload.html", &got)

	if got.Opened == 0 {
		t.Error("the gauges are not a way into the machine screen")
	}
	if got.Blank != 0 {
		t.Errorf("a snapshot without a host still drew %d gauges", got.Blank)
	}
}

// The load stays available in the shell while its section changes.
func TestTheStatusBarCarriesTheLoadControl(t *testing.T) {
	shell := stripComments(srcFiles(t)["src/desktop/shell.js"])
	start := strings.Index(shell, `<footer class="dkstatus">`)
	if start < 0 {
		t.Fatal("the shell has no status bar")
	}
	end := strings.Index(shell[start:], "</footer>")
	if end < 0 || !strings.Contains(shell[start:start+end], "<${HeadLoad}") {
		t.Error("the status bar has no host load control")
	}
}
