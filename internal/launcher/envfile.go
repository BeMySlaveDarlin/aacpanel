package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sessionEnv struct {
	vars []string
	dir  string
	path string
}

const envDirPrefix = "aacpanel-launch-"

func writeSessionEnv(vars []string) (*sessionEnv, []string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	dir, err := os.MkdirTemp(base, envDirPrefix)
	if err != nil {
		return nil, nil, fmt.Errorf("could not create the directory for the session environment: %w", err)
	}

	var warns []string
	var b strings.Builder
	b.WriteString("# aacpanel session environment: read once and removed\n")
	b.WriteString("rm -f \"$0\" 2>/dev/null\n")
	b.WriteString("exec env -i \\\n")
	for _, kv := range vars {
		if strings.ContainsRune(kv, 0) {
			name, _, _ := strings.Cut(kv, "=")
			warns = append(warns, fmt.Sprintf(
				"variable %s did not reach the session: its value contains a NUL byte", name))
			continue
		}
		b.WriteString(shellQuote(kv) + " \\\n")
	}
	b.WriteString("\"$@\"\n")

	path := filepath.Join(dir, "env.sh")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("session environment was not written: %w", err)
	}
	return &sessionEnv{vars: vars, dir: dir, path: path}, warns, nil
}

func (e *sessionEnv) remove() {
	if e == nil || e.dir == "" {
		return
	}
	_ = os.RemoveAll(e.dir)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
