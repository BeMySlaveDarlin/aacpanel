package executor

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A cgroup past its MemoryHigh is slowed and its pages are reclaimed; it is
// never killed there. With swap to push into, that is a brake. With none left,
// every page it takes waits, the cgroup creeps upward by a megabyte or so a
// second and may never reach its MemoryMax, where the kernel would kill its
// biggest process: everything in it stands, claude included. The watcher
// reads the agents' slice and every unit directly in it that is held to a
// High of its own, lifts the High of one that stands stuck at it, so that
// MemoryMax decides, and sets the High back once the cgroup is well under it
// again.
const (
	// stallLook is how often the watcher reads the memory of the slice.
	stallLook = 5 * time.Second
	// stallReads is how many reads in a row find a cgroup stuck before its
	// High is lifted: fifteen seconds and more of a stall on top of the ten
	// the pressure is averaged over, so a stuck cgroup is let go some half a
	// minute after it reaches its High. A build that touches the High for a
	// moment and goes on is left alone.
	stallReads = 4
	// stallPressure is the share of the last ten seconds, in per cent, in
	// which every task of the cgroup waited on memory at once: full avg10 of
	// memory.pressure. A cgroup with no swap left to push into passes 50
	// within ten to fifteen seconds of reaching its High and stays between 80
	// and 98 after it; one that still pushes its pages out to swap stays
	// under one.
	stallPressure = 50.0
	// stallNearShare and stallNearMin say how close under its High a cgroup
	// counts as at it: a hundredth of the High, and no less than 32 MiB. A
	// stuck cgroup stands above its High; one the brake holds stands just
	// under it, and its pressure tells the two apart.
	stallNearShare = 100
	stallNearMin   = 32 << 20
	// stallBackTenths is how far under the High it was held to a lifted
	// cgroup must fall, in tenths, to get it back. One that lost its biggest
	// process at the Max falls far under at once; one that fitted stays above
	// and keeps the High lifted until it lets the memory go or ends. The
	// tenth spared keeps the High from coming back at the very edge, where
	// the cgroup would stand stuck again at once.
	stallBackTenths = 9
)

// noHigh is the High of a cgroup that has none.
const noHigh = math.MaxUint64

// cgroupRoot is where the kernel shows the cgroups.
var cgroupRoot = "/sys/fs/cgroup"

// cgroupMemory is what the kernel tells of the memory of one cgroup: the bytes
// it holds, its High, and full avg10 of its memory pressure.
type cgroupMemory struct {
	unit    string
	current uint64
	high    uint64
	full    float64
}

// stuck says whether the cgroup stands at its High and waits on memory most
// of the time.
func (m cgroupMemory) stuck() bool {
	if m.high == noHigh {
		return false
	}
	near := max(m.high/stallNearShare, stallNearMin)
	return m.current+near >= m.high && m.full >= stallPressure
}

// stallWatch remembers the cgroups it found stuck and those whose High it
// lifted. It knows only what it did itself: a High lifted before the
// executor started is not set back.
type stallWatch struct {
	sliceHigh string
	cgroups   map[string]*stall
}

type stall struct {
	since  time.Time // the first read of the stall
	reads  int       // reads in a row that found the cgroup stuck
	lifted uint64    // the High the cgroup was held to before it was lifted; 0 while it is held
	back   string    // the value MemoryHigh is set back to
	failed string    // the failure last logged, so one that lasts is logged once
}

// newStallWatch watches the slice the description holds to sliceHigh, as
// systemd takes it; empty is no High.
func newStallWatch(sliceHigh string) *stallWatch {
	return &stallWatch{sliceHigh: sliceHigh, cgroups: map[string]*stall{}}
}

// look takes one read of the slice and its units. A cgroup not in it has
// ended and is forgotten.
func (w *stallWatch) look(ctx context.Context, now time.Time, read []cgroupMemory) {
	present := make(map[string]bool, len(read))
	for _, m := range read {
		present[m.unit] = true
		w.lookAt(ctx, now, m)
	}
	for unit, s := range w.cgroups {
		if present[unit] {
			continue
		}
		if s.lifted != 0 {
			log.Printf("memory: %s ended with its MemoryHigh lifted; nothing to set back", unit)
		}
		delete(w.cgroups, unit)
	}
}

