package executor

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testMiB = uint64(1) << 20
	testGiB = uint64(1) << 30
)

// stallScene is a slice of the agents laid out in a temporary directory as
// the kernel shows it, with systemctl standing in to write down its calls.
type stallScene struct {
	t     *testing.T
	slice string
	calls string
	w     *stallWatch
	now   time.Time
	log   *bytes.Buffer
}

func newStallScene(t *testing.T, answer string) *stallScene {
	t.Helper()
	journal := &bytes.Buffer{}
	before := log.Writer()
	log.SetOutput(journal)
	t.Cleanup(func() { log.SetOutput(before) })
	s := &stallScene{t: t, slice: t.TempDir(), w: newStallWatch("20G"), now: time.Unix(1_800_000_000, 0), log: journal}
	s.answer(answer)
	s.set("", 4*testGiB, 20*testGiB, 0)
	return s
}

// answer has systemctl answer every call with the shell given, and write the
// call down on a line of its own.
func (s *stallScene) answer(answer string) {
	dir := s.t.TempDir()
	s.calls = filepath.Join(dir, "calls")
	script := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$*\" >> "+s.calls+"\n"+answer+"\n"), 0o755); err != nil {
		s.t.Fatal(err)
	}
	s.t.Setenv(systemctlEnv, script)
}

// set writes the memory of a unit of the slice, or of the slice itself for
// no unit; a high of noHigh is max.
func (s *stallScene) set(unit string, current, high uint64, full float64) {
	s.t.Helper()
	dir := filepath.Join(s.slice, unit)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.t.Fatal(err)
	}
	h := "max"
	if high != noHigh {
		h = strconv.FormatUint(high, 10)
	}
	files := map[string]string{
		"memory.current": strconv.FormatUint(current, 10) + "\n",
		"memory.high":    h + "\n",
		"memory.pressure": "some avg10=" + fmt.Sprintf("%.2f", full) + " avg60=0.00 avg300=0.00 total=1\n" +
			"full avg10=" + fmt.Sprintf("%.2f", full) + " avg60=0.00 avg300=0.00 total=1\n",
		"cgroup.procs": "1\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			s.t.Fatal(err)
		}
	}
}

// look has the watcher read the slice once, a read after the one before.
func (s *stallScene) look(times int) {
	for range times {
		s.w.look(context.Background(), s.now, agentsMemory(s.slice))
		s.now = s.now.Add(stallLook)
	}
}

