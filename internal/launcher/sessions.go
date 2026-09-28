package launcher

import (
	"encoding/json"
	"fmt"
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
	_, ok := sessionOf(path, pid)
	return ok
}

// sessionFile is what the launcher reads of the file claude keeps of a live
// session.
type sessionFile struct {
	PID       int    `json:"pid"`
	Kind      string `json:"kind"`
	ProcStart string `json:"procStart"`
	SessionID string `json:"sessionId"`
}

// sessionOf reads the file of a session, and only as that process's: a file
// born with another process is one a dead claude with the same pid left.
func sessionOf(path string, pid int) (sessionFile, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return sessionFile{}, false
	}
	var file sessionFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return sessionFile{}, false
	}
	if file.PID != pid || (file.Kind != "" && file.Kind != "interactive") {
		return sessionFile{}, false
	}
	start, ok := procStart(pid)
	return file, ok && file.ProcStart != "" && file.ProcStart == start
}

// Conversation returns the conversation a live claude process is in, from
// the file claude keeps of itself: sessions/<pid>.json in its config
// directory. That directory is the one the process was started with — its
// own CLAUDE_CONFIG_DIR, read from its environment — and after it the ones
// the contours name. The id is read anew on every call: /clear starts another
// conversation in the same process.
func Conversation(pid int) (string, error) {
	var dirs []string
	add := func(dir string) {
		if dir != "" && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	for _, kv := range procEnviron(pid) {
		if dir, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok {
			add(dir)
		}
	}
	add(os.Getenv("CLAUDE_CONFIG_DIR"))
	for _, root := range projectRoots() {
		add(root)
	}
	for _, dir := range dirs {
		file, ok := sessionOf(filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json"), pid)
		if ok && file.SessionID != "" {
			return file.SessionID, nil
		}
	}
	return "", fmt.Errorf("the conversation of claude process %d is not known: "+
		"there is no live file of its session in %s", pid, strings.Join(dirs, ", "))
}
