package executor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalNotFoundNamesTmuxFirstAndKonsoleSecond(t *testing.T) {
	t.Setenv(tmuxEnv, filepath.Join(t.TempDir(), "no-such-binary"))
	procFS(t, fakeProc{pid: 700, comm: "claude", args: []string{"claude"}, ppid: 1})

	_, err := termFor(t.Context(), 700)
	if err == nil {
		t.Fatal("a terminal was found where there is neither tmux nor konsole")
	}
	said := err.Error()
	for _, want := range []string{"tmux", "konsole"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal has no %q — one road out of two is named, and the human fixes the wrong thing: %s", want, said)
		}
	}
	if strings.Index(said, "tmux") > strings.Index(said, "konsole") {
		t.Errorf("konsole is named before tmux: on a machine without KDE that advises installing something "+
			"that will never be there at all — %s", said)
	}
}

const trayIdleScreen = `● Started.

────────────────────────────────────────
❯ 
────────────────────────────────────────
   🛡  session   ctx 83%   queue empty  ☆ ☆
   CPU 46 · RAM 13 · I/O 35 · NET 211 · DSK 125
   📍 user
  ────────────────────────────────────────
   ✦ Opus 5 (1M context)   🧠  xhigh   ⏱  2m 02s
  ⏵⏵ auto mode on · 1 shell

  ● main
  ◯ general-purpose  sleep 400 and return ok                     6m 44s · ↓ 57.2k tokens
`

const traySelectedScreen = `● Started.

────────────────────────────────────────
❯ 
────────────────────────────────────────
   🛡  session   ctx 83%   queue empty  ☆ ☆
   CPU 46 · RAM 13 · I/O 35 · NET 211 · DSK 125
   📍 user
  ────────────────────────────────────────
   ✦ Opus 5 (1M context)   🧠  xhigh   ⏱  13m 11s
  Enter to view · x to stop

  ● main
❯ ◯ general-purpose  Monitoring background sleep 600            41s · ↓ 58.2k tokens
`

const trayTypedScreen = `● Started.

────────────────────────────────────────
❯ a probe of composer parsing
────────────────────────────────────────
   🛡  session   ctx 83%   queue empty  ☆ ☆
   📍 user
  ────────────────────────────────────────
   ✦ Opus 5 (1M context)   🧠  xhigh   ⏱  2m 02s
  ⏵⏵ auto mode on · 1 shell

  ● main
  ◯ general-purpose  Waiting on background sleep 400            6m 47s · ↓ 57.2k tokens
`

const traySentScreen = `❯ reply with the word accepted

● accepted

✻ Waiting for 1 background agent to finish

────────────────────────────────────────
❯ 
────────────────────────────────────────
   🛡  session   ctx 83%   queue empty  ☆ ☆
   📍 user
  ────────────────────────────────────────
   ✦ Opus 5 (1M context)   🧠  xhigh   ⏱  29m 06s
  ⏵⏵ auto mode on · 1 shell, 1 monitor

  ● main
  ◯ general-purpose  Awaiting background sleep via Monitor
`

const busyScreen = `✢ Crafting… (3s · thought for 2s)
  ⎿  Tip: Hit shift+tab to cycle between manual mode, auto-accept edit mode

────────────────────────────────────────
❯ 
────────────────────────────────────────
   🛡  session   ctx 83%   queue empty  ☆ ☆
   📍 user
  ────────────────────────────────────────
   ✦ Opus 5 (1M context)   🧠  xhigh   ⏱  33m 30s
  ⏵⏵ auto mode on · 1 monitor

  ● main
  ◯ general-purpose  Waiting on background sleep 300
`

const tasksScreen = `❯ Start exactly one general-purpose subagent

● Agent(sleep 400 and return ok)
  ⎿  Backgrounded agent (↓ to manage · ctrl+o to expand)

✻ Baked for 7m 0s · done 16:19

▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
   Background

   No tasks currently running

   ↑/↓ to select · Enter to view · Esc to close
`

const transcriptScreen = `● ok

✻ Baked for 7m 0s · done 16:19

────────────────────────────────────────
  Showing detailed transcript · ctrl+o to toggle · ↑↓ scroll · v to open in vi · ? for shortcuts
`