// taken are the calls of systemctl since the last time they were taken.
func (s *stallScene) taken() []string {
	s.t.Helper()
	raw, err := os.ReadFile(s.calls)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		s.t.Fatal(err)
	}
	if err := os.Remove(s.calls); err != nil {
		s.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func lifting(unit string) string {
	return "--user --runtime set-property " + unit + " MemoryHigh=infinity"
}

// A unit that stands at its High with every task waiting on memory is let go
// of its High once it has done so for stallReads reads in a row; a read in
// between that finds it moving starts the count again.
func TestAUnitStuckAtItsHighIsLiftedAfterReadsInARow(t *testing.T) {
	s := newStallScene(t, "exit 0")
	stuck := func() { s.set("run-a.service", 300*testMiB, 256*testMiB, 96) }
	stuck()
	s.look(stallReads - 1)
	s.set("run-a.service", 300*testMiB, 256*testMiB, 20)
	s.look(1)
	stuck()
	s.look(stallReads - 1)
	if got := s.taken(); got != nil {
		t.Fatalf("a unit stuck for fewer than %d reads in a row is touched: %q", stallReads, got)
	}
	s.look(1)
	if got := s.taken(); !slices.Equal(got, []string{lifting("run-a.service")}) {
		t.Fatalf("a unit stuck for %d reads gets %q", stallReads, got)
	}
	if line := s.log.String(); !strings.Contains(line, "run-a.service stood at its MemoryHigh of 256.0 MB for 15s, "+
		"holding 300.0 MB with full memory pressure 96%") {
		t.Errorf("the lift is logged as %q", line)
	}
	s.set("run-a.service", 400*testMiB, noHigh, 0)
	s.look(stallReads)
	if got := s.taken(); got != nil {
		t.Errorf("a unit growing with its High lifted gets %q", got)
	}
}

// A cgroup is stuck only at its High and only when it waits on memory: high
// pressure far under the High, or a cgroup at the High that the brake slows
// while it still runs, is left alone, and so is a unit with no High of its own.
func TestOnlyACgroupAtItsHighThatWaitsIsStuck(t *testing.T) {
	cases := []struct {
		what          string
		current, high uint64
		full          float64
		stuck         bool
	}{
		{"pressure far under the High", 100 * testMiB, 256 * testMiB, 95, false},
		{"just over the margin under the High", 223 * testMiB, 256 * testMiB, 95, false},
		{"within 32 MiB under the High", 225 * testMiB, 256 * testMiB, 95, true},
		{"within a hundredth under a big High", 6*testGiB - 60*testMiB, 6 * testGiB, 95, true},
		{"over the High, the brake still lets it run", 300 * testMiB, 256 * testMiB, 30, false},
		{"over the High at the pressure that counts", 300 * testMiB, 256 * testMiB, stallPressure, true},
		{"no High of its own", 9 * testGiB, noHigh, 99, false},
	}
	for _, c := range cases {
		s := newStallScene(t, "exit 0")
		s.set("run-a.service", c.current, c.high, c.full)
		s.look(2 * stallReads)
		if got := s.taken(); (got != nil) != c.stuck {
			t.Errorf("%s: systemctl is called with %q", c.what, got)
		}
	}
}

// A unit whose High was lifted gets it back, as it was read before the lift,
// once it is under nine tenths of it; above that it stays lifted.
func TestALiftedUnitGetsItsHighBackUnderNineTenths(t *testing.T) {
	s := newStallScene(t, "exit 0")
	s.set("run-a.service", 300*testMiB, 256*testMiB, 96)
	s.look(stallReads)
	s.taken()
	s.set("run-a.service", 240*testMiB, noHigh, 0)
	s.look(2)
	if got := s.taken(); got != nil {
		t.Fatalf("a unit above nine tenths of its High gets %q", got)
	}
	s.set("run-a.service", 200*testMiB, noHigh, 0)
	s.look(1)
	want := "--user --runtime set-property run-a.service MemoryHigh=" + strconv.FormatUint(256*testMiB, 10)
	if got := s.taken(); !slices.Equal(got, []string{want}) {
		t.Fatalf("a unit under nine tenths of its High gets %q, want %q", got, want)
	}
	if line := s.log.String(); !strings.Contains(line, "run-a.service is down to 200.0 MB") ||
		!strings.Contains(line, "after 15s lifted") {
		t.Errorf("the High set back is logged as %q", line)
	}
	s.set("run-a.service", 200*testMiB, 256*testMiB, 0)
	s.look(stallReads)
	if got := s.taken(); got != nil {
		t.Errorf("a unit held to its High again gets %q", got)
	}
}

// The slice itself is lifted as a unit is, and gets back the High the
// description gives it, not a number read off the disk.
func TestTheSliceGetsTheHighOfTheDescriptionBack(t *testing.T) {
	s := newStallScene(t, "exit 0")
	s.set("", 20*testGiB+300*testMiB, 20*testGiB, 70)
	s.set("run-a.service", 5*testGiB, 6*testGiB, 70)
	s.look(stallReads)
	if got := s.taken(); !slices.Equal(got, []string{lifting(AgentsSlice)}) {
		t.Fatalf("a stuck slice gets %q", got)
	}
	s.set("", 17*testGiB, noHigh, 0)
	s.look(1)
	if got := s.taken(); !slices.Equal(got, []string{"--user --runtime set-property " + AgentsSlice + " MemoryHigh=20G"}) {
		t.Errorf("the slice is set back with %q", got)
	}
}

// A unit that ends is forgotten: one lifted is said to have ended so, and one
// that was being counted starts the count again if its name comes back.
func TestAUnitThatEndsIsForgotten(t *testing.T) {
	s := newStallScene(t, "exit 0")
	s.set("run-a.service", 300*testMiB, 256*testMiB, 96)
	s.look(stallReads)
	s.set("run-b.service", 300*testMiB, 256*testMiB, 96)
	s.look(stallReads - 1)
	s.taken()
	for _, unit := range []string{"run-a.service", "run-b.service"} {
		if err := os.RemoveAll(filepath.Join(s.slice, unit)); err != nil {
			t.Fatal(err)
		}
	}
	s.look(1)
	if len(s.w.cgroups) != 0 {
		t.Errorf("the watcher still remembers %v", s.w.cgroups)
	}
	if line := s.log.String(); !strings.Contains(line, "run-a.service ended with its MemoryHigh lifted") ||
		strings.Contains(line, "run-b.service ended") {
		t.Errorf("the end is logged as %q", line)
	}
	s.set("run-b.service", 300*testMiB, 256*testMiB, 96)
	s.look(1)
	if got := s.taken(); got != nil {
		t.Errorf("a unit come back under the same name is lifted at once: %q", got)
	}
}

// A refused set-property is tried again on the next read, and logged once
// for as long as it is refused the same way.
func TestARefusalIsLoggedOnce(t *testing.T) {
	s := newStallScene(t, "echo 'Failed to set unit properties on run-a.service: Access denied' >&2; exit 1")
	s.set("run-a.service", 300*testMiB, 256*testMiB, 96)
	s.look(stallReads + 5)
	if got := s.taken(); len(got) != 6 {
		t.Errorf("a refused lift is tried %d times, want once on each of the last 6 reads: %q", len(got), got)
	}
	if n := strings.Count(s.log.String(), "MemoryHigh of run-a.service was not lifted"); n != 1 {
		t.Errorf("the refusal is logged %d times: %q", n, s.log.String())
	}
	if !strings.Contains(s.log.String(), "Access denied") {
		t.Errorf("the refusal does not say why: %q", s.log.String())
	}
	s.answer("exit 0")
	s.look(1)
	if !strings.Contains(s.log.String(), "run-a.service stood at its MemoryHigh") {
		t.Errorf("the lift that went through is not logged: %q", s.log.String())
	}
}

// The watcher keeps quiet over a slice with no directory yet, a unit that
// ended between the reads and a file that is not a cgroup.
func TestAnEmptyOrHalfGoneSliceIsReadQuietly(t *testing.T) {
	if got := agentsMemory(filepath.Join(t.TempDir(), "absent")); got != nil {
		t.Errorf("a slice with no directory reads as %v", got)
	}
	s := newStallScene(t, "exit 0")
	s.set("run-a.service", 300*testMiB, 256*testMiB, 96)
	if err := os.Remove(filepath.Join(s.slice, "run-a.service", "memory.pressure")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.slice, "run-b.service"), 0o755); err != nil {
		t.Fatal(err)
	}
	read := agentsMemory(s.slice)
	if len(read) != 1 || read[0].unit != AgentsSlice || read[0].high != 20*testGiB || read[0].current != 4*testGiB {
		t.Errorf("the slice reads as %+v", read)
	}
	s.look(stallReads)
	if got := s.taken(); got != nil || s.log.Len() != 0 {
		t.Errorf("a half-gone unit gets %q and logs %q", got, s.log.String())
	}
}

// The directory of the slice is the one the user's manager names, and none
// while the slice has no cgroup.
func TestTheSliceDirIsAskedOfTheManager(t *testing.T) {
	root := t.TempDir()
	before := cgroupRoot
	cgroupRoot = root
	t.Cleanup(func() { cgroupRoot = before })
	s := newStallScene(t, "echo /user.slice/user-1001.slice/user@1001.service/aacp.slice/aacp-agents.slice")
	dir, err := sliceDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "user.slice/user-1001.slice/user@1001.service/aacp.slice/aacp-agents.slice"); dir != want {
		t.Errorf("the slice is looked for in %q, want %q", dir, want)
	}
	if got := s.taken(); !slices.Equal(got, []string{"--user show --property=ControlGroup --value " + AgentsSlice}) {
		t.Errorf("the manager is asked with %q", got)
	}
	s.answer("echo")
	if dir, err := sliceDir(context.Background()); dir != "" || err != nil {
		t.Errorf("a slice with no cgroup is looked for in %q (%v)", dir, err)
	}
}
