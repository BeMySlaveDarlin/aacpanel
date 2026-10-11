package action

import (
	"path/filepath"
	"slices"
	"strings"
)

// CodexLaunch overrides the map for one start, without changing the project.
// Approval is deliberately absent: a helper has nobody to answer a prompt.
type CodexLaunch struct {
	Model         string   `json:"model,omitempty"`
	Effort        string   `json:"effort,omitempty"`
	Sandbox       string   `json:"sandbox,omitempty"`
	WritableRoots []string `json:"writableRoots,omitempty"`
}

func (c CodexLaunch) Validate() error {
	if c.Model != "" && !codexModel.MatchString(c.Model) {
		return badRequest("model %q is not a Codex model name", c.Model)
	}
	if c.Effort != "" && !slices.Contains([]string{"low", "medium", "high", "xhigh", "max", "ultra"}, c.Effort) {
		return badRequest("unknown Codex effort %q", c.Effort)
	}
	if c.Sandbox != "" && c.Sandbox != "workspace-write" && c.Sandbox != "read-only" {
		return badRequest("sandbox %q must be workspace-write or read-only", c.Sandbox)
	}
	if c.Sandbox == "read-only" && len(c.WritableRoots) > 0 {
		return badRequest("read-only takes no writable roots")
	}
	for _, dir := range c.WritableRoots {
		if !filepath.IsAbs(dir) || len(dir) > pathMax || strings.ContainsAny(dir, "\x00\r\n") {
			return badRequest("writable root %q must be an absolute path without control characters", dir)
		}
	}
	return nil
}
