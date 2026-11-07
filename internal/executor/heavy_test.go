package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/heavy"
)

func TestHeavyRunFindsItsSessionThroughThePeerCgroup(t *testing.T) {
	procFS(t,
		fakeProc{pid: 100, comm: "claude", args: []string{"claude", "-n", "lab"}},
		fakeProc{pid: 101, comm: "claude", args: []string{"claude", "-n", "other"}},
		fakeProc{pid: 200, comm: "bash", ppid: 100},
		fakeProc{pid: 201, comm: "aacpanel-exec", ppid: 200},
		fakeProc{pid: 202, comm: "aacpanel-exec", ppid: 1})
	sessionFiles(t)
	root := procDir()
	write := func(pid int, group string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, strconv.Itoa(pid), "cgroup"), []byte("0::/user.slice/"+group+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const unit = "run-session.service"
	for _, pid := range []int{100, 101, 200, 201, 202} {
		write(pid, AgentsSlice+"/"+unit+"/nested")
	}
	units := map[string][]heavySession{unit: {{pid: 100, name: "lab"}, {pid: 101, name: "other"}}}
	if got := sessionOfHeavy(root, 201, units); got != "lab" {
		t.Errorf("a peer under lab in a shared unit belongs to %q", got)
	}
	if got := NewHeavyHost().Session(201); got != "lab" {
		t.Errorf("the host's live session map identifies the peer as %q", got)
	}
	if got := sessionOfHeavy(root, 202, units); got != "" {
		t.Errorf("a shared unit without an ancestor names %q", got)
	}
	delete(units, unit)
	if got := sessionOfHeavy(root, 201, units); got != "" {
		t.Errorf("an unknown unit names %q", got)
	}
	units[unit] = []heavySession{{pid: 100, name: "lab"}}
	if got := sessionOfHeavy(root, 202, units); got != "lab" {
		t.Errorf("a unit of one session names %q", got)
	}
	write(201, "app.slice/"+unit)
	if got := sessionOfHeavy(root, 201, units); got != "" {
		t.Errorf("a peer outside the agents' slice names %q", got)
	}
	if got := sessionOfHeavy(root, 999, units); got != "" {
		t.Errorf("a process that has gone names %q", got)
	}
}

// heavyScene is a host for the queue of heavy runs: busctl standing in for
// the manager's bus, and the processes and the pressure of the kernel laid
// out in temporary directories.
type heavyScene struct {
	t     *testing.T
	calls string
	bus   string
	proc  string
}

func newHeavyScene(t *testing.T, answer string) *heavyScene {
	t.Helper()
	dir := t.TempDir()
	s := &heavyScene{t: t, calls: filepath.Join(dir, "calls"), bus: filepath.Join(dir, "bus"), proc: t.TempDir()}
	script := filepath.Join(dir, "busctl")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + s.calls + "\necho \"$DBUS_SESSION_BUS_ADDRESS\" > " + s.bus +
		"\n" + answer + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(busctlEnv, script)
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1001")
	before := procRoot
	procRoot = s.proc
	t.Cleanup(func() { procRoot = before })
	return s
}

// in puts the process pid in the cgroup of unit.
func (s *heavyScene) in(pid, unit string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Join(s.proc, pid), 0o755); err != nil {
		s.t.Fatal(err)
	}
	line := "0::/user.slice/user-1001.slice/user@1001.service/aacp.slice/aacp-agents.slice/" + unit + "\n"
	if err := os.WriteFile(filepath.Join(s.proc, pid, "cgroup"), []byte(line), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *heavyScene) read(path string) []string {
	s.t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// The manager starts the scope of a run with the run's process in it, in the
// agents' slice and held to what the run asked for, on the bus of the user's
// manager whatever bus the executor was started with.
func TestTheScopeOfAHeavyRunIsStartedByTheManager(t *testing.T) {
	s := newHeavyScene(t, "echo 'o \"/org/freedesktop/systemd1/job/7\"'")
	s.in("4242", "aacp-heavy-3.scope")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/tmp/dbus-elsewhere")
	err := NewHeavyHost().Start(context.Background(), 4242,
		heavy.Scope{Unit: "aacp-heavy-3.scope", Name: "make check", Mem: 8 * testGiB, Swap: testGiB, CPUs: 4})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--user", "call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1",
		"org.freedesktop.systemd1.Manager", "StartTransientUnit", "ssa(sv)a(sa(sv))", "aacp-heavy-3.scope", "fail", "8",
		"Description", "s", "heavy run: make check",
		"Slice", "s", AgentsSlice,
		"PIDs", "au", "1", "4242",
		"OOMPolicy", "s", "continue",
		"CollectMode", "s", "inactive-or-failed",
		"MemoryMax", "t", "8589934592",
		"MemorySwapMax", "t", "1073741824",
		"CPUQuotaPerSecUSec", "t", "4000000",
		"0"}
	if got := s.read(s.calls); !slices.Equal(got, want) {
		t.Errorf("busctl is called with\n%q\nwant\n%q", got, want)
	}
	if got := s.read(s.bus); !slices.Equal(got, []string{"unix:path=/run/user/1001/bus"}) {
		t.Errorf("busctl talks to %q, not the bus of the user's manager", got)
	}
}

// A run that asks for no memory and takes no quota of cores is held by the
// slice alone.
func TestAScopeWithNoCeilingsNamesNone(t *testing.T) {
	s := newHeavyScene(t, "exit 0")
	s.in("4242", "aacp-heavy-1.scope")
	if err := NewHeavyHost().Start(context.Background(), 4242, heavy.Scope{Unit: "aacp-heavy-1.scope"}); err != nil {
		t.Fatal(err)
	}
	got := s.read(s.calls)
	if got[9] != "5" || slices.ContainsFunc(got, func(a string) bool {
		return a == "MemoryMax" || a == "MemorySwapMax" || a == "CPUQuotaPerSecUSec"
	}) {
		t.Errorf("a run with no ceilings is started with %q", got)
	}
}

// The start of a scope fails with what the manager said, and a process the
// manager did not move fails it once the wait is over.
func TestAScopeThatDoesNotStartSaysWhy(t *testing.T) {
	s := newHeavyScene(t, "echo 'Call failed: Unit aacp-heavy-1.scope already exists.' >&2; exit 1")
	err := NewHeavyHost().Start(context.Background(), 4242, heavy.Scope{Unit: "aacp-heavy-1.scope"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a refusal of the manager reads %v", err)
	}

	s = newHeavyScene(t, "exit 0")
	s.in("4242", "run-p1-i2.service")
	before := heavyStartWait
	heavyStartWait = 50 * time.Millisecond
	t.Cleanup(func() { heavyStartWait = before })
	err = NewHeavyHost().Start(context.Background(), 4242, heavy.Scope{Unit: "aacp-heavy-1.scope"})
	if err == nil || !strings.Contains(err.Error(), "process 4242 is not in aacp-heavy-1.scope") {
		t.Errorf("a process left where it was reads %v", err)
	}
}

// The queue reads the machine, the slice and the runs going in it from the
// files of the kernel: a run is a scope of the queue in the slice, asking for
// its MemoryMax, and the slice and a run hold what they hold besides the
// cache of files, shared memory counted in.
func TestTheHostOfTheQueueIsReadFromTheKernel(t *testing.T) {
	root := t.TempDir()
	beforeRoot, beforePressure := cgroupRoot, pressureRoot
	cgroupRoot, pressureRoot = root, t.TempDir()
	t.Cleanup(func() { cgroupRoot, pressureRoot = beforeRoot, beforePressure })
	const group = "user.slice/user-1001.slice/user@1001.service/aacp.slice/aacp-agents.slice"
	fakeSystemctl(t, "echo /"+group)
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	slice := filepath.Join(root, group)
	stat := func(file, shmem uint64) string {
		return fmt.Sprintf("anon 1\nfile %d\nkernel 1\nshmem %d\nactive_file 1\ninactive_file 1\n", file, shmem)
	}
	write(filepath.Join(slice, "memory.current"), "21474836480\n")
	write(filepath.Join(slice, "memory.high"), "32212254720\n")
	write(filepath.Join(slice, "memory.stat"), stat(16*testGiB, 2*testGiB))
	write(filepath.Join(slice, "aacp-heavy-1.scope", "memory.current"), "3221225472\n")
	write(filepath.Join(slice, "aacp-heavy-1.scope", "memory.stat"), stat(2*testGiB, 0))
	write(filepath.Join(slice, "aacp-heavy-1.scope", "memory.max"), "8589934592\n")
	write(filepath.Join(slice, "aacp-heavy-2.scope", "memory.current"), "4096\n")
	write(filepath.Join(slice, "aacp-heavy-2.scope", "memory.stat"), stat(8192, 0))
	write(filepath.Join(slice, "aacp-heavy-2.scope", "memory.max"), "max\n")
	write(filepath.Join(slice, "run-p1-i2.service", "memory.current"), "1073741824\n")
	write(filepath.Join(slice, "run-p1-i2.service", "memory.stat"), stat(0, 0))
	write(filepath.Join(slice, "run-p1-i2.service", "memory.max"), "10737418240\n")
	write(filepath.Join(pressureRoot, "memory"), "some avg10=3.00 avg60=1.00 avg300=0.00 total=1\n"+
		"full avg10=1.50 avg60=0.50 avg300=0.00 total=1\n")
	write(filepath.Join(pressureRoot, "io"), "some avg10=9.00 avg60=1.00 avg300=0.00 total=1\n"+
		"full avg10=7.25 avg60=0.50 avg300=0.00 total=1\n")

	h := NewHeavyHost()
	h.available = func() (uint64, error) { return 40 * testGiB, nil }
	m, err := h.Measure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := heavy.Measure{Available: 40 * testGiB, Slice: 6 * testGiB, MemoryFull: 1.5, IOFull: 7.25,
		Runs: []heavy.Running{{Unit: "aacp-heavy-1.scope", Ask: 8 * testGiB, Taken: testGiB},
			{Unit: "aacp-heavy-2.scope", Ask: 0, Taken: 0}}}
	if m.Available != want.Available || m.Slice != want.Slice || m.MemoryFull != want.MemoryFull ||
		m.IOFull != want.IOFull || !slices.Equal(m.Runs, want.Runs) {
		t.Errorf("the host reads as %+v, want %+v", m, want)
	}
	beforeProc := procRoot
	procRoot = t.TempDir()
	t.Cleanup(func() { procRoot = beforeProc })
	write(filepath.Join(procRoot, "sys/kernel/random/boot_id"), "host-boot\n")
	room, err := h.Headroom(t.Context())
	if err != nil || room == nil || room.BootID != "host-boot" || room.High != 30*testGiB || room.Measure.Slice != 6*testGiB {
		t.Fatalf("recovery headroom: %+v %v", room, err)
	}

	// A kernel that keeps no account of pressure has none to wait for.
	pressureRoot = filepath.Join(root, "absent")
	if m, err := h.Measure(context.Background()); err != nil || m.MemoryFull != 0 || m.IOFull != 0 {
		t.Errorf("a kernel without pressure reads as %+v, %v", m, err)
	}
}
