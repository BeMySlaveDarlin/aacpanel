package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
	"aacpanel/internal/schema"
)

// OpenName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__session_open.
const OpenName = "session_open"

// OpenInstructions is the tool's line in the server's word to every session
// that has it.
const OpenInstructions = "Use session_open only when asked for a new session or a resumed conversation " +
	"(«открой сессию в …», «подними сессию»); send its task with send_to_session after it opens."

// helperCycle is shared by the three tools so each names the whole channel.
const helperCycle = " Open with agent codex, then send the task with send_to_session: its last message comes back " +
	"as a letter after every turn, including failure or interruption. Reply by letter to continue with the same " +
	"agent; session_close with its name closes it."

// OpenDescription is the tool's own word to the model.
const OpenDescription = openDescription + openRole + helperCycle

// openRole says what the role of the session opened is: its word about which
// work of the repository is its, told to it before its first word.
const openRole = " role, at most 500 characters, is told to the session as its role in its project."

const openDescription = "Opens only on the person's request: another session spends its account's limits. " +
	"Give absolute dir in a mapped project or its worktree; outside the map it is not opened. Optional name " +
	"overrides the map's; a busy name gets a free one. To resume a closed Claude conversation, give resume, " +
	"its id; dir may be omitted or belong to that project, and agent and Codex options are forbidden. A " +
	"same-project session opens under the caller. Account and launch come from the map; agent and Codex " +
	"model, effort, sandbox and writableRoots override this start only. Codex uses approvalPolicy never, " +
	"workspace-write with network on unless read-only is chosen; writableRoots adds absolute paths only " +
	"with workspace-write. For an existing session use send_to_session, for an internal subagent use the " +
	"client's agent tools. Tell the person the returned name and account, then send the task: immediately " +
	"for Codex, after Claude appears in send_to_session's list. If the outcome is unknown, check that list " +
	"before retrying. Continue independent work or end the turn for the report. Reports reach a live stream " +
	"or daemon parent; tmux or gone parents get none. Long reports arrive as file paths."

// openWait bounds the wait for the panel's answer. The panel answers once the
// claude of the new session is up: the launcher waits a quarter of a minute
// for it, after a check of the permission mode of up to five seconds, and the
// holder of a session on the stream is started, not waited for. The panel
// gives the executor two minutes; a session not up by this wait is coming up
// late or not at all, and which is not known here.
var openWait = time.Minute

// Open is the tool that opens a new session on the host h. It is not allowed:
// a new session starts work in its account and spends its limits, and the
// question claude asks before the call is the person's word to open it.
func Open(h Host) mcp.Tool {
	return mcp.Tool{
		Name:         OpenName,
		Title:        "Open a new session",
		Description:  OpenDescription,
		InputSchema:  openSchema(),
		Instructions: OpenInstructions,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return open(ctx, h, bind, args)
		},
	}
}

// A Codex server is shared by a daemon: a call without a proven thread may
// not open a helper on behalf of an unknown conversation.
func CodexOpen(h Host) mcp.Tool {
	t := Open(h)
	t.Call = func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
		if _, err := bind(); err != nil {
			return "Nothing was opened: " + err.Error(), true
		}
		return open(ctx, h, bind, args)
	}
	return t
}

// openSchema takes no first message: words the tool typed into the new
// session would reach its model as its person's. Codex choices belong to this
// start alone, leaving the project's launch on the map intact. A resume needs
// no directory, since the conversation goes on where it ran, so the schema
// requires nothing and the call checks that it names dir or resume.
func openSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir": map[string]any{"type": "string", "minLength": 1,
				"description": "The absolute directory to open the session in: a project of the map, a directory inside one or a worktree of one. Needed unless resume is given."},
			"name": map[string]any{"type": "string",
				"description": "The name of the new session. Left out, the project's session name from the map, or for resume the name the conversation had; when a live session answers to the name, the panel takes a free one."},
			"resume": map[string]any{"type": "string",
				"description": "The id of a closed Claude conversation to go on with instead of a new one. No agent or Codex options with it."},
			"role": map[string]any{"type": "string", "maxLength": role.MaxText,
				"description": "The role of the new session in its project: which work of the repository is its. " +
					"Left out, a session keeps the role its name has; empty, it clears it."},
			"agent": map[string]any{"type": "string", "enum": []string{schema.AgentClaude, schema.AgentCodex},
				"description": "The agent to start: claude or codex. Left out, the project's agent from the map. codex starts only where the project's account has a codex home on this machine."},
			"model":   map[string]any{"type": "string", "description": "Codex model for this start; otherwise the map's."},
			"effort":  map[string]any{"type": "string", "enum": []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
			"sandbox": map[string]any{"type": "string", "enum": []string{"workspace-write", "read-only"}},
			"writableRoots": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "Extra absolute host directories writable by this Codex helper; only with workspace-write."},
		},
		"additionalProperties": false,
	}
}