// What the screen of a session looks like on a phone-sized window when a dialog
// of the session stands above the composer: the lower rule of the composer and
// the chips under it are pushed off the bottom, and the prompt is the last line
// there is.
const cutComposerScreen = `╭──────────────────────────────────────╮
│ ✻ Bug report drafted: the router was stopped                                 │
│ │ - What happened: in a prod security check the model listed four actions    │
│ 1 to review · 2 to send · 0 to dismiss                                       │
╰──────────────────────────────────────╯


────────────────────────────────────────
❯ `

// The same cut screen, but the prompt at the bottom marks a row of the subagent
// tray rather than the composer.
const cutListScreen = `● Started.

✻ Baked for 7m 0s · done 16:19

────────────────────────────────────────
❯ ◯ general-purpose  Monitoring background sleep 600            41s · ↓ 58.2k tokens`

func TestComposerReadyOnRealScreens(t *testing.T) {
	cases := []struct {
		name   string
		screen string
		ready  bool
	}{
		{name: "the subagent list is there, input belongs to the composer", screen: trayIdleScreen, ready: true},
		{name: "our own reply stands in the composer", screen: trayTypedScreen, ready: true},
		{name: "the subagent list took the input", screen: traySelectedScreen, ready: false},
		{name: "the session is busy, a turn is running", screen: busyScreen, ready: true},
		{name: "the background task screen is open", screen: tasksScreen, ready: false},
		{name: "the detailed transcript is open", screen: transcriptScreen, ready: false},
		{name: "a dialog pushed the lower rule of the composer off the screen", screen: cutComposerScreen, ready: true},
		{name: "the cut screen ends on a row of the tray, not on the composer", screen: cutListScreen, ready: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			busy, ready := composerReady(c.screen)
			if ready != c.ready {
				t.Fatalf("ready=%v, expected %v (said: %q)", ready, c.ready, busy)
			}
			if ready && busy != "" {
				t.Errorf("the composer is ready, yet a reason is named: %q", busy)
			}
			if !ready && busy == "" {
				t.Error("a refusal without a reason: the person on the phone has nothing to press")
			}
		})
	}
}

// A composer that runs to the bottom of the screen is still a composer: the
// window of a phone is short, and what stands above it decides how much of it
// is drawn. Reading only the boxed shape left a session unable to receive
// anything at all, and the refusal said the session was showing a screen of
// its own — which it was not.
func TestTheComposerIsFoundWhenTheScreenCutItsLowerRule(t *testing.T) {
	body, toBottom, ok := composerAt(cutComposerScreen)
	if !ok {
		t.Fatal("the composer was not found on a screen that ends with one")
	}
	if !toBottom {
		t.Error("the composer runs to the bottom of the screen, and it was not read as one")
	}
	if strings.Contains(body, "to review") {
		t.Errorf("the dialog above the composer was taken for its text: %q", body)
	}

	if _, _, ok := composerAt(cutListScreen); ok {
		t.Error("a row of the tray at the bottom of the screen was taken for the composer")
	}
}

// A refusal names the fact — the session is showing a screen of its own — and
// the fact alone sends the person to a terminal to see which screen that is.
// The lines it carries are that look, taken from the end of the screen where
// what holds the keyboard says what it wants.
func TestARefusalCarriesTheEndOfTheScreen(t *testing.T) {
	term := &fakeTerm{screen_: draftDialogScreen, known: true}
	if _, err := pasteAndSend(context.Background(), term, "restart the router", nil); err == nil {
		t.Fatal("a message into a dialog was passed off as delivered")
	} else {
		said := err.Error()
		for _, want := range []string{"What happened", "1 to review"} {
			if !strings.Contains(said, want) {
				t.Errorf("the refusal %q carries no %q — which dialog holds the keyboard is invisible from the panel", said, want)
			}
		}
		if strings.Contains(said, "╰") {
			t.Errorf("the refusal %q carries the drawing of the box: those lines say nothing and crowd out the ones that do", said)
		}
	}
	if len(term.sent) != 0 {
		t.Errorf("%d payloads went into the dialog: %q", len(term.sent), term.sent)
	}
}

