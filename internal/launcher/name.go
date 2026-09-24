package launcher

import (
	"fmt"
	"strings"

	"aacpanel/internal/stream"
)

const maxOrdinal = 99

func takenNames() map[string]bool {
	out := map[string]bool{}
	for _, pid := range claudePIDs() {
		args := procArgs(pid)
		if oneShot(pid, args) {
			continue
		}
		if name, ok := argValue(args, "-n", "--name"); ok && name != "" {
			out[name] = true
		}
	}
	return out
}

// oneShot says whether a claude process is a run of its own rather than a
// session; the rule is the stream's, since only a holder knows its sessions.
func oneShot(pid int, args []string) bool { return stream.OneShot(pid, args) }

func freeName(base string, taken map[string]bool) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("session name is empty: the panel must send it together with the project")
	}
	if !taken[base] {
		return base, nil
	}
	for n := 2; n <= maxOrdinal; n++ {
		name := fmt.Sprintf("%s-%d", base, n)
		if !taken[name] {
			return name, nil
		}
	}
	return "", fmt.Errorf("no free name for %q: everything up to %s-%d is taken", base, base, maxOrdinal)
}
