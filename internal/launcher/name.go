package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"aacpanel/internal/codex"
	"aacpanel/internal/stream"
)

const maxOrdinal = 99

func takenNames(exceptThread ...string) map[string]bool {
	out := map[string]bool{}
	for _, session := range Open() {
		out[session.Session] = true
	}
	for _, name := range tmuxSessions() {
		out[name] = true
	}
	entries, _ := os.ReadDir(stream.Dir())
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(stream.Dir(), entry.Name()))
		var state codex.State
		if err != nil || json.Unmarshal(raw, &state) != nil || state.Agent != codex.Agent ||
			slices.Contains(exceptThread, state.SessionID) {
			continue
		}
		if _, live := stream.Held(state.SessionID, 0); !live {
			continue
		}
		out[state.Name] = true
		if state.Title != "" {
			out[state.Title] = true
		}
	}
	return out
}

// FreeName shares the launcher's naming rule with daemon threads. Their
// summaries carry the titles a launcher in another process must reserve too.
func FreeName(base string) (string, error) { return freeName(base, takenNames()) }

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
