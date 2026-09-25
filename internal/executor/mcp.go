package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// Mcp answers what a session says about its MCP servers. A session on the
// stream is asked with claude's own request; a terminal lists its servers on a
// screen driven by keys, and the answer says where it lives.
func (e *Executor) Mcp(ctx context.Context, target string) (*action.Mcp, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}
	if !onStream(s) {
		return &action.Mcp{Transport: action.SwitchConsole}, nil
	}
	reply, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "mcp_status"})
	if err != nil {
		return nil, err
	}
	servers, err := mcpServers(reply.Response)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", s.Name, err)
	}
	return &action.Mcp{Transport: action.SwitchStream, Servers: servers}, nil
}

type mcpStatus struct {
	McpServers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Error  string `json:"error"`
		Scope  string `json:"scope"`
		Source string `json:"source"`
		Config struct {
			Type    string `json:"type"`
			URL     string `json:"url"`
			Command string `json:"command"`
		} `json:"config"`
		ServerInfo struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
		} `json:"serverInfo"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	} `json:"mcpServers"`
}

// mcpServers reads the servers out of claude's answer to mcp_status.
func mcpServers(raw json.RawMessage) ([]action.McpServer, error) {
	var body struct {
		mcpStatus
		Response mcpStatus `json:"response"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("the list of MCP servers did not parse: %w", err)
	}
	list := body.Response.McpServers
	if list == nil {
		list = body.McpServers
	}
	out := make([]action.McpServer, 0, len(list))
	for _, m := range list {
		if m.Name == "" {
			continue
		}
		tools := make([]string, 0, len(m.Tools))
		for _, t := range m.Tools {
			tools = append(tools, t.Name)
		}
		out = append(out, action.McpServer{
			Name: m.Name, Status: m.Status, Error: m.Error, Source: m.Source, Scope: m.Scope,
			Type: m.Config.Type, URL: bareURL(m.Config.URL), Command: commandName(m.Config.Command),
			Title: m.ServerInfo.Name, Version: m.ServerInfo.Version, Description: m.ServerInfo.Description,
			Tools: tools,
		})
	}
	return out, nil
}

// bareURL is the address of a server without what can carry a key: the user
// and the password before the host, the query and the fragment after the path.
func bareURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}

// commandName is the program a server runs, without the directory it lies in.
func commandName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// sessionMcp reconnects, enables or disables one MCP server of a session on
// the stream, with claude's own request for it.
func (e *Executor) sessionMcp(ctx context.Context, target string, change *action.McpChange) (string, error) {
	if change == nil {
		return "", fmt.Errorf("no MCP server was named: there is nothing to change")
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if !onStream(s) {
		return "", fmt.Errorf("session %s runs in a terminal: its MCP servers are managed on its own screen "+
			"with /mcp, and the panel does not drive it", s.Name)
	}
	req := stream.Request{Op: stream.OpControl, Fields: map[string]any{"serverName": change.Server}}
	var done string
	switch change.Do {
	case action.McpReconnect:
		req.Subtype, done = "mcp_reconnect", "reconnected"
	case action.McpEnable:
		req.Subtype, done = "mcp_toggle", "enabled"
		req.Fields["enabled"] = true
	case action.McpDisable:
		req.Subtype, done = "mcp_toggle", "disabled"
		req.Fields["enabled"] = false
	default:
		return "", fmt.Errorf("an MCP server is reconnected, enabled or disabled, not %q", change.Do)
	}
	if _, err := streamAsk(ctx, s, req); err != nil {
		return "", err
	}
	return fmt.Sprintf("MCP server %s of %s is %s", change.Server, s.Name, done), nil
}
