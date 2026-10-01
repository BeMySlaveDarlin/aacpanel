package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

// OpenName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__session_open.
const OpenName = "session_open"

// OpenInstructions is the tool's line in the server's word to every session
// that has it.
const OpenInstructions = "When the person asks for a new session («открой сессию в …», «подними сессию»), " +
	"and only then, use session_open."

// OpenDescription is the tool's own word to the model.
const OpenDescription = "Opens a new Claude session of this machine through the panel, as a project from the map. " +
	"dir names the project: its own directory, one inside it or a worktree of it, and the session runs there; " +
	"a directory no project of the map holds is not opened. The account, the launch parameters — tmux or the " +
	"stream, the model — and the project's first message come from the map. Only on the person's word: a new " +
	"session works in its account and spends that account's limits, and this session does not decide on one by " +
	"itself. Tell the person the name the session came up under and its account. The tool takes no words for " +
	"the new session: words typed into a session its model takes for its person's. To give it work, write it a " +
	"letter with send_to_session once it shows in that tool's list."

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
		Call: func(ctx context.Context, _ mcp.Bind, args json.RawMessage) (string, bool) {
			return open(ctx, h, args)
		},
	}
}

// openSchema takes no first message: words the tool typed into the new
// session would reach its model as its person's.
func openSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir": map[string]any{"type": "string", "minLength": 1,
				"description": "The absolute directory to open the session in: a project of the map, a directory inside one or a worktree of one."},
			"name": map[string]any{"type": "string",
				"description": "The name of the new session. Left out, the project's session name from the map; when a live session answers to the name, the panel takes a free one."},
		},
		"required":             []string{"dir"},
		"additionalProperties": false,
	}
}

func open(ctx context.Context, h Host, raw json.RawMessage) (string, bool) {
	var args struct {
		Dir  string `json:"dir"`
		Name string `json:"name"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "Nothing was opened: the arguments are not the tool's (" + err.Error() + ").", true
		}
	}
	if !filepath.IsAbs(args.Dir) {
		return fmt.Sprintf("Nothing was opened: dir %q is not an absolute directory — name the directory of a "+
			"project of the map.", args.Dir), true
	}
	if args.Name != "" {
		req := action.Request{ID: "open", Kind: action.SessionOpen, Target: args.Name,
			Project: &action.Project{Path: args.Dir, Session: args.Name}}
		if err := req.Validate(); err != nil {
			return "Nothing was opened: " + err.Error() + ".", true
		}
	}
	got, err := h.act(ctx, string(action.SessionOpen), args.Name, map[string]any{"path": args.Dir}, openWait)
	switch {
	case err != nil:
		return "Nothing was opened: " + err.Error() + ".", true
	case got.Late:
		return "The panel did not answer within " + openWait.String() + ": whether the session came up is not " +
			"known. Do not open it again blindly — look for it in the list of send_to_session in a minute.", true
	case !got.Taken:
		return "Nothing was opened: " + got.Said, true
	}
	return "The panel opened a session in " + args.Dir + ", account " + orUnknown(got.Contour) + ": " + got.Said +
		". Tell the person its name and account. To give it work, write it a letter with send_to_session once it " +
		"shows in that tool's list.", false
}
