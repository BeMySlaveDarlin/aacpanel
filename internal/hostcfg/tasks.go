package hostcfg

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	SessionTasksMaxEnv = "AACP_SESSION_TASKS_MAX"
	AgentsTasksMaxEnv  = "AACP_AGENTS_TASKS_MAX"
)

// Tasks are the task ceilings of one agent unit and of their shared slice;
// an empty value is no ceiling.
type Tasks struct {
	Session string
	Agents  string
}

// ReadTasks uses the description's ceilings, with defaults where it names
// none or gives a value that would stop systemd from starting a unit.
func ReadTasks() (Tasks, error) {
	session, sessionErr := taskCeiling(SessionTasksMaxEnv, "8192")
	agents, agentsErr := taskCeiling(AgentsTasksMaxEnv, "24576")
	return Tasks{Session: session, Agents: agents}, errors.Join(sessionErr, agentsErr)
}

func taskCeiling(name, fallback string) (string, error) {
	raw, named := lookup(name)
	if !named {
		return fallback, nil
	}
	v := strings.TrimSpace(raw)
	if v == "" || v == "0" {
		return "", nil
	}
	if v == "infinity" {
		return v, nil
	}
	if n, err := strconv.ParseUint(v, 10, 64); err == nil && n > 0 {
		return v, nil
	}
	return fallback, fmt.Errorf("%s=%s is not a task ceiling systemd takes: it is a positive whole number or infinity; "+
		"empty or 0 is no ceiling. The ceiling is %s, the default, until it is corrected", name, raw, fallback)
}
