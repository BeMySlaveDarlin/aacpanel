package hook

import (
	"path/filepath"
	"time"

	"aacpanel/internal/hostcfg"
	"aacpanel/internal/observer"
)

// The turn reminder asks the main conversation to declare whose move follows
// its answer. It reads the turn from the last human prompt and looks for the
// panel's turn_end call; a declaration in an earlier turn or a sidechain says
// nothing about this one. The process is found by the same parent chain and
// live session file as the checklist reminder, so a nested conversation
// cannot declare for the session above it.
//
// A different event, an unreadable payload, a session that cannot be found,
// or a process without the tool stays silent. So does a turn that already
// called turn_end with owner or done, or with self and something to wake it.
// A self declaration with no wake left is held once too; an unknown source
// is never taken as proof that nothing is set. A forgotten call or wake costs
// one extra model round: the Stop is held with the instruction to make the
// call or set a wake and end the turn. The answer to
// that hold carries stop_hook_active and is never held again. Claude combines
// parallel Stop blocks in that round, so the checklist reminder and this one
// cannot keep waking each other. The cases live in
// deploy/claude/testdata/turn-reminder.json.

const turnTool = "mcp__aacpanel__turn_end"

const turnReason = "Before ending the turn, say whose move it is: call turn_end (" + turnTool + ") — " +
	"owner with what you need from the person, self if something set will wake you, done if nothing waits. " +
	"Then end the turn without a word about this."

const stalledReason = "You said something will wake you, but nothing is set to: no background task, agent, alarm, observer or child session is running. " +
	"Set what will wake you (an alarm or an observer), or call turn_end again with owner or done. " +
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
	_, _, called, input := turnOf(path, turnTool)
	if !called {
		return Reply{Block: turnReason}
	}
	if input["next"] != "self" || backgroundWake(p) {
		return Reply{}
	}
	id, _ := session["sessionId"].(string)
	pending, err := observer.Pending(observer.Dir(), id)
	if err != nil || pending || childWake(sessionName(pid, session)) {
		return Reply{}
	}
	return Reply{Block: stalledReason}
}

// backgroundWake takes every unfinished kind, including agents and workflows.
// A payload that does not name its background work cannot prove it has none.
func backgroundWake(payload map[string]any) bool {
	tasks, ok := payload["background_tasks"].([]any)
	if !ok {
		return true
	}
	for _, raw := range tasks {
		task, ok := raw.(map[string]any)
		if !ok {
			return true
		}
		switch task["status"] {
		case "completed", "failed", "killed", "stopped", "cancelled":
		default:
			return true
		}
	}
	return false
}

// childWake reads the same snapshot as the prompt stamp. A child that has
// not declared done may still report, even if its own self declaration has
// stalled; neither this hook nor the collector walks down the family tree.
func childWake(name string) bool {
	if name == "" {
		return true
	}
	state := readState(filepath.Join(hostcfg.Load().StateDir, "state.json"))
	rows, ok := state["sessions"].([]any)
	if !ok {
		return true
	}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return true
		}
		parent, _ := row["parent"].(map[string]any)
		if row["parent"] != nil && parent == nil {
			return true
		}
		if parent["session"] != name {
			continue
		}
		turn, _ := row["turn"].(map[string]any)
		if row["status"] != "idle" || turn["next"] != "done" {
			return true
		}
	}
	return false
}
