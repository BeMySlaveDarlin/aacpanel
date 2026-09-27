package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The subscription limits of a contour are known to claude alone: a terminal
// hands them to its status line, which writes the contour's snapshot, and
// claude -p runs no status line. So a contour whose snapshot has aged is
// asked by a probe of its own: a claude -p on haiku, started in the contour,
// says one word and tells the limits of the account with the answer. It keeps
// no session and no transcript, loads no MCP server and no tool, and leaves
// the sessions of the contour alone.

// ProbeArgs are what the probe starts claude with, after the binary.
var ProbeArgs = []string{
	"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
	"--model", "haiku", "--no-session-persistence", "--strict-mcp-config", "--setting-sources", "project",
	"--max-turns", "1", "--tools", "", "--system-prompt", "Reply with one word.",
}

// probeWait is how long a probe may take: a claude starts in a second or
// two, and the answer to one word is shorter still.
const probeWait = 60 * time.Second

type limitWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *int64   `json:"resetsAt"`
}

// ProbeLimits starts a probe in dir with bin and returns the snapshot of the
// contour's limits in the form the status line writes.
func ProbeLimits(ctx context.Context, bin, dir string, now time.Time) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, probeWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, ProbeArgs...)
	cmd.Dir = dir
	cmd.Env = probeEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("the probe did not start: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	send := func(obj any) error {
		line, _ := json.Marshal(obj)
		_, err := stdin.Write(append(line, '\n'))
		return err
	}
	if err := send(map[string]any{"type": "control_request", "request_id": "limits-1",
		"request": map[string]any{"subtype": "initialize"}}); err != nil {
		return nil, err
	}
	var five, seven *limitWindow
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<16), 1<<22)
	for sc.Scan() {
		var ev struct {
			Type     string `json:"type"`
			Response struct {
				RequestID string `json:"request_id"`
			} `json:"response"`
			Info struct {
				Windows struct {
					Five  *limitWindow `json:"five_hour"`
					Seven *limitWindow `json:"seven_day"`
				} `json:"unifiedWindows"`
			} `json:"rate_limit_info"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "control_response":
			if ev.Response.RequestID == "limits-1" {
				if err := send(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "ok"}}); err != nil {
					return nil, err
				}
			}
		case "rate_limit_event":
			if ev.Info.Windows.Five != nil {
				five, seven = ev.Info.Windows.Five, ev.Info.Windows.Seven
			}
		case "result":
			if five == nil || five.Utilization == nil {
				return nil, errors.New("claude answered and said nothing of the limits")
			}
			return snapshotOf(five, seven, now), nil
		}
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("the probe did not answer in %s", probeWait)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("the probe's answer was not read: %w", err)
	}
	if tail := strings.TrimSpace(stderr.String()); tail != "" {
		line, _, _ := strings.Cut(tail, "\n")
		return nil, fmt.Errorf("the probe ended before it answered: %.300s", line)
	}
	return nil, errors.New("the probe ended before it answered")
}

// probeEnv is the environment of the probe: the wrapper of the contours picks
// the account and the directory by where it starts, so nothing of the session
// the executor runs beside is passed on.
func probeEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "CLAUDE_CODE_") || name == "CLAUDECODE" || name == "CLAUDE_CONFIG_DIR" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// snapshotOf writes the windows as the status line does: the share spent in
// percent and when the window resets, in seconds.
func snapshotOf(five, seven *limitWindow, now time.Time) []byte {
	part := func(w *limitWindow) map[string]any {
		out := map[string]any{"pct": nil, "resetsAt": nil}
		if w == nil {
			return out
		}
		if w.Utilization != nil {
			out["pct"] = math.Round(*w.Utilization*1000) / 10
		}
		if w.ResetsAt != nil {
			out["resetsAt"] = *w.ResetsAt
		}
		return out
	}
	snap, _ := json.Marshal(map[string]any{"at": now.Unix(), "fiveHour": part(five), "sevenDay": part(seven)})
	return append(snap, '\n')
}

// WriteSnapshot puts a snapshot where the collector reads it, in one rename.
func WriteSnapshot(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
