package hostcfg

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The queue of heavy runs: how many run at once, how much memory the machine
// keeps available past them, what a run that names nothing asks for, and how
// many cores one run takes.
const (
	HeavySlotsEnv   = "AACP_HEAVY_SLOTS"
	HeavyReserveEnv = "AACP_HEAVY_RESERVE"
	HeavyMemEnv     = "AACP_HEAVY_MEM"
	HeavyCPUsEnv    = "AACP_HEAVY_CPUS"
)

// Heavy is what the queue of heavy runs admits by. Sizes are in the form
// systemd takes them; nought and empty are none: no bound on the runs at
// once, no quota of cores, no memory kept back, no ceiling for a run that
// names none.
type Heavy struct {
	Slots   int
	CPUs    int
	Reserve string
	Mem     string
}

// Three runs at once, four cores each. A build or a test run spreads over
// every core it sees, and four of them is as much as one session is given on
// a shared machine; three such runs are twelve cores, and the sessions that
// are not running anything heavy keep the rest. The memory is the queue's
// other bound, so more slots buy nothing on a machine whose memory three runs
// already fill.
const (
	defaultHeavySlots = 3
	defaultHeavyCPUs  = 4
)

// ReadHeavy is the queue the description gives. A size it does not name is a
// share of the machine's memory, a count it does not name is the default,
// and one it names as empty or 0 is none. A value systemd would refuse is
// named in the error and the default takes its place.
func ReadHeavy() (Heavy, error) {
	total, err := machineMemory()
	shares := Heavy{Slots: defaultHeavySlots, CPUs: defaultHeavyCPUs}
	if err == nil {
		shares = DefaultHeavy(total)
	} else {
		err = fmt.Errorf("the memory of the machine is not known, so the queue keeps no memory back and a run "+
			"that names none has no ceiling: %w", err)
	}
	errs := []error{err}
	h := Heavy{}
	h.Slots, err = count(HeavySlotsEnv, shares.Slots)
	errs = append(errs, err)
	h.CPUs, err = count(HeavyCPUsEnv, shares.CPUs)
	errs = append(errs, err)
	h.Reserve, err = sized(HeavyReserveEnv, shares.Reserve, "reserve")
	errs = append(errs, err)
	h.Mem, err = sized(HeavyMemEnv, shares.Mem, "ceiling")
	errs = append(errs, err)
	return h, errors.Join(errs...)
}

// DefaultHeavy is the queue of a machine with total bytes of memory. A fifth
// of the machine stays available past the runs: the desktop, docker and the
// cache of the files every session reads live there, and a run admitted into
// the last of it would push them into swap. A run that names nothing asks for
// a tenth: on 62 GB that is 6G, near twice the 2.3G to 3.4G a full check of
// the panel's own repository peaks at in its scope, which leaves room for a
// cold build cache and for heavier tests than its own.
func DefaultHeavy(total uint64) Heavy {
	return Heavy{
		Slots:   defaultHeavySlots,
		CPUs:    defaultHeavyCPUs,
		Reserve: share(total, 1, 5),
		Mem:     share(total, 1, 10),
	}
}

// count is one count of the description, or def where it names none.
func count(name string, def int) (int, error) {
	raw, named := lookup(name)
	if !named {
		return def, nil
	}
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def, fmt.Errorf("%s=%s is not a whole number; empty or 0 is none. It is %d until it is corrected",
			name, raw, def)
	}
	return n, nil
}
