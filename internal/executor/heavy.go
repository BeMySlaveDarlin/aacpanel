package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/heavy"
	"aacpanel/internal/hostcfg"
)

// heavyStartWait bounds the start of the scope of a heavy run: the call to
// the user's manager and the wait for the run to be moved into it.
var heavyStartWait = 5 * time.Second

var (
	// procRoot is where the kernel shows the processes.
	procRoot = "/proc"
	// pressureRoot is where the kernel tells the pressure on the machine.
	pressureRoot = "/proc/pressure"
)

// HeavyHost is the host as the queue of heavy runs reads it: the memory of
// the machine and of the agents' slice, the pressure on the machine, and the
// scopes of the runs going in the slice. Everything is read from the files
// of the kernel: the cgroups are read-only to the executor, and reading
// them asks nothing of the manager.
type HeavyHost struct {
	available func() (uint64, error)

	mu  sync.Mutex
	dir string
}

// NewHeavyHost is the host of the queue of heavy runs.
func NewHeavyHost() *HeavyHost { return &HeavyHost{available: hostcfg.AvailableMemory} }

type heavySession struct {
	pid  int
	name string
}

// Session reads the same live process names as session actions. A tmux
// server may hold several sessions in one unit, so their process ancestry
// disambiguates them; a shared unit alone is not a session identity.
func (h *HeavyHost) Session(pid int) string {
	units := map[string][]heavySession{}
	pids, _ := claudePIDs()
	for _, p := range pids {
		name, ok := sessionName(p)
		unit := heavySessionUnit(procDir(), p)
		if ok && unit != "" {
			units[unit] = append(units[unit], heavySession{pid: p, name: name})
		}
	}
	return sessionOfHeavy(procDir(), pid, units)
}

func sessionOfHeavy(root string, pid int, units map[string][]heavySession) string {
	unit := heavySessionUnit(root, pid)
	if unit == "" {
		return ""
	}
	sessions := units[unit]
	cur := pid
	for range 64 {
		for _, s := range sessions {
			if s.pid == cur {
				return s.name
			}
		}
		parent, ok := parentFrom(root, cur)
		if !ok || parent <= 1 || parent == cur {
			break
		}
		cur = parent
	}
	if len(sessions) == 1 {
		return sessions[0].name
	}
	return ""
}

func heavySessionUnit(root string, pid int) string {
	raw, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		group, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		parts := strings.Split(group, "/")
		for i, part := range parts {
			if part == AgentsSlice && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	return ""
}

// WithHeavy shares the queue with read-only questions on the action socket.
func (e *Executor) WithHeavy(q *heavy.Queue) { e.heavy = q }

func (e *Executor) Heavy(ctx context.Context) (*heavy.View, error) {
	if e.heavy == nil {
		return nil, fmt.Errorf("the queue of heavy runs is unavailable")
	}
	v, err := e.heavy.View(ctx)
	return &v, err
}

// Headroom uses the queue's measurements, and reads the kernel ceiling rather
// than the description: a running slice can have a different MemoryHigh.
func (e *Executor) Headroom(ctx context.Context) (*action.Headroom, error) {
	h := NewHeavyHost()
	room, err := h.Headroom(ctx)
	if err != nil {
		return nil, err
	}
	// A slice with no units has no cgroup yet. Its configured ceiling still
	// bounds the first batch; once active, the kernel's value takes over.
	if dir, err := h.sliceDir(ctx); err == nil && dir == "" {
		room.High, _ = hostcfg.SizeBytes(e.memory.Agents.High)
	}
	return room, nil
}

func (h *HeavyHost) Headroom(ctx context.Context) (*action.Headroom, error) {
	m, err := h.Measure(ctx)
	if err != nil {
		return nil, err
	}
	dir, err := h.sliceDir(ctx)
	if err != nil {
		return nil, err
	}
	var high uint64
	if dir != "" {
		high, err = readCgroupNumber(filepath.Join(dir, "memory.high"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if high == noHigh {
			high = 0
		}
	}
	raw, err := os.ReadFile(filepath.Join(procRoot, "sys/kernel/random/boot_id"))
	if err != nil {
		return nil, err
	}
	return &action.Headroom{BootID: strings.TrimSpace(string(raw)), High: high, Measure: m}, nil
}

// Measure reads the host once. The runs going are the scopes of the queue in
// the slice, whoever admitted them: a restart of the executor keeps them
// counted, ceilings included, since a scope keeps its MemoryMax. The slice
// and a run are read by what they hold besides the cache of files, as the
// machine is by MemAvailable: the cache of the files the sessions read fills
// the slice up to its MemoryHigh and stays there, and the kernel gives it up
// to a run that grows before it slows anything. Counted whole, the slice
// would stand at its High for good, and no run would be admitted again.
func (h *HeavyHost) Measure(ctx context.Context) (heavy.Measure, error) {
	var m heavy.Measure
	var err error
	if m.Available, err = h.available(); err != nil {
		return m, err
	}
	if m.MemoryFull, err = machinePressure("memory"); err != nil {
		return m, err
	}
	if m.IOFull, err = machinePressure("io"); err != nil {
		return m, err
	}
	dir, err := h.sliceDir(ctx)
	if err != nil || dir == "" {
		return m, err
	}
	m.Slice, err = heldBesidesCache(dir)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, heavy.UnitPrefix) || !strings.HasSuffix(name, ".scope") {
			continue
		}
		taken, err := heldBesidesCache(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		ask, err := readCgroupNumber(filepath.Join(dir, name, "memory.max"))
		if err != nil {
			continue
		}
		if ask == noHigh {
			ask = 0
		}
		m.Runs = append(m.Runs, heavy.Running{Unit: name, Ask: ask, Taken: taken})
	}
	return m, nil
}

// heldBesidesCache is what the cgroup in dir holds besides the cache of
// files the kernel can drop: memory.current less the file pages of
// memory.stat, those of shared memory and tmpfs excepted, which only swap
// takes.
func heldBesidesCache(dir string) (uint64, error) {
	current, err := readCgroupNumber(filepath.Join(dir, "memory.current"))
	if err != nil {
		return 0, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "memory.stat"))
	if err != nil {
		return 0, err
	}
	stat := map[string]uint64{}
	for _, line := range strings.Split(string(raw), "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			stat[f[0]], _ = strconv.ParseUint(f[1], 10, 64)
		}
	}
	cache := stat["file"] - min(stat["shmem"], stat["file"])
	return current - min(cache, current), nil
}

