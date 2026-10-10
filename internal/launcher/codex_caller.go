package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"aacpanel/internal/codex"
	"aacpanel/internal/mcp"
)

// What codex gives the panel's server: a server of its own for every thread
// it holds, started in the directory the thread works in, with an environment
// of codex's making — HOME, PATH, the locale and little more, no CODEX_HOME
// and no word of the thread — and the thread named in the _meta of every call
// of a tool, as threadId. A daemon holds many threads in one process, so the
// parent of a server does not tell the thread; the call does. The codex that
// writes a thread holds a lock on it, a file named by the thread under its
// home, so the lock in the parent's open files says the thread is one that
// codex runs, and where its home is.

// codexProgram is what codex calls its process, whoever started it.
const codexProgram = "codex"

// codexLocks is the directory of a codex home that keeps a lock per thread
// being written.
const codexLocks = "thread-writer-locks"

// IsCodex says whether a process is codex.
func IsCodex(pid int) bool { return procComm(pid) == codexProgram }

// CodexCaller finds the codex thread one call of the panel's server comes
// from: the thread the call names, which the codex that started the server
// must hold the lock of. The thread is not taken on the call's word alone,
// and a call that names none has no caller: one process serves many threads.
// The directory is the one codex started the server in.
func CodexCaller(parent int, meta json.RawMessage) (mcp.Binding, error) {
	var said struct {
		Thread string `json:"threadId"`
	}
	if len(meta) > 0 && json.Unmarshal(meta, &said) != nil {
		said.Thread = ""
	}
	if said.Thread == "" {
		return mcp.Binding{}, errors.New("the call names no codex thread: codex names the thread in the _meta of " +
			"every call, and a call without it has no sender")
	}
	if !uuidRE.MatchString(said.Thread) {
		return mcp.Binding{}, fmt.Errorf("the call names thread %q, and a codex thread is named by a uuid", said.Thread)
	}
	home, ok := codexHomeOf(parent, said.Thread)
	if !ok {
		return mcp.Binding{}, fmt.Errorf("codex process %d holds no thread %s: the call names a thread "+
			"of another codex", parent, said.Thread)
	}
	dir, err := os.Getwd()
	if err != nil {
		return mcp.Binding{}, fmt.Errorf("the directory thread %s works in is not known: %w", said.Thread, err)
	}
	return mcp.Binding{Place: mcp.Place{ConfigDir: home, Dir: dir}, Name: codex.SessionName(said.Thread),
		SessionID: said.Thread, PID: parent, Codex: true}, nil
}

// codexHomeOf is the home of a thread a codex process holds the lock of.
func codexHomeOf(pid int, thread string) (string, bool) {
	fds := filepath.Join(procRoot(), strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fds)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(fds, e.Name()))
		if err != nil || filepath.Base(target) != thread+".lock" {
			continue
		}
		if locks := filepath.Dir(target); filepath.Base(locks) == codexLocks && filepath.IsAbs(locks) {
			return filepath.Dir(locks), true
		}
	}
	return "", false
}
