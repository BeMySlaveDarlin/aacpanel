package notify

import (
	"fmt"
	"strings"
)

// RevivalMessage is one report per host boot. A partial recovery names each
// conversation left behind, so a person can resume it without guessing.
func RevivalMessage(boot string, total, raised int, failed []string) Message {
	body := fmt.Sprintf("Host rebooted: revived %d of %d", raised, total)
	severity := Info
	if len(failed) > 0 {
		var reasons []string
		byReason := map[string][]string{}
		for _, failure := range failed {
			name, reason, ok := strings.Cut(failure, ": ")
			if !ok {
				name, reason = "recovery", failure
			}
			reason = clipRunes(reason, 100)
			if _, exists := byReason[reason]; !exists {
				reasons = append(reasons, reason)
			}
			byReason[reason] = append(byReason[reason], name)
		}
		var groups []string
		for _, reason := range reasons {
			groups = append(groups, strings.Join(byReason[reason], ", ")+": "+reason)
		}
		body += "; not revived: " + strings.Join(groups, "; ")
		severity = Warning
	}
	return Message{Title: "Sessions after a host reboot", Body: body, Tag: "reboot:" + boot, Kind: KindReboot, Severity: severity}
}
