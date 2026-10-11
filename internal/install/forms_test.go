package install

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/hook"
)

// former is an account wired by an older install from another clone, by hand
// in places, with hooks of the machine's own between the panel's: the forms
// test -f … && python3 …, with the state directory and without, a bare
// python3 of a hand, the cost snapshot the kit no longer has, and the
// session context and budget check of the machine in the groups and events
// the panel uses.
const former = `{
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "bash /home/u/atlas-scripts/agent-context/session-context.sh", "timeout": 5}]},
      {"hooks": [{"type": "command", "command": "test -f /srv/old/deploy/claude/brief-waiting.py && python3 /srv/old/deploy/claude/brief-waiting.py", "timeout": 5}]}
    ],
    "PreToolUse": [
      {"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "python3 /srv/old/agent/ask-hook.py", "timeout": 5}]}
    ],
    "PostToolBatch": [
      {"matcher": "*", "hooks": [
        {"type": "command", "command": "test -f /srv/old/deploy/claude/prompt-stamp.py && AACP_STATE_DIR='/srv/st' python3 /srv/old/deploy/claude/prompt-stamp.py PostToolBatch", "timeout": 5},
        {"type": "command", "command": "python3 /home/u/atlas-scripts/agent-budget/agent-budget.py"}
      ]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "test -f /srv/old/deploy/claude/context-guard.py && python3 /srv/old/deploy/claude/context-guard.py", "timeout": 5}]},
      {"hooks": [{"type": "command", "command": "test -f /srv/old/deploy/claude/cost-snapshot.py && python3 /srv/old/deploy/claude/cost-snapshot.py"}]},
      {"hooks": [{"type": "command", "command": "test -f /srv/old/deploy/claude/background-reminder.py && AACP_STATE_DIR='/srv/st' python3 /srv/old/deploy/claude/background-reminder.py", "timeout": 5}]},
      {"hooks": [{"type": "command", "command": "python3 /opt/elsewhere/deploy/claude/checklist-reminder.py"}]}
    ],
    "SubagentStop": [
      {"hooks": [{"type": "command", "command": "python3 /srv/old/deploy/claude/cost-snapshot.py"}]}
    ]
  }
}
`

const (
	atlasContext = "bash /home/u/atlas-scripts/agent-context/session-context.sh"
	atlasBudget  = "python3 /home/u/atlas-scripts/agent-budget/agent-budget.py"
)

