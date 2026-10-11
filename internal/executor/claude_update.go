package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"aacpanel/internal/stream/contract"
)

const ClaudeContractFlag = "-check-claude"

// CheckClaude runs in its own unit: the feed check reads only the transcripts
// of its temporary project, while the executor cannot read transcripts at all.
func CheckClaude(out io.Writer) error {
	report, _ := contract.Run(contract.Options{Claude: nativeClaude(), Model: "haiku"}, func(s string) {
		fmt.Fprintln(os.Stderr, s)
	})
	return json.NewEncoder(out).Encode(report)
}

func (e *Executor) checkClaude(ctx context.Context) (string, error) {
	version, err := programVersion(ctx, nativeClaude())
	if err != nil {
		return "", err
	}
	bin, err := launcherPath()
	if err != nil {
		return "", err
	}
	env := cleanEnv(os.Environ())
	args := launchArgs(env, bin, ClaudeContractFlag, e.cores)
	// Killing systemd-run does not stop its unit. Bound the probe and all its
	// children even when the update's client or executor goes away.
	args = append(args[:len(args)-3], "--property=KillMode=control-group", "--property=RuntimeMaxSec=12min", "--", bin, ClaudeContractFlag)
	cmd := exec.CommandContext(ctx, systemdRunPath(), args...)
	cmd.Env = env
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	err = cmd.Run()
	state := readProgram("claude")
	state.Version, state.CheckedVersion = version, version
	state.Verdict = "failed"
	if err != nil {
		state.Detail = fmt.Sprintf("Claude feed check: %v: %s", err, shortenLines(errs.String()))
	} else {
		var report contract.Report
		if err = json.Unmarshal(out.Bytes(), &report); err != nil {
			state.Detail = "Claude feed check returned an invalid report: " + err.Error()
		} else {
			state.Detail = report.Text()
			if len(report) > 0 && report.ExitCode() == 0 {
				state.Verdict = "passed"
			}
		}
	}
	err = saveProgram("claude", state)
	if state.Verdict == "failed" {
		err = errors.Join(err, fmt.Errorf("Claude %s failed the feed check: %s", version, state.Detail))
	}
	return state.Detail, err
}
