package hook

import "time"

// The turn reminder asks the main conversation to declare whose move follows
// its answer. It reads the turn from the last human prompt and looks for the
// panel's turn_end call; a declaration in an earlier turn or a sidechain says
// nothing about this one. The process is found by the same parent chain and
// live session file as the checklist reminder, so a nested conversation
// cannot declare for the session above it.
//
// A different event, an unreadable payload, a session that cannot be found,
// or a process without the tool stays silent. So does a turn that already
// called turn_end. A forgotten call costs one extra model round: the Stop is
// held with the instruction to make the call and end the turn. The answer to
// that hold carries stop_hook_active and is never held again. Claude combines
// parallel Stop blocks in that round, so the checklist reminder and this one
// cannot keep waking each other. The cases live in
// deploy/claude/testdata/turn-reminder.json.

const turnTool = "mcp__aacpanel__turn_end"

const turnReason = "Before ending the turn, say whose move it is: call turn_end (" + turnTool + ") — " +
	"owner with what you need from the person, self if something set will wake you, done if nothing waits. " +
	"Then end the turn without a word about this."

func remindTurn(event string, payload []byte, _ time.Time) Reply {
	p, ok := loadNumbers(payload).(map[string]any)
	if !ok || event != "Stop" || truthy(p["stop_hook_active"]) {
		return Reply{}
	}
	if named, ok := p["hook_event_name"]; ok && named != "Stop" {
		return Reply{}
	}
	pid, session := claudeOf(p["session_id"])
	if session == nil || !hasTool(pid, turnTool) {
		return Reply{}
	}
	path, _ := p["transcript_path"].(string)
	_, _, called := turnOf(path, turnTool)
	if called {
		return Reply{}
	}
	return Reply{Block: turnReason}
}
