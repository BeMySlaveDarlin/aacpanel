package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
)

// The confirmation claude shows before it changes the model of a conversation
// it holds cached, as a live console drew it.
const switchModelScreen = `  По итогу проверки два хвоста, оба — твои действия, не мои:

  1. ssh-fix-aliases — порты открыты, но алиасов нет.
  2.
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
   Switch model?
   Your next response will be slower and use more tokens

   This conversation is cached for the current model. Switching to Opus 5.5
   (1M context) means the full history gets re-read on your next message.

   ❯ 1. Yes, switch to Opus 5.5 (1M context)
     2. No, go back
`

const switchEffortScreen = `● done
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
   Change effort level?
   Your next response will be slower and use more tokens

   This conversation is cached for the current effort level. Switching to max
   means the full history gets re-read on your next message.

   ❯ 1. Yes, switch to max
     2. No, go back
`

const switchHookScreen = `● done
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
   Switch model?
   A PreModelSwitch hook asked you to confirm

   Fable costs more: ask the team lead first.

   ❯ 1. Yes, switch to Fable 5.1
     2. No, go back
`

// A console that answers what is typed: once a payload a rule waits for goes
// in, the screen becomes the next one and the rule's effect happens.
type scriptTerm struct {
	sent  []string
	shows string
	rules []termRule
}

type termRule struct {
	on     func(payload string) bool
	screen string
	then   func()
}

func (f *scriptTerm) kind() string  { return "tmux" }
func (f *scriptTerm) attempts() int { return 1 }
func (f *scriptTerm) send(_ context.Context, payload string) error {
	f.sent = append(f.sent, payload)
	for i, r := range f.rules {
		if r.on(payload) {
			f.shows = r.screen
			if r.then != nil {
				r.then()
			}
			f.rules = append(f.rules[:i], f.rules[i+1:]...)
			break
		}
	}
	return nil
}
func (f *scriptTerm) screen(context.Context) (string, bool) { return f.shows, true }

func (f *scriptTerm) count(payload string) int {
	n := 0
	for _, s := range f.sent {
		if s == payload {
			n++
		}
	}
	return n
}

func typedWith(words string) func(string) bool {
	return func(p string) bool { return strings.Contains(p, words) && strings.HasSuffix(p, enterKey) }
}

func key(k string) func(string) bool { return func(p string) bool { return p == k } }

