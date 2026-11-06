package heavy

import (
	"fmt"
	"strings"
)

// The pressure on the machine past which the queue admits nothing: full
// avg10 of /proc/pressure/memory and /proc/pressure/io, the share of the last
// ten seconds in which every task that was not idle waited on memory or on
// the disk at once. At rest both stand at nought; a full check of the panel's
// repository beside the sessions working on the machine keeps memory at
// nought and io under 15, a few per cent most of the time. The lines are far
// above what work that goes on makes, and under the tens a machine that
// thrashes in its swap or chokes on its disk shows, where a run admitted
// would only add to it.
const (
	memoryPressureMax = 10.0
	ioPressureMax     = 30.0
)

// Limits are what the queue admits by. A count of nought and a size of
// nought are none.
type Limits struct {
	// Slots bounds the runs going at once.
	Slots int
	// Reserve is the memory the machine keeps available past every run.
	Reserve uint64
	// Budget is the MemoryHigh of the agents' slice, which every run lives in.
	Budget uint64
	// Total is the memory of the machine.
	Total uint64
	// Ask is what a run that names nothing asks for.
	Ask uint64
	// CPUs is how many cores a run may take.
	CPUs int
}

// Measure is one read of the host.
type Measure struct {
	// Available is MemAvailable of the machine.
	Available uint64
	// Slice is what the agents' slice holds besides the cache of files,
	// which the kernel gives up to a run that grows.
	Slice uint64
	// MemoryFull and IOFull are full avg10 of the machine's pressure.
	MemoryFull float64
	IOFull     float64
	// Runs are the runs going in the slice.
	Runs []Running
}

// Running is one run going: its scope, its ceiling and what it holds now
// besides the cache of files.
type Running struct {
	Unit  string
	Ask   uint64
	Taken uint64
}

// toCome is what a run is still to grow by before it reaches what it asked
// for. A run admitted a moment ago holds nothing yet; counted by what it
// holds, three runs admitted at once would each find the memory the others
// are about to take still free.
func (r Running) toCome() uint64 {
	if r.Taken >= r.Ask {
		return 0
	}
	return r.Ask - r.Taken
}

// Admit says which runs of the queue go now, by their places in it, and what
// keeps the first of the rest waiting. The queue is strictly in order: once
// the head does not fit, the runs behind it wait too, even one small enough
// to fit, or a stream of small runs would keep a big one waiting for ever.
// A run that goes is counted at once as a run going that holds nothing yet,
// so the one after it is weighed against it.
func Admit(l Limits, m Measure, asks []uint64) (goes []int, waits string) {
	runs := append([]Running(nil), m.Runs...)
	for i, ask := range asks {
		if why := blocked(l, m, runs, ask); why != "" {
			return goes, why
		}
		goes = append(goes, i)
		runs = append(runs, Running{Ask: ask})
	}
	return goes, ""
}

// blocked is what keeps a run asking for ask from starting beside runs, or
// empty when nothing does.
func blocked(l Limits, m Measure, runs []Running, ask uint64) string {
	var toCome uint64
	for _, r := range runs {
		toCome += r.toCome()
	}
	var why []string
	if l.Slots > 0 && len(runs) >= l.Slots {
		why = append(why, fmt.Sprintf("a slot: %d of %d runs are going", len(runs), l.Slots))
	}
	if int64(m.Available)-int64(toCome)-int64(ask) < int64(l.Reserve) {
		why = append(why, fmt.Sprintf("memory: %s available, %s still to come to the runs going, %s asked, %s kept free",
			Size(m.Available), Size(toCome), Size(ask), Size(l.Reserve)))
	}
	if l.Budget > 0 && m.Slice+toCome+ask > l.Budget {
		why = append(why, fmt.Sprintf("the agents' budget: %s held in their slice, %s still to come to the runs going, "+
			"%s asked, of %s", Size(m.Slice), Size(toCome), Size(ask), Size(l.Budget)))
	}
	if m.MemoryFull >= memoryPressureMax || m.IOFull >= ioPressureMax {
		why = append(why, fmt.Sprintf("the pressure to fall: full avg10 of memory %.1f%%, of io %.1f%%, "+
			"a run starts under %.0f%% and %.0f%%", m.MemoryFull, m.IOFull, memoryPressureMax, ioPressureMax))
	}
	return strings.Join(why, "; ")
}

// Never says why a run asking for ask would wait for ever, or empty when it
// can be admitted once the runs before it end.
func Never(l Limits, ask uint64) string {
	if l.Budget > 0 && ask > l.Budget {
		return fmt.Sprintf("it asks for %s, more than the %s every agent shares", Size(ask), Size(l.Budget))
	}
	if l.Total > 0 && int64(ask) > int64(l.Total)-int64(l.Reserve) {
		return fmt.Sprintf("it asks for %s, more than the machine's %s less the %s it keeps free",
			Size(ask), Size(l.Total), Size(l.Reserve))
	}
	return ""
}

// Size is bytes as a person reads them: gigabytes to a tenth, megabytes
// below one.
func Size(n uint64) string {
	const mib, gib = 1 << 20, 1 << 30
	if n >= gib {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/gib), ".0") + "G"
	}
	return fmt.Sprintf("%dM", (n+mib/2)/mib)
}
