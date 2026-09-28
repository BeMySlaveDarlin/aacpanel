package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"aacpanel/internal/mcp"
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
	Cwd       string `json:"cwd"`
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

// Where returns where a live claude process works and the conversation it is
// in, from the file claude keeps of itself: sessions/<pid>.json in its config
// directory, which names the conversation and the directory the session runs
// in. The config directory is the one the process was started with — its own
// CLAUDE_CONFIG_DIR, read from its environment — and after it the ones the
// contours name; the collector finds the session in the same file, so the
// place it shows a checklist by is this one. The file is read anew on every call:
// /clear starts another conversation in the same process.
//
// A process with no file of itself yet — a server asked as the session
// starts — is placed by its environment and its working directory, where
// claude starts and which it writes into the file, and has no conversation.
func Where(pid int) (mcp.Binding, error) {
	var dirs []string
	add := func(dir string) {
		if dir != "" && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	environ := procEnviron(pid)
	own := envValue(environ, "CLAUDE_CONFIG_DIR")
	add(own)
	add(os.Getenv("CLAUDE_CONFIG_DIR"))
	for _, root := range projectRoots() {
		add(root)
	}
	for _, dir := range dirs {
		file, ok := sessionOf(filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json"), pid)
		if ok && file.SessionID != "" && file.Cwd != "" {
			return mcp.Binding{Place: mcp.Place{ConfigDir: dir, Dir: file.Cwd}, SessionID: file.SessionID, PID: pid}, nil
		}
	}

	cwd, err := os.Readlink(filepath.Join(procRoot(), strconv.Itoa(pid), "cwd"))
	if err != nil {
		return mcp.Binding{}, fmt.Errorf("where claude process %d works is not known: there is no live file "+
			"of its session in %s, and its working directory is not to be read", pid, strings.Join(dirs, ", "))
	}
	config := own
	if config == "" {
		config = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if config == "" {
		home := envValue(environ, "HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		config = filepath.Join(home, ".claude")
	}
	return mcp.Binding{Place: mcp.Place{ConfigDir: config, Dir: cwd}, PID: pid}, nil
}

func envValue(environ []string, name string) string {
	for _, kv := range environ {
		if value, ok := strings.CutPrefix(kv, name+"="); ok {
			return value
		}
	}
	return ""
}
