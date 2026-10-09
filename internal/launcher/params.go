package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"aacpanel/internal/mcp"
	"aacpanel/internal/schema"
	"aacpanel/internal/toolset"
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
	keyPanelTools     = "panelTools"
	keyAgent          = "agent"
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
	// PanelTools is off only where the map says so: the panel's default is on.
	PanelTools *bool
	// Agent is what the project starts: claude where the map says nothing.
	// This launcher starts claude alone, and refuses a new conversation of a
	// project whose agent is codex rather than start claude in its place.
	Agent string

	// tools is the MCP configuration that hands the session the panel's
	// tools. The launch sets it, not the map: it names the executor's own
	// path on the host, which a preview drawn in the panel's container does
	// not know.
	tools string

	// chrome hands a session on the stream Claude in Chrome. The launch sets
	// it from the account, not the map: claude in a terminal turns Chrome on
	// by the account's own setting, while claude on the stream runs
	// non-interactive, where that setting is not read and only the flag
	// turns it on.
	chrome bool
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
		// A parameter of codex is not claude's launch to read.
		if known.Host || known.Agent == schema.AgentCodex {
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
		case keyAgent:
			str(key, &p.Agent)
			if p.Agent != schema.AgentClaude && p.Agent != schema.AgentCodex {
				warns = append(warns, fmt.Sprintf("parameter agent is %q, neither %q nor %q — the session starts claude",
					p.Agent, schema.AgentClaude, schema.AgentCodex))
				p.Agent = ""
			}
		case keyTransport:
			str(key, &p.Transport)
			if p.Transport != TransportTmux && p.Transport != TransportStream {
				warns = append(warns, fmt.Sprintf("parameter transport is %q, neither %q nor %q — the session goes to tmux",
					p.Transport, TransportTmux, TransportStream))
				p.Transport = ""
			}
		case keyRemoteControl, keyPanelTools:
			var on bool
			if err := json.Unmarshal(obj[key], &on); err != nil {
				warns = append(warns, fmt.Sprintf("parameter %s is not true/false — skipped", key))
				break
			}
			if key == keyPanelTools {
				p.PanelTools = &on
			} else {
				p.RemoteControl = &on
			}
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
	out = append(out, toolWords(p)...)
	if p.chrome {
		out = append(out, schema.Word{Text: "--chrome"})
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
	out := toolWords(p)
	out = append(out, schema.Word{Text: "-n"}, schema.Word{Text: name})
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

// toolWords hand a session the panel's tools: the executor as the panel's MCP
// server, and the tools the list marks allowed named to claude as allowed, so
// a call of one never waits on a person. Both flags take a list of values, so
// the words go before the session's name: the name is a flag and ends the
// list, where a prompt after them would be read as one more server.
func toolWords(p Params) []schema.Word {
	if p.tools == "" {
		return nil
	}
	out := []schema.Word{{Text: "--mcp-config", Key: keyPanelTools}, {Text: p.tools, Key: keyPanelTools}}
	allowed := toolset.Allowed()
	if len(allowed) == 0 {
		return out
	}
	out = append(out, schema.Word{Text: "--allowedTools", Key: keyPanelTools})
	for _, name := range allowed {
		out = append(out, schema.Word{Text: name, Key: keyPanelTools})
	}
	return out
}

// toolsOn says whether the launch hands the session the panel's tools.
func toolsOn(p Params) bool { return p.PanelTools == nil || *p.PanelTools }

// toolServer is how a session reaches the panel's tools: this very binary as
// the panel's MCP server. A test puts a path of its own here.
var toolServer = os.Executable

// toolsPreview stands in a preview for the configuration a launch writes.
const toolsPreview = "<the panel's tools>"

// withTools puts the configuration of the panel's tools into the parameters
// of a launch, or says why the session starts without them.
func withTools(p Params) (Params, string) {
	if !toolsOn(p) {
		return p, ""
	}
	path, err := toolServer()
	if err != nil {
		return p, "the session starts without the panel's tools: the executor does not know its own path: " + err.Error()
	}
	config, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		mcp.ServerName: map[string]any{"type": "stdio", "command": path, "args": []string{mcp.Flag}},
	}})
	if err != nil {
		return p, "the session starts without the panel's tools: " + err.Error()
	}
	p.tools = string(config)
	return p, ""
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
	if toolsOn(p) {
		p.tools = toolsPreview
	}
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
