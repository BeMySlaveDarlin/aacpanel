package install

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const repo = "/home/u/aacpanel"

// fullKit is the kit a run suggests: the parts always in and the quiet ones.
var fullKit = Wiring{Clone: repo, Kit: []string{"relay", "limits", "tools", "copies", "brief", "cap", "nudge", "restart"}}

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

func hooked(t *testing.T, raw, event, script string) int {
	t.Helper()
	n, err := wiredHooks([]byte(raw), event, script)
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
		if got := statusLineOf([]byte(out)); got != "bash "+repo+"/agent/rate-snapshot.sh" {
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
	if hooked(t, out, "Stop", "deploy/claude/context-guard.py") != 1 || hooked(t, out, "Stop", "cost.sh") != 1 {
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
	if !strings.HasPrefix(cmd, StatusNextEnv+"=") || !strings.HasSuffix(cmd, " bash "+repo+"/agent/rate-snapshot.sh") {
		t.Fatalf("the status line is %q", cmd)
	}
	next, ok := statusNext(cmd)
	if !ok || next != theirs {
		t.Fatalf("the chain reads %q (%v), want %q", next, ok, theirs)
	}
	// The shell is the one that reads the prefix: what it hands on is the
	// person's line, byte for byte.
	probe := strings.TrimSuffix(cmd, " bash "+repo+"/agent/rate-snapshot.sh") + ` sh -c 'printf %s "$` + StatusNextEnv + `"'`
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
	if groupsOf(t, twice, "PreToolUse") != 2 || strings.Count(statusLineOf([]byte(twice)), "rate-snapshot.sh") != 1 {
		t.Errorf("a second wiring doubled something:\n%s", twice)
	}
}

// TestAMovedCloneIsChangedWhereItStands: the hooks and the status line of
// a clone that moved are the panel's; their path changes in the same group,
// and the status line is not wrapped again.
func TestAMovedCloneIsChangedWhereItStands(t *testing.T) {
	old := Wiring{Clone: "/srv/old-clone", Kit: fullKit.Kit}
	was, _ := wire(t, person, old)
	now, changed := wire(t, was, fullKit)
	if !changed || strings.Contains(now, "/srv/old-clone") {
		t.Fatalf("the old path stayed:\n%s", now)
	}
	for _, e := range []string{"PreToolUse", "Stop", "PostToolUse", "SessionStart"} {
		if groupsOf(t, now, e) != groupsOf(t, was, e) {
			t.Errorf("%s: %d groups, was %d:\n%s", e, groupsOf(t, now, e), groupsOf(t, was, e), now)
		}
	}
	cmd := statusLineOf([]byte(now))
	if strings.Count(cmd, "rate-snapshot.sh") != 1 {
		t.Errorf("the status line wraps the snapshot again: %q", cmd)
	}
	if next, _ := statusNext(cmd); next != `bash -c 'echo "$HOME" it'\''s mine' | tr a-z A-Z` {
		t.Errorf("the person's line after the move: %q", next)
	}
}

// TestAChainWrappedTwiceIsPeeled: a snapshot that an older hand wrapped in
// a snapshot calls it once after the wiring.
func TestAChainWrappedTwiceIsPeeled(t *testing.T) {
	inner := statusCommand("/srv/old", "~/bin/line.sh")
	raw := `{"statusLine": {"type": "command", "command": ` + quoteJSON(statusCommand("/srv/old", inner)) + `}}`
	out, _ := wire(t, raw, fullKit)
	if cmd := statusLineOf([]byte(out)); cmd != statusCommand(repo, "~/bin/line.sh") {
		t.Errorf("the status line is %q", cmd)
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
	if !strings.Contains(out, "python3 "+repo+"/agent/ask-hook.py") {
		t.Errorf("the hook left kept an old path:\n%s", out)
	}
}

// TestTheKitDecidesWhatStays: a part taken off the kit takes its hook and
// its rule with it, and leaves the rest.
func TestTheKitDecidesWhatStays(t *testing.T) {
	full := Wiring{Clone: repo, Kit: append(slices.Clone(fullKit.Kit), "stamp", "cost")}
	was, _ := wire(t, person, full)
	if hooked(t, was, "PostToolBatch", "deploy/claude/prompt-stamp.py") != 1 || hooked(t, was, "SubagentStop", "deploy/claude/cost-snapshot.py") != 1 {
		t.Fatalf("the stamp and the cost are not wired:\n%s", was)
	}
	if !strings.Contains(was, `"command": "python3 `+repo+`/deploy/claude/prompt-stamp.py PostToolBatch"`) {
		t.Errorf("the stamp between tools lacks its argument:\n%s", was)
	}
	less := Wiring{Clone: repo, Kit: []string{"relay", "limits", "tools", "cap"}}
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

func TestAStateDirectoryOfItsOwnIsNamedToTheHooksThatReadIt(t *testing.T) {
	w := fullKit
	w.State = "/srv/aacpanel-state"
	out, _ := wire(t, "{}", w)
	if !strings.Contains(out, `"command": "AACP_STATE_DIR=/srv/aacpanel-state python3 `+repo+`/deploy/claude/context-guard.py"`) ||
		!strings.Contains(out, `"command": "python3 `+repo+`/agent/ask-hook.py"`) {
		t.Errorf("the state directory:\n%s", out)
	}
	w.State = DefaultStateDir
	if out, _ := wire(t, "{}", w); strings.Contains(out, "AACP_STATE_DIR") {
		t.Errorf("the default state directory is named:\n%s", out)
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
	full := Wiring{Clone: repo, Kit: append(slices.Clone(fullKit.Kit), "stamp", "cost")}
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
	if got := keysOf(t, wired); !slices.Equal(got, []string{"hooks", "permissions", "statusLine"}) {
		t.Errorf("the keys are %q:\n%s", got, wired)
	}
	if hooked(t, wired, "Stop", "deploy/claude/context-guard.py") != 1 || groupsOf(t, wired, "Stop") != 2 {
		t.Errorf("the Stop hooks:\n%s", wired)
	}
}

// TestAChainWrittenByHandStaysAsItIsWritten: a line in double quotes, with
// a variable the shell expands, is the panel's all the same. The wiring
// leaves it be, a moved clone changes only the path, and the unwiring gives
// the person's line back as the shell would run it.
func TestAChainWrittenByHandStaysAsItIsWritten(t *testing.T) {
	line := `AACP_STATUSLINE_NEXT="bash $CLAUDE_CONFIG_DIR/statusline-command.sh \"a b\"" bash ` + repo + `/agent/rate-snapshot.sh`
	raw := `{"statusLine": {"type": "command", "command": ` + quoteJSON(line) + `}}`
	if _, changed := wire(t, raw, fullKit); !changed {
		t.Fatal("the rules and hooks were not added")
	}
	if out, _ := wire(t, raw, fullKit); statusLineOf([]byte(out)) != line {
		t.Errorf("the status line became %q", statusLineOf([]byte(out)))
	}
	moved, _ := wire(t, raw, Wiring{Clone: "/srv/new", Kit: fullKit.Kit})
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
	if got := statusLineOf([]byte(out)); got != "bash "+repo+"/agent/rate-snapshot.sh | cat" {
		t.Errorf("the status line is %q", got)
	}
}