// commandsOf are the commands of an event's entries, group by group, in the
// order of the file.
func commandsOf(t *testing.T, raw, event string) []string {
	t.Helper()
	o, err := parseSettings([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := o.get("hooks")
	hs, _ := v.(*object)
	if hs == nil {
		return nil
	}
	var out []string
	for _, g := range hs.list(event) {
		for _, e := range g.(*object).list("hooks") {
			out = append(out, e.(*object).str("command"))
		}
	}
	return out
}

// timeoutOf is the timeout of the entry of an event that runs the command.
func timeoutOf(t *testing.T, raw, event, command string) string {
	t.Helper()
	o, err := parseSettings([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := o.get("hooks")
	hs, _ := v.(*object)
	if hs == nil {
		return ""
	}
	for _, g := range hs.list(event) {
		for _, e := range g.(*object).list("hooks") {
			if entry := e.(*object); entry.str("command") == command {
				n, _ := entry.get("timeout")
				return fmt.Sprint(n)
			}
		}
	}
	return ""
}

// An update over the former forms rewrites each entry where it stands into
// the form of this install: the same group, the same matcher, no second
// entry. The hook the executor took over runs the executor; the part the kit
// no longer has is gone from both of its events; the machine's own hooks stay
// where they were.
func TestAnUpdateRewritesTheFormerFormsWhereTheyStand(t *testing.T) {
	w := Wiring{Clone: repo, Exec: executor, State: "/srv/st", Kit: append(slices.Clone(fullKit.Kit), "stamp", "background")}
	out, changed := wire(t, former, w)
	if !changed {
		t.Fatal("nothing changed")
	}
	state := "AACP_STATE_DIR=/srv/st "
	want := map[string][]string{
		"SessionStart":  {atlasContext, "test -f " + repo + "/deploy/claude/brief-waiting.py && python3 " + repo + "/deploy/claude/brief-waiting.py"},
		"PreToolUse":    {"test -f " + repo + "/agent/ask-hook.py && python3 " + repo + "/agent/ask-hook.py"},
		"PostToolBatch": {state + executor + " hook prompt-stamp PostToolBatch", atlasBudget},
		"Stop": {state + executor + " hook context-guard",
			state + executor + " hook background-reminder",
			"test -f " + repo + "/deploy/claude/checklist-reminder.py && python3 " + repo + "/deploy/claude/checklist-reminder.py"},
	}
	for event, cmds := range want {
		if got := commandsOf(t, out, event); !slices.Equal(got, cmds) {
			t.Errorf("%s runs\n  %s\nwant\n  %s", event, strings.Join(got, "\n  "), strings.Join(cmds, "\n  "))
		}
	}
	if groupsOf(t, out, "Stop") != 3 || groupsOf(t, out, "PostToolBatch") != 1 || groupsOf(t, out, "SubagentStop") != 0 {
		t.Errorf("the groups: Stop %d, PostToolBatch %d, SubagentStop %d:\n%s",
			groupsOf(t, out, "Stop"), groupsOf(t, out, "PostToolBatch"), groupsOf(t, out, "SubagentStop"), out)
	}
	// The stamp before every prompt had no entry: it is the one group added.
	if got := commandsOf(t, out, "UserPromptSubmit"); !slices.Equal(got, []string{state + executor + " hook prompt-stamp"}) {
		t.Errorf("the stamp before a prompt: %q", got)
	}
	// The guard's timeout is rewritten with its command: the guard looks at
	// the work of the session for longer than the former form's 5 seconds.
	if got := timeoutOf(t, out, "Stop", state+executor+" hook context-guard"); got != "20" {
		t.Errorf("the guard keeps a timeout of %s:\n%s", got, out)
	}
	again, changed := wire(t, out, w)
	if changed || again != out {
		t.Errorf("a second update changed the file:\n%s", again)
	}
}

// The undo takes the panel out in any of its forms, the retired part
// included, and leaves the machine's own hooks.
func TestUnwiringTakesOutEveryForm(t *testing.T) {
	w := Wiring{Clone: repo, Exec: executor, Kit: append(slices.Clone(fullKit.Kit), "stamp", "background")}
	wired, _ := wire(t, former, w)
	for name, raw := range map[string]string{"the former forms": former, "the forms of this install": wired} {
		out, changed, err := Unwire([]byte(raw))
		if err != nil || !changed {
			t.Fatalf("%s: unwire %v, changed %v", name, err, changed)
		}
		if got := commandsOf(t, string(out), "SessionStart"); !slices.Equal(got, []string{atlasContext}) {
			t.Errorf("%s: SessionStart keeps %q", name, got)
		}
		if got := commandsOf(t, string(out), "PostToolBatch"); !slices.Equal(got, []string{atlasBudget}) {
			t.Errorf("%s: PostToolBatch keeps %q", name, got)
		}
		for _, e := range []string{"PreToolUse", "Stop", "SubagentStop", "UserPromptSubmit"} {
			if got := commandsOf(t, string(out), e); len(got) != 0 {
				t.Errorf("%s: %s keeps %q", name, e, got)
			}
		}
	}
}

// The executor's hook is told by the executor and the name, wherever an
// install put the executor; a command that only mentions the name is not it.
func TestTheExecutorsHookIsToldWhereverTheExecutorLies(t *testing.T) {
	h := kitHooks[slices.IndexFunc(kitHooks, func(h kitHook) bool { return h.Part == "background" })]
	for cmd, want := range map[string]bool{
		"/home/u/bin/aacpanel-exec hook background-reminder":                                                 true,
		"AACP_STATE_DIR=/x /opt/other/aacpanel-exec hook background-reminder":                                true,
		"aacpanel-exec hook background-reminder":                                                             true,
		"test -f /a/deploy/claude/background-reminder.py && python3 /a/deploy/claude/background-reminder.py": true,
		"/home/u/bin/aacpanel-exec hook context-guard":                                                       false,
		"/home/u/bin/aacpanel-exec -mcp background-reminder":                                                 false,
		"bash ~/bin/my-hook.sh hook background-reminder":                                                     false,
	} {
		if h.owns(cmd) != want {
			t.Errorf("%q: the background reminder's %v, want %v", cmd, h.owns(cmd), want)
		}
	}
	// The guard and the stamp, in the forms an install wrote before the
	// executor took them over and in the executor's.
	for part, cmds := range map[string]map[string]bool{
		"cap": {
			"/home/u/bin/aacpanel-exec hook context-guard":                                                               true,
			"test -f /a/deploy/claude/context-guard.py && AACP_STATE_DIR='/s' python3 /a/deploy/claude/context-guard.py": true,
			"python3 /srv/old/deploy/claude/context-guard.py":                                                            true,
			"/home/u/bin/aacpanel-exec hook prompt-stamp":                                                                false,
		},
		"stamp": {
			"AACP_STATE_DIR=/x /opt/other/aacpanel-exec hook prompt-stamp PostToolBatch":                         true,
			"test -f /a/deploy/claude/prompt-stamp.py && python3 /a/deploy/claude/prompt-stamp.py PostToolBatch": true,
			"python3 /srv/old/deploy/claude/prompt-stamp.py":                                                     true,
			"/home/u/bin/aacpanel-exec hook context-guard":                                                       false,
		},
	} {
		for _, h := range kitHooks {
			if h.Part != part {
				continue
			}
			for cmd, want := range cmds {
				if h.owns(cmd) != want {
					t.Errorf("%q: the %s hook on %s's %v, want %v", cmd, part, h.Event, h.owns(cmd), want)
				}
			}
		}
	}
}

// A hook of the executor that waits before it answers has a timeout past
// the wait with a second to spare: claude kills a hook at its timeout, and
// the answer of the guard killed mid-look is lost — the session past its cap
// is not asked to restart, and stays there until someone writes to it.
func TestEveryTimeoutOfTheKitCoversTheHooksWait(t *testing.T) {
	for _, h := range kitHooks {
		if h.Hook == "" {
			continue
		}
		if timeout := time.Duration(h.Timeout) * time.Second; timeout < hook.Wait(h.Hook)+time.Second {
			t.Errorf("%s on %s: a timeout of %v for a hook that may take %v", h.Hook, h.Event, timeout, hook.Wait(h.Hook))
		}
	}
}

// Every hook of the kit that runs the executor names a hook the executor
// serves: a name it does not know would exit 0 with nothing done.
func TestEveryExecutorHookOfTheKitIsServed(t *testing.T) {
	for _, h := range kitHooks {
		if h.Hook != "" && !slices.Contains(hook.Names(), h.Hook) {
			t.Errorf("%s runs aacpanel-exec hook %s, which the executor does not serve (%v)", h.Part, h.Hook, hook.Names())
		}
	}
}
