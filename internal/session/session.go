// Package session holds the tools of the panel's server a session acts with
// on itself and on the sessions next to it: its own restart, a letter to
// another live session of the machine, and a new session opened as a project
// of the map. They go through the panel, which runs them as actions of this
// machine and puts them in its journal; what the machine holds, they read from
// the collector's snapshot.
package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
)

// PanelEnv names the address of the panel's local listener, which takes the
// actions of this machine without a login.
const PanelEnv = "AACP_PANEL_URL"

const defaultPanel = "http://127.0.0.1:8777"

// Host is where the tools find the machine: the snapshot the collector writes
// and the panel's local listener, with the clock the waits are measured by.
type Host struct {
	// State is the collector's snapshot, state.json of the state directory.
	State string
	// Panel is the address of the local listener.
	Panel string
	Now   func() time.Time
	Sleep func(time.Duration)
}

// Here is the host this process runs on: the state directory of its
// description and the listener its environment names.
func Here() Host {
	panel := os.Getenv(PanelEnv)
	if panel == "" {
		panel = defaultPanel
	}
	return Host{
		State: filepath.Join(hostcfg.Load().StateDir, "state.json"),
		Panel: panel,
		Now:   time.Now,
		Sleep: time.Sleep,
	}
}

// row is a live session as the snapshot has it. Agent is "codex" for a codex
// thread, which Outside marks as running on its own, out of the panel's
// reach, and Title names as the panel does.
type row struct {
	Name      string
	SessionID string
	Profile   string
	CWD       string
	Work      json.RawMessage
	Agent     string
	Outside   bool
	Title     string
}

// snapshot is what the tools read of the collector's snapshot: the live
// sessions and when they were read, in seconds of Unix time.
type snapshot struct {
	Sessions []row
	At       float64
	Dated    bool
}

// read reads the snapshot, or reports it is not there. It is read field by
// field, the way the scripts of the delivery read it: a row that is not an
// object is passed over, a field of another type is no field, and a time
// that is not a number is no time.
func (h Host) read() (snapshot, error) {
	raw, err := os.ReadFile(h.State)
	if err != nil {
		return snapshot{}, err
	}
	var file struct {
		Sessions   []json.RawMessage `json:"sessions"`
		SessionsAt json.RawMessage   `json:"sessionsAt"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return snapshot{}, fmt.Errorf("the snapshot %s is not read: %w", h.State, err)
	}
	var out snapshot
	for _, one := range file.Sessions {
		var fields map[string]json.RawMessage
		if json.Unmarshal(one, &fields) != nil || fields == nil {
			continue
		}
		out.Sessions = append(out.Sessions, row{
			Name: text(fields["session"]), SessionID: text(fields["sessionId"]),
			Profile: text(fields["profile"]), CWD: text(fields["cwd"]), Work: fields["work"],
			Agent: text(fields["agent"]), Outside: string(bytes.TrimSpace(fields["outside"])) == "true",
			Title: text(fields["title"]),
		})
	}
	if json.Unmarshal(file.SessionsAt, &out.At) == nil && string(bytes.TrimSpace(file.SessionsAt)) != "null" {
		out.Dated = true
	}
	return out, nil
}

// text is a string field, or nothing when the field is not a string.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// answer is what the panel said to an action.
type answer struct {
	// Taken is an action the panel ran, or is running past the wait.
	Taken bool
	// Late is an action the panel did not answer within the wait.
	Late bool
	// Said is the panel's detail, or its refusal.
	Said string
	// Contour is the account of the map the panel ran the action in, where it
	// names one.
	Contour string
}

// act asks the panel to run an action on this machine, waiting for its answer
// up to wait. A refusal is an answer, not an error; the error is a panel that
// could not be asked at all.
func (h Host) act(ctx context.Context, kind, target string, params map[string]any, wait time.Duration) (answer, error) {
	body, err := json.Marshal(map[string]any{"kind": kind, "target": target, "params": params})
	if err != nil {
		return answer{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.Panel, "/")+"/api/actions",
		bytes.NewReader(body))
	if err != nil {
		return answer{}, fmt.Errorf("the panel's address %q is not one: %w", h.Panel, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			return answer{Taken: true, Late: true}, nil
		}
		return answer{}, fmt.Errorf("the panel does not answer at %s: %w", h.Panel, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var reply struct {
		Detail  string `json:"detail"`
		Error   string `json:"error"`
		Contour string `json:"contour"`
	}
	_ = json.Unmarshal(raw, &reply)
	if resp.StatusCode != http.StatusOK {
		said := reply.Error
		if said == "" {
			said = strings.TrimSpace(string(raw))
		}
		if said == "" {
			said = resp.Status
		}
		return answer{Said: said}, nil
	}
	return answer{Taken: true, Said: reply.Detail, Contour: reply.Contour}, nil
}
