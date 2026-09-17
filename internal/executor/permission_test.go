package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

const permScreen = `❯ check what is going on with the disk
  Looking now. the bank key hunter2 and a private conversation
  Do you want to proceed? — that is how the human asked earlier, this is not a dialog

Bash command
  touch ~/permtest-tmux
  Create test file in home directory
Do you want to proceed?
❯ 1. Yes
  2. Yes, and always allow access to /home/u from this project
  3. Yes, and switch to auto mode
  4. No
Esc to cancel · Tab to amend`

func TestPermissionParsesLiveDialog(t *testing.T) {
	d, ok := parsePermission(permScreen)
	if !ok {
		t.Fatal("the dialog was not found on a screen that has one")
	}
	if d.Tool != "Bash command" {
		t.Errorf("tool %q, expected «Bash command»", d.Tool)
	}
	want := []string{"touch ~/permtest-tmux", "Create test file in home directory"}
	if strings.Join(d.Action, "|") != strings.Join(want, "|") {
		t.Errorf("action %q, expected %q — the human presses «yes» going by this text", d.Action, want)
	}
	if len(d.Options) != 4 {
		t.Fatalf("%d items, expected 4: the set depends on the tool and the mode, "+
			"they must not be filled in from memory", len(d.Options))
	}
	if d.Options[0].N != 1 || d.Options[0].Text != "Yes" {
		t.Errorf("the first item %+v, expected «1. Yes»", d.Options[0])
	}
	if d.Options[3].N != 4 || d.Options[3].Text != "No" {
		t.Errorf("the last item %+v, expected «4. No»", d.Options[3])
	}
	if d.Partial {
		t.Error("the list is declared incomplete on a whole dialog")
	}
}

// permUnparsed is the same dialog with the numbering gone: the parser finds the
// anchor and no items, which is the shape a changed dialog arrives in.
var permUnparsed = strings.NewReplacer(
	"❯ 1. Yes", "❯ Yes",
	"  2. Yes, and always allow access to /home/u from this project", "  Yes, and always allow access to /home/u from this project",
	"  3. Yes, and switch to auto mode", "  Yes, and switch to auto mode",
	"  4. No", "  No",
).Replace(permScreen)

// permUnparsedFramed puts the same unnumbered dialog in a box under the conversation:
// there the edge is the frame and not a blank line.
var permUnparsedFramed = strings.SplitN(permScreen, "\n\n", 2)[0] + "\n" + strings.NewReplacer(
	"❯ 1. Yes", "❯ Yes   ",
	"  2. Yes, and always allow", "  Yes, and always allow",
	"  3. Yes, and switch to auto mode", "  Yes, and switch to auto mode   ",
	"  4. No", "  No   ",
).Replace(permFramed)

func TestPermissionShowsTheDialogItCouldNotParse(t *testing.T) {
	for _, c := range []struct{ name, screen string }{
		{"the dialog opens after a blank line", permUnparsed},
		{"the dialog stands in a box", permUnparsedFramed},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := parsePermission(c.screen); ok {
				t.Fatal("the fixture parses — it proves nothing about a dialog the panel does not know")
			}
			d := permissionFor(c.screen)
			if d == nil || !d.Unknown {
				t.Fatalf("an unparsed dialog went out as %+v", d)
			}
			if len(d.Raw) == 0 {
				t.Fatal("a dialog the panel does not know went out without its lines — " +
					"the human is sent to the console to find out what is being asked")
			}
			if d.Raw[0] != "Bash command" {
				t.Errorf("the first line is %q: the frame arrived as an indent and takes the "+
					"width of a phone screen away from the text", d.Raw[0])
			}
			body := strings.Join(d.Raw, "\n")
			for _, want := range []string{
				"Bash command",
				"touch ~/permtest-tmux",
				"Create test file in home directory",
				permAnchor,
				"Yes, and always allow access to /home/u from this project",
				"No",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("the dialog line %q is not shown: the human is told a dialog is open "+
						"and not a word about what it asks", want)
				}
			}
		})
	}
}

func TestRawDialogStopsAtTheDialogEdge(t *testing.T) {
	for _, c := range []struct{ name, screen string }{
		{"a blank line above the dialog", permUnparsed},
		{"a frame above the dialog", permUnparsedFramed},
	} {
		t.Run(c.name, func(t *testing.T) {
			lines := rawDialog(c.screen)
			if len(lines) == 0 {
				t.Fatal("nothing is shown of a dialog whose edges are both on the screen")
			}
			body := strings.Join(lines, "\n")
			for _, leak := range []string{
				"hunter2",
				"a private conversation",
				"check what is going on with the disk",
				"Looking now",
				"that is how the human asked earlier",
			} {
				if strings.Contains(body, leak) {
					t.Errorf("this leaked from the conversation into the shown dialog: %q", leak)
				}
			}
		})
	}
}

