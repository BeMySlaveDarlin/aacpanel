// Package rundir finds the user's runtime directory, where the executor and
// the processes it talks to meet on their sockets. Every side of a socket
// takes the path from here: two rules for one place give a client that dials
// where nobody listens.
package rundir

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// Path is name, and elem under it, in the user's runtime directory:
// $XDG_RUNTIME_DIR, or /run/user/<uid> when the variable is not set and that
// directory is the user's own. The second place matters for a process started
// with a trimmed environment: codex starts its MCP servers with a short list
// of variables, without XDG_RUNTIME_DIR, while the executor it calls listens
// under the runtime directory. Without either, the path is under the
// temporary directory, with the uid in the name of the first element.
func Path(name string, elem ...string) string {
	return pathIn(os.Getenv, "/run/user", os.Getuid(), name, elem...)
}

func pathIn(get func(string) string, runUser string, uid int, name string, elem ...string) string {
	if base := runtimeDir(get, runUser, uid); base != "" {
		return filepath.Join(append([]string{base, name}, elem...)...)
	}
	return filepath.Join(append([]string{os.TempDir(), fmt.Sprintf("%s-%d", name, uid)}, elem...)...)
}

func runtimeDir(get func(string) string, runUser string, uid int) string {
	if base := get("XDG_RUNTIME_DIR"); base != "" {
		return base
	}
	dir := filepath.Join(runUser, strconv.Itoa(uid))
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return ""
	}
	return dir
}
