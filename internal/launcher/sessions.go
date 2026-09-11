package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func claudePIDs() []int {
	out := pidsByComm("claude")
	for _, pid := range sessionFilePIDs() {
		if !slices.Contains(out, pid) {
			out = append(out, pid)
		}
	}
	slices.Sort(out)
	return out
}

func sessionFilePIDs() []int {
	var out []int
	for _, dir := range sessionDirs() {
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
			if !liveSession(filepath.Join(dir, e.Name()), pid) {
				continue
			}
			if !slices.Contains(out, pid) {
				out = append(out, pid)
			}
		}
	}
	return out
}

func sessionDirs() []string {
	roots := projectRoots()
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		out = append(out, filepath.Join(root, "sessions"))
	}
	return out
}

func liveSession(path string, pid int) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var file struct {
		PID       int    `json:"pid"`
		Kind      string `json:"kind"`
		ProcStart string `json:"procStart"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return false
	}
	if file.PID != pid || (file.Kind != "" && file.Kind != "interactive") {
		return false
	}
	start, ok := procStart(pid)
	return ok && file.ProcStart != "" && file.ProcStart == start
}
