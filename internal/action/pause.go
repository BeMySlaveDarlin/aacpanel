package action

import (
	"fmt"
	"time"
)

// Pause names a place on the map, never a client-supplied list of sessions.
type Pause struct {
	Project int    `json:"project"`
	Group   bool   `json:"group"`
	Until   string `json:"until"`
	Resume  bool   `json:"resume"`
}

func (p Pause) Validate(now time.Time) (time.Time, error) {
	if p.Project <= 0 {
		return time.Time{}, badRequest("a pause needs a project of the map")
	}
	if p.Resume {
		if p.Until != "" {
			return time.Time{}, badRequest("lifting a pause takes no end time")
		}
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339, p.Until)
	if err != nil || !at.After(now) || at.After(now.Add(7*24*time.Hour)) {
		return time.Time{}, badRequest("a pause must end in the next seven days, as an RFC 3339 moment")
	}
	return at, nil
}

func PauseMessage(until time.Time) string {
	text := fmt.Sprintf("Pause until %s. Finish the current step at a clean stopping point; start no new work. "+
		"Replace any existing alarm named pause, then call alarm_set with name: \"pause\", at: \"%s\", "+
		"and note: \"The pause is over — continue your work\". End the turn and wait for that alarm.",
		until.Format("15:04, 2 Jan 2006 (UTC-07:00)"), until.Format(time.RFC3339))
	return panelMessage("pause", text, until.Format(time.RFC3339))
}