// statusLine writes what the status line of a console shows, the way its
// script does, and returns the session id it is written for.
func statusLine(t *testing.T) (string, func(model, name, effort string, window int)) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "session-models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sessionModelsEnv, dir)
	const sid = "0f5c7a52-6d0e-4a55-9a52-2b9c1d6f3e10"
	return sid, func(model, name, effort string, window int) {
		body, _ := json.Marshal(map[string]any{"at": time.Now().Unix(), "sessionId": sid,
			"model": map[string]string{"id": model, "displayName": name}, "effort": effort, "contextWindow": window})
		if err := os.WriteFile(filepath.Join(dir, sid+".json"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheConfirmationOfASwitchIsAnsweredForThePerson(t *testing.T) {
	sid, show := statusLine(t)
	show("claude-opus-4-8", "Opus 4.8", "max", 200000)
	term := &scriptTerm{shows: busyScreen}
	term.rules = []termRule{
		{on: typedWith("/model opus[1m]"), screen: switchModelScreen},
		{on: key("1"), screen: busyScreen, then: func() { show("claude-opus-5-5", "Opus 5.5", "max", 1000000) }},
	}
	cmd := &action.Command{Name: "model", Arg: "opus[1m]"}
	set := newSetting(liveSession{SessionID: sid}, cmd)

	confirmed, err := pasteAndSendWith(context.Background(), term, typed(cmd), nil, set.watch())
	if err != nil {
		t.Fatalf("the switch was reported lost: %v", err)
	}
	if !confirmed {
		t.Error("the status line shows the new model, yet the switch is unconfirmed")
	}
	if term.count("1") != 1 || term.count("2") != 0 {
		t.Errorf("the confirmation was answered %d times with yes and %d with no: %q", term.count("1"), term.count("2"), term.sent)
	}
	said := set.said()
	if !strings.Contains(said, "reads the whole history again") {
		t.Errorf("the reply does not say what the switch costs: %q", said)
	}
	if !strings.Contains(said, "Opus 5.5") {
		t.Errorf("the reply does not say what the console shows now: %q", said)
	}
}

func TestAnEffortTakenWhileTheSessionAnswersIsNotReportedLost(t *testing.T) {
	sid, show := statusLine(t)
	show("claude-opus-5-5", "Opus 5.5", "xhigh", 1000000)
	// Claude runs the command at once while it answers: the words leave the
	// composer and appear nowhere, only the status line is drawn again.
	term := &scriptTerm{shows: busyScreen}
	term.rules = []termRule{{on: typedWith("/effort max"), screen: busyScreen,
		then: func() { show("claude-opus-5-5", "Opus 5.5", "max", 1000000) }}}
	cmd := &action.Command{Name: "effort", Arg: "max"}
	set := newSetting(liveSession{SessionID: sid}, cmd)

	confirmed, err := pasteAndSendWith(context.Background(), term, typed(cmd), nil, set.watch())
	if err != nil {
		t.Fatalf("an effort the console shows was reported lost: %v", err)
	}
	if !confirmed {
		t.Error("the status line shows the effort, yet the change is unconfirmed")
	}
	if !strings.Contains(set.said(), "effort max") {
		t.Errorf("the reply does not say what the console shows now: %q", set.said())
	}
}

func TestTheConfirmationOfAnEffortIsReadByItsOwnHeading(t *testing.T) {
	sid, show := statusLine(t)
	show("claude-opus-5-5", "Opus 5.5", "xhigh", 1000000)
	term := &scriptTerm{shows: busyScreen}
	term.rules = []termRule{
		{on: typedWith("/effort max"), screen: switchEffortScreen},
		{on: key("1"), screen: busyScreen, then: func() { show("claude-opus-5-5", "Opus 5.5", "max", 1000000) }},
	}
	cmd := &action.Command{Name: "effort", Arg: "max"}
	set := newSetting(liveSession{SessionID: sid}, cmd)

	if _, err := pasteAndSendWith(context.Background(), term, typed(cmd), nil, set.watch()); err != nil {
		t.Fatalf("the change of effort was reported lost: %v", err)
	}
	if term.count("1") != 1 {
		t.Errorf("the confirmation of the effort was not answered once: %q", term.sent)
	}
}

func TestAHookThatAsksIsNotAnsweredForThePerson(t *testing.T) {
	sid, show := statusLine(t)
	show("claude-opus-5-5", "Opus 5.5", "xhigh", 1000000)
	term := &scriptTerm{shows: busyScreen}
	term.rules = []termRule{
		{on: typedWith("/model fable"), screen: switchHookScreen},
		{on: key("2"), screen: busyScreen},
	}
	cmd := &action.Command{Name: "model", Arg: "fable"}
	set := newSetting(liveSession{SessionID: sid}, cmd)

	_, err := pasteAndSendWith(context.Background(), term, typed(cmd), nil, set.watch())
	if err == nil {
		t.Fatal("a switch a hook asked the person to confirm was reported made")
	}
	if term.count("1") != 0 {
		t.Errorf("the panel agreed in the person's place: %q", term.sent)
	}
	if term.count("2") != 1 {
		t.Errorf("the session was left on the confirmation: %q", term.sent)
	}
	if !strings.Contains(err.Error(), "hook") {
		t.Errorf("the refusal does not say a hook asked: %v", err)
	}
}

func TestAConfirmationThatStaysIsNotPassedOffAsASwitch(t *testing.T) {
	sid, show := statusLine(t)
	show("claude-opus-5-5", "Opus 5.5", "xhigh", 1000000)
	term := &scriptTerm{shows: busyScreen}
	term.rules = []termRule{{on: typedWith("/model fable"), screen: switchModelScreen}}
	cmd := &action.Command{Name: "model", Arg: "fable"}
	set := newSetting(liveSession{SessionID: sid}, cmd)

	_, err := pasteAndSendWith(context.Background(), term, typed(cmd), nil, set.watch())
	if err == nil {
		t.Fatal("a confirmation still on the screen was passed off as a switch")
	}
	if term.count("1") != 1 {
		t.Errorf("the answer was pressed %d times — a second press lands on whatever comes next: %q", term.count("1"), term.sent)
	}
}

func TestSwitchDialogOfReadsOnlyTheConfirmation(t *testing.T) {
	cases := []struct {
		name, screen, title string
		ok, hook            bool
	}{
		{"model", switchModelScreen, "Switch model?", true, false},
		{"effort", switchEffortScreen, "Change effort level?", true, false},
		{"another heading", switchEffortScreen, "Switch model?", false, false},
		{"a hook asks", switchHookScreen, "Switch model?", true, true},
		{"a composer", busyScreen, "Switch model?", false, false},
		{"words of the conversation", "● Switch model?\n  2. No, go back\n", "Switch model?", false, false},
	}
	for _, c := range cases {
		hook, ok := switchDialogOf(c.screen, c.title)
		if ok != c.ok || (ok && hook != c.hook) {
			t.Errorf("%s: read as a confirmation %v with a hook %v, expected %v and %v", c.name, ok, hook, c.ok, c.hook)
		}
	}
}

func TestOnlyAModelAndAnEffortAreWatched(t *testing.T) {
	for _, cmd := range []*action.Command{{Name: "compact"}, {Name: "model"}, {Name: "context", Arg: "all"}} {
		if set := newSetting(liveSession{SessionID: "x"}, cmd); set != nil {
			t.Errorf("/%s %s is watched as a change of a setting", cmd.Name, cmd.Arg)
		}
	}
}

func TestAStatusLineDrawnAgainWithTheSameModelIsNoProof(t *testing.T) {
	sid, show := statusLine(t)
	show("claude-opus-5-5", "Opus 5.5", "xhigh", 1000000)
	// A session that answers draws its status line again and again; the
	// command went nowhere, and the line shows the model it had.
	term := &scriptTerm{shows: busyScreen}
	term.rules = []termRule{{on: typedWith("/model fable"), screen: busyScreen,
		then: func() { show("claude-opus-5-5", "Opus 5.5", "xhigh", 1000000) }}}
	cmd := &action.Command{Name: "model", Arg: "fable"}
	set := newSetting(liveSession{SessionID: sid}, cmd)

	confirmed, _ := pasteAndSendWith(context.Background(), term, typed(cmd), nil, set.watch())
	if confirmed {
		t.Error("a status line drawn again with the same model was taken for the switch")
	}
}

func TestAnUnreadableScreenIsConfirmedByTheStatusLine(t *testing.T) {
	sid, show := statusLine(t)
	show("claude-opus-5-5", "Opus 5.5", "xhigh", 1000000)
	term := &fakeTerm{known: false}
	cmd := &action.Command{Name: "effort", Arg: "max"}
	set := newSetting(liveSession{SessionID: sid}, cmd)
	show("claude-opus-5-5", "Opus 5.5", "max", 1000000)

	confirmed, err := pasteAndSendWith(context.Background(), term, typed(cmd), nil, set.watch())
	if err != nil {
		t.Fatalf("the change went unconfirmed with an error: %v", err)
	}
	if !confirmed {
		t.Error("the screen cannot be read, the status line shows the effort, and the change is unconfirmed")
	}
}
