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

	"aacpanel/internal/mcp"
	"aacpanel/internal/plan"
)

// lead is the server's first sentence to a session: what of the session the
// panel puts before the person, which the line of every tool goes on from.
const lead = "The panel shows the person this session's plan on their phone and desk."

func tools() []mcp.Tool {
	return []mcp.Tool{
		plan.Tool(plan.Dir(), time.Now),
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
