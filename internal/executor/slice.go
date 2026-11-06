package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
)

// AgentsSlice is the slice of the user's manager every unit the executor
// starts for the agents lives in: its ceilings are the budget of them all.
// systemd reads the dash as nesting, so it sits in aacp.slice.
const AgentsSlice = "aacp-agents.slice"

// sliceWait bounds the one call that sets the slice up at the executor's start.
const sliceWait = 15 * time.Second

// inSlice are the properties that put a unit in the agents' slice and hold it
// to ceilings of its own; a ceiling that is not set is not passed, and the
// unit is held only by the slice there. At a MemoryMax the kernel kills the
// biggest process of the cgroup, and while the main process of the unit runs
// — an observer, the feed check, a launcher still starting what it starts —
// systemd would stop the whole unit after it. OOMPolicy=continue leaves the
// rest running, and whoever started the killed command sees it killed; a
// unit whose main process is the one killed ends by itself. A unit whose main
// process has already ended, as the launcher's does once the session runs, is
// not stopped either way.
func inSlice(held hostcfg.Ceilings) []string {
	args := []string{"--slice=" + AgentsSlice, "--property=OOMPolicy=continue"}
	for _, p := range []struct{ name, value string }{
		{"MemoryHigh", held.High}, {"MemoryMax", held.Max}, {"MemorySwapMax", held.Swap},
	} {
		if p.value != "" {
			args = append(args, "--property="+p.name+"="+p.value)
		}
	}
	return args
}

// SetUpAgentsSlice holds the agents' slice to its ceilings for as long as the
// user's manager runs. The values go to the runtime drop-ins of the manager,
// so every start of the executor writes them again from the description, and
// a ceiling it turned off is lifted rather than left from the start before.
// The slice is set up whether a unit lives in it yet or not; one that does
// takes the new ceilings at once.
func SetUpAgentsSlice(ctx context.Context, c hostcfg.Ceilings) error {
	ctx, cancel := context.WithTimeout(ctx, sliceWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlPath(), sliceArgs(c)...).CombinedOutput()
	if err != nil {
		if said := strings.TrimSpace(string(out)); said != "" {
			err = fmt.Errorf("%w: %s", err, shortenLines(said))
		}
		return fmt.Errorf("the ceilings of %s were not set (systemctl --user set-property): %w", AgentsSlice, err)
	}
	return nil
}

func sliceArgs(c hostcfg.Ceilings) []string {
	lifted := func(v string) string {
		if v == "" {
			return "infinity"
		}
		return v
	}
	return []string{"--user", "--runtime", "set-property", AgentsSlice,
		"MemoryHigh=" + lifted(c.High), "MemoryMax=" + lifted(c.Max), "MemorySwapMax=" + lifted(c.Swap)}
}
