package launcher

import (
	"fmt"

	registry "aacpanel/internal/contours"
	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionintent"
)

// The free name and the account must be known before selecting a message:
// another session of the same project has its own rules. An unreadable override
// leaves the inherited message in place and returns a warning for the report,
// since losing the override must not keep the session from starting.
func sessionMessage(spec Spec, name string, env []string, inherited string) (string, error) {
	if spec.IntentKey == "" {
		return inherited, nil
	}
	if spec.IntentKey != sessionintent.First && spec.IntentKey != sessionintent.Restart {
		return inherited, fmt.Errorf("unknown session message %q", spec.IntentKey)
	}
	config := envValue(env, "CLAUDE_CONFIG_DIR")
	if config == "" {
		config = spec.ConfigDir
	}
	if config == "" {
		config = registry.DefaultConfig(envValue(env, "HOME"))
	}
	values, err := sessionintent.Read(sessionintent.Dir(), mcp.Place{ConfigDir: config, Dir: spec.Dir}, name)
	if err != nil {
		return inherited, fmt.Errorf("the session messages were not read: %w", err)
	}
	return values.Pick(spec.IntentKey, inherited), nil
}
