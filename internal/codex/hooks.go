package codex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// HookCheck is an installed command hook as its owner expects the daemon to
// see it. The key names its place in hooks.json, not its locally computed
// hash: only the daemon can say whether the trust written there still holds.
type HookCheck struct {
	Name, Key, Command string
}

// CheckHooks asks the daemon, or a private stdio app-server when its socket
// is absent, without loading a thread. The home is also the discovery directory:
// project hooks must not stand in for the hooks installed for the whole home.
// The returned text names which server answered, including on a failed check.
func CheckHooks(ctx context.Context, program, home string, want []HookCheck) (source string, err error) {
	ctx, cancel := context.WithTimeout(ctx, callWait)
	defer cancel()
	var c *conn
	_, statErr := os.Stat(SocketPath(home))
	private := errors.Is(statErr, os.ErrNotExist)
	if private {
		c, err = hookServer(ctx, program, home)
		if err != nil {
			return "", fmt.Errorf("codex could not be started: %w", err)
		}
		source = "asked a codex app-server started for the check"
	} else {
		c, err = dial(ctx, SocketPath(home), func(message) {})
		source = "asked the running daemon"
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s: %w", source, err)
		}
	}()
	if err != nil {
		return source, fmt.Errorf("codex hooks/list in %s: %w", home, err)
	}
	defer c.close()
	if _, err := initialize(ctx, c); err != nil {
		if private {
			return source, fmt.Errorf("codex could not be started: initialize failed: %w", err)
		}
		return source, err
	}
	var answer struct {
		Data []struct {
			CWD    string `json:"cwd"`
			Errors []struct {
				Path    string `json:"path"`
				Message string `json:"message"`
			} `json:"errors"`
			Hooks []struct {
				Key         string `json:"key"`
				Command     string `json:"command"`
				HandlerType string `json:"handlerType"`
				Enabled     bool   `json:"enabled"`
				TrustStatus string `json:"trustStatus"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err := c.call(ctx, "hooks/list", map[string]any{"cwds": []string{home}}, &answer); err != nil {
		return source, err
	}
	if len(answer.Data) != 1 || answer.Data[0].CWD != home {
		return source, fmt.Errorf("codex hooks/list did not return the hooks of %s", home)
	}
	entry := answer.Data[0]
	var problems []error
	for _, e := range entry.Errors {
		problems = append(problems, fmt.Errorf("codex could not load hooks from %s: %s", e.Path, e.Message))
	}
	for _, w := range want {
		found := 0
		for _, h := range entry.Hooks {
			if h.Key != w.Key {
				continue
			}
			found++
			if h.HandlerType != "command" || h.Command != w.Command {
				problems = append(problems, fmt.Errorf("codex hook %s at %s does not run the installed command", w.Name, w.Key))
			}
			if h.TrustStatus != "trusted" {
				problems = append(problems, fmt.Errorf("codex does not trust hook %s at %s (trustStatus=%q): the trust formula changed?", w.Name, w.Key, h.TrustStatus))
			}
			if !h.Enabled {
				problems = append(problems, fmt.Errorf("codex hook %s at %s is disabled", w.Name, w.Key))
			}
		}
		if found != 1 {
			problems = append(problems, fmt.Errorf("codex hooks/list lists hook %s at %s %d times, want once", w.Name, w.Key, found))
		}
	}
	return source, errors.Join(problems...)
}

// hookServer belongs only to this check: closing stdin lets codex exit, and
// the context kills it if it stops answering or refuses to exit. Waiting reaps
// it before the caller returns. No shared daemon or socket is started here.
func hookServer(ctx context.Context, program, home string) (*conn, error) {
	path, err := exec.LookPath(program)
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "app-server", "--stdio")
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, err
	}
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 4096), readLimit)
	c := &conn{
		receive: func() ([]byte, error) {
			if lines.Scan() {
				return lines.Bytes(), nil
			}
			if err := lines.Err(); err != nil {
				return nil, err
			}
			return nil, io.EOF
		},
		write: func(_ context.Context, body []byte) error {
			_, err := in.Write(append(body, '\n'))
			return err
		},
		disconnect: func() {
			_ = in.Close()
			_ = cmd.Wait()
		},
		handle: func(message) {}, calls: map[int64]chan message{}, closed: make(chan struct{}),
	}
	go c.read()
	return c, nil
}