func TestRawDialogWithoutAnEdgeShowsNothing(t *testing.T) {
	const talk = `❯ remind me what the panel asks
  It asks like this:
Do you want to proceed?
  and the key it asked about was hunter2`

	noTop := strings.Repeat("  the conversation goes on, and the key is hunter2\n", 9) +
		"Do you want to proceed?\n❯ Yes\n  No\nEsc to cancel · Tab to amend"

	for _, c := range []struct{ name, screen string }{
		{"a dialog with no anchor", permHeldScreen},
		{"the anchor in the conversation, with nothing closing it", talk},
		{"the conversation runs into the anchor, with nothing opening it", noTop},
	} {
		t.Run(c.name, func(t *testing.T) {
			if lines := rawDialog(c.screen); lines != nil {
				t.Errorf("the dialog has no edge on this screen, and %d lines went out anyway: %q",
					len(lines), lines)
			}
		})
	}
}

func TestPermissionTellsLastingFromOnce(t *testing.T) {
	d, ok := parsePermission(permScreen)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	want := []bool{false, true, true, false}
	for i, o := range d.Options {
		if o.Lasting != want[i] {
			t.Errorf("item %d (%q): lasting %v, expected %v", o.N, o.Text, o.Lasting, want[i])
		}
	}

	odd := strings.Replace(permScreen, "3. Yes, and switch to auto mode",
		"3. Yes, and remember this decision forever", 1)
	got, ok := parsePermission(odd)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	if !got.Options[2].Lasting {
		t.Error("an unfamiliar item was taken for a one-off one — that is how a lasting right is granted silently")
	}
}

func TestPermissionTakesTheDialogNotTheScreen(t *testing.T) {
	d, ok := parsePermission(permScreen)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	all := d.Tool + "\n" + strings.Join(d.Action, "\n")
	for _, o := range d.Options {
		all += "\n" + o.Text
	}
	for _, leak := range []string{
		"the bank key hunter2",
		"a private conversation",
		"check what is going on with the disk",
		"Looking now",
		"Esc to cancel",
	} {
		if strings.Contains(all, leak) {
			t.Errorf("this leaked from the screen into the parsed dialog: %q", leak)
		}
	}
}

func TestPermissionSaysWhenTheListIsShort(t *testing.T) {
	for _, c := range []struct {
		name   string
		screen string
		opts   int
	}{
		{
			name:   "a foreign line among the items",
			screen: strings.Replace(permScreen, "  3. Yes, and switch to auto mode", "  (something new)", 1),
			opts:   3,
		},
		{
			name:   "numbers with a gap",
			screen: strings.Replace(permScreen, "  3. Yes, and switch to auto mode", "  5. Yes, and switch to auto mode", 1),
			opts:   4,
		},
		{
			name:   "no footer — where the list ended is unknown",
			screen: strings.Replace(permScreen, "Esc to cancel · Tab to amend", "", 1),
			opts:   4,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			if !d.Partial {
				t.Error("the list is incomplete and the card is not told so")
			}
			if len(d.Options) != c.opts {
				t.Errorf("%d items, expected %d", len(d.Options), c.opts)
			}
			for _, o := range d.Options {
				if strings.Contains(o.Text, "something new") {
					t.Error("a line nobody understood went in as an item — that is a digit landing on the wrong item")
				}
			}
		})
	}
}

func TestPermissionIgnoresTalkAboutIt(t *testing.T) {
	talk := `❯ ask me Do you want to proceed?
  All right, I will ask.
  1. This is just a list
  2. And it is not a dialog`
	if _, ok := parsePermission(talk); ok {
		t.Error("talk about a permission was taken for a permission — the panel would show buttons " +
			"with nothing behind them")
	}
	if _, ok := parsePermission("an ordinary screen without a single question"); ok {
		t.Error("a dialog was found where there is none")
	}
}

func TestPermissionFingerprintSurvivesRedraw(t *testing.T) {
	base, ok := parsePermission(permScreen)
	if !ok {
		t.Fatal("the dialog was not found")
	}

	same := []struct{ name, screen string }{
		{"a line of output arrived on top", "some command output\n" + permScreen},
		{"the conversation scrolled up", strings.Replace(permScreen, "❯ check what is going on with the disk", "", 1)},
		{"an item wrapped at the window width", strings.Replace(permScreen,
			"  2. Yes, and always allow access to /home/u from this project",
			"  2. Yes,   and always allow   access to /home/u from this project", 1)},
	}
	for _, c := range same {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			if got.Fingerprint != base.Fingerprint {
				t.Errorf("the fingerprint changed though the dialog is the same — the check will refuse on a live question")
			}
		})
	}

	other := []struct{ name, screen string }{
		{"the command changed", strings.Replace(permScreen, "touch ~/permtest-tmux", "rm -rf /", 1)},
		{"an item changed", strings.Replace(permScreen, "4. No", "4. Never", 1)},
		{"the tool changed", strings.Replace(permScreen, "Bash command", "Write file", 1)},
	}
	for _, c := range other {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			if got.Fingerprint == base.Fingerprint {
				t.Errorf("the fingerprint did not change on a different dialog — the digit will go to the wrong place")
			}
		})
	}
}