func (w *stallWatch) lookAt(ctx context.Context, now time.Time, m cgroupMemory) {
	s := w.cgroups[m.unit]
	if s != nil && s.lifted != 0 {
		if m.current >= s.lifted/10*stallBackTenths {
			return
		}
		if err := setHigh(ctx, m.unit, s.back); err != nil {
			s.fail(fmt.Sprintf("memory: MemoryHigh=%s of %s was not set back: %v", s.back, m.unit, err))
			return
		}
		log.Printf("memory: %s is down to %s, under nine tenths of the MemoryHigh it was held to; "+
			"MemoryHigh=%s is set back after %s lifted", m.unit, size(int(m.current)), s.back, now.Sub(s.since).Round(time.Second))
		delete(w.cgroups, m.unit)
		return
	}
	if !m.stuck() {
		delete(w.cgroups, m.unit)
		return
	}
	if s == nil {
		s = &stall{since: now}
		w.cgroups[m.unit] = s
	}
	s.reads++
	if s.reads < stallReads {
		return
	}
	if err := setHigh(ctx, m.unit, "infinity"); err != nil {
		s.fail(fmt.Sprintf("memory: MemoryHigh of %s was not lifted: %v", m.unit, err))
		return
	}
	s.lifted, s.back, s.failed = m.high, w.backTo(m), ""
	log.Printf("memory: %s stood at its MemoryHigh of %s for %s, holding %s with full memory pressure %.0f%%; "+
		"MemoryHigh is lifted, MemoryMax decides", m.unit, size(int(m.high)), now.Sub(s.since).Round(time.Second),
		size(int(m.current)), m.full)
	s.since = now
}

// backTo is what the High of a cgroup is set back to: the slice's from the
// description, and a unit's as it was read before it was lifted.
func (w *stallWatch) backTo(m cgroupMemory) string {
	if m.unit != AgentsSlice {
		return strconv.FormatUint(m.high, 10)
	}
	if w.sliceHigh == "" {
		return "infinity"
	}
	return w.sliceHigh
}

func (s *stall) fail(line string) {
	if line != s.failed {
		log.Print(line)
	}
	s.failed = line
}

// setHigh sets the MemoryHigh of a unit for the run of the manager, as the
// slice is set up: the cgroup takes it at once.
func setHigh(ctx context.Context, unit, value string) error {
	ctx, cancel := context.WithTimeout(ctx, sliceWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlPath(), "--user", "--runtime", "set-property", unit,
		"MemoryHigh="+value).CombinedOutput()
	if err != nil {
		if said := strings.TrimSpace(string(out)); said != "" {
			return fmt.Errorf("%w: %s", err, shortenLines(said))
		}
		return err
	}
	return nil
}

// WatchStalls reads the memory of the agents' slice every stallLook until ctx
// ends, and lifts the High of a cgroup in it that stands stuck at it until it
// is well under it again. sliceHigh is the slice's High the description gives.
// The slice's directory is asked of the manager once it has one: a slice with
// no unit in it yet has none, and there is nothing to watch.
func WatchStalls(ctx context.Context, sliceHigh string) {
	w := newStallWatch(sliceHigh)
	tick := time.NewTicker(stallLook)
	defer tick.Stop()
	dir, said := "", ""
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if dir == "" {
				var err error
				if dir, err = sliceDir(ctx); err != nil {
					if err.Error() != said {
						log.Printf("memory: the slice is not watched for now: %v", err)
					}
					said = err.Error()
					continue
				}
				said = ""
			}
			if dir != "" {
				w.look(ctx, now, agentsMemory(dir))
			}
		}
	}
}

// sliceDir is the directory of the agents' slice under cgroupRoot, as the
// user's manager names it, or empty while the slice has none.
func sliceDir(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, sliceWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlPath(), "--user", "show", "--property=ControlGroup", "--value",
		AgentsSlice).Output()
	if err != nil {
		return "", fmt.Errorf("systemctl --user show %s: %w", AgentsSlice, err)
	}
	group := strings.TrimSpace(string(out))
	if group == "" {
		return "", nil
	}
	return filepath.Join(cgroupRoot, group), nil
}

// agentsMemory reads the slice in dir and every unit directly in it. A unit
// that ends between the reads is left out, and so is everything when the
// slice has no directory.
func agentsMemory(dir string) []cgroupMemory {
	var read []cgroupMemory
	if m, err := readCgroupMemory(dir, AgentsSlice); err == nil {
		read = append(read, m)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if m, err := readCgroupMemory(filepath.Join(dir, e.Name()), e.Name()); err == nil {
			read = append(read, m)
		}
	}
	return read
}

func readCgroupMemory(dir, unit string) (cgroupMemory, error) {
	m := cgroupMemory{unit: unit}
	current, err := readCgroupNumber(filepath.Join(dir, "memory.current"))
	if err != nil {
		return m, err
	}
	high, err := readCgroupNumber(filepath.Join(dir, "memory.high"))
	if err != nil {
		return m, err
	}
	full, err := fullPressure(filepath.Join(dir, "memory.pressure"))
	if err != nil {
		return m, err
	}
	m.current, m.high, m.full = current, high, full
	return m, nil
}

// readCgroupNumber reads a file of one number of bytes, where max is noHigh.
func readCgroupNumber(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v := strings.TrimSpace(string(raw))
	if v == "max" {
		return noHigh, nil
	}
	return strconv.ParseUint(v, 10, 64)
}

// fullPressure is avg10 of the full line of a pressure file: the share of the
// last ten seconds in which every task of the cgroup waited at once.
func fullPressure(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) < 2 || fields[0] != "full" {
			continue
		}
		if v, ok := strings.CutPrefix(fields[1], "avg10="); ok {
			return strconv.ParseFloat(v, 64)
		}
	}
	if err := lines.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("%s has no full avg10", path)
}
