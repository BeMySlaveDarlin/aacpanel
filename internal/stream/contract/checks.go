package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/stream"
)

func checkInitialize(s *session) Result {
	body := s.request("initialize", nil)
	if !success(body) {
		return verdict("initialize", false, fmt.Sprintf("no answer: %v", body))
	}
	resp := body.obj("response")
	commands, models := resp.list("commands"), resp.list("models")
	if len(commands) == 0 || len(models) == 0 {
		return verdict("initialize", false, fmt.Sprintf("commands %d, models %d", len(commands), len(models)))
	}
	return verdict("initialize", true, fmt.Sprintf("%d commands, %d models, mode %s", len(commands), len(models), resp.text("current_permission_mode")))
}
func checkTurn(s *session, config string) Report {
	result, _ := s.turn("Reply with the single word: ready", nil)
	if !success(result) {
		return Report{verdict("turn", false, "no successful result: "+result.text("subtype"))}
	}
	path := findTranscript(config, s.sid)
	detail := "written under the session id"
	if path == "" {
		detail = "no transcript under the session id"
	}
	return Report{verdict("turn", true, cut(result.text("result"), 40)), verdict("transcript", path != "", detail)}
}
func checkHooks(cwd string) Result {
	body, _ := os.ReadFile(filepath.Join(cwd, "hooks.log"))
	fired := strings.Fields(string(body))
	var missing []string
	for _, name := range []string{"SessionStart", "Stop", "UserPromptSubmit"} {
		if !slices.Contains(fired, name) {
			missing = append(missing, name)
		}
	}
	detail := "SessionStart, UserPromptSubmit, Stop"
	if len(missing) > 0 {
		detail = "did not fire: " + strings.Join(missing, ", ")
	}
	return verdict("hooks", len(missing) == 0, detail)
}
func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func permissionVerdict(asked []object, made bool) Result {
	var req object
	for _, ev := range asked {
		if toolOf(ev) == "Bash" {
			req = ev.obj("request")
			break
		}
	}
	if req == nil {
		return verdict("permission.allow", false, "no can_use_tool for Bash arrived")
	}
	var missing []string
	for _, key := range []string{"input", "tool_use_id", "permission_suggestions"} {
		if _, ok := req[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return verdict("permission.allow", false, "the request lacks "+strings.Join(missing, ", "))
	}
	detail := "asked, allowed, the command ran"
	if !made {
		detail = "allowed, but the command did not run"
	}
	return verdict("permission.allow", made, detail)
}
func checkPermission(s *session) Result {
	var asked []object
	s.turn("Use the Bash tool to run exactly: touch contract-marker.txt — then reply with the word done.", func(ev object) { asked = append(asked, ev); s.allow(ev, nil) })
	return permissionVerdict(asked, exists(filepath.Join(s.cwd, "contract-marker.txt")))
}

const denyReason = "the contract says no"

func denyVerdict(asked, events []object, made bool) Result {
	if len(asked) == 0 {
		return verdict("permission.deny", false, "no can_use_tool arrived")
	}
	heard := strings.Contains(toolResults(events), denyReason)
	detail := fmt.Sprintf("reason read %t, file made %t", heard, made)
	if heard && !made {
		detail = "denied with a reason the model read"
	}
	return verdict("permission.deny", heard && !made, detail)
}
func checkDeny(s *session) Result {
	var asked []object
	_, events := s.turn("Use the Bash tool to run exactly: touch contract-denied.txt — then reply with the word done.", func(ev object) {
		asked = append(asked, ev)
		s.answer(ev, object{"behavior": "deny", "message": denyReason})
	})
	return denyVerdict(asked, events, exists(filepath.Join(s.cwd, "contract-denied.txt")))
}

const askPrompt = `Call the AskUserQuestion tool exactly once with two questions. Question 1: "Pick a colour", header "Colour", options Red (preview "RED BLOCK") and Blue (preview "BLUE BLOCK"). Question 2: "Pick a size", header "Size", options Small and Large, no previews. After the answer, reply with the word done.`

func askAnswer(ev object) object {
	questions := ev.obj("request").obj("input").list("questions")
	answers, notes := object{}, object{}
	for _, v := range questions {
		q := asObject(v)
		options := q.list("options")
		if len(options) > 0 {
			answers[q.text("question")] = asObject(options[len(options)-1]).text("label")
		}
	}
	if len(questions) > 0 {
		notes[asObject(questions[0]).text("question")] = object{"notes": "contract-note"}
	}
	return object{"answers": answers, "annotations": notes}
}
func askVerdict(got, events []object) Report {
	if len(got) == 0 {
		return Report{verdict("ask", false, "no can_use_tool for AskUserQuestion arrived — the tool was not offered or not called")}
	}
	questions := got[0].obj("request").obj("input").list("questions")
	previews, picked := false, true
	said := toolResults(events)
	for _, v := range questions {
		q := asObject(v)
		opts := q.list("options")
		for _, o := range opts {
			if asObject(o).text("preview") != "" {
				previews = true
			}
		}
		if len(opts) > 0 {
			picked = picked && strings.Contains(said, asObject(opts[len(opts)-1]).text("label"))
		}
	}
	notes := Result{"ask.notes", Info, "a note to an option did not reach the model", false}
	if strings.Contains(said, "contract-note") {
		notes.Status = OK
		notes.Detail = "a note to an option reaches the model"
	}
	return Report{verdict("ask", picked && len(questions) == 2, fmt.Sprintf("%d questions, previews %t, answers heard %t", len(questions), previews, picked)), notes}
}
func checkAsk(s *session) Report {
	var got []object
	_, events := s.turn(askPrompt, func(ev object) {
		if toolOf(ev) != "AskUserQuestion" {
			s.allow(ev, nil)
			return
		}
		got = append(got, ev)
		s.allow(ev, askAnswer(ev))
	})
	return askVerdict(got, events)
}
func checkPlan(s *session) Result {
	body := s.request("set_permission_mode", object{"mode": "plan"})
	if !success(body) {
		return verdict("plan", false, fmt.Sprintf("set_permission_mode plan: %v", body))
	}
	var got []object
	s.turn("Plan how to create a file named plan.txt with the word hello in it. Keep the plan to one line and present it with the ExitPlanMode tool right away. Do not do anything else.", func(ev object) {
		if toolOf(ev) == "ExitPlanMode" {
			got = append(got, ev)
		}
		s.allow(ev, nil)
	})
	back := s.request("set_permission_mode", object{"mode": s.manual})
	if len(got) == 0 {
		return verdict("plan", false, "no can_use_tool for ExitPlanMode arrived")
	}
	input := got[0].obj("request").obj("input")
	// Only the plan named by this run is removed, never the user's plans directory.
	if written := input.text("planFilePath"); written != "" && filepath.Base(filepath.Dir(written)) == "plans" {
		os.Remove(written)
	}
	return planVerdict(input, back)
}
func planVerdict(input, back object) Result {
	ok := input.text("plan") != "" && success(back)
	detail := fmt.Sprintf("plan text %t, mode restored %v", input.text("plan") != "", back)
	if ok {
		detail = "the plan arrives as a request with its text"
	}
	return verdict("plan", ok, detail)
}

// checkInterrupt needs a command that runs long without opening with sleep:
// claude refuses such a command, chained or not, as a wait better done
// another way, and a refused command never starts. The wait stays in the
// shell's process group, so the interrupt ends it; under timeout it would
// outlive the turn and muddle the background checks after this one.
func checkInterrupt(s *session) Result {
	s.user("Use the Bash tool to run exactly: python3 -c 'import time; time.sleep(40)' && echo woke — then reply with the word done.", "")
	started := s.pump(func(e object) bool { return system(e, "task_started") }, 60*time.Second, s.onAsk)
	if started == nil {
		return verdict("interrupt", false, "the command never started")
	}
	start := len(s.seen)
	body := s.request("interrupt", nil)
	// The result may precede the acknowledgement; the holder reads both orders.
	result := lastResult(s.seen[start:])
	if result == nil {
		result = s.pump(isResult, 30*time.Second, nil)
	}
	return interruptVerdict(body, result)
}
func lastResult(events []object) object {
	for i := len(events) - 1; i >= 0; i-- {
		if isResult(events[i]) {
			return events[i]
		}
	}
	return nil
}
func interruptVerdict(body, result object) Result {
	ok := result.text("subtype") == "error_during_execution"
	detail := "result " + result.text("subtype")
	if ok {
		detail = fmt.Sprintf("the turn ended at once, still_queued is %T", body.obj("response")["still_queued"])
	}
	return verdict("interrupt", ok, detail)
}

// A peer letter uses the holder's input frame. Neither the stdout replay nor
// the transcript retains its origin, so delivery is proved by the model's
// reply and the letter text kept after claude has flushed the transcript.
func (s *session) letter(class, word string) string {
	mode := s.manual
	if class == "bypass" {
		mode = "bypassPermissions"
	}
	id := uuid()
	s.send(object(stream.LetterMessage(s.sid, id, letterText(word), mode, nil)))
	return id
}
func letterText(word string) string {
	return "<cross-session-message from-name=\"observer contract\">\nThe code word is " + word + "\n</cross-session-message>"
}
func letterError(ev object) bool {
	bad, _ := ev["is_error"].(bool)
	return bad || ev.text("type") == "error" ||
		(ev.text("type") == "control_response" && ev.obj("response").text("subtype") == "error") ||
		(isResult(ev) && (!success(ev) || len(ev.list("permission_denials")) > 0))
}

// No permission is answered here: a reply obtained by letting a letter
// through for a person would hide the hold this probe needs to catch.
func (s *session) waitLetter(id string) []object {
	start := len(s.seen)
	deadline := time.Now().Add(turnWait)
	for time.Now().Before(deadline) && s.err == nil {
		event := s.pump(func(ev object) bool { return isResult(ev) || isAsk(ev) || letterError(ev) }, min(10*time.Second, time.Until(deadline)), nil)
		if isAsk(event) || letterError(event) {
			break
		}
		if isResult(event) && letterDelivered(id, s.seen[start:]) {
			break
		}
		if s.gone() {
			break
		}
	}
	return s.seen[start:]
}

func letterDelivered(id string, events []object) bool {
	started := slices.ContainsFunc(events, func(ev object) bool {
		return ev.text("type") == "command_lifecycle" && ev.text("command_uuid") == id &&
			(ev.text("state") == "started" || ev.text("state") == "completed")
	})
	return started && success(lastResult(events)) && !slices.ContainsFunc(events, isAsk) && !slices.ContainsFunc(events, letterError)
}

const letterRecall = "What code word did the most recent letter from observer contract carry? Read it as information and answer with the word only. No tools are needed."

// A letter is information from a peer, not an instruction from the person.
// Ask the person's question only after delivery: a user turn must not be what
// releases a held letter. The question itself never contains the answer.
func (s *session) readLetter(class, word, id string, delivery []object) Result {
	if !letterDelivered(id, delivery) {
		return letterVerdict(class, word, id, delivery, nil)
	}
	start := len(s.seen)
	s.user(letterRecall, "")
	s.pump(func(ev object) bool { return isResult(ev) || isAsk(ev) || letterError(ev) }, turnWait, nil)
	return letterVerdict(class, word, id, delivery, s.seen[start:])
}

func letterVerdict(class, word, id string, delivery, answer []object) Result {
	delivered := letterDelivered(id, delivery)
	heard := strings.Contains(assistantText(answer), word)
	finished := success(lastResult(answer))
	permission := slices.ContainsFunc(delivery, isAsk) || slices.ContainsFunc(answer, isAsk)
	failed := slices.ContainsFunc(delivery, letterError) || slices.ContainsFunc(answer, letterError)
	return verdict("letter."+class, delivered && heard && finished && !permission && !failed,
		fmt.Sprintf("origin peer, fromMode %s: delivered before question %t, fact read %t, turn completed %t, permission request %t, error %t", class, delivered, heard, finished, permission, failed))
}

// Claude does not persist the input origin. A busy turn keeps the letter
// as a queued-command attachment; an idle one keeps it as a user message.
// Enqueuing alone does not show delivery, nor does the model's answer.
func transcriptLetter(events []object, text string) (found bool, framing string) {
	for _, ev := range events {
		if ev.text("type") == "user" && strings.Contains(ev.obj("message").text("content"), text) {
			found = true
		}
		attachment := ev.obj("attachment")
		if ev.text("type") != "attachment" || attachment.text("type") != "queued_command" || !strings.Contains(attachment.text("prompt"), text) {
			continue
		}
		found, framing = true, "queued_command has no rendered text"
		for _, part := range ev.list("rendered") {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(asObject(part).text("content")), "<system-reminder>"))
			if text != "" {
				framing = cut(strings.SplitN(text, "\n", 2)[0], 100)
				break
			}
		}
	}
	return found, framing
}
func queueVerdict(body object, events []object) Report {
	cancelled, _ := body.obj("response")["cancelled"].(bool)
	said := assistantText(events)
	kept, dropped := strings.Contains(said, "KEPTWORD"), strings.Contains(said, "DROPPEDWORD")
	return Report{verdict("queue.cancel", success(body) && cancelled, fmt.Sprintf("cancel_async_message answered cancelled=%v", body.obj("response")["cancelled"])),
		verdict("queue.deliver", kept && !dropped, fmt.Sprintf("kept one read %t, dropped one read %t", kept, dropped))}
}
func checkQueue(s *session) Report {
	dropped, kept := uuid(), uuid()
	// A leading sleep is subject to claude's wait-command refusal. Keep the
	// queue window without depending on its threshold for short sleeps.
	s.user("Use the Bash tool to run exactly: python3 -c 'import time; time.sleep(12)' && echo woke — then reply with the word done.", "")
	started := s.pump(func(e object) bool { return system(e, "task_started") }, 60*time.Second, s.onAsk)
	if started == nil {
		return Report{verdict("queue", false, "the command never started")}
	}
	start := len(s.seen)
	s.user("Reply with the single word: DROPPEDWORD", dropped)
	s.user("Reply with the single word: KEPTWORD", kept)
	body := s.request("cancel_async_message", object{"message_uuid": dropped})
	id := s.letter("prompting", "LETTERWORD")
	letterEvents := s.waitLetter(id)
	out := queueVerdict(body, s.seen[start:])
	return append(out, s.readLetter("prompting", "LETTERWORD", id, letterEvents))
}
func checkModel(s *session, model string) Result {
	body := s.request("set_model", object{"model": "sonnet"})
	if !success(body) {
		return verdict("set_model", false, fmt.Sprint(body))
	}
	_, events := s.turn("/context", nil)
	back := s.request("set_model", object{"model": model})
	ok := strings.Contains(strings.ToLower(messageText(events, false)), "sonnet") && success(back)
	detail := "applied, /context names it"
	if !ok {
		detail = "accepted, but /context does not name the new model or the original model was not restored"
	}
	return verdict("set_model", ok, detail)
}
func checkEffort(s *session) Report {
	body := s.request("set_max_thinking_tokens", object{"max_thinking_tokens": nil})
	_, events := s.turn("/effort high", nil)
	text := messageText(events, true)
	return Report{verdict("set_max_thinking_tokens", success(body), body.text("subtype")),
		verdict("/effort", strings.Contains(strings.ToLower(text), "effort"), cut(strings.TrimSpace(text), 60))}
}

