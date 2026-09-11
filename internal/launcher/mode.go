package launcher

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const modeProbeWait = 5 * time.Second

const modeSaidMax = 300

func checkPermissionMode(ctx context.Context, dir, bin, mode string) (string, error) {
	if mode == "" {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(ctx, modeProbeWait)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--permission-mode", mode, "--version")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return "", nil
	}

	var exit *exec.ExitError
	if errors.As(err, &exit) && strings.Contains(string(out), "--permission-mode") {
		return "", fmt.Errorf(
			"claude did not accept permission mode %q: %s. Fix it in the profile map — "+
				"with this value the session does not start at all", mode, said(out))
	}
	if s := said(out); s != "" {
		return fmt.Sprintf("permission mode %q was not checked: claude did not answer (%v: %s)", mode, err, s), nil
	}
	return fmt.Sprintf("permission mode %q was not checked: claude did not answer (%v)", mode, err), nil
}

func said(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), ".")
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > modeSaidMax {
			return string(r[:modeSaidMax]) + "…"
		}
		return line
	}
	return ""
}
