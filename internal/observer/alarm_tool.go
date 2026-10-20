package observer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

// The alarm tools' names on the panel's server: the model calls them
// mcp__aacpanel__alarm_set and so on.
const (
	AlarmSetName    = "alarm_set"
	AlarmListName   = "alarm_list"
	AlarmCancelName = "alarm_cancel"
)

// The alarm tools' lines in the server's word to every session that has them.
const (
	AlarmSetInstructions = "Use alarm_set for 'come back in ten minutes' or a repeated check; it wakes this " +
		"session, not the phone, so end the turn instead of sleeping."
	AlarmListInstructions   = "Use alarm_list to check scheduled wakes before adding, changing or cancelling one."
	AlarmCancelInstructions = "Use alarm_cancel when a timed follow-up is no longer needed or before replacing its schedule."
)

// alarmSetDescription is the tool's own word to the model; busy says when a
// busy session reads a letter.
func alarmSetDescription(busy string) string {
	return "Schedules a letter to this session, running no command. Give name and either in (90s, 10m, " +
		"1h30m) or at (the next 23:30 on the host's clock, or RFC 3339 with offset); do not combine in and at. " +
		"Optional every repeats after that; every alone first rings one interval from now. note says what to " +
		"do when woken. A repeat is at least " + spanWords(EveryMin) + ", the first ring at most " +
		fmt.Sprint(AheadDays) + " days ahead; at most " + fmt.Sprint(AlarmsPerSession) + " alarms per session. " +
		"Check alarm_list before adding one; cancel an existing name before replacing it. Continue independent " +
		"work or end the turn, without sleeping or polling. For a command's result use observer_start; to call " +
		"the person use notify. This is a session wake, not a phone reminder. The letter is from the alarm, " +
		"not the person; " + busy + " It survives a panel restart and delivers an overdue ring on return, " +
		"but ends when this session closes or restarts. Cancel repeating checks with alarm_cancel when done. " + wakeTransports
}

// The list's and the cancel's own words to the model.
var (
	alarmListDescription = "Call with no arguments to read this session's alarms: names, next rings, repeat " +
		"intervals, notes and ring counts. Check before adding, replacing or cancelling a schedule. A one-time " +
		"alarm disappears after ringing. Use observer_list for command watches and alarm_cancel to remove " +
		"a schedule. This call waits for no ring: continue work or end the turn, without polling."
	alarmCancelDescription = "Pass name from alarm_list to cancel this session's alarm; it rings no more. " +
		"Use when the follow-up is no longer needed or before alarm_set replaces its schedule. This stops no " +
		"command: use observer_stop for a command watch. After success continue work; no wake or reply is needed."
)

// alarmTools are the three alarm tools. All are allowed: an alarm runs
// nothing, and the tools touch only the alarms of this session.
func alarmTools(path, busy string) []mcp.Tool {
	return []mcp.Tool{
		{
			Name: AlarmSetName, Title: "Set an alarm", Description: alarmSetDescription(busy),
			InputSchema: alarmSchema(), Instructions: AlarmSetInstructions, Allowed: true,
			Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
				return setAlarm(ctx, path, bind, args)
			},
		},
		{
			Name: AlarmListName, Title: "List alarms", Description: alarmListDescription,
			InputSchema:  map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
			Instructions: AlarmListInstructions, Allowed: true,
			Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
				return listAlarms(ctx, path, bind)
			},
		},
		{
			Name: AlarmCancelName, Title: "Cancel an alarm", Description: alarmCancelDescription,
			InputSchema: nameSchema("alarm", nil), Instructions: AlarmCancelInstructions, Allowed: true,
			Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
				return cancelAlarm(ctx, path, bind, args)
			},
		},
	}
}

func alarmSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "pattern": namePattern.String(), "maxLength": NameMax,
				"description": "What the alarm is called: its letters are signed with it, and it is cancelled by it."},
			"in": map[string]any{"type": "string",
				"description": "The first ring, a span from now in the syntax of Go's durations: 90s, 10m, 1h30m."},
			"at": map[string]any{"type": "string",
				"description": "The first ring, a moment: a time of the day on the host's clock, 23:30 or 23:30:15, " +
					"the next to come, or RFC 3339, 2026-10-12T09:00:00+03:00."},
			"every": map[string]any{"type": "string",
				"description": "The span from a ring to the next, as in; left out, the alarm rings once. Alone, it " +
					"is the span to the first ring as well."},
			"note": map[string]any{"type": "string", "maxLength": TextMax,
				"description": "What every letter says first: what to do when woken."},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func setAlarm(ctx context.Context, path string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args AlarmAsk
	if err := strictly(raw, &args); err != nil {
		return "No alarm was set: the arguments are not the tool's (" + err.Error() + ").", true
	}
	c, b, err := callerOf(bind)
	if err != nil {
		return "No alarm was set: " + err.Error() + ".", true
	}
	reply, err := ask(ctx, path, Request{Op: OpAlarmSet, Caller: c, Alarm: &args})
	if err != nil {
		return "No alarm was set: " + err.Error(), true
	}
	v := reply.Alarm
	if v == nil {
		return "The executor set the alarm and said nothing of it: alarm_list shows it. " +
			"The work started, not written to the panel's journal: the executor supplied no schedule.", false
	}
	record := action.WorkRecord{Name: v.Name, At: v.Next.Format(time.RFC3339)}
	if v.Every > 0 {
		record.Every = v.Every.String()
	}
	text := fmt.Sprintf("Alarm %s set: it %s. This session gets a letter from it when it rings, and "+
		"alarm_cancel %s ends it.", v.Name, ringWords(*v, time.Now()), v.Name)
	return journalWork(ctx, b, action.AlarmSet, record, text), false
}

func listAlarms(ctx context.Context, path string, bind mcp.Bind) (string, bool) {
	c, _, err := callerOf(bind)
	if err != nil {
		return "The alarms are not listed: " + err.Error() + ".", true
	}
	reply, err := ask(ctx, path, Request{Op: OpAlarmList, Caller: c})
	if err != nil {
		return "The alarms are not listed: " + err.Error(), true
	}
	if len(reply.Alarms) == 0 {
		return "This session has no alarms.", false
	}
	now := time.Now()
	lines := []string{"The alarms of this session:"}
	for _, v := range reply.Alarms {
		lines = append(lines, "- "+describeAlarm(v, now))
	}
	return strings.Join(lines, "\n"), false
}

func cancelAlarm(ctx context.Context, path string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Name string `json:"name"`
	}
	if err := strictly(raw, &args); err != nil || args.Name == "" {
		return "Nothing was cancelled: the tool takes the name of an alarm and nothing else.", true
	}
	c, _, err := callerOf(bind)
	if err != nil {
		return "Nothing was cancelled: " + err.Error() + ".", true
	}
	if _, err := ask(ctx, path, Request{Op: OpAlarmCancel, Caller: c, Name: args.Name}); err != nil {
		return "Nothing was cancelled: " + err.Error(), true
	}
	return "Alarm " + args.Name + " cancelled: it rings no more.", false
}