func TestPermitRefusesWhenTheDialogChanged(t *testing.T) {
	shown, ok := parsePermission(permScreen)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	moved := strings.Replace(permScreen, "touch ~/permtest-tmux", "rm -rf /", 1)
	now, ok := parsePermission(moved)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	if now.Fingerprint == shown.Fingerprint {
		t.Fatal("the fingerprint did not change on a different command — the check is useless")
	}

	found := false
	for _, o := range shown.Options {
		if o.N == 9 {
			found = true
		}
	}
	if found {
		t.Fatal("the fixture has item 9 — the check is meaningless")
	}
}

func TestPermissionErrorsCarryNoScreen(t *testing.T) {
	for _, text := range []string{
		"the screen of session aacpanel cannot be read",
		"the screen of session aacpanel was not captured",
		"the dialog of session aacpanel changed",
		"session aacpanel is asking nothing",
	} {
		for _, leak := range []string{"hunter2", "a private conversation", "Do you want to proceed"} {
			if strings.Contains(text, leak) {
				t.Errorf("the refusal %q carries this from the screen: %q", text, leak)
			}
		}
	}

	d, ok := parsePermission(permScreen)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"tool": true, "action": true, "options": true, "partial": true,
		"note": true, "cut": true, "fingerprint": true, "tail": true, "unknown": true, "raw": true}
	for name := range fields {
		if !want[name] {
			t.Errorf("a new field %q appeared in the permission — check that the screen is not put into it", name)
		}
	}
	for name := range want {
		if _, ok := fields[name]; !ok {
			t.Errorf("the field %q disappeared from the permission", name)
		}
	}
}

const permFramed = `╭──────────────────────────────────────────────────────────────╮
│ Bash command                                                 │
│   touch ~/permtest-tmux                                      │
│   Create test file in home directory                         │
│ Do you want to proceed?                                      │
│ ❯ 1. Yes                                                     │
│   2. Yes, and always allow access to /home/u from this project│
│   3. Yes, and switch to auto mode                            │
│   4. No                                                      │
│ Esc to cancel · Tab to amend                                 │
╰──────────────────────────────────────────────────────────────╯`

func TestPermissionParsesFramedScreen(t *testing.T) {
	d, ok := parsePermission(permFramed)
	if !ok {
		t.Fatal("the dialog was not found on a screen from a real terminal")
	}
	if d.Tool != "Bash command" {
		t.Errorf("tool %q, expected «Bash command» — the frame counts as a dialog line again", d.Tool)
	}
	if len(d.Action) != 2 || d.Action[0] != "touch ~/permtest-tmux" {
		t.Errorf("action %q, expected two lines of the command", d.Action)
	}
	if len(d.Options) != 4 {
		t.Errorf("%d items, expected 4", len(d.Options))
	}
	if d.Partial {
		t.Error("the list is declared incomplete on a whole dialog from a real terminal")
	}
}

const permLive = `───────────────────────────────────────────────────────────────────────────────────────────────────────
 Bash command

   touch probe.txt
   Create empty file probe.txt

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and always allow access to /home/u/.cache/claude-tmp/claude-1000/-srv-proj-Beta-service-moni
      tor/bbbbbbbb-0000-4000-8000-000000000001/scratchpad/permlab from this project
   3. No

 Esc to cancel · Tab to amend`

func TestPermissionParsesLiveClaudeDialog(t *testing.T) {
	d, ok := parsePermission(permLive)
	if !ok {
		t.Fatal("the dialog of a live session was not found")
	}
	if d.Tool != "Bash command" {
		t.Errorf("tool %q, expected «Bash command»", d.Tool)
	}
	if len(d.Action) != 2 || d.Action[0] != "touch probe.txt" {
		t.Errorf("action %q — blank lines inside the dialog cut the block short again, "+
			"and the human does not see what is being allowed", d.Action)
	}
	if len(d.Options) != 3 {
		t.Fatalf("%d items, expected 3: there is no «switch to auto» in this mode — "+
			"the set depends on the tool and the mode, it must not be filled in from memory", len(d.Options))
	}
	if !strings.HasSuffix(d.Options[1].Text, "from this project") {
		t.Errorf("item 2 is cut at the wrap: %q", d.Options[1].Text)
	}
	if strings.Contains(d.Options[1].Text, "service-moni tor") {
		t.Errorf("a space appeared where the line wrapped — the path is torn in two: %q", d.Options[1].Text)
	}
	if d.Partial {
		t.Error("a complete list is declared incomplete — a wrap was taken for a line nobody understood")
	}
	if !d.Options[1].Lasting {
		t.Error("«always allow» is not marked as lasting")
	}
}