// sliceDir is the directory of the agents' slice, asked of the manager until
// it names one: a slice that has held no unit yet has none.
func (h *HeavyHost) sliceDir(ctx context.Context) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.dir != "" {
		return h.dir, nil
	}
	dir, err := sliceDir(ctx)
	h.dir = dir
	return dir, err
}

// machinePressure is full avg10 of one pressure of the machine. A kernel that
// keeps no account of pressure has none to wait for.
func machinePressure(what string) (float64, error) {
	v, err := fullPressure(filepath.Join(pressureRoot, what))
	if os.IsNotExist(err) {
		return 0, nil
	}
	return v, err
}

// Start has the user's manager start the scope of a run with the run's
// process in it, and returns once the process is there. Only the manager
// moves a process between cgroups: the executor's own view of them is
// read-only. The run goes on in the scope when the kernel kills a process of
// it at its MemoryMax, as every unit in the slice does, and whoever started
// the run sees that process killed; the scope ends with its last process,
// and is not kept after it, failed or not.
func (h *HeavyHost) Start(ctx context.Context, pid int, s heavy.Scope) error {
	ctx, cancel := context.WithTimeout(ctx, heavyStartWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, busctlBin(), scopeCall(pid, s)...)
	if run := os.Getenv("XDG_RUNTIME_DIR"); run != "" {
		cmd.Env = withBus(os.Environ(), "unix:path="+filepath.Join(run, "bus"))
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		if said := strings.TrimSpace(string(out)); said != "" {
			err = fmt.Errorf("%w: %s", err, shortenLines(said))
		}
		return fmt.Errorf("the user's manager did not start %s: %w", s.Unit, err)
	}
	for {
		if inUnit(pid, s.Unit) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("process %d is not in %s within %s", pid, s.Unit, heavyStartWait)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// scopeCall is the call of busctl that starts the scope: StartTransientUnit
// of the user's manager, which takes the processes of a scope by their pids,
// where systemd-run --scope would put only itself in it.
func scopeCall(pid int, s heavy.Scope) []string {
	props := [][]string{
		{"Description", "s", "heavy run: " + s.Name},
		{"Slice", "s", AgentsSlice},
		{"PIDs", "au", "1", strconv.Itoa(pid)},
		{"OOMPolicy", "s", "continue"},
		{"CollectMode", "s", "inactive-or-failed"},
	}
	if s.Mem > 0 {
		props = append(props,
			[]string{"MemoryMax", "t", strconv.FormatUint(s.Mem, 10)},
			[]string{"MemorySwapMax", "t", strconv.FormatUint(s.Swap, 10)})
	}
	if s.CPUs > 0 {
		props = append(props, []string{"CPUQuotaPerSecUSec", "t", strconv.Itoa(s.CPUs * 1000000)})
	}
	args := []string{"--user", "call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1",
		"org.freedesktop.systemd1.Manager", "StartTransientUnit", "ssa(sv)a(sa(sv))", s.Unit, "fail",
		strconv.Itoa(len(props))}
	for _, p := range props {
		args = append(args, p...)
	}
	return append(args, "0")
}

// inUnit says whether the process pid lives in the cgroup of unit.
func inUnit(pid int, unit string) bool {
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(line, "0::") && strings.HasSuffix(line, "/"+unit) {
			return true
		}
	}
	return false
}
