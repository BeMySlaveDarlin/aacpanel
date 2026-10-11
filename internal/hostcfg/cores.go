package hostcfg

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// CoresEnv names the cores every unit the executor starts is held to: the
// sessions with their windows, the terminals of the panel, the codex daemons
// and the observers. Unset, they run where the system puts them.
const CoresEnv = "AACP_CORES"

// presentFile lists the cores of the machine, in the form the setting takes.
var presentFile = "/sys/devices/system/cpu/present"

// maxCores is how many cores systemd counts: a core numbered past them is
// refused by its CPUAffinity=.
const maxCores = 8192

// Cores are the cores of the description in the form systemd-run is given
// them, ranges joined by commas, or empty when the description names none.
// The setting is read the way CPUAffinity= reads it — core numbers and
// ranges, apart by commas or spaces — and a value systemd would refuse, or
// one naming a core the kernel does not list among the machine's, is an
// error here: the executor that reads it refuses to start, rather than every
// unit it starts failing on it later, or running on no core at all.
func Cores() (string, error) {
	raw := strings.TrimSpace(described(CoresEnv))
	if raw == "" {
		return "", nil
	}
	cores, err := parseCores(raw)
	if err != nil {
		return "", fmt.Errorf("%s=%s is not a list of cores systemd takes: %v; it is core numbers and ranges "+
			"apart by commas, like 8-23 or 0,2,4-7", CoresEnv, raw, err)
	}
	if present, err := os.ReadFile(presentFile); err == nil {
		machine, err := parseCores(strings.TrimSpace(string(present)))
		if err != nil {
			return "", fmt.Errorf("the cores of the machine are not read from %s: %v", presentFile, err)
		}
		var missing []int
		for _, c := range cores {
			if !slices.Contains(machine, c) {
				missing = append(missing, c)
			}
		}
		if len(missing) > 0 {
			return "", fmt.Errorf("%s=%s names cores %s the machine does not have: its cores are %s",
				CoresEnv, raw, formatCores(missing), formatCores(machine))
		}
	}
	return formatCores(cores), nil
}

// parseCores reads a list of cores as CPUAffinity= does and returns the cores
// it names, in order and each once.
func parseCores(raw string) ([]int, error) {
	seen := map[int]bool{}
	items := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	for _, item := range items {
		lo, hi, isRange := strings.Cut(item, "-")
		first, err := coreNumber(lo, item)
		if err != nil {
			return nil, err
		}
		last := first
		if isRange {
			if last, err = coreNumber(hi, item); err != nil {
				return nil, err
			}
			if last < first {
				return nil, fmt.Errorf("the range %s runs backwards", item)
			}
		}
		for c := first; c <= last; c++ {
			seen[c] = true
		}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("it names no core")
	}
	cores := make([]int, 0, len(seen))
	for c := range seen {
		cores = append(cores, c)
	}
	slices.Sort(cores)
	return cores, nil
}

func coreNumber(s, item string) (int, error) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, fmt.Errorf("%q is not a core number or a range of them", item)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n >= maxCores {
		return 0, fmt.Errorf("%s is past the %d cores systemd counts", s, maxCores)
	}
	return n, nil
}

// formatCores writes cores in order as ranges joined by commas: 0-3,8.
func formatCores(cores []int) string {
	var parts []string
	for i := 0; i < len(cores); {
		j := i
		for j+1 < len(cores) && cores[j+1] == cores[j]+1 {
			j++
		}
		if j == i {
			parts = append(parts, strconv.Itoa(cores[i]))
		} else {
			parts = append(parts, strconv.Itoa(cores[i])+"-"+strconv.Itoa(cores[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}