const permTip = `───────────────────────────────────────────────────────────────────────────────────────────────────────
 Bash command
 Tip: auto mode handles these prompts for you — choose "switch to auto mode" below and never see
      this line again

   echo ok > probe.txt && cat probe.txt
   Write ok to probe.txt

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and switch to auto mode · auto mode handles these prompts for you
   3. No

 Esc to cancel · Tab to amend`

func TestPermissionDropsTheAutoModeTip(t *testing.T) {
	d, ok := parsePermission(permTip)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	if d.Tool != "Bash command" {
		t.Errorf("tool %q: the tip pushed out the heading", d.Tool)
	}
	for _, line := range d.Action {
		if strings.HasPrefix(line, "Tip:") {
			t.Errorf("the tip went into the card: %q", line)
		}
		if strings.Contains(line, "this line again") {
			t.Errorf("the tail of the wrapped tip went into the card: %q", line)
		}
	}
	want := []string{"echo ok > probe.txt && cat probe.txt", "Write ok to probe.txt"}
	if len(d.Action) != len(want) {
		t.Fatalf("action %q, expected %q — something else was thrown out along with the tip", d.Action, want)
	}
	for i := range want {
		if d.Action[i] != want[i] {
			t.Errorf("line %d of the action is %q, expected %q", i, d.Action[i], want[i])
		}
	}
}

const permHeldScreen = `❯ check what is going on with the disk
  Looking now.
───────────────────────────────────────────────────────────────────────────────
 Held message from another session

 Another Claude session sent a message: from uds:/run/user/1000/cc-socks/12345.sock
 [verified pid 12345] (peer claims name: notes)

 The sending session's permission mode class doesn't match this session's, so it
 wasn't delivered automatically.

 Message body (this is what will be delivered):
 «Second run …
 …[9 lines, 1116 chars total — full body will be delivered on approve]»

 › Deny — drop it and tell the sender it was declined
   Deliver this message to Claude`

const permIdleScreen = `  Got it, writing the fix.
──────────── aacpanel ─
❯ tell me when they are done
──────────────────────
  ──────────
   Opus 5 (1M context)`

var permTalkedAbout = "❯ what was on the screen just now?\n" +
	"  At the bottom it said «Esc to cancel · Tab to amend», so I pressed Esc.\n" +
	strings.Repeat("  and the conversation went on as usual\n", 25) +
	permIdleScreen

func TestUnknownDialogTellsItselfFromAnEmptyScreen(t *testing.T) {
	for _, c := range []struct {
		name   string
		screen string
		want   bool
	}{
		{"a held letter: a cursor with no numbers", permHeldScreen, true},
		{"the footer is there, the anchor is not", strings.Replace(permScreen, "Do you want to proceed?\n❯ 1. Yes", "Ready to continue?\n❯ 1. Yes", 1), true},
		{"the box is there, the anchor and the footer are not", strings.NewReplacer(
			"Do you want to proceed?", "Ready to continue?  ",
			"Esc to cancel · Tab to amend", "arrows and enter            ",
		).Replace(permFramed), true},
		{"a live dialog with no anchor", strings.Replace(permLive, "Do you want to proceed?", "Ready to continue?", 1), true},
		{"the composer and the idle hint", permIdleScreen, false},
		{"an arrow list inside a model answer", "› first — ask claude\n› second — hardcode the list", true},
		{"the dialog footer retold in the conversation", permTalkedAbout, false},
		{"talk about a permission", "❯ ask me Do you want to proceed?\n  All right, I will ask.\n  1. This is just a list", false},
		{"an empty screen", "an ordinary screen without a single question", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := dialogOnScreen(c.screen); got != c.want {
				t.Errorf("dialog on screen=%v, expected %v", got, c.want)
			}
		})
	}
}

func TestUnknownDialogCarriesNothingFromTheScreen(t *testing.T) {
	d := permissionFor(permHeldScreen)
	if d == nil {
		t.Fatal("on a screen with a held letter the panel still sees nothing")
	}
	if !d.Unknown {
		t.Fatal("a held letter was parsed as a permission — there is nothing to press in it")
	}
	if d.Tool != "" || len(d.Action) != 0 || len(d.Options) != 0 || d.Fingerprint != "" {
		t.Errorf("an unknown dialog went out carrying content: %+v", d)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"check what is going on with the disk", "Deny", "peer claims name", "Held message"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("this leaked from the screen into the unknown dialog: %q", leak)
		}
	}
}