func TestNothingIsTypedIntoAList(t *testing.T) {
	term := &fakeTerm{screen_: traySelectedScreen, known: true}
	confirmed, err := pasteAndSend(context.Background(), term, "check the stack logs", nil)
	if err == nil {
		t.Fatal("a reply into a list was passed off as delivered")
	}
	if confirmed {
		t.Error("delivery was confirmed where there was none")
	}
	if len(term.sent) != 0 {
		t.Errorf("%d payloads went into the list: %q", len(term.sent), term.sent)
	}
	if !strings.Contains(err.Error(), "Esc") {
		t.Errorf("the refusal does not say what to press in the session: %s", err)
	}
}

func TestReplyGoesInWhenSubagentsAreOnlyListed(t *testing.T) {
	const text = "a probe of composer parsing"
	if _, ok := composerText(trayTypedScreen); !ok {
		t.Fatal("the composer was not found on a screen that has one")
	}
	st, known := composerStateOf(trayTypedScreen, composerMark(text))
	if !known {
		t.Fatal("the screen capture was not parsed")
	}
	if !st.held {
		t.Error("our own reply in the composer went unrecognised — delivery would report «typed» on a stuck one")
	}

	term := &fakeTerm{screen_: traySentScreen, known: true}
	confirmed, err := pasteAndSend(context.Background(), term, "reply with the word accepted", nil)
	if err != nil {
		t.Fatalf("delivery to a screen with a list refused: %v", err)
	}
	if !confirmed {
		t.Error("the reply is visible in the conversation, yet the send is unconfirmed")
	}
	if len(term.sent) == 0 {
		t.Error("nothing went into the composer")
	}
}

func TestBlindEnterOnlyWhenTheScreenIsUnreadable(t *testing.T) {
	seen := &fakeTerm{screen_: traySelectedScreen, known: true}
	if _, err := pasteAndSend(context.Background(), seen, "a reply", nil); err == nil {
		t.Fatal("the list accepted a reply")
	}
	if seen.enters() != 0 {
		t.Errorf("%d Enter sent into the list", seen.enters())
	}

	blind := &fakeTerm{known: false}
	if _, err := pasteAndSend(context.Background(), blind, "a reply", nil); err != nil {
		t.Fatalf("an unreadable screen refused instead of keeping the former behaviour: %v", err)
	}
	if blind.enters() == 0 {
		t.Error("on an unreadable screen no blind Enter was sent — the former behaviour is lost")
	}
}

func TestCursorAtTellsThePromptFromTheText(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{line: "❯ ", want: "❯"},
		{line: "❯ a probe of composer parsing", want: "❯"},
		{line: "❯ ◯ general-purpose  Monitoring background sleep 600", want: "❯"},
		{line: "     git log > out.txt", want: ""},
		{line: "  ◯ general-purpose  sleep 400 and return ok", want: ""},
		{line: "  ⏵⏵ auto mode on · 1 shell", want: ""},
	}

	for _, c := range cases {
		if got := cursorAt(c.line); got != c.want {
			t.Errorf("cursorAt(%q) = %q, expected %q", c.line, got, c.want)
		}
	}
}

