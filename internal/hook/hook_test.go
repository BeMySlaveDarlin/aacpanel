package hook

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// with puts a hook of the test among the handlers for the length of the test.
func with(t *testing.T, name string, h handler) {
	t.Helper()
	handlers[name] = h
	t.Cleanup(func() { delete(handlers, name) })
}

func run(t *testing.T, args []string, payload string) (code int, out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Run(args, strings.NewReader(payload), &o, &e)
	return code, o.String(), e.String()
}

// A Stop is held by the answer, never by the exit: an exit of 2 would not let
// the session stop and would be asked again at the next stop.
func TestAStopIsHeldByItsAnswerNeverByItsExit(t *testing.T) {
	with(t, "holds", handler{Event: "Stop", Answer: func(string, []byte, time.Time) Reply {
		return Reply{Block: "the checklist waits"}
	}})
	code, out, _ := run(t, []string{"holds"}, `{"hook_event_name": "Stop"}`)
	if code != 0 {
		t.Errorf("a Stop held exits %d", code)
	}
	var reply map[string]string
	if err := json.Unmarshal([]byte(out), &reply); err != nil || reply["decision"] != "block" || reply["reason"] != "the checklist waits" {
		t.Errorf("the answer %q (%v)", out, err)
	}
}

// Whatever befalls a hook, its exit is 0: a hook that panics would exit 2 on
// its own, which holds a Stop in a loop and refuses a tool call.
func TestNothingAHookMeetsHoldsTheSession(t *testing.T) {
	with(t, "panics", handler{Event: "Stop", Answer: func(string, []byte, time.Time) Reply { panic("a nil map") }})
	with(t, "says", handler{Event: "PreToolUse", Answer: func(string, []byte, time.Time) Reply {
		return Reply{Context: "a word <&> for the model"}
	}})
	for _, c := range []struct {
		args              []string
		out, errs, reason string
	}{
		{[]string{"panics"}, "", "a nil map", "a hook that panics"},
		{[]string{"no-such-hook"}, "", `no hook "no-such-hook"`, "a name the executor does not serve"},
		{nil, "", "no hook named", "no name at all"},
		{[]string{"says"}, `{"hookSpecificOutput":{"additionalContext":"a word <&> for the model","hookEventName":"PreToolUse"}}` + "\n", "", "a word for the model"},
	} {
		code, out, errs := run(t, c.args, "{}")
		if code != 0 || out != c.out || !strings.Contains(errs, c.errs) {
			t.Errorf("%s: exit %d, answer %q, stderr %q", c.reason, code, out, errs)
		}
	}
}

// Every hook the executor serves answers one of the events whose exit the
// dispatcher keeps at 0.
func TestEveryHookNamesItsEvent(t *testing.T) {
	for _, name := range Names() {
		if handlers[name].Event == "" || handlers[name].Answer == nil {
			t.Errorf("%s answers no event", name)
		}
	}
}

// Reading the payload is the dispatcher's first step into a hook. A settings
// duplicate must not get that far, while callbacks and other hooks still do.
type countedInput struct {
	*strings.Reader
	reads int
}

func (in *countedInput) Read(p []byte) (int, error) {
	in.reads++
	return in.Reader.Read(p)
}

func TestSettingsCallsLeaveOnlyHeldHooksToTheHolder(t *testing.T) {
	dir, _, _ := inPlace(t)
	t.Setenv("AACP_STATE_DIR", dir)
	for _, held := range []string{"", "1"} {
		for _, pid := range []string{"", "42"} {
			for _, name := range Names() {
				t.Run(name+"/held="+held+"/pid="+pid, func(t *testing.T) {
					t.Setenv(HeldEnv, held)
					t.Setenv(PIDEnv, pid)
					in := &countedInput{Reader: strings.NewReader("{}")}
					var out, errs bytes.Buffer
					code := Run([]string{name}, in, &out, &errs)
					kept := name == "prompt-stamp" || name == "checklist-reminder" || name == "background-reminder"
					quiet := held == "1" && pid == "" && kept
					if quiet != (in.reads == 0) || code != 0 || errs.Len() != 0 {
						t.Errorf("quiet %v, reads %d, exit %d, stderr %q", quiet, in.reads, code, errs.String())
					}
					if quiet && out.Len() != 0 {
						t.Errorf("the settings duplicate answered %q", out.String())
					}
				})
			}
		}
	}
}
