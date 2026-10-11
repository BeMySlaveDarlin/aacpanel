package contract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Generate has codex write the schema of its protocol and returns its version.
func Generate(ctx context.Context, codex, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, codex, "--version").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("no codex to ask: %s is not found — name the program with -codex or %s", codex, "AACP_CODEX")
		}
		return "", fmt.Errorf("%s --version: %v", codex, err)
	}
	version := strings.TrimSpace(string(out))
	cmd := exec.CommandContext(ctx, codex, "app-server", "generate-json-schema", "--experimental", "--out", dir)
	if said, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s did not write its schema: %v\n%s", version, err, said)
	}
	return version, nil
}

// Probe runs the same protocol check as cmd/codex-contract without spending tokens.
// Hook trust is checked separately against each contour after its daemon restarts.
func Probe(ctx context.Context, program string) (Report, error) {
	dir, err := os.MkdirTemp("", "codex-contract-")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(dir)
	version, err := Generate(ctx, program, dir)
	if err != nil {
		return Report{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, BundleFile))
	if err != nil {
		return Report{}, err
	}
	current, err := Load(data)
	if err != nil {
		return Report{}, err
	}
	reference, err := Reference()
	if err != nil {
		return Report{}, err
	}
	panel, err := Panel()
	if err != nil {
		return Report{}, err
	}
	report := Check(current, reference, panel)
	report.Codex = version
	return report, nil
}
