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
		Rows   int      `json:"rows"`
		Tips   []string `json:"tips"`
	} `json:"before"`
	Opened int `json:"opened"`
	Blank  int `json:"blank"`
}

// The top bar answers "what is this machine doing" without leaving the screen
// you are on: processor, memory and disk, in that order, on one line.
func TestTheTopBarCarriesTheLoadOfTheHost(t *testing.T) {
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
	if got.Before.Rows != 1 {
		t.Errorf("the gauges are on %d lines: the top bar is one line high", got.Before.Rows)
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
	// The bar is the number in another form: a fifth full looks a fifth full.
	if got.Before.Fills[1] < 18 || got.Before.Fills[1] > 28 {
		t.Errorf("the bars do not follow the numbers: %v", got.Before.Fills)
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
