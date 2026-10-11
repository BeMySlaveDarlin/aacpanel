// Package hook answers the hooks of claude the executor serves:
// aacpanel-exec hook <name> reads the payload of the event on stdin and
// answers on stdout. A hook of the executor needs neither python nor the
// tree of the panel on the machine, only the binary every install has.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// A hook never holds the session it serves, so every one of them exits 0,
// whatever befalls it. An exit of 2 is claude's word for "hold": a Stop hook
// that exits 2 does not let the session stop and is asked again at the next
// stop, a loop that lasts as long as the hook keeps failing; a PreToolUse hook
// refuses the call, a UserPromptSubmit one throws the prompt away. What a hook
// has to say goes in the JSON of its answer instead: a Stop is held by
// {"decision": "block", "reason": …}, which lets the turn go on for one round
// with the reason, and the stop after that round comes with stop_hook_active.
// A Go panic exits 2 as well; the dispatcher takes it back and says it on
// stderr, which claude shows in its log and passes over.

// Command is the word of the executor's command line that answers a hook:
// <home>/bin/aacpanel-exec hook <name>.
const Command = "hook"

// Reply is what a hook answers: nothing, the event held with a reason, or
// words for the model beside the event.
type Reply struct {
	// Block holds the event: a Stop goes on for a round with the reason as
	// its prompt, a tool call is refused with it.
	Block string
	// Context is said to the model as additional context of the event, which
	// holds nothing.
	Context string
}

// handler is a hook of the executor: the event it answers and the answer to
// a payload at a moment.
type handler struct {
	Event  string
	Answer func(payload []byte, now time.Time) Reply
}

// handlers are the hooks the executor serves, by the name the command of the
// hook gives: the base name of the script of the panel's tree whose work it
// does.
var handlers = map[string]handler{
	"background-reminder": {Event: "Stop", Answer: remindBackground},
}

// Names are the hooks the executor serves, in order.
func Names() []string {
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Run answers one event: args name the hook, in carries its payload. The
// answer goes to out and what went wrong to errs. The exit is 0 for every
// event and every outcome — an unknown name, a payload that does not read, a
// hook that panics — for the reason above.
func Run(args []string, in io.Reader, out, errs io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(errs, "aacpanel-exec hook: no hook named; the executor serves %s\n", strings.Join(Names(), ", "))
		return 0
	}
	name := args[0]
	h, ok := handlers[name]
	if !ok {
		fmt.Fprintf(errs, "aacpanel-exec hook: no hook %q; the executor serves %s\n", name, strings.Join(Names(), ", "))
		return 0
	}
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(errs, "aacpanel-exec hook %s: %v\n", name, p)
		}
	}()
	payload, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintf(errs, "aacpanel-exec hook %s: the payload: %v\n", name, err)
		return 0
	}
	if err := answer(out, h.Event, h.Answer(payload, time.Now())); err != nil {
		fmt.Fprintf(errs, "aacpanel-exec hook %s: the answer: %v\n", name, err)
	}
	return 0
}

// answer writes a reply as claude reads it, and nothing for an empty one.
func answer(out io.Writer, event string, r Reply) error {
	var v any
	switch {
	case r.Block != "":
		v = map[string]any{"decision": "block", "reason": r.Block}
	case r.Context != "":
		v = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": r.Context}}
	default:
		return nil
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
