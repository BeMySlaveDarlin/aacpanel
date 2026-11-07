package action

import (
	"fmt"
	"time"
)

// The alarms a paused Codex session keeps for its pause. PauseAlarm rings at the end
// of a pause until a moment. A pause with no end has no moment to ring at, and
// an alarm rings at most AheadDays ahead, so PauseNoEndAlarm repeats at that
// longest span and only reminds the session it is still paused: the snapshot
// reads it as the mark of the pause, and the resume cancels it as it cancels
// the other.
const (
	PauseAlarm      = "pause"
	PauseNoEndAlarm = "pause-no-end"
	PauseNoEndEvery = "168h"
)

// Pause names a place on the map, never a client-supplied list of sessions.
// Session narrows the project to one of its sessions, by name; the
// service checks that the session runs in that project. NoEnd is a pause with
// no end time: it lasts until the person lifts it.
type Pause struct {
	Project int    `json:"project"`
	Group   bool   `json:"group"`
	Session string `json:"session,omitempty"`
	Until   string `json:"until"`
	NoEnd   bool   `json:"noEnd"`
	Resume  bool   `json:"resume"`
}

// Validate reads when the pause ends: the zero time for a resume and for a
// pause with no end.
func (p Pause) Validate(now time.Time) (time.Time, error) {
	if p.Project <= 0 {
		return time.Time{}, badRequest("a pause needs a project of the map")
	}
	if p.Session != "" && p.Group {
		return time.Time{}, badRequest("a pause of one session cannot take in its group")
	}
	if p.Resume {
		if p.Until != "" || p.NoEnd {
			return time.Time{}, badRequest("lifting a pause takes no end time")
		}
		return time.Time{}, nil
	}
	if p.NoEnd {
		if p.Until != "" {
			return time.Time{}, badRequest("a pause with no end takes no end time")
		}
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339, p.Until)
	if err != nil || !at.After(now) || at.After(now.Add(7*24*time.Hour)) {
		return time.Time{}, badRequest("a pause must end in the next seven days, as an RFC 3339 moment")
	}
	return at, nil
}

// PauseMessage is the instruction of a pause until a moment, or of a pause
// with no end for the zero time; the latter has no moment for its envelope,
// so its sender tells the feed what it is. Either replaces the alarm of a
// pause the session already keeps, of either kind: two would leave the
// session paused in the snapshot after a resume that cancels one.
func PauseMessage(until time.Time) string {
	if until.IsZero() {
		text := fmt.Sprintf("Pause with no end. Finish the current step at a clean stopping point; start no new work. "+
			"Cancel any existing alarm named %[1]s or %[2]s, then call alarm_set with name: \"%[2]s\", every: \"%[3]s\", "+
			"and note: \"The pause has no end — start no work, end the turn and keep waiting\". "+
			"End the turn and wait: the pause lasts until the person lifts it.",
			PauseAlarm, PauseNoEndAlarm, PauseNoEndEvery)
		return panelMessage("pause with no end", text, "")
	}
	text := fmt.Sprintf("Pause until %s. Finish the current step at a clean stopping point; start no new work. "+
		"Cancel any existing alarm named %s or %s, then call alarm_set with name: \"%s\", at: \"%s\", "+
		"and note: \"The pause is over — continue your work\". End the turn and wait for that alarm.",
		until.Format("15:04, 2 Jan 2006 (UTC-07:00)"), PauseAlarm, PauseNoEndAlarm, PauseAlarm, until.Format(time.RFC3339))
	return panelMessage("pause", text, until.Format(time.RFC3339))
}

// SleepMessage leaves the deadline with the panel: an alarm inside the
// process cannot wake a conversation whose process has been closed.
func SleepMessage(until time.Time) string {
	text := "Finish the current step at a clean stopping point; start no new work and end the turn. " +
		"The panel will put this session to sleep and wake it with a message when the pause ends. Do not set a pause alarm."
	if until.IsZero() {
		return panelMessage("pause with no end", "Pause with no end, until the person lifts it. "+text, "")
	}
	return panelMessage("pause", "Pause until "+until.Format("15:04, 2 Jan 2006 (UTC-07:00)")+". "+text, until.Format(time.RFC3339))
}
