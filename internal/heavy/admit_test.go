package heavy

import (
	"slices"
	"strings"
	"testing"
)

const gib = uint64(1) << 30

// calm is a machine at rest with room for a few runs: 40G available, 4G
// held in the slice of 20G, no pressure, nothing going.
func calm() Measure { return Measure{Available: 40 * gib, Slice: 4 * gib} }

// roomy are limits that keep nothing back, so a case says only the one it
// is about.
func roomy() Limits { return Limits{Total: 62 * gib} }

func TestTheQueueAdmitsInOrder(t *testing.T) {
	for _, c := range []struct {
		what  string
		l     Limits
		m     Measure
		asks  []uint64
		goes  []int
		waits string
	}{
		{"an empty queue", roomy(), calm(), nil, nil, ""},
		{"everyone fits", roomy(), calm(), []uint64{8 * gib, 8 * gib}, []int{0, 1}, ""},
		{"a small run behind a big one that does not fit waits for it",
			Limits{Reserve: 12 * gib, Total: 62 * gib}, calm(), []uint64{30 * gib, 1 * gib}, nil, "memory"},
		{"the runs behind a head that went are weighed against it",
			Limits{Reserve: 12 * gib, Total: 62 * gib}, calm(), []uint64{8 * gib, 8 * gib, 8 * gib, 8 * gib},
			[]int{0, 1, 2}, "memory"},
	} {
		goes, waits := Admit(c.l, c.m, c.asks)
		if !slices.Equal(goes, c.goes) {
			t.Errorf("%s: %v go, want %v", c.what, goes, c.goes)
		}
		if c.waits == "" && waits != "" || !strings.Contains(waits, c.waits) {
			t.Errorf("%s: the rest wait for %q, want %q", c.what, waits, c.waits)
		}
	}
}

func TestARunWaitsForWhatTheMachineLacks(t *testing.T) {
	going := func(runs ...Running) Measure {
		m := calm()
		m.Runs = runs
		return m
	}
	for _, c := range []struct {
		what  string
		l     Limits
		m     Measure
		ask   uint64
		waits string
	}{
		{"a free slot", Limits{Slots: 3}, going(Running{}, Running{}), 8 * gib, ""},
		{"every slot taken", Limits{Slots: 3}, going(Running{}, Running{}, Running{}), 8 * gib, "a slot: 3 of 3"},
		{"no bound on the slots", Limits{}, going(Running{}, Running{}, Running{}), 8 * gib, ""},

		{"the reserve kept", Limits{Reserve: 12 * gib}, Measure{Available: 20 * gib}, 8 * gib, ""},
		{"the reserve broken into", Limits{Reserve: 12 * gib}, Measure{Available: 20 * gib}, 9 * gib, "memory"},

		{"a run going that holds what it asked for",
			Limits{Reserve: 12 * gib}, Measure{Available: 25 * gib, Runs: []Running{{Ask: 8 * gib, Taken: 8 * gib}}},
			8 * gib, ""},
		{"a run going that is still to grow",
			Limits{Reserve: 12 * gib}, Measure{Available: 25 * gib, Runs: []Running{{Ask: 8 * gib, Taken: 1 * gib}}},
			8 * gib, "7G still to come"},

		{"the agents' budget kept", Limits{Budget: 20 * gib}, Measure{Available: 40 * gib, Slice: 6 * gib,
			Runs: []Running{{Ask: 8 * gib, Taken: 2 * gib}}}, 8 * gib, ""},
		{"the agents' budget passed", Limits{Budget: 20 * gib}, Measure{Available: 40 * gib, Slice: 6 * gib,
			Runs: []Running{{Ask: 8 * gib, Taken: 1 * gib}}}, 8 * gib, "the agents' budget"},

		{"memory pressure", Limits{}, Measure{Available: 40 * gib, MemoryFull: memoryPressureMax}, gib, "pressure"},
		{"io pressure", Limits{}, Measure{Available: 40 * gib, IOFull: ioPressureMax + 1}, gib, "pressure"},
		{"pressure of a machine at work", Limits{}, Measure{Available: 40 * gib, MemoryFull: 0.5, IOFull: 12}, gib, ""},
	} {
		goes, waits := Admit(c.l, c.m, []uint64{c.ask})
		if c.waits == "" {
			if len(goes) != 1 || waits != "" {
				t.Errorf("%s: the run does not go, it waits for %q", c.what, waits)
			}
			continue
		}
		if len(goes) != 0 || !strings.Contains(waits, c.waits) {
			t.Errorf("%s: %v go, the rest wait for %q; want it to wait for %q", c.what, goes, waits, c.waits)
		}
	}
}

// A head that waits names everything it waits for, so a person does not
// free one thing to find the next.
func TestTheHeadNamesEverythingItWaitsFor(t *testing.T) {
	m := Measure{Available: 10 * gib, Slice: 19 * gib, IOFull: 50, Runs: []Running{{}}}
	_, waits := Admit(Limits{Slots: 1, Reserve: 12 * gib, Budget: 20 * gib}, m, []uint64{4 * gib})
	for _, want := range []string{"a slot", "memory", "the agents' budget", "pressure"} {
		if !strings.Contains(waits, want) {
			t.Errorf("the head waits for %q, which does not name %q", waits, want)
		}
	}
}

func TestARunThatCouldNeverGoIsRefused(t *testing.T) {
	l := Limits{Reserve: 12 * gib, Budget: 20 * gib, Total: 62 * gib}
	for _, c := range []struct {
		what string
		l    Limits
		ask  uint64
		why  string
	}{
		{"within the budget", l, 20 * gib, ""},
		{"past the budget", l, 21 * gib, "more than the 20G every agent shares"},
		{"past the machine less its reserve", Limits{Reserve: 12 * gib, Total: 62 * gib}, 51 * gib,
			"more than the machine's 62G less the 12G"},
		{"the whole of the machine less its reserve", Limits{Reserve: 12 * gib, Total: 62 * gib}, 50 * gib, ""},
		{"a machine not known", Limits{}, 1000 * gib, ""},
	} {
		if got := Never(c.l, c.ask); c.why == "" && got != "" || !strings.Contains(got, c.why) {
			t.Errorf("%s: %q, want %q", c.what, got, c.why)
		}
	}
}

func TestSizesReadAsAPersonWritesThem(t *testing.T) {
	for n, want := range map[uint64]string{8 * gib: "8G", 6*gib + gib/2: "6.5G", 768 << 20: "768M", 0: "0M"} {
		if got := Size(n); got != want {
			t.Errorf("%d bytes read as %q, want %q", n, got, want)
		}
	}
}