func TestPermissionForKeepsTheThreeAnswersApart(t *testing.T) {
	known := permissionFor(permScreen)
	if known == nil || known.Unknown {
		t.Fatalf("the parsed dialog got lost: %+v", known)
	}
	if len(known.Options) != 4 {
		t.Errorf("%d items, expected 4", len(known.Options))
	}
	if got := permissionFor(permIdleScreen); got != nil {
		t.Errorf("on a screen with no dialog the panel found %+v — it will claim «a dialog is waiting» where there is none", got)
	}
}

// The wordings below are the ones the client actually asks with; only the frame
// around them is written here. The question is not what the dialog is found by, and
// these are what would break it if it were.
const permEditScreen = `❯ tighten the rule

Edit file
  internal/rules/rules.go
  Replace the threshold with the one the config gives
Do you want to make this edit to rules.go?
❯ 1. Yes
  2. Yes, allow all edits during this session
  3. No, and tell Claude what to do differently
Esc to cancel`

const permFetchScreen = `Fetch content
  https://example.invalid/docs
Do you want to allow Claude to fetch this content?
❯ 1. Yes
  2. Yes, and don't ask again for example.invalid
  3. No
esc to cancel`

func TestPermissionReadsTheOtherWordings(t *testing.T) {
	for _, c := range []struct {
		name, screen, tool string
		action             []string
		options            int
	}{
		{
			name:    "an edit to a named file",
			screen:  permEditScreen,
			tool:    "Edit file",
			action:  []string{"internal/rules/rules.go", "Replace the threshold with the one the config gives"},
			options: 3,
		},
		{
			name:    "a page to fetch, and a lowercase footer",
			screen:  permFetchScreen,
			tool:    "Fetch content",
			action:  []string{"https://example.invalid/docs"},
			options: 3,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not read: the panel knows one wording and the client has many")
			}
			if d.Tool != c.tool {
				t.Errorf("tool %q, expected %q", d.Tool, c.tool)
			}
			if strings.Join(d.Action, "|") != strings.Join(c.action, "|") {
				t.Errorf("action %q, expected %q — this is the text the human answers by", d.Action, c.action)
			}
			if len(d.Options) != c.options {
				t.Fatalf("%d items, expected %d", len(d.Options), c.options)
			}
			if d.Options[0].N != 1 {
				t.Errorf("the list starts at %d", d.Options[0].N)
			}
		})
	}
}

func TestANumberedListInTheTalkIsNoDialog(t *testing.T) {
	const talk = `❯ what is left to do
  Three things:
  1. the parser
  2. the screen
  3. the tests
  Tell me which one to take.`

	if d, ok := parsePermission(talk); ok {
		t.Errorf("a list in an answer was read as a dialog asking %q", d.Tool)
	}
	if lines := rawDialog(talk); lines != nil {
		t.Errorf("an answer was shown as a dialog: %q", lines)
	}
}

// The dialogs below are as tall as the console draws them: a rule across the
// screen, the heading, the command — barred down the left side inside its box when
// it runs to several lines — with its description, and, when a hook asks for the
// confirmation, the hook's words barred at the edge of the dialog under the box.
// The question stands well below the heading, further than a short dialog puts it.
const permLongScreen = `❯ collect the ids
  Looking at the rows now, the token is hunter2 by the way
  ⎿  $ cat in.json | head -3

───────────────────────────────────────────────────────────────────────────────────────
 Bash command

   │ python3 - <<'EOF'
   │ import json, sys
   │ rows = json.load(open("in.json"))
   │ out = []
   │ for r in rows:
   │     if r.get("ok"):
   │         out.append(r["id"])
   │ json.dump(out, sys.stdout)
   │ EOF
   Collect the ids of the rows that passed

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for python3 commands in /home/u/work/thing
   3. No, and tell Claude what to do differently (esc)

 Esc to cancel · Tab to amend`

const permHookScreen = `❯ run the check
  Running it now, the token is hunter2 by the way

───────────────────────────────────────────────────────────────────────────────────────
 Bash command

   │ python3 - <<'EOF'
   │ print("hi")
   │ EOF
   Say hi from a script

 │ Hook PreToolUse:Bash requires confirmation for this command:
 │ Scripts run outside the sandbox here: confirm it, or run it in a scratch
 │ copy of the tree instead. [settings]
 settings.json to update hooks

 Do you want to proceed?
 ❯ 1. Yes
   2. No

 Esc to cancel · Tab to amend`

// permHookNote is the note of permHookScreen as the card shows it: the heading says
// who asks, the tag naming where the hook is configured is gone from the last line,
// and so is the hint under the note.
var permHookNote = []string{
	"Hook PreToolUse:Bash requires confirmation for this command:",
	"Scripts run outside the sandbox here: confirm it, or run it in a scratch",
	"copy of the tree instead.",
}

// permCutScreen is permLongScreen as a shorter screen shows it: the dialog is taller
// than the screen, and its opening — the rule and the heading — has scrolled off.
var permCutScreen = permLongScreen[strings.Index(permLongScreen, "   │     if r.get"):]

