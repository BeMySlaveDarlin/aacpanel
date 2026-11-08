package action

import "time"

// Timeout leaves enough time for a package download without lengthening session actions.
func Timeout(kind Kind, ordinary time.Duration) time.Duration {
	if kind == TaskSummary {
		return 130 * time.Second
	}
	if kind == CodexUpdate || kind == ClaudeUpdate {
		return 15 * time.Minute
	}
	return ordinary
}
