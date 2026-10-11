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
	AlarmSetInstructions = "To be woken at a moment or every so long — to come back to a task, to look at something " +
		"again later — set an alarm of the panel with alarm_set: it outlives this turn, and it ends with this session."
	AlarmListInstructions   = "alarm_list lists the alarms of this session."
	AlarmCancelInstructions = "alarm_cancel ends an alarm that is no longer needed."
)

// alarmSetDescription is the tool's own word to the model; busy says when a
// busy session reads a letter.
func alarmSetDescription(busy string) string {
	return "Sets an alarm of the panel: the host keeps it, apart from this session, and wakes this session with a " +
		"letter from the alarm at a moment — in, a span from now (90s, 10m, 1h30m), or at, a time of the day on " +
		"the host's clock (23:30, the next to come) or a moment in RFC 3339 — and, with every, again every so long " +
		"after; every alone rings first one span from now. note is said first in every letter: what to do when " +
		"woken. A letter is not the person's words; " + busy + " The alarm runs nothing — to wait on a command, " +
		"start an observer — and outlives a restart of the panel: a ring due while the panel was down comes once " +
		"it is back, and says it is late. It does not outlive this session: when the session closes or starts " +
		"again, its alarms end, and a session started again sets its own. A repeat is " + spanWords(EveryMin) +
		" apart at least, the first ring " + fmt.Sprint(AheadDays) + " days ahead at most, and a session keeps " +
		fmt.Sprint(AlarmsPerSession) + " alarms. " + wakeTransports
}

// The list's and the cancel's own words to the model.
var (
	alarmListDescription = "Lists the alarms of this session: when each rings next, how often it repeats, its " +
		"note, and how often it has rung. An alarm that rings once is gone once it has rung."
	alarmCancelDescription = "Cancels an alarm of this session by its name: it rings no more."
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
