package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"aacpanel/internal/hook"
)

type hookMatcher struct {
	Matcher     string   `json:"matcher"`
	CallbackIDs []string `json:"hookCallbackIds"`
}

// The hook gets time to return after its own wait, as it does in the kit.
// Tests shorten the grace and put a stand-in at the installed path.
var (
	hookGrace      = 5 * time.Second
	hookExecutable = os.Executable
)

// onHook runs beside the reader: claude can ask another callback or report
// state while a hook is still working. Every outcome gets an answer, since
// a request left unanswered would hold the turn indefinitely.
func (h *Holder) onHook(requestID, name string, input json.RawMessage) {
	response := map[string]json.RawMessage{}
	defer func() {
		if p := recover(); p != nil {
			h.logf("the %s hook failed inside the holder: %v\n%s", name, p, debug.Stack())
		}
		_ = h.write(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "success", "request_id": requestID, "response": response,
		}})
	}()
	event := hook.HeldEvent(name)
	var payload struct {
		Event string `json:"hook_event_name"`
	}
	if event == "" || json.Unmarshal(input, &payload) != nil || payload.Event != event {
		return
	}
	ctx, cancel := context.WithTimeout(h.hookCtx, hook.Wait(name)+hookGrace)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.hookPath, hook.Command, name, event)
	cmd.Dir = h.spec.Dir
	cmd.Env = append(h.cmd.Environ(), hook.PIDEnv+"="+strconv.Itoa(h.cmd.Process.Pid))
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = io.Discard
	// A child keeping stdout open must not outlive the deadline of the hook.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		h.logf("the %s hook did not answer: %v", name, err)
		return
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return
	}
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(out, &answer); err != nil || answer == nil {
		h.logf("the %s hook did not answer with a JSON object", name)
		return
	}
	response = answer
}