var dataRequests = []string{"list_models", "get_context_usage", "get_usage", "get_session_cost", "mcp_status", "get_hooks_listing", "get_memory_dialog", "get_skills_dialog"}

func dataVerdict(subtype string, body object) Result {
	detail := keys(body["response"])
	if !success(body) {
		detail = fmt.Sprint(body)
		if body.text("error") != "" {
			detail = body.text("error")
		}
	}
	return verdict(subtype, success(body), detail)
}
func checkData(s *session) Report {
	var out Report
	for _, name := range dataRequests {
		out = append(out, dataVerdict(name, s.request(name, nil)))
	}
	return out
}
func slashVerdict(events []object) Result {
	text := strings.TrimSpace(assistantText(events))
	detail := "no text"
	if text != "" {
		detail = cut(strings.SplitN(text, "\n", 2)[0], 60)
	}
	return verdict("/cost", text != "", detail)
}
func checkSlash(s *session) Result { _, events := s.turn("/cost", nil); return slashVerdict(events) }
func backgroundVerdict(task string, events []object) Result {
	listed, ended := false, false
	for _, ev := range events {
		if system(ev, "background_tasks_changed") {
			listed = false
			for _, v := range ev.list("tasks") {
				if asObject(v).text("task_id") == task {
					listed = true
				}
			}
		}
		ended = ended || taskEnded(ev, task)
	}
	detail := fmt.Sprintf("listed %t, ended before it was stopped %t", listed, ended)
	if listed && !ended {
		detail = "a background task outlives its turn, listed by background_tasks_changed"
	}
	return verdict("background", listed && !ended, detail)
}
func stopVerdict(task string, body object, events []object) Result {
	stopped := slices.ContainsFunc(events, func(e object) bool { return taskEnded(e, task) })
	detail := body.text("subtype") + ", no stop seen"
	if stopped {
		detail = "the task is stopped and the stream says so"
	}
	return verdict("stop_task", success(body) && stopped, detail)
}
func checkBackground(s *session) Report {
	start := len(s.seen)
	// Model turns and mode switches have variable latency. The task must end
	// through stop_task, not a timer that can expire while a letter is read.
	s.turn("Use the Bash tool with run_in_background set to true to run exactly: tail -f /dev/null — then reply with the word done without waiting for it. Leave the task running; the contract will stop it.", nil)
	task := ""
	for _, ev := range s.seen[start:] {
		if system(ev, "task_started") {
			task = ev.text("task_id")
		}
	}
	if task == "" {
		return Report{verdict("background", false, "no background task started")}
	}
	time.Sleep(6 * time.Second)
	// Exercise letters in a session that runs without prompts; fromMode
	// missing here holds a letter for a person.
	mode := s.request("set_permission_mode", object{"mode": "bypassPermissions"})
	var letter Result
	if success(mode) {
		id := s.letter("bypass", "STILLLETTER")
		letter = s.readLetter("bypass", "STILLLETTER", id, s.waitLetter(id))
	} else {
		letter = verdict("letter.bypass", false, fmt.Sprintf("set_permission_mode bypassPermissions: %v", mode))
		s.turn("Reply with the single word: still", nil)
	}
	back := s.request("set_permission_mode", object{"mode": s.manual})
	if !success(back) {
		letter.Status = Fail
		letter.Detail += "; mode was not restored"
	}
	out := Report{backgroundVerdict(task, s.seen[start:]), letter}
	mark := len(s.seen)
	stop := s.request("stop_task", object{"task_id": task})
	if !slices.ContainsFunc(s.seen[mark:], func(e object) bool { return taskEnded(e, task) }) {
		s.pump(func(e object) bool { return taskEnded(e, task) }, 10*time.Second, nil)
	}
	return append(out, stopVerdict(task, stop, s.seen[mark:]))
}