// permHookCutScreen is permHookScreen with the heading scrolled off the same way.
var permHookCutScreen = permHookScreen[strings.Index(permHookScreen, "   │ print(\"hi\")"):]

// permWrappedCutScreen is the tail of a command with a line too long for the screen:
// the console wraps its rest onto a row of its own, in the first column and with no
// bar, and a blank line of the command stands above it.
var permWrappedCutScreen = strings.Replace(permCutScreen,
	"   │     if r.get(\"ok\"):\n",
	"   │     if r.get(\"ok\"):\n   │\n   │ note = \"the rows that passed the check, and nothing\nelse\"\n", 1)

func TestPermissionReadsATallDialogWhole(t *testing.T) {
	for _, c := range []struct {
		name, screen string
		action, note []string
		options      []string
		lasting      []bool
	}{
		{
			name:   "a long command, the heading on the screen",
			screen: permLongScreen,
			action: []string{
				"python3 - <<'EOF'",
				"import json, sys",
				"rows = json.load(open(\"in.json\"))",
				"out = []",
				"for r in rows:",
				"if r.get(\"ok\"):",
				"out.append(r[\"id\"])",
				"json.dump(out, sys.stdout)",
				"EOF",
				"Collect the ids of the rows that passed",
			},
			options: []string{
				"Yes",
				"Yes, and don't ask again for python3 commands in /home/u/work/thing",
				"No, and tell Claude what to do differently (esc)",
			},
			lasting: []bool{false, true, false},
		},
		{
			name:   "a hook asks under the command",
			screen: permHookScreen,
			action: []string{
				"python3 - <<'EOF'",
				"print(\"hi\")",
				"EOF",
				"Say hi from a script",
			},
			note:    permHookNote,
			options: []string{"Yes", "No"},
			lasting: []bool{false, false},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			if d.Tool != "Bash command" {
				t.Errorf("tool %q, expected «Bash command»: the heading stands further up than the parser looks", d.Tool)
			}
			if strings.Join(d.Action, "|") != strings.Join(c.action, "|") {
				t.Errorf("action %q,\nexpected %q — the human presses «yes» going by this text", d.Action, c.action)
			}
			if strings.Join(d.Note, "|") != strings.Join(c.note, "|") {
				t.Errorf("note %q,\nexpected %q — without it the human does not know what they confirm", d.Note, c.note)
			}
			if got := optionTexts(d.Options); strings.Join(got, "|") != strings.Join(c.options, "|") {
				t.Errorf("options %q, expected %q", got, c.options)
			}
			for i, o := range d.Options {
				if i < len(c.lasting) && o.Lasting != c.lasting[i] {
					t.Errorf("item %d (%q): lasting %v, expected %v", o.N, o.Text, o.Lasting, c.lasting[i])
				}
			}
			if d.Partial {
				t.Error("a whole dialog is declared incomplete")
			}
			if d.Cut {
				t.Error("a dialog whose opening is on the screen is declared cut")
			}
			all := d.Tool + "\n" + strings.Join(d.Action, "\n") + "\n" + strings.Join(d.Note, "\n")
			for _, leak := range []string{"hunter2", "Looking at the rows", "Running it now", "collect the ids", "run the check"} {
				if strings.Contains(all, leak) {
					t.Errorf("this leaked from the conversation into the dialog: %q", leak)
				}
			}
		})
	}
}

func TestPermissionSaysWhenTheOpeningIsOffTheScreen(t *testing.T) {
	for _, c := range []struct {
		name, screen, tool string
		action, note       []string
		options            int
	}{
		{
			name:   "the tail of a long command",
			screen: permCutScreen,
			action: []string{
				"if r.get(\"ok\"):",
				"out.append(r[\"id\"])",
				"json.dump(out, sys.stdout)",
				"EOF",
				"Collect the ids of the rows that passed",
			},
			options: 3,
		},
		{
			name:   "the tail of a command with a wrapped row in the first column",
			screen: permWrappedCutScreen,
			action: []string{
				"if r.get(\"ok\"):",
				"note = \"the rows that passed the check, and nothing",
				"else\"",
				"out.append(r[\"id\"])",
				"json.dump(out, sys.stdout)",
				"EOF",
				"Collect the ids of the rows that passed",
			},
			options: 3,
		},
		{
			name:   "the tail of a command and the hook under it",
			screen: permHookCutScreen,
			tool:   "Bash",
			action: []string{
				"print(\"hi\")",
				"EOF",
				"Say hi from a script",
			},
			note:    permHookNote,
			options: 2,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			if !d.Cut {
				t.Error("the heading is off the screen and the card is not told so — it draws an empty frame as the whole dialog")
			}
			if d.Tool != c.tool {
				t.Errorf("tool %q, expected %q: with the heading off the screen the tool is what the hook names, and nothing else", d.Tool, c.tool)
			}
			if strings.Join(d.Action, "|") != strings.Join(c.action, "|") {
				t.Errorf("action %q,\nexpected %q — what is on the screen is what the human decides by", d.Action, c.action)
			}
			if strings.Join(d.Note, "|") != strings.Join(c.note, "|") {
				t.Errorf("note %q,\nexpected %q", d.Note, c.note)
			}
			if len(d.Options) != c.options {
				t.Fatalf("%d items, expected %d", len(d.Options), c.options)
			}
			if d.Options[0].Text != "Yes" {
				t.Errorf("the first item is %q", d.Options[0].Text)
			}
			last := d.Options[len(d.Options)-1]
			if !strings.HasPrefix(last.Text, "No") || last.Lasting {
				t.Errorf("the last item %+v: a refusal, marked as granting something for good", last)
			}
			if d.Partial {
				t.Error("the list is whole and is declared incomplete")
			}
		})
	}
}

