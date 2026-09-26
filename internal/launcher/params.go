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
		known, ok := schema.Find(key)
		if !ok {
			if _, gone := schema.Retire(key); !gone {
				unknown = append(unknown, key)
			}
			continue
		}
		// A parameter the host reads is not the launch's: the context guard
		// and the prompt stamp take it from what the executor keeps of the map.
		if known.Host {
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

// streamWords are the arguments of a session held on the stream protocol,
// each with the parameter that put it there. The permission prompt tool
// pointed at the host is what makes a question or a permission arrive as a
// request: without it claude refuses whatever would ask, and does not offer
// the question tool to the model at all. The opening message is not an
// argument here — the holder sends it once claude has answered the handshake.
func streamWords(name, sessionID, resume string, p Params) []schema.Word {
	var out []schema.Word
	for _, w := range []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--replay-user-messages", "--permission-prompt-tool", "stdio"} {
		out = append(out, schema.Word{Text: w, Key: keyTransport})
	}
	out = append(out, schema.Word{Text: "-n"}, schema.Word{Text: name})
	out = append(out, flagWords(p)...)
	if resume != "" {
		out = append(out, schema.Word{Text: "--resume"}, schema.Word{Text: resume})
	} else {
		out = append(out, schema.Word{Text: "--session-id"}, schema.Word{Text: sessionID})
	}
	return append(out, argWords(p)...)
}

func streamArgs(name, sessionID, resume string, p Params) []string {
	return texts(streamWords(name, sessionID, resume, p))
}

func claudeWords(name, resume string, p Params) []schema.Word {
	out := []schema.Word{{Text: "-n"}, {Text: name}}
	// Remote control is on only when the map says so: the screen shows an
	// unset switch as off, and a launch that quietly turned it on would
	// contradict what the human just read there.
	if p.RemoteControl != nil && *p.RemoteControl {
		out = append(out, schema.Word{Text: "--remote-control", Key: keyRemoteControl},
			schema.Word{Text: name, Key: keyRemoteControl})
	}
	out = append(out, flagWords(p)...)
	if resume != "" {
		out = append(out, schema.Word{Text: "--resume"}, schema.Word{Text: resume})
	}
	if p.Intent != "" {
		out = append(out, schema.Word{Text: p.Intent, Key: keyIntent})
	}
	return append(out, argWords(p)...)
}

func claudeArgs(name, resume string, p Params) []string {
	return texts(claudeWords(name, resume, p))
}

func flagWords(p Params) []schema.Word {
	var out []schema.Word
	for _, f := range []struct{ flag, value, key string }{
		{"--model", p.Model, keyModel},
		{"--effort", p.Effort, keyEffort},
		{"--permission-mode", p.PermissionMode, keyPermissionMode},
	} {
		if f.value != "" {
			out = append(out, schema.Word{Text: f.flag, Key: f.key}, schema.Word{Text: f.value, Key: f.key})
		}
	}
	return out
}

func argWords(p Params) []schema.Word {
	out := make([]schema.Word, 0, len(p.Args))
	for _, a := range p.Args {
		out = append(out, schema.Word{Text: a, Key: keyArgs})
	}
	return out
}

func texts(words []schema.Word) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, w.Text)
	}
	return out
}

// Preview returns the command the launch parameters run, word by word, with
// what the launcher would warn of — the same code that starts a session, and
// nothing is started. A session on the stream has no conversation yet, and
// says what its holder does past the handshake.
func Preview(name string, launch json.RawMessage) schema.Line {
	p, warns := parseParams(launch)
	line := schema.Line{Warnings: warns}
	if p.Transport == TransportStream {
		line.Words = append([]schema.Word{{Text: "claude"}}, streamWords(name, "<new conversation>", "", p)...)
		if p.RemoteControl != nil && *p.RemoteControl {
			line.Then = append(line.Then, schema.Word{Text: "remote control on", Key: keyRemoteControl})
		}
		if p.Intent != "" {
			line.Then = append(line.Then, schema.Word{Text: p.Intent, Key: keyIntent})
		}
		return line
	}
	line.Words = append([]schema.Word{{Text: "claude"}}, claudeWords(name, "", p)...)
	return line
}
