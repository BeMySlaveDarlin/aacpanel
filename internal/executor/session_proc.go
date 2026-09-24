package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"aacpanel/internal/stream"
)

func isAncestor(pid int) bool {
	cur := os.Getpid()
	for range 32 {
		if cur == pid {
			return true
		}
		ppid, ok := parentFrom("/proc", cur)
		if !ok || ppid <= 1 || ppid == cur {
			return false
		}
		cur = ppid
	}
	return false
}

func claudePIDs() ([]int, error) {
	entries, err := os.ReadDir(procDir())
	if err != nil {
		return nil, fmt.Errorf("reading /proc: %w", err)
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(procDir(), e.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) != "claude" {
			continue
		}
		out = append(out, pid)
	}
	for _, pid := range sessionFilePIDs() {
		if !slices.Contains(out, pid) {
			out = append(out, pid)
		}
	}
	slices.Sort(out)
	return out, nil
}

func sessionFilePIDs() []int {
	var out []int
	for _, dir := range sessionsDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".json")
			if !ok {
				continue
			}
			pid, err := strconv.Atoi(name)
			if err != nil {
				continue
			}
			if _, ok := nameFromSessionFile(pid); !ok {
				continue
			}
			if !slices.Contains(out, pid) {
				out = append(out, pid)
			}
		}
	}
	return out
}

func sessionName(pid int) (string, bool) {
	args, err := procArgs(pid)
	if err != nil {
		return "", false
	}
	if stream.OneShot(pid, args) {
		return "", false
	}

	if name, ok := nameFromSessionFile(pid); ok {
		return name, true
	}

	for i, a := range args {
		if a == "-n" && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func nameFromSessionFile(pid int) (string, bool) {
	var raw []byte
	var err error
	for _, dir := range sessionsDirs() {
		raw, err = os.ReadFile(filepath.Join(dir, strconv.Itoa(pid)+".json"))
		if err == nil {
			break
		}
	}
	if err != nil || raw == nil {
		return "", false
	}
	var file struct {
		PID       int    `json:"pid"`
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		ProcStart string `json:"procStart"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return "", false
	}
	if file.PID != pid || file.Name == "" || !consoleKind(file.Kind) {
		return "", false
	}
	start, ok := procStart(pid)
	if !ok || file.ProcStart == "" || file.ProcStart != start {
		return "", false
	}
	return file.Name, true
}

func consoleKind(kind string) bool {
	return kind == "" || kind == "interactive"
}

func procStart(pid int) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(procDir(), strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", false
	}
	line := string(raw)
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return "", false
	}
	fields := strings.Fields(line[i+1:])
	if len(fields) < 20 {
		return "", false
	}
	return fields[19], true
}

func konsoleOf(pid int) int {
	for range 5 {
		ppid, ok := procParent(pid)
		if !ok || ppid <= 1 {
			return 0
		}
		comm, err := os.ReadFile(filepath.Join(procDir(), strconv.Itoa(ppid), "comm"))
		if err != nil {
			return 0
		}
		if strings.TrimSpace(string(comm)) == "konsole" {
			return ppid
		}
		pid = ppid
	}
	return 0
}

func procArgs(pid int) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(procDir(), strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	return parts, nil
}

func procParent(pid int) (int, bool) {
	return parentFrom(procDir(), pid)
}

func parentFrom(root string, pid int) (int, bool) {
	raw, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	line := string(raw)
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(line[i+1:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

func procCwd(pid int) string {
	dir, err := os.Readlink(filepath.Join(procDir(), strconv.Itoa(pid), "cwd"))
	if err != nil {
		return ""
	}
	return strings.TrimRight(dir, "/")
}

func procDir() string {
	if p := os.Getenv(procEnv); p != "" {
		return p
	}
	return "/proc"
}

const procEnv = "AACP_PROC"