// permNarrowScreen is the same dialog on a screen sixty columns wide: the items wrap,
// and the wrapped tail carries the mark that tells a lasting grant from a one-off.
const permNarrowScreen = `────────────────────────────────────────────────────────────
 Bash command

   │ python3 - <<'EOF'
   │ print("hi")
   │ EOF
   Say hi from a script

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for python3 commands in
      /home/u/work/thing
   3. No, and tell Claude what to do differently
      (esc)

 Esc to cancel · Tab to amend`

func TestPermissionJoinsTheItemsOfANarrowScreen(t *testing.T) {
	d, ok := parsePermission(permNarrowScreen)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	want := []string{
		"Yes",
		"Yes, and don't ask again for python3 commands in /home/u/work/thing",
		"No, and tell Claude what to do differently (esc)",
	}
	if got := optionTexts(d.Options); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("options %q, expected %q — the wrapped tails were left on the floor", got, want)
	}
	lasting := []bool{false, true, false}
	for i, o := range d.Options {
		if i < len(lasting) && o.Lasting != lasting[i] {
			t.Errorf("item %d (%q): lasting %v, expected %v", o.N, o.Text, o.Lasting, lasting[i])
		}
	}
	if d.Partial {
		t.Error("a wrapped item was taken for a line nobody understood")
	}
	if d.Cut {
		t.Error("a whole dialog is declared cut")
	}
}

func TestPermissionDoesNotCutAWholeDialog(t *testing.T) {
	for _, c := range []struct{ name, screen string }{
		{"after a blank line", permScreen},
		{"in a box", permFramed},
		{"under a rule", permLive},
		{"with a tip", permTip},
		{"an edit", permEditScreen},
		{"a fetch, opening the screen", permFetchScreen},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			if d.Cut {
				t.Error("a dialog whose opening is on the screen is declared cut")
			}
			if d.Tool == "" {
				t.Error("the tool is lost")
			}
		})
	}
}

func optionTexts(options []action.PermOption) []string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = o.Text
	}
	return out
}

// permRuleScreen is the dialog a permission rule asks with: the note is one line
// and names no hook, and the hint under it points at the rules.
var permRuleScreen = strings.NewReplacer(
	" │ Hook PreToolUse:Bash requires confirmation for this command:\n",
	" │ Permission rule python3:* requires confirmation for this command.\n",
	" │ Scripts run outside the sandbox here: confirm it, or run it in a scratch\n", "",
	" │ copy of the tree instead. [settings]\n", "",
	" settings.json to update hooks\n", " /permissions to update rules\n",
).Replace(permHookScreen)

func TestPermissionReadsTheNoteOfARule(t *testing.T) {
	d, ok := parsePermission(permRuleScreen)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	if d.Tool != "Bash command" {
		t.Errorf("tool %q, expected «Bash command»", d.Tool)
	}
	want := []string{"Permission rule python3:* requires confirmation for this command."}
	if strings.Join(d.Note, "|") != strings.Join(want, "|") {
		t.Errorf("note %q, expected %q — the rule's own words are what tells this dialog from a hook's", d.Note, want)
	}
	for _, line := range d.Action {
		if strings.Contains(line, "/permissions") || strings.Contains(line, "to update") {
			t.Errorf("the hint under the note went into the command: %q", line)
		}
	}
	if d.Cut || d.Partial {
		t.Errorf("a whole dialog went out as cut=%v partial=%v", d.Cut, d.Partial)
	}
}

func TestPermissionFingerprintFollowsTheNote(t *testing.T) {
	base, ok := parsePermission(permHookScreen)
	if !ok {
		t.Fatal("the dialog was not found")
	}
	other, ok := parsePermission(strings.Replace(permHookScreen,
		"Scripts run outside the sandbox here", "The tree is dirty", 1))
	if !ok {
		t.Fatal("the dialog was not found")
	}
	if base.Fingerprint == other.Fingerprint {
		t.Error("the note changed and the fingerprint did not — the digit will answer a question the human did not read")
	}
	if len(base.Note) == 0 {
		t.Fatal("the fixture has no note — the check proves nothing")
	}
}