// open asks the panel to open or resume a session.
func open(ctx context.Context, h Host, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Dir    string  `json:"dir"`
		Name   string  `json:"name"`
		Agent  string  `json:"agent"`
		Resume string  `json:"resume"`
		Role   *string `json:"role"`
		action.CodexLaunch
	}
	if len(raw) > 0 {
		read := json.NewDecoder(bytes.NewReader(raw))
		read.DisallowUnknownFields()
		if err := read.Decode(&args); err != nil {
			return "Nothing was opened: the arguments are not the tool's (" + err.Error() + ").", true
		}
	}
	if err := args.CodexLaunch.Validate(); err != nil {
		return "Nothing was opened: " + err.Error() + ".", true
	}
	if args.Role != nil {
		text, err := role.Clean(*args.Role)
		if err != nil {
			return "Nothing was opened: " + err.Error() + ".", true
		}
		args.Role = &text
	}
	hasCodex := args.Model != "" || args.Effort != "" || args.Sandbox != "" || args.WritableRoots != nil
	if hasCodex && args.Agent == schema.AgentClaude {
		return "Nothing was opened: model, effort, sandbox and writableRoots are Codex options.", true
	}
	kind := action.SessionOpen
	if args.Resume != "" {
		if args.Agent != "" || hasCodex {
			return "Nothing was opened: resume goes on with a Claude conversation as the map launches it; agent, " +
				"model, effort, sandbox and writableRoots are for a new session.", true
		}
		kind = action.SessionResume
		probe := action.Request{ID: "open", Kind: kind, Target: "open", Resume: args.Resume}
		if err := probe.Validate(); err != nil {
			return "Nothing was opened: " + err.Error() + ".", true
		}
	} else if args.Dir == "" {
		return "Nothing was opened: name dir, the directory of a project of the map to open a session in, or " +
			"resume, the conversation to go on with.", true
	}
	if args.Dir != "" && !filepath.IsAbs(args.Dir) {
		return fmt.Sprintf("Nothing was opened: dir %q is not an absolute directory — name the directory of a "+
			"project of the map.", args.Dir), true
	}
	if args.Name != "" {
		at := args.Dir
		if at == "" {
			at = "/"
		}
		req := action.Request{ID: "open", Kind: action.SessionOpen, Target: args.Name,
			Project: &action.Project{Path: at, Session: args.Name}}
		if err := req.Validate(); err != nil {
			return "Nothing was opened: " + err.Error() + ".", true
		}
	}
	if args.Agent != "" && args.Agent != schema.AgentClaude && args.Agent != schema.AgentCodex {
		return fmt.Sprintf("Nothing was opened: agent %q is neither %s nor %s.", args.Agent, schema.AgentClaude,
			schema.AgentCodex), true
	}
	params := map[string]any{}
	if args.Dir != "" {
		params["path"] = args.Dir
	}
	// The name goes along even when empty: empty, the session comes back under
	// the name the conversation had.
	if kind == action.SessionResume {
		params["session"], params["name"] = args.Resume, args.Name
	}
	if args.Agent != "" {
		params["agent"] = args.Agent
	}
	if args.Role != nil {
		params["role"] = *args.Role
	}
	if parent := callerName(bind); parent != "" {
		params["parent"] = parent
		if b, err := bind(); err == nil && b.SessionID != "" {
			params["openedBy"] = b.SessionID
		}
	}
	if hasCodex {
		params["codex"] = args.CodexLaunch
	}
	got, err := h.act(ctx, string(kind), args.Name, params, openWait)
	switch {
	case err != nil:
		return "Nothing was opened: " + err.Error() + ".", true
	case got.Late:
		return "The panel did not answer within " + openWait.String() + ": whether the session came up is not " +
			"known. Do not open it again blindly — look for it in the list of send_to_session in a minute.", true
	case !got.Taken:
		return "Nothing was opened: " + got.Said, true
	}
	what := "opened a session in " + args.Dir
	if kind == action.SessionResume {
		what = "resumed conversation " + args.Resume
	}
	return "The panel " + what + ", account " + orUnknown(got.Contour) + ": " + got.Said +
		". Tell the person its name and account. A Codex thread can take send_to_session immediately; for Claude " +
		"wait until it shows in that tool's list. Reports go only to the opening conversation, while it lives " +
		"on the Claude stream or in a Codex daemon; tmux and gone parents get none." + helperCycle, false
}

// callerName is the name of the session that calls the tool, the parent the
// new session is shown under in the panel while the two are of one project of
// the map, which the panel decides, since the map is there. A caller whose
// place is not known, one started without a name and one whose name the panel
// would not take as a session's open the session all the same, with no
// parent: where the new one stands in a list is no reason to refuse the open.
func callerName(bind mcp.Bind) string {
	if bind == nil {
		return ""
	}
	b, err := bind()
	if err != nil || b.Name == "" {
		return ""
	}
	probe := action.Request{ID: "open", Kind: action.SessionOpen, Target: "open",
		Project: &action.Project{Path: "/", Session: "open", Parent: b.Name}}
	if probe.Validate() != nil {
		return ""
	}
	return b.Name
}
