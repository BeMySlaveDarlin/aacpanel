// Package toolset is the list of tools the panel gives a session over MCP:
// the executor serves them, and the launcher allows the ones marked allowed.
// Both take the list from here, so a tool is added in one place and a
// session is never allowed a tool the server does not have.
//
// A tool lives in the package of what it works on and is added to tools; its
// package does not import the launcher, which reads this list.
package toolset

import (
	"encoding/json"
	"errors"
	"time"

	"aacpanel/internal/checklist"
	"aacpanel/internal/collector"
	"aacpanel/internal/mcp"
	"aacpanel/internal/session"
)

// lead is the server's first sentence to a session: the tools reach the
// person through the panel, and a line of every tool says when to reach for it.
const lead = "The person follows this session in the panel, on their phone and desk: " +
	"these tools reach them there, and the terminal does not show what they do."

// WordEnv and WordMax lift what claude keeps of a server's word: by default it
// keeps 2048 UTF-16 units and cuts the rest, and the lines of the last tools
// never reach the model. The launcher sets the variable for every session it
// hands the panel's tools; a session started otherwise keeps the default.
// Claude holds the description of every tool of every server to the same
// ceiling, so the variable lifts those too.
const (
	WordEnv = "CLAUDE_CODE_MAX_MCP_DESCRIPTION_LENGTH"
	WordMax = 4096
)

func tools() []mcp.Tool {
	return []mcp.Tool{
		checklist.Tool(checklist.Dir(), time.Now),
		collector.BriefPublish(collector.BriefSocket(), collector.HostGuide()),
		collector.BriefDelete(collector.BriefSocket()),
		collector.Notify(collector.NotifySocket()),
		collector.SecretAsk(collector.NotifySocket()),
		session.Restart(session.Here()),
		session.Letter(session.Here()),
		session.Open(session.Here()),
	}
}

// Server is the panel's MCP server for the claude bind finds.
func Server(bind mcp.Bind) *mcp.Server {
	return &mcp.Server{Lead: lead, Tools: tools(), Bind: bind}
}

// codexTools are the tools the panel gives a thread of codex, each bound to
// the thread the call names: its checklist is kept by the thread, its briefs,
// calls and secrets go under it. Two of a claude session's are not here. The
// restart is claude's: the daemon keeps the thread, no process of the
// panel's is there to start again, and the panel refuses it. A new session
// spends the limits of an account, and a thread is not given that besides
// what codex starts on its own.
func codexTools() []mcp.Tool {
	return []mcp.Tool{
		checklist.Tool(checklist.Dir(), time.Now),
		collector.CodexBriefPublish(collector.BriefSocket(), collector.HostGuide()),
		collector.BriefDelete(collector.BriefSocket()),
		collector.Notify(collector.NotifySocket()),
		collector.SecretAsk(collector.NotifySocket()),
		session.CodexLetter(session.Here()),
	}
}

// CodexServer is the panel's MCP server for a codex: caller finds the thread
// each call comes from, since one codex holds many threads. Nothing is known
// of the thread before a call, and the server's word says nothing of it.
func CodexServer(caller func(meta json.RawMessage) (mcp.Binding, error)) *mcp.Server {
	return &mcp.Server{Lead: lead, Tools: codexTools(),
		Bind: func() (mcp.Binding, error) {
			return mcp.Binding{}, errors.New("a codex thread is known by its call alone")
		},
		Caller: func(meta json.RawMessage) mcp.Bind {
			return func() (mcp.Binding, error) { return caller(meta) }
		},
	}
}

// CodexAllowed are the tools a thread of codex the panel starts calls without
// asking, by their names on the server: the ones a claude session is allowed.
func CodexAllowed() []string {
	var out []string
	for _, t := range codexTools() {
		if t.Allowed {
			out = append(out, t.Name)
		}
	}
	return out
}

// Allowed are the tools the launcher allows a session, by the names claude
// gives them.
func Allowed() []string {
	return Server(nil).Allowed()
}