func TestPermissionHasNoNoteOnThePlainDialogs(t *testing.T) {
	for _, c := range []struct{ name, screen string }{
		{"after a blank line", permScreen},
		{"in a box", permFramed},
		{"under a rule", permLive},
		{"with a tip", permTip},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			if len(d.Note) != 0 {
				t.Errorf("a note appeared on the console's own question: %q", d.Note)
			}
		})
	}
}

// A long dialog read from a phone. The terminal of the panel attaches to the
// same tmux window and sizes it to the phone, so the question wraps one way
// while it is read in the conversation and another way while it is read in the
// terminal, and a dialog taller than the screen shows a different part of
// itself at every width. None of that is the dialog changing, and a keypress
// refused over it is a permission the person cannot give from the phone at all.
func permLongDialog(wrap int, head bool) string {
	body := []string{
		"   │ curl -sS -X POST https://jira.example.test/rest/api/3/issue/LMS-13562/comment \\",
		"   │   -H 'Content-Type: application/json' \\",
		"   │   -d '{\"body\": \"the copy endpoint takes five requests a minute, the key is counted",
		"   │        by route and by address, and clients behind one address share the counter\"}'",
		"   Comment on the issue from the console",
	}

	note := []string{
		" │ Hook PreToolUse:mcp__atlassian__jira_add_comment requires confirmation for this tool:",
		" │ Jira, a comment on LMS-13562: the person allows this one at a time. [settings]",
		" settings.json to update hooks",
	}
	if wrap > 0 {
		// The console breaks a line wherever the window ends, inside a word as
		// readily as between two — that is how mcp__atlassian__jira_add_comment
		// comes back as "jira_add_c omment" on a phone.
		var rewrapped []string
		for _, line := range note {
			for len(line) > wrap {
				rewrapped = append(rewrapped, line[:wrap])
				line = " │ " + line[wrap:]
			}
			rewrapped = append(rewrapped, line)
		}
		note = rewrapped
	}
	screen := []string{"❯ comment on the issue", ""}
	if head {
		screen = append(screen,
			"───────────────────────────────────────────────────────────────────────────────────────",
			" Add Comment Tool", "")
	} else {
		// The dialog is taller than the screen: the rule, the heading and the
		// first lines of the command have scrolled off the top of it.
		screen = screen[:0]
		body = body[2:]
	}
	screen = append(screen, body...)
	screen = append(screen, "")
	screen = append(screen, note...)
	screen = append(screen, "", " Do you want to proceed?", " ❯ 1. Yes", "   2. No", "",
		" Esc to cancel · Tab to amend")
	return strings.Join(screen, "\n")
}

func TestPermissionFingerprintSurvivesTheWidthOfThePhone(t *testing.T) {
	base, ok := parsePermission(permLongDialog(0, true))
	if !ok {
		t.Fatal("the dialog was not found")
	}

	same := []struct {
		name   string
		screen string
	}{
		{"the note wrapped at a narrower window", permLongDialog(60, true)},
		{"a word of the note broke in two", strings.Replace(permLongDialog(0, true),
			"the person allows this one at a time", "the person all\n │ ows this one at a time", 1)},
		{"the head of the command scrolled off the screen", permLongDialog(0, false)},
		{"both at once, as it happens on a phone", permLongDialog(60, false)},
	}
	for _, c := range same {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			held := &action.Permit{Option: 1, Fingerprint: base.Fingerprint, Tail: base.Tail}
			if !permSame(&got, held) {
				t.Errorf("the keypress was refused though the dialog did not change: the person is told to "+
					"look again at the same screen\nbase: %v %v\nnow:  %v %v",
					base.Action, base.Note, got.Action, got.Note)
			}
		})
	}

	// What the tail carries still has to be the dialog on the screen.
	other := []struct {
		name   string
		screen string
	}{
		{"the comment changed", strings.Replace(permLongDialog(0, true),
			"share the counter", "are counted apart", 1)},
		{"the hook changed its mind", strings.Replace(permLongDialog(0, true),
			"the person allows this one at a time", "anything goes here", 1)},
		{"an item changed", strings.Replace(permLongDialog(0, true), "2. No", "2. Never", 1)},
	}
	for _, c := range other {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parsePermission(c.screen)
			if !ok {
				t.Fatal("the dialog was not found")
			}
			held := &action.Permit{Option: 1, Fingerprint: base.Fingerprint, Tail: base.Tail}
			if permSame(&got, held) {
				t.Error("a different dialog was taken for the one that was read — the digit goes into a question nobody saw")
			}
		})
	}
}
