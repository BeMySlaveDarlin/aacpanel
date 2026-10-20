package executor

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
	"aacpanel/internal/sessionintent"
)

func TestSessionMessagesAreSavedWithoutSendingThem(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f := onTheStream(t, false)
	e, _ := newTest(t, "")
	set := func(values sessionintent.Values) {
		t.Helper()
		r := req(action.SessionIntent, "demo")
		r.Intents = values
		if _, err := e.Execute(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	set(sessionintent.Values{"intent": "Queue", "restartIntent": "Continue"})
	set(sessionintent.Values{"restartIntent": ""})
	values, err := e.Intents(t.Context(), "demo")
	if err != nil || values["intent"] != "Queue" || values["restartIntent"] != "" {
		t.Fatalf("the saved messages: %v, %v", values, err)
	}
	if len(f.asked()) != 0 {
		t.Fatalf("saving messages sent them into the running conversation: %v", f.asked())
	}
	p := sessionPlace(t, "demo")
	r := req(action.SessionRename, "demo")
	r.Rename = "pilot"
	if _, err := e.Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	values, err = sessionintent.Read(sessionintent.Dir(), p, "pilot")
	if err != nil || values["intent"] != "Queue" {
		t.Fatalf("the renamed session lost its message: %v, %v", values, err)
	}
	if _, err := e.Intents(t.Context(), "gone"); err == nil {
		t.Fatal("a missing session answered")
	}
}

func TestRestartSelectsItsRuleUnlessTheContinuedConversationHasWords(t *testing.T) {
	for _, c := range []struct {
		name, resume, text, key string
	}{
		{"fresh", "", "", "restartIntent"},
		{"continue with words", consoleSID, "The next step", ""},
		{"continue without words", consoleSID, "", "restartIntent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := consoleStand(t, "idle")
			log := fakeLauncher(t, launcher.Report{Session: "demo"})
			e, _ := newTest(t, "")
			withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})
			r := restartOf(dir, c.resume)
			r.Text = c.text
			if _, err := e.Execute(t.Context(), r); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var spec launcher.Spec
			if err := json.Unmarshal(raw[strings.IndexByte(string(raw), '{'):strings.LastIndexByte(string(raw), '}')+1], &spec); err != nil {
				t.Fatal(err)
			}
			if spec.IntentKey != c.key {
				t.Fatalf("the restart selects %q, want %q", spec.IntentKey, c.key)
			}
		})
	}
}

func TestOpeningAndResumingSelectTheFirstMessage(t *testing.T) {
	_, dir := launchDir(t, "shop")
	for _, kind := range []action.Kind{action.SessionOpen, action.SessionResume} {
		trustFile(t, map[string]bool{dir: true})
		log := fakeLauncher(t, launcher.Report{Session: "shop"})
		e, _ := newTest(t, "")
		resume := ""
		if kind == action.SessionResume {
			resume = consoleSID
		}
		if _, err := e.Execute(t.Context(), openWith(kind, &action.Project{Path: dir, Session: "shop"}, resume)); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		var spec launcher.Spec
		if err := json.Unmarshal(raw[strings.IndexByte(string(raw), '{'):strings.LastIndexByte(string(raw), '}')+1], &spec); err != nil {
			t.Fatal(err)
		}
		if spec.IntentKey != "intent" {
			t.Errorf("%s selected %q", kind, spec.IntentKey)
		}
	}
}
