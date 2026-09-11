package launcher

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const procEnv = "AACP_PROC"

func procRoot() string {
	if p := os.Getenv(procEnv); p != "" {
		return p
	}
	return "/proc"
}

func pidsByComm(name string) []int {
	entries, err := os.ReadDir(procRoot())
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if procComm(pid) == name {
			out = append(out, pid)
		}
	}
	return out
}

func procComm(pid int) string {
	raw, err := os.ReadFile(filepath.Join(procRoot(), strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func procArgs(pid int) []string {
	raw, err := os.ReadFile(filepath.Join(procRoot(), strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil
	}
	trimmed := strings.TrimRight(string(raw), "\x00")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\x00")
}

func procEnviron(pid int) []string {
	raw, err := os.ReadFile(filepath.Join(procRoot(), strconv.Itoa(pid), "environ"))
	if err != nil {
		return nil
	}
	trimmed := strings.TrimRight(string(raw), "\x00")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\x00")
}

func procStart(pid int) (string, bool) {
	fields, ok := statFields(pid)
	if !ok || len(fields) < 20 {
		return "", false
	}
	return fields[19], true
}

func statFields(pid int) ([]string, bool) {
	raw, err := os.ReadFile(filepath.Join(procRoot(), strconv.Itoa(pid), "stat"))
	if err != nil {
		return nil, false
	}
	line := string(raw)
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return nil, false
	}
	return strings.Fields(line[i+1:]), true
}

func procParent(pid int) (int, bool) {
	fields, ok := statFields(pid)
	if !ok || len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

func konsoleOf(pid int) int {
	for range 5 {
		parent, ok := procParent(pid)
		if !ok || parent <= 1 {
			return 0
		}
		if procComm(parent) == "konsole" {
			return parent
		}
		pid = parent
	}
	return 0
}

func argValue(args []string, names ...string) (string, bool) {
	for i, a := range args {
		for _, name := range names {
			if a == name && i+1 < len(args) {
				return args[i+1], true
			}
			if value, ok := strings.CutPrefix(a, name+"="); ok {
				return value, true
			}
		}
	}
	return "", false
}
