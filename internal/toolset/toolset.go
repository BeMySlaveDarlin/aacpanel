// Package toolset is the list of tools the panel gives a session over MCP:
// the executor serves them, and the launcher allows the ones marked allowed.
// Both take the list from here, so a tool is added in one place and a
// session is never allowed a tool the server does not have.
//
// A tool lives in the package of what it works on and is added to tools; its
// package does not import the launcher, which reads this list.
package toolset

import (
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
		session.Restart(session.Here()),
		session.Letter(session.Here()),
		session.Open(session.Here()),
	}
}

// Server is the panel's MCP server for the claude bind finds.
func Server(bind mcp.Bind) *mcp.Server {
	return &mcp.Server{Lead: lead, Tools: tools(), Bind: bind}
}

// Allowed are the tools the launcher allows a session, by the names claude
// gives them.
func Allowed() []string {
	return Server(nil).Allowed()
}
