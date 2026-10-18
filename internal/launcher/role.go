package launcher

import (
	"time"

	registry "aacpanel/internal/contours"
	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
)

// keepRole keeps the role the opener gave the session under the name it comes
// up under, in the account it runs in: the config directory its environment
// names, else the one of its contour, else the default of its home — where
// claude keeps the file of the session and the server finds the session by.
// A role not kept is a warning: the session comes up all the same, and the
// opener reads why and sets the role again.
func keepRole(spec Spec, name string, env []string) string {
	if spec.Role == nil {
		return ""
	}
	config := envValue(env, "CLAUDE_CONFIG_DIR")
	if config == "" {
		config = spec.ConfigDir
	}
	if config == "" {
		config = registry.DefaultConfig(envValue(env, "HOME"))
	}
	_, err := role.Keep(role.Dir(), mcp.Place{ConfigDir: config, Dir: spec.Dir}, name, spec.Role.Text,
		role.ByOpener(spec.Role.Opener), time.Now())
	if err != nil {
		return "the role was not kept: " + err.Error()
	}
	return ""
}
