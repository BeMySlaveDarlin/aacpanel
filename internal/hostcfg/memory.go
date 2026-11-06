package hostcfg

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// The ceilings of the memory the agents take. Every unit the executor starts
// for them lives in one slice of the user's manager, held to the first three;
// a session on the stream is held to the last three inside it.
const (
	AgentsMemoryHighEnv  = "AACP_AGENTS_MEMORY_HIGH"
	AgentsMemoryMaxEnv   = "AACP_AGENTS_MEMORY_MAX"
	AgentsSwapMaxEnv     = "AACP_AGENTS_SWAP_MAX"
	SessionMemoryHighEnv = "AACP_SESSION_MEMORY_HIGH"
	SessionMemoryMaxEnv  = "AACP_SESSION_MEMORY_MAX"
	SessionSwapMaxEnv    = "AACP_SESSION_SWAP_MAX"
)

// Ceilings are the memory ceilings of one cgroup in the form systemd takes
// them, like 6G or 768M; an empty one is no ceiling.
type Ceilings struct {
	High string
	Max  string
	Swap string
}

// Memory is the budget of the agents: the ceilings of their slice, and those
// of one session on the stream in it.
type Memory struct {
	Agents  Ceilings
	Session Ceilings
}

// meminfoFile is where the kernel tells the memory of the machine.
var meminfoFile = "/proc/meminfo"

// ReadMemory is the budget the description gives. A ceiling it does not name
// is a share of the machine's memory, and one it names as empty or 0 is none.
// A value systemd would refuse is named in the error and the share takes its
// place: a unit started with it would not start at all.
func ReadMemory() (Memory, error) {
	total, err := machineMemory()
	var shares Memory
	if err == nil {
		shares = DefaultMemory(total)
	} else {
		err = fmt.Errorf("the memory of the machine is not known, so a ceiling the description does not name is none: %w", err)
	}
	errs := []error{err}
	read := func(name, share string) string {
		v, err := ceiling(name, share)
		errs = append(errs, err)
		return v
	}
	m := Memory{
		Agents: Ceilings{
			High: read(AgentsMemoryHighEnv, shares.Agents.High),
			Max:  read(AgentsMemoryMaxEnv, shares.Agents.Max),
			Swap: read(AgentsSwapMaxEnv, shares.Agents.Swap),
		},
		Session: Ceilings{
			High: read(SessionMemoryHighEnv, shares.Session.High),
			Max:  read(SessionMemoryMaxEnv, shares.Session.Max),
			Swap: read(SessionSwapMaxEnv, shares.Session.Swap),
		},
	}
	return m, errors.Join(errs...)
}

// DefaultMemory is the budget of a machine with total bytes of memory, as
// shares of it. The agents together may grow to 45 per cent before the kernel
// slows them down and never past 55: the rest is the desktop, docker and the
// panel, which a pile of test runs must not push out. A session may take a
// tenth before it is slowed and a sixth before it is killed, so one runaway
// is stopped long before the slice is full and the others go on. Swap is a
// sixteenth for all of them and a sixty-fourth for one: a little lets cold
// pages out, and a lot only turns a kill into minutes of thrashing.
func DefaultMemory(total uint64) Memory {
	return Memory{
		Agents: Ceilings{
			High: share(total, 45, 100),
			Max:  share(total, 55, 100),
			Swap: share(total, 1, 16),
		},
		Session: Ceilings{
			High: share(total, 1, 10),
			Max:  share(total, 1, 6),
			Swap: share(total, 1, 64),
		},
	}
}

const (
	mib = uint64(1) << 20
	gib = uint64(1) << 30
)

// share is num/den of total, rounded to a whole gigabyte from 4G up and to a
// quarter of one below: a big machine gets the round figures a person writes,
// and a small one ceilings that neither round to nothing nor merge with each
// other.
func share(total, num, den uint64) string {
	v := total / den * num
	unit := gib
	if v < 4*gib {
		unit = gib / 4
	}
	n := (v + unit/2) / unit * unit
	if n == 0 {
		n = unit
	}
	if n%gib == 0 {
		return strconv.FormatUint(n/gib, 10) + "G"
	}
	return strconv.FormatUint(n/mib, 10) + "M"
}

// machineMemory is the memory of the machine in bytes, as the kernel tells it.
func machineMemory() (uint64, error) {
	kb, err := meminfo("MemTotal")
	if err == nil && kb == 0 {
		err = fmt.Errorf("%s gives MemTotal as 0", meminfoFile)
	}
	return kb << 10, err
}

// MachineMemory is the memory of the machine in bytes, as the kernel tells it.
func MachineMemory() (uint64, error) { return machineMemory() }

// AvailableMemory is how many bytes the kernel counts it could give a new
// load now without swapping: MemAvailable, the free memory and the cache it
// can take back.
func AvailableMemory() (uint64, error) {
	kb, err := meminfo("MemAvailable")
	return kb << 10, err
}

// meminfo is one line of the kernel's account of memory, in kB.
func meminfo(field string) (uint64, error) {
	f, err := os.Open(meminfoFile)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		f := strings.Fields(lines.Text())
		if len(f) == 3 && f[0] == field+":" && f[2] == "kB" {
			kb, err := strconv.ParseUint(f[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("%s gives %s as %q", meminfoFile, field, f[1])
			}
			return kb, nil
		}
	}
	if err := lines.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("%s names no %s in kB", meminfoFile, field)
}

// ceiling is one ceiling of the description, or share where it names none.
func ceiling(name, share string) (string, error) { return sized(name, share, "ceiling") }

// sized is one size of the description, or share where it names none; what
// names the size in the error, a ceiling or a reserve.
func sized(name, share, what string) (string, error) {
	raw, named := lookup(name)
	if !named {
		return share, nil
	}
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	zero, ok := sizeForm(v)
	if ok && zero {
		return "", nil
	}
	if !ok {
		return share, fmt.Errorf("%s=%s is not a size systemd takes: it is a whole number of bytes, or of K, M, G "+
			"or T, like 6G or 768M; empty or 0 is no %s. The %s is %s, the machine's share, until it is "+
			"corrected", name, raw, what, what, orNone(share))
	}
	return v, nil
}

// SizeBytes is a size in the form systemd takes it, like 6G or 768M, in
// bytes; ok is false for a value systemd would refuse.
func SizeBytes(v string) (n uint64, ok bool) {
	v = strings.TrimSpace(v)
	if _, ok := sizeForm(v); !ok {
		return 0, false
	}
	digits := strings.TrimRight(v, "KMGT")
	n, _ = strconv.ParseUint(digits, 10, 64)
	shift := map[string]uint{"": 0, "K": 10, "M": 20, "G": 30, "T": 40}[v[len(digits):]]
	if n > math.MaxUint64>>shift {
		return 0, false
	}
	return n << shift, true
}

// sizeForm says whether v is a whole number with one of the suffixes systemd
// reads as powers of 1024, or none, and whether that number is nought.
func sizeForm(v string) (zero, ok bool) {
	digits := strings.TrimRight(v, "KMGT")
	if len(v)-len(digits) > 1 || digits == "" {
		return false, false
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	return n == 0, err == nil
}

func orNone(v string) string {
	if v == "" {
		return "none"
	}
	return v
}
