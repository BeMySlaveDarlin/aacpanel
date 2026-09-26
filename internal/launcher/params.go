package launcher

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"aacpanel/internal/schema"
)

const (
	keyModel          = "model"
	keyEffort         = "effort"
	keyPermissionMode = "permissionMode"
	keyRemoteControl  = "remoteControl"
	keyFinalizeAt     = "finalizeAt"
	keyEnv            = "env"
	keyArgs           = "args"
	keyIntent         = "intent"
	keyTransport      = "transport"
)

// How a session is kept. A terminal in tmux is the default; the stream is
// `claude -p` on stream-json under a holder, answered with structure rather
// than keystrokes.
const (
	TransportTmux   = "tmux"
	TransportStream = "stream"
)

// Params is what makes one launch differ from another.
type Params struct {
	Model          string
	Effort         string
	PermissionMode string
	RemoteControl  *bool
	FinalizeAt     int
	Env            map[string]string
	Args           []string
	Intent         string
	Transport      string
}

func parseParams(raw json.RawMessage) (Params, []string) {
	var p Params
	if len(raw) == 0 {
		return p, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return p, []string{"launch parameters were not parsed: " + err.Error()}
	}

	var warns []string
	str := func(key string, dst *string) {
		if err := json.Unmarshal(obj[key], dst); err != nil {
			warns = append(warns, fmt.Sprintf("parameter %s is not a string — skipped", key))
		}
	}

	var unknown []string
	for key := range obj {
		// The schema is the list of keys: a key it does not hold is unknown,
		// and a retired one is passed over in silence — scripts outside the
		// panel still send it, and it asks nothing of the launch.
		if _, ok := schema.Find(key); !ok {
			if _, gone := schema.Retire(key); !gone {
				unknown = append(unknown, key)
			}
			continue
		}
		switch key {
		case keyModel:
			str(key, &p.Model)
		case keyEffort:
			// The efforts of the schema are the ones claude takes at launch.
			// Ultracode is not among them: claude drops it with a line on the
			// terminal and starts at its default effort, so the launcher names
			// it instead of passing it on — a live session takes it from the
			// composer.
			str(key, &p.Effort)
			if effort, _ := schema.Find(key); p.Effort != "" && !effort.Offers(p.Effort) {
				warns = append(warns, fmt.Sprintf("parameter effort is %q, which claude does not take at launch — "+
					"the session starts at its default effort", p.Effort))
				p.Effort = ""
			}
		case keyPermissionMode:
			str(key, &p.PermissionMode)
		case keyIntent:
			str(key, &p.Intent)
		case keyTransport:
			str(key, &p.Transport)
			if p.Transport != TransportTmux && p.Transport != TransportStream {
				warns = append(warns, fmt.Sprintf("parameter transport is %q, neither %q nor %q — the session goes to tmux",
					p.Transport, TransportTmux, TransportStream))
				p.Transport = ""
			}
		case keyRemoteControl:
			var on bool
			if err := json.Unmarshal(obj[key], &on); err != nil {
				warns = append(warns, "parameter remoteControl is not true/false — skipped")
				break
			}
			p.RemoteControl = &on
		case keyFinalizeAt:
			var at int
			if err := json.Unmarshal(obj[key], &at); err != nil {
				warns = append(warns, "parameter finalizeAt is not a whole number — skipped")
				break
			}
			if at < 1 || at > 99 {
				warns = append(warns, fmt.Sprintf("parameter finalizeAt is %d, outside 1–99 — skipped", at))
				break
			}
			p.FinalizeAt = at
		case keyEnv:
			if err := json.Unmarshal(obj[key], &p.Env); err != nil {
				p.Env = nil
				warns = append(warns, "parameter env is not an object of strings — skipped")
			}
		case keyArgs:
			if err := json.Unmarshal(obj[key], &p.Args); err != nil {
				p.Args = nil
				warns = append(warns, "parameter args is not a list of strings — skipped")
			}
		default:
			warns = append(warns, fmt.Sprintf("parameter %s is in the schema, but the launcher has no use for it — skipped", key))
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		warns = append(warns, "the launcher does not know these parameters: "+strings.Join(unknown, ", "))
	}
	slices.Sort(warns)
	return p, warns
}

// streamArgs are the arguments of a session held on the stream protocol. The
// permission prompt tool pointed at the host is what makes a question or a
// permission arrive as a request: without it claude refuses whatever would
// ask, and does not offer the question tool to the model at all. The opening
// message is not an argument here — the holder sends it once claude has
// answered the handshake.
func streamArgs(name, sessionID, resume string, p Params) []string {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--replay-user-messages", "--permission-prompt-tool", "stdio", "-n", name}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	if p.Effort != "" {
		args = append(args, "--effort", p.Effort)
	}
	if p.PermissionMode != "" {
		args = append(args, "--permission-mode", p.PermissionMode)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	} else {
		args = append(args, "--session-id", sessionID)
	}
	return append(args, p.Args...)
}

func claudeArgs(name, resume string, p Params) []string {
	args := []string{"-n", name}
	// Remote control is on only when the map says so: the screen shows an
	// unset switch as off, and a launch that quietly turned it on would
	// contradict what the human just read there.
	if p.RemoteControl != nil && *p.RemoteControl {
		args = append(args, "--remote-control", name)
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	if p.Effort != "" {
		args = append(args, "--effort", p.Effort)
	}
	if p.PermissionMode != "" {
		args = append(args, "--permission-mode", p.PermissionMode)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	if p.Intent != "" {
		args = append(args, p.Intent)
	}
	return append(args, p.Args...)
}