// The panel is the keyboard of the person at it, and what goes in that way is
// their own message. Text handed to the console in one write is folded into a
// paste: the console marks it in the transcript as pasted content, and the
// words of a person then reach the session as data rather than as what they
// said. So a message is typed — a handful of characters at a time, at the pace
// of a hand — and only what typing would change is pasted.
func TestAMessageIsTypedRatherThanHandedOverAsAPaste(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		paste  bool
		closes bool
	}{
		{name: "a short line", text: "ok", paste: false},
		{name: "a long line", text: strings.Repeat("check the stack logs and say what crashed, ", 5), paste: false},
		{name: "several lines", text: "check the stack logs\nand say what crashed", paste: false},
		{name: "an at-sign in the middle", text: "ask about @CLAUDE.md and the base", paste: false},
		// What typing would change into something else.
		{name: "a slash command", text: "/status", paste: true},
		{name: "a slash command with an argument", text: "/model haiku", paste: true},
		{name: "a bang hands the line to a shell", text: "!git status", paste: true},
		{name: "a bang hands the long line to a shell", text: "!echo a long shell command well over thirty characters", paste: true},
		// A hash opens nothing in the composer: typed, a long one arrives unmarked.
		{name: "a hash is text", text: "#the base is main, and the next release waits for the review", paste: false},
		{name: "an unfinished name of a file at the end", text: "look at @CLAUDE.md", paste: false, closes: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			term := &fakeTerm{screen_: screenWithReply(c.text), known: true}
			confirmed, err := pasteAndSend(context.Background(), term, c.text, nil)
			if err != nil {
				t.Fatalf("the text was not delivered: %v", err)
			}
			if !confirmed {
				t.Error("the text stands in the composer on the screen, yet the send is unconfirmed")
			}

			whole := strings.Join(term.sent, "")
			if !strings.HasPrefix(whole, clearLine) {
				t.Errorf("the composer was not cleared before the text: %q", term.sent)
			}
			if !strings.HasSuffix(whole, enterKey) {
				t.Errorf("nothing sent the text: no Enter at the end of %q", term.sent)
			}
			marked := strings.Contains(whole, pasteStart) || strings.Contains(whole, pasteEnd)
			switch {
			case marked && !c.paste:
				t.Errorf("the message went in as a paste: the session reads it as quoted data instead of "+
					"words addressed to it — %q", term.sent)
			case !marked && c.paste:
				t.Errorf("a line the composer reads as a key was typed in: what arrives is no longer the "+
					"message that was written — %q", term.sent)
			}

			if c.paste {
				if len(term.sent) != 1 {
					t.Errorf("a paste went in as %d writes, expected one: %q", len(term.sent), term.sent)
				}
				if !strings.Contains(whole, c.text) {
					t.Errorf("the text itself is missing from what went out: %q", whole)
				}
				return
			}

			// Typed: the text arrives whole, in writes small enough that the
			// console does not take the run for a paste of its own.
			body := strings.TrimSuffix(strings.TrimPrefix(whole, clearLine), enterKey)
			if c.closes {
				if !strings.HasSuffix(body, " ") {
					t.Errorf("the list of files is left open under the Enter that sends the message: %q", term.sent)
				}
				body = strings.TrimSuffix(body, " ")
			}
			if body != c.text {
				t.Errorf("what was typed is not what was asked for:\n got %q\nwant %q", body, c.text)
			}
			for _, write := range term.sent {
				if write == clearLine || write == enterKey {
					continue
				}
				if n := len([]rune(write)); n > typeChunk {
					t.Errorf("a write of %d characters went in at once, past the %d a keyboard is read at: %q",
						n, typeChunk, write)
				}
			}
			if strings.Contains(body, "\r") {
				t.Errorf("a carriage return went in with the text: it ends the message where it stands "+
					"and leaves the rest in the composer — %q", body)
			}
		})
	}
}

// A message written with the line breaks of a text file arrives as one message
// all the same: what the console sends on is the carriage return, so that is
// the one thing the panel never types.
func TestLineBreaksOfAFileDoNotEndTheMessage(t *testing.T) {
	text := "first line\r\nsecond line\rthird line"
	term := &fakeTerm{screen_: screenWithReply(text), known: true}
	if _, err := pasteAndSend(context.Background(), term, text, nil); err != nil {
		t.Fatalf("the text was not delivered: %v", err)
	}
	whole := strings.Join(term.sent, "")
	body := strings.TrimSuffix(strings.TrimPrefix(whole, clearLine), enterKey)
	if want := "first line\nsecond line\nthird line"; body != want {
		t.Errorf("the message was typed as %q, expected %q", body, want)
	}
}

// screenWithReply is a session whose composer is empty and whose last message
// is the one just sent: what the screen looks like once a message has left.
func screenWithReply(text string) string {
	rule := strings.Repeat("─", 40)
	return "❯ " + text + "\n\n" + rule + "\n❯ \n" + rule + "\n"
}
