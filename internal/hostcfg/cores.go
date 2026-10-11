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
func Cores() (string, error) {
	present, _ := os.ReadFile(presentFile)
	return ValidateCores(described(CoresEnv), present)
}

// ValidateCores checks a value as CPUAffinity= reads it, against the kernel's
// list of present cores, and returns ranges joined by commas. A nil list
// means the kernel's list could not be read: only the syntax is checked.
// Both the installer and the executor use this rule, so a typed answer and
// a description carried from another machine get the same checks.
func ValidateCores(raw string, present []byte) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	cores, err := parseCores(raw)
	if err != nil {
		return "", fmt.Errorf("%s=%s is not a list of cores systemd takes: %v; it is core numbers and ranges "+
			"apart by commas, like 8-23 or 0,2,4-7", CoresEnv, raw, err)
	}
	if present != nil {
		machine, err := parseCores(strings.TrimSpace(string(present)))
		if err != nil {
			return "", fmt.Errorf("%s cannot be checked against the kernel's list of cores: %v", CoresEnv, err)
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
