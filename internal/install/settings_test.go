package install

import (
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/toolset"
)

const (
	repo     = "/home/u/aacpanel"
	executor = "/home/u/bin/aacpanel-exec"
)

// fullKit is the kit a run suggests: the parts always in and the quiet ones.
var fullKit = Wiring{Clone: repo, Exec: executor, Kit: []string{"relay", "limits", "tools", "copies", "nudge", "restart"}}

func wire(t *testing.T, raw string, w Wiring) (string, bool) {
	t.Helper()
	out, changed, err := Wire([]byte(raw), w)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	return string(out), changed
}

// keysOf are the top keys of a settings file, in the file's order.
func keysOf(t *testing.T, raw string) []string {
	t.Helper()
	o, err := parseSettings([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range o.members {
		out = append(out, m.Key)
	}
	return out
}

// hooked counts the entries of the hook of the kit that runs the script on
// the event, in any of its forms; a script of no hook of the kit is counted
// as a hook of its own.
func hooked(t *testing.T, raw, event, script string) int {
	t.Helper()
	h := kitHook{Event: event, Script: script}
	for _, k := range slices.Concat(kitHooks, retiredHooks) {
		if k.Event == event && k.Script == script {
			h = k
		}
	}
	n, err := wiredHooks([]byte(raw), h)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// groupsOf counts the groups of an event.
func groupsOf(t *testing.T, raw, event string) int {
	t.Helper()
	o, _ := parseSettings([]byte(raw))
	v, _ := o.get("hooks")
	hs, _ := v.(*object)
	if hs == nil {
		return 0
	}
	return len(hs.list(event))
}

func TestWiringANewOrEmptyFile(t *testing.T) {
	for _, raw := range []string{"", "{}", "{}\n", "  \n"} {
		out, changed := wire(t, raw, fullKit)
		if !changed {
			t.Fatalf("%q: nothing changed", raw)
		}
		for _, h := range kitHooks {
			want := 0
			if slices.Contains(fullKit.Kit, h.Part) {
				want = 1
			}
			if got := hooked(t, out, h.Event, h.Script); got != want {
				t.Errorf("%q: %s on %s is there %d times, want %d:\n%s", raw, h.Script, h.Event, got, want, out)
			}
		}
		if got := statusLineOf([]byte(out)); got != executor+" statusline" {
			t.Errorf("%q: the status line is %q", raw, got)
		}
		if !strings.Contains(out, `"mcp__aacpanel__session_restart"`) || !strings.Contains(out, `"mcp__aacpanel__checklist"`) {
			t.Errorf("%q: the allow rules are missing:\n%s", raw, out)
		}
		if !strings.Contains(out, "\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\n        \"matcher\": \"AskUserQuestion\",") || !strings.HasSuffix(out, "}\n") {
			t.Errorf("%q: not written the way claude writes it:\n%s", raw, out)
		}
	}
}

// The installer allows the panel's tools by a list of its own, since the
// kit splits them into two parts: the restart is a part apart. A tool the
// server allows and the list does not would ask for permission on every
// call in a session started by hand, and one the list allows and the server
// does not have is a rule for nothing.
func TestTheToolRulesAreWhatTheServerAllows(t *testing.T) {
	got := append(append([]string{}, toolRules...), restartRule)
	want := toolset.Allowed()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("the installer allows %v, the server allows %v", got, want)
	}
}

// The number of allow rules the plan, the line of a wired account and the
// demo name is the number the wiring writes into the settings, with
// Self-restart and without: a literal would hold the count of the day it was
// written, and the next tool of the panel would make it wrong.
func TestTheRulesCountedAreTheRulesWritten(t *testing.T) {
	for _, kit := range [][]string{fullKit.Kit, {"relay", "limits", "tools"}} {
		w := Wiring{Clone: repo, Kit: kit}
		out, _ := wire(t, "", w)
		var settings struct {
			Permissions struct {
				Allow []string `json:"allow"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal([]byte(out), &settings); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(settings.Permissions.Allow, "mcp__aacpanel__turn_end") {
			t.Error("turn_end would ask permission at every Stop")
		}
		if n := AllowRules(w.has); n != len(settings.Permissions.Allow) || n == 0 {
			t.Errorf("kit %v: %d allow rules are counted, %d are written: %v", kit, n,
				len(settings.Permissions.Allow), settings.Permissions.Allow)
		}
		s := &Survey{given: map[string]Given{"kit": {Value: strings.Join(kit, ",")}}}
		if s.allows() != AllowRules(w.has) {
			t.Errorf("kit %v: the plan counts %d allow rules, the wiring %d", kit, s.allows(), AllowRules(w.has))
		}
	}
}

// person is a settings file as claude writes it, with the person's own
// hooks on the events the panel uses, a status line with quotes and a
// dollar, rules of their own and keys in an order of their own.
const person = `{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "model": "opus",
  "env": {
    "FOO": "a & b <c>"
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "~/bin/guard.sh",
            "timeout": 3
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash ~/bin/cost.sh"
          }
        ]
      }
    ]
  },
  "statusLine": {
    "type": "command",
    "command": "bash -c 'echo \"$HOME\" it'\\''s mine' | tr a-z A-Z",
    "padding": 0
  },
  "permissions": {
    "allow": [
      "Bash(ls:*)"
    ],
    "defaultMode": "acceptEdits"
  },
  "effortLevel": 1.50
}
`

func TestTheirsStaysAndOrderHolds(t *testing.T) {
	out, changed := wire(t, person, fullKit)
	if !changed {
		t.Fatal("nothing changed")
	}
	if got, want := keysOf(t, out), []string{"$schema", "model", "env", "hooks", "statusLine", "permissions", "effortLevel"}; !slices.Equal(got, want) {
		t.Errorf("the keys are %q, want %q", got, want)
	}
	for _, s := range []string{`"command": "~/bin/guard.sh"`, `"command": "bash ~/bin/cost.sh"`, `"Bash(ls:*)"`, `"defaultMode": "acceptEdits"`,
		`"FOO": "a & b <c>"`, `"effortLevel": 1.50`, `"padding": 0`} {
		if !strings.Contains(out, s) {
			t.Errorf("the person's %s is gone:\n%s", s, out)
		}
	}
	if groupsOf(t, out, "PreToolUse") != 2 || hooked(t, out, "PreToolUse", "agent/ask-hook.py") != 1 {
		t.Errorf("the question hook is not a group of its own beside theirs:\n%s", out)
	}
	if hooked(t, out, "Stop", "deploy/claude/context-guard.py") != 0 || hooked(t, out, "Stop", "cost.sh") != 1 {
		t.Errorf("the Stop hooks:\n%s", out)
	}
}

// TestTheStatusLineChainsTheirsWhateverItCarries: the person's line with
// quotes and a dollar comes back out of the command exactly, as the shell
// hands it to the snapshot.
func TestTheStatusLineChainsTheirsWhateverItCarries(t *testing.T) {
	theirs := `bash -c 'echo "$HOME" it'\''s mine' | tr a-z A-Z`
	out, _ := wire(t, person, fullKit)
	cmd := statusLineOf([]byte(out))
	if !strings.HasPrefix(cmd, StatusNextEnv+"=") || !strings.HasSuffix(cmd, " "+executor+" statusline") {
		t.Fatalf("the status line is %q", cmd)
	}
	next, ok := statusNext(cmd)
	if !ok || next != theirs {
		t.Fatalf("the chain reads %q (%v), want %q", next, ok, theirs)
	}
	// The shell is the one that reads the prefix: what it hands on is the
	// person's line, byte for byte.
	probe := strings.TrimSuffix(cmd, " "+executor+" statusline") + ` sh -c 'printf %s "$` + StatusNextEnv + `"'`
	got, err := exec.Command("sh", "-c", probe).Output()
	if err != nil || string(got) != theirs {
		t.Errorf("the shell hands on %q (%v), want %q", got, err, theirs)
	}
}

func TestWiringAgainChangesNothing(t *testing.T) {
	once, _ := wire(t, person, fullKit)
	twice, changed := wire(t, once, fullKit)
	if changed || twice != once {
		t.Errorf("a second wiring changed the file:\n%s", twice)
	}
	if groupsOf(t, twice, "PreToolUse") != 2 || strings.Count(statusLineOf([]byte(twice)), "aacpanel-exec statusline") != 1 {
		t.Errorf("a second wiring doubled something:\n%s", twice)
	}
}

// TestAMovedCloneIsChangedWhereItStands: the hooks and the status line of
// a clone that moved, and the hooks of an executor that moved with its home,
// are the panel's; their path changes in the same group, and the status line
// is not wrapped again.
func TestAMovedCloneIsChangedWhereItStands(t *testing.T) {
	old := Wiring{Clone: "/srv/old-clone", Exec: "/srv/old-home/bin/aacpanel-exec", Kit: fullKit.Kit}
	was, _ := wire(t, person, old)
	now, changed := wire(t, was, fullKit)
	if !changed || strings.Contains(now, "/srv/old-clone") || strings.Contains(now, "/srv/old-home") {
		t.Fatalf("the old path stayed:\n%s", now)
	}
	for _, e := range []string{"PreToolUse", "Stop", "PostToolUse", "SessionStart"} {
		if groupsOf(t, now, e) != groupsOf(t, was, e) {
			t.Errorf("%s: %d groups, was %d:\n%s", e, groupsOf(t, now, e), groupsOf(t, was, e), now)
		}
	}
	cmd := statusLineOf([]byte(now))
	if strings.Count(cmd, "aacpanel-exec statusline") != 1 {
		t.Errorf("the status line wraps the snapshot again: %q", cmd)
	}
	if next, _ := statusNext(cmd); next != `bash -c 'echo "$HOME" it'\''s mine' | tr a-z A-Z` {
		t.Errorf("the person's line after the move: %q", next)
	}
}

// TestAChainWrappedTwiceIsPeeled: a snapshot that an older hand wrapped in
// a snapshot calls it once after the wiring.
func TestAChainWrappedTwiceIsPeeled(t *testing.T) {
	inner := statusCommand("/srv/old/aacpanel-exec", "~/bin/line.sh")
	raw := `{"statusLine": {"type": "command", "command": ` + quoteJSON(statusCommand("/srv/old/aacpanel-exec", inner)) + `}}`
	out, _ := wire(t, raw, fullKit)
	if cmd := statusLineOf([]byte(out)); cmd != statusCommand(executor, "~/bin/line.sh") {
		t.Errorf("the status line is %q", cmd)
	}
}

func TestBothStatusLineFormsUnchainAndMixedChainsRunOnce(t *testing.T) {
	old := "bash /srv/old/agent/rate-snapshot.sh"
	current := executor + " statusline"
	for _, base := range []string{old, current} {
		for _, next := range []string{"", "printf 'mine'"} {
			command := base
			if next != "" {
				command = StatusNextEnv + "=" + singleQuote(next) + " " + base
			}
			raw := []byte(`{"statusLine":{"type":"command","command":` + quoteJSON(command) + `}}`)
			out, changed, err := Unwire(raw)
			if err != nil || !changed || statusLineOf(out) != next {
				t.Errorf("unwire %q: %s, changed %v, %v", command, out, changed, err)
			}
			wrapped := StatusNextEnv + "=" + singleQuote(command) + " " + current
			raw = []byte(`{"statusLine":{"type":"command","command":` + quoteJSON(wrapped) + `}}`)
			out, _, err = Wire(raw, fullKit)
			if err != nil || statusLineOf(out) != statusCommand(executor, next) {
				t.Errorf("mixed chain %q: %s, %v", wrapped, out, err)
			}
		}
	}
}

func quoteJSON(s string) string {
	var b strings.Builder
	o := &object{}
	o.set("s", s)
	b.Write(encodeSettings(o))
	v := strings.TrimSpace(b.String())
	return strings.TrimSuffix(strings.TrimPrefix(v, "{\n  \"s\": "), "\n}")
}

// TestDuplicatesOfThePanelsGo: two copies of the question hook, one under
// another matcher, and two copies of a rule become one of each.
func TestDuplicatesOfThePanelsGo(t *testing.T) {
	raw := `{"hooks": {"PreToolUse": [
  {"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "python3 /a/agent/ask-hook.py", "timeout": 5}]},
  {"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "python3 /b/agent/ask-hook.py"}, {"type": "command", "command": "mine.sh"}]},
  {"matcher": ".*", "hooks": [{"type": "command", "command": "python3 /c/agent/ask-hook.py"}]}
]}, "permissions": {"allow": ["mcp__aacpanel__notify", "x", "mcp__aacpanel__notify"]}}`
	out, _ := wire(t, raw, fullKit)
	if hooked(t, out, "PreToolUse", "agent/ask-hook.py") != 1 || groupsOf(t, out, "PreToolUse") != 2 || !strings.Contains(out, "mine.sh") {
		t.Errorf("the duplicates:\n%s", out)
	}
	if strings.Count(out, "mcp__aacpanel__notify") != 1 || !strings.Contains(out, `"x"`) {
		t.Errorf("the rules:\n%s", out)
	}
	if !strings.Contains(out, `"command": "`+executor+` hook ask-hook"`) {
		t.Errorf("the hook left kept an old path or is not the executor's:\n%s", out)
	}
}

// TestTheKitDecidesWhatStays: a part taken off the kit takes its hook and
// its rule with it, and leaves the rest.
func TestTheKitDecidesWhatStays(t *testing.T) {
	full := Wiring{Clone: repo, Exec: executor, Kit: append(slices.Clone(fullKit.Kit), "stamp", "background")}
	was, _ := wire(t, person, full)
	if hooked(t, was, "UserPromptSubmit", "deploy/claude/prompt-stamp.py") != 1 || hooked(t, was, "Stop", "deploy/claude/background-reminder.py") != 1 {
		t.Fatalf("the stamp and the background reminder are not wired:\n%s", was)
	}
	if len(commandsOf(t, was, "PostToolBatch")) != 0 {
		t.Errorf("a tool batch hook is wired:\n%s", was)
	}
	if !strings.Contains(was, `"command": "`+executor+` hook background-reminder"`) {
		t.Errorf("the background reminder is not the executor's hook:\n%s", was)
	}
	less := Wiring{Clone: repo, Exec: executor, Kit: []string{"relay", "limits", "tools", "cap"}}
	now, _ := wire(t, was, less)
	for _, h := range kitHooks {
		want := 0
		if slices.Contains(less.Kit, h.Part) {
			want = 1
		}
		if got := hooked(t, now, h.Event, h.Script); got != want {
			t.Errorf("%s on %s: %d, want %d", h.Script, h.Event, got, want)
		}
	}
	if strings.Contains(now, restartRule) || !strings.Contains(now, "mcp__aacpanel__brief_publish") {
		t.Errorf("the rules after Self-restart came off:\n%s", now)
	}
	if strings.Contains(now, "UserPromptSubmit") || strings.Contains(now, "SubagentStop") {
		t.Errorf("an event emptied by the kit stays:\n%s", now)
	}
}

// Retiring one event of a hook must not take it off another: the stamp at a
// prompt shares both its executor name and its script with the batch entry.
func TestWiringRetiresTheBatchStampAndKeepsThePromptStamp(t *testing.T) {
	w := Wiring{Clone: repo, Exec: executor, Kit: []string{"stamp"}}
	for name, command := range map[string]string{
		"executor":            "aacpanel-exec hook prompt-stamp PostToolBatch",
		"executor with state": "AACP_STATE_DIR=/srv/st /srv/old/bin/aacpanel-exec hook prompt-stamp PostToolBatch",
		"python":              "python3 /srv/old/deploy/claude/prompt-stamp.py PostToolBatch",
		"python with state":   "AACP_STATE_DIR=/srv/st python3 /srv/old/deploy/claude/prompt-stamp.py PostToolBatch",
		"guarded python":      "test -f /srv/old/deploy/claude/prompt-stamp.py && python3 /srv/old/deploy/claude/prompt-stamp.py PostToolBatch",
		"guarded with state":  "test -f /srv/old/deploy/claude/prompt-stamp.py && AACP_STATE_DIR='/srv/st' python3 /srv/old/deploy/claude/prompt-stamp.py PostToolBatch",
	} {
		t.Run(name, func(t *testing.T) {
			raw := `{"hooks": {
  "PostToolBatch": [{"matcher": "*", "hooks": [{"type": "command", "command": ` + quoteJSON(command) + `}]}],
  "UserPromptSubmit": [{"hooks": [
    {"type": "command", "command": "bash ~/my-prompt.sh"},
    {"type": "command", "command": "python3 /srv/old/deploy/claude/prompt-stamp.py"}
  ]}]
}}`
			out, changed := wire(t, raw, w)
			if !changed || groupsOf(t, out, "PostToolBatch") != 0 {
				t.Errorf("the batch stamp stayed:\n%s", out)
			}
			if got := commandsOf(t, out, "UserPromptSubmit"); !slices.Equal(got, []string{"bash ~/my-prompt.sh", executor + " hook prompt-stamp"}) {
				t.Errorf("the prompt hooks: %q", got)
			}
			again, changed := wire(t, out, w)
			if changed || again != out {
				t.Errorf("a second wiring changed the file:\n%s", again)
			}
		})
	}
}

// A missing executor must not hold a Stop or refuse a prompt or a tool call:
// claude takes exit 2 for a blocking hook failure.
func TestAHookWhoseExecutorIsGoneHoldsNothing(t *testing.T) {
	clone := t.TempDir()
	w := Wiring{Clone: clone, Exec: filepath.Join(clone, "bin", "aacpanel-exec"), Kit: fullKit.Kit}
	for _, h := range kitHooks {
		cmd := exec.Command("sh", "-c", w.command(h))
		cmd.Stdin = strings.NewReader("{}")
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() == 2 {
			t.Errorf("%s on %s with its executor gone: %v — expected an exit other than 0 and 2", h.Hook, h.Event, err)
		}
	}
}

func TestAStateDirectoryOfItsOwnIsNamedToTheHooksThatReadIt(t *testing.T) {
	w := Wiring{Clone: repo, Exec: executor, Kit: append(slices.Clone(fullKit.Kit), "background")}
	w.State = "/srv/aacpanel-state"
	out, _ := wire(t, "{}", w)
	if strings.Contains(out, "context-guard") ||
		!strings.Contains(out, `"command": "AACP_STATE_DIR=/srv/aacpanel-state `+executor+` hook background-reminder"`) ||
		!strings.Contains(out, `"command": "`+executor+` hook ask-hook"`) {
		t.Errorf("the state directory:\n%s", out)
	}
	w.State = DefaultStateDir
	if out, _ := wire(t, "{}", w); strings.Contains(out, "AACP_STATE_DIR") {
		t.Errorf("the default state directory is named:\n%s", out)
	}
}

// TestThePlanCountsEveryHookPart: the parts the plan counts as hooks are the
// parts with a hook in the wiring, the question relay aside; a part wired but
// not counted would put in a hook the plan never mentioned.
func TestThePlanCountsEveryHookPart(t *testing.T) {
	var wired []string
	for _, h := range kitHooks {
		if h.Part != "relay" && !slices.Contains(wired, h.Part) {
			wired = append(wired, h.Part)
		}
	}
	if !slices.Equal(wired, hookParts) {
		t.Errorf("the wiring hooks %v, the plan counts %v", wired, hookParts)
	}
	for _, p := range Kit {
		if !slices.Contains(wired, p.ID) {
			continue
		}
		if !slices.ContainsFunc(kitHooks, func(h kitHook) bool { return h.Part == p.ID && h.Script == p.Trace }) {
			t.Errorf("%s is told by %q, which none of its hooks runs", p.ID, p.Trace)
		}
	}
}

func TestABrokenFileIsRefused(t *testing.T) {
	for _, raw := range []string{`{"hooks": {`, `{"a": 1,}`, `[1, 2]`, `"x"`, "{} {}"} {
		if out, _, err := Wire([]byte(raw), fullKit); err == nil {
			t.Errorf("%q wired into:\n%s", raw, out)
		}
		if _, _, err := Unwire([]byte(raw)); err == nil {
			t.Errorf("%q unwired", raw)
		}
	}
}

// TestUnwiringGivesTheFileBack: a file claude wrote, wired and unwired, is
// the file it was, byte for byte; a file of only the panel's is empty.
func TestUnwiringGivesTheFileBack(t *testing.T) {
	full := Wiring{Clone: repo, Exec: executor, Kit: append(slices.Clone(fullKit.Kit), "stamp", "background")}
	for _, raw := range []string{person, "{\n  \"model\": \"opus\"\n}\n"} {
		wired, _ := wire(t, raw, full)
		back, changed, err := Unwire([]byte(wired))
		if err != nil || !changed {
			t.Fatalf("unwire: %v, changed %v", err, changed)
		}
		if string(back) != raw {
			t.Errorf("unwired:\n%s\nwant:\n%s", back, raw)
		}
	}
	wired, _ := wire(t, "", full)
	back, _, _ := Unwire([]byte(wired))
	if !emptySettings(back) {
		t.Errorf("a file of only the panel's is left with:\n%s", back)
	}
	if same, changed, _ := Unwire([]byte(person)); changed || string(same) != person {
		t.Error("a file without the panel was rewritten")
	}
}

// TestAnEmptyContainerOfThePersonStays: the wiring fills the person's empty
// lists where they stand, and an unwiring of a file without the panel
// leaves them be. Once the panel is in them, only the copy kept aside
// knows they were empty: the undo gives that copy back.
func TestAnEmptyContainerOfThePersonStays(t *testing.T) {
	raw := "{\n  \"hooks\": {\n    \"Stop\": []\n  },\n  \"permissions\": {\n    \"allow\": []\n  }\n}\n"
	if back, changed, _ := Unwire([]byte(raw)); changed || string(back) != raw {
		t.Errorf("unwired:\n%s", back)
	}
	wired, _ := wire(t, raw, fullKit)
	if got := keysOf(t, wired); !slices.Equal(got, []string{"hooks", "permissions", "statusLine", "env"}) {
		t.Errorf("the keys are %q:\n%s", got, wired)
	}
	if hooked(t, wired, "Stop", "deploy/claude/context-guard.py") != 0 || groupsOf(t, wired, "Stop") != 2 {
		t.Errorf("the Stop hooks:\n%s", wired)
	}
}

// TestAChainWrittenByHandStaysAsItIsWritten: a line in double quotes, with
// a variable the shell expands, is the panel's all the same. The wiring
// leaves it be, a moved clone changes only the path, and the unwiring gives
// the person's line back as the shell would run it.
func TestAChainWrittenByHandStaysAsItIsWritten(t *testing.T) {
	line := `AACP_STATUSLINE_NEXT="bash $CLAUDE_CONFIG_DIR/statusline-command.sh \"a b\"" ` + executor + ` statusline`
	raw := `{"statusLine": {"type": "command", "command": ` + quoteJSON(line) + `}}`
	if _, changed := wire(t, raw, fullKit); !changed {
		t.Fatal("the rules and hooks were not added")
	}
	if out, _ := wire(t, raw, fullKit); statusLineOf([]byte(out)) != line {
		t.Errorf("the status line became %q", statusLineOf([]byte(out)))
	}
	moved, _ := wire(t, raw, Wiring{Clone: "/srv/new", Exec: "/srv/new/aacpanel-exec", Kit: fullKit.Kit})
	if next, ok := statusNext(statusLineOf([]byte(moved))); !ok || next != `bash $CLAUDE_CONFIG_DIR/statusline-command.sh "a b"` {
		t.Errorf("after a move the chain reads %q (%v)", next, ok)
	}
	back, _, _ := Unwire([]byte(raw))
	if got := statusLineOf(back); got != `bash $CLAUDE_CONFIG_DIR/statusline-command.sh "a b"` {
		t.Errorf("the unwiring gave back %q", got)
	}
}

func TestAStatusLineOfThePanelsItCannotReadKeepsItsForm(t *testing.T) {
	raw := `{"statusLine": {"type": "command", "command": "bash /srv/old/agent/rate-snapshot.sh | cat"}}`
	out, _ := wire(t, raw, fullKit)
	if got := statusLineOf([]byte(out)); got != executor+" statusline | cat" {
		t.Errorf("the status line is %q", got)
	}
}

// wordIs is the length the env of the settings gives a server's word, as
// written, or none.
func wordIs(t *testing.T, raw string) string {
	t.Helper()
	v, ok := wordIn([]byte(raw))
	if !ok {
		return "none"
	}
	return v
}

// The wiring raises the length of a server's word claude keeps to the
// launcher's, in the env claude puts into its own environment: a session
// started by hand keeps the panel's word whole. A length of the person's at
// least as large stays, a smaller one is raised, and the person's other
// variables stay; without the panel's tools there is nothing to raise it for.
// The unwiring takes out the panel's length alone.
func TestTheLengthOfTheWordIsRaisedInTheEnv(t *testing.T) {
	want := strconv.Itoa(toolset.WordMax)
	for _, c := range []struct{ name, raw, after string }{
		{"no env", "{}", want},
		{"smaller", `{"env": {"FOO": "1", "` + toolset.WordEnv + `": "100"}}`, want},
		{"not a number", `{"env": {"` + toolset.WordEnv + `": "lots"}}`, want},
		{"larger", `{"env": {"` + toolset.WordEnv + `": "10000"}}`, "10000"},
		{"larger, as a number", `{"env": {"` + toolset.WordEnv + `": 8000}}`, "8000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, _ := wire(t, c.raw, fullKit)
			if got := wordIs(t, out); got != c.after {
				t.Errorf("the length is %s, want %s:\n%s", got, c.after, out)
			}
			if strings.Contains(c.raw, "FOO") && !strings.Contains(out, `"FOO": "1"`) {
				t.Errorf("the person's variable is gone:\n%s", out)
			}
			back, _, err := Unwire([]byte(out))
			if err != nil {
				t.Fatal(err)
			}
			if got := wordIs(t, string(back)); c.after == want && got != "none" || c.after != want && got != c.after {
				t.Errorf("unwired, the length is %s:\n%s", got, back)
			}
		})
	}
	bare := Wiring{Clone: repo, Exec: executor, Kit: []string{"relay", "limits"}}
	if out, _ := wire(t, "{}", bare); wordIs(t, out) != "none" {
		t.Errorf("a kit without the panel's tools raised the length:\n%s", out)
	}
}

func TestTheTurnReminderBelongsToTheClaudeNudge(t *testing.T) {
	s := &Survey{given: map[string]Given{"kit": {Value: "relay,nudge"}}}
	if s.hooks() != 3 {
		t.Fatalf("the plan counts %d hooks for relay and nudge, want 3", s.hooks())
	}
	i := slices.IndexFunc(kitHooks, func(h kitHook) bool { return h.Hook == "turn-reminder" })
	if i < 0 {
		t.Fatal("the turn reminder is not wired")
	}
	h := kitHooks[i]
	if h.Part != "nudge" || h.Event != "Stop" || h.Timeout != 5 || h.Codex || !h.State {
		t.Fatalf("the turn reminder is wired as %+v", h)
	}
}
