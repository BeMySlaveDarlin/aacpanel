package action

import "strings"

// Mcp is what a session says about its MCP servers. A session on the stream
// answers claude's own request for them; a terminal lists them on a screen
// driven by keys, and the answer only says where it lives.
type Mcp struct {
	Transport string      `json:"transport"`
	Servers   []McpServer `json:"servers,omitempty"`
}

// McpServer is one server as the panel shows it. Only what is shown comes
// along: the headers, the arguments and the environment of a configuration
// can carry keys, and so can the query of a URL, so they stay on the host.
type McpServer struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Error       string   `json:"error,omitempty"`
	Source      string   `json:"source,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Type        string   `json:"type,omitempty"`
	URL         string   `json:"url,omitempty"`
	Command     string   `json:"command,omitempty"`
	Title       string   `json:"title,omitempty"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools,omitempty"`
}

// What session.mcp does to a server.
const (
	McpReconnect = "reconnect"
	McpEnable    = "enable"
	McpDisable   = "disable"
)

// McpChange is what session.mcp does, and to which server. The server is
// named the way the session reported it.
type McpChange struct {
	Server string `json:"server"`
	Do     string `json:"do"`
}

const mcpServerMax = 200

func (c *McpChange) validate() error {
	if c == nil {
		return badRequest("a change of MCP that names no server")
	}
	switch c.Do {
	case McpReconnect, McpEnable, McpDisable:
	default:
		return badRequest("an MCP server is reconnected, enabled or disabled, not %q", c.Do)
	}
	if strings.TrimSpace(c.Server) == "" {
		return badRequest("a change of MCP that names no server")
	}
	if len(c.Server) > mcpServerMax {
		return badRequest("the name of an MCP server is longer than %d characters", mcpServerMax)
	}
	for _, r := range c.Server {
		if r < 0x20 || r == 0x7f {
			return badRequest("the name of an MCP server contains a control character %q", r)
		}
	}
	return nil
}
