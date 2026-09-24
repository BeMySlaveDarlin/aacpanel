package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
)

func askFile(t *testing.T, sessionID, toolUseID string, questions ...askQ) {
	t.Helper()
	type option struct {
		Label string `json:"label"`
	}
	type question struct {
		Multi   bool     `json:"multi"`
		Options []option `json:"options"`
	}
	var qs []question
	for _, q := range questions {
		var opts []option
		for _, label := range q.options {
			opts = append(opts, option{Label: label})
		}
		qs = append(qs, question{Multi: q.multi, Options: opts})
	}
	body, err := json.Marshal(map[string]any{
		sessionID: map[string]any{
			"sessionId": sessionID,
			"toolUseId": toolUseID,
			"questions": qs,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "asked.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(askStoreEnv, path)
}

func askFileWithPreview(t *testing.T, sessionID, toolUseID string, questions ...askQ) {
	t.Helper()
	book := map[string]any{sessionID: storedAskJSON(sessionID, toolUseID, questions)}
	body, err := json.Marshal(book)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "asked.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(askStoreEnv, path)
}

func storedAskJSON(sessionID, toolUseID string, questions []askQ) map[string]any {
	var qs []map[string]any
	for _, q := range questions {
		var opts []map[string]any
		for i, label := range q.options {
			opt := map[string]any{"label": label}
			if i == 0 && q.preview != "" {
				opt["preview"] = q.preview
			}
			opts = append(opts, opt)
		}
		qs = append(qs, map[string]any{"multi": q.multi, "options": opts})
	}
	return map[string]any{"sessionId": sessionID, "toolUseId": toolUseID, "questions": qs}
}

type askQ struct {
	multi   bool
	options []string
	preview string
}

func pasteStep(text string) string { return "paste:" + text }

func stepNames(steps []dialogStep) []string {
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		if step.paste {
			out = append(out, pasteStep(step.keys))
			continue
		}
		out = append(out, step.keys)
	}
	return out
}

func storedFrom(questions ...askQ) storedAsk {
	var ask storedAsk
	for _, q := range questions {
		block := storedQuestion{Multi: q.multi}
		for i, label := range q.options {
			opt := storedOption{Label: label}
			if i == 0 {
				opt.Preview = q.preview
			}
			block.Options = append(block.Options, opt)
		}
		ask.Questions = append(ask.Questions, block)
	}
	return ask
}

func TestAnswerKeysFollowsDialog(t *testing.T) {
	tests := []struct {
		name      string
		questions []askQ
		picks     [][]int
		texts     []string
		want      []string
	}{{
		name:      "one question: the digit confirms by itself",
		questions: []askQ{{options: []string{"Alpha", "Beta", "Gamma"}}},
		picks:     [][]int{{2}},
		want:      []string{"2"},
	}, {
		name:      "an option with a preview: the digit moves the cursor, Enter picks",
		questions: []askQ{{options: []string{"Side menu", "Top bar"}, preview: "+---+\n|   |\n+---+"}},
		picks:     [][]int{{1}},
		want:      []string{"1", enterKey},
	}, {
		name: "a batch with a preview: Enter picks there and moves on, the digit alone leaves the dialog standing",
		questions: []askQ{
			{options: []string{"Red", "Blue"}, preview: "RED BLOCK"},
			{options: []string{"Small", "Large"}},
		},
		picks: [][]int{{2}, {2}},
		want:  []string{"2", enterKey, "2", submitPick},
	}, {
		name: "a batch of previews only: Enter after every digit",
		questions: []askQ{
			{options: []string{"Agents", "Router", "Skills"}, preview: "agents/"},
			{options: []string{"Five", "Three", "One"}, preview: "roles"},
		},
		picks: [][]int{{1}, {3}},
		want:  []string{"1", enterKey, "3", enterKey, submitPick},
	}, {
		name: "a skipped question with a preview is passed with Tab: the arrow moves nothing there",
		questions: []askQ{
			{options: []string{"Red", "Blue"}, preview: "RED BLOCK"},
			{options: []string{"Small", "Large"}},
		},
		picks: [][]int{{}, {2}},
		want:  []string{keyTab, "2", submitPick},
	}, {
		name:      "several picks: checkboxes, a move and Submit",
		questions: []askQ{{multi: true, options: []string{"One", "Two", "Three", "Four"}}},
		picks:     [][]int{{2, 4}},
		want:      []string{"2", "4", keyRight, submitPick},
	}, {
		name: "a batch: the digit moves on to the next question by itself",
		questions: []askQ{
			{options: []string{"Porridge", "Omelette"}},
			{options: []string{"Coffee", "Tea"}},
			{options: []string{"Taxi", "Metro"}},
		},
		picks: [][]int{{1}, {1}, {1}},
		want:  []string{"1", "1", "1", submitPick},
	}, {
		name: "a skipped question is passed with an arrow, not with Tab",
		questions: []askQ{
			{options: []string{"Porridge", "Omelette"}},
			{options: []string{"Coffee", "Tea"}},
		},
		picks: [][]int{{}, {2}},
		want:  []string{keyRight, "2", submitPick},
	}, {
		name:      "own words: item N+1, the text, Enter",
		questions: []askQ{{options: []string{"Alpha", "Beta", "Gamma"}}},
		picks:     [][]int{{}},
		texts:     []string{"none of these fit, let us take a third way"},
		want:      []string{"4", pasteStep("none of these fit, let us take a third way"), enterKey},
	}, {
		name: "a batch: own words are counted from their own question",
		questions: []askQ{
			{options: []string{"Porridge", "Omelette"}},
			{options: []string{"Coffee", "Tea"}},
			{options: []string{"Taxi", "Metro"}},
		},
		picks: [][]int{{1}, {}, {2}},
		texts: []string{"", "cocoa", ""},
		want:  []string{"1", "3", pasteStep("cocoa"), enterKey, "2", submitPick},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := answerKeys(storedFrom(tt.questions...), tt.picks, tt.texts)
			if err != nil {
				t.Fatalf("the keystrokes did not come together: %v", err)
			}
			got := stepNames(keys)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("keystrokes %q, expected %q", got, tt.want)
			}
		})
	}
}

func TestAnswerKeysRefusesImpossible(t *testing.T) {
	tests := []struct {
		name      string
		questions []askQ
		picks     [][]int
		texts     []string
	}{{
		name:      "fewer options than the number",
		questions: []askQ{{options: []string{"Alpha", "Beta"}}},
		picks:     [][]int{{3}},
	}, {
		name:      "the choice is single, but two arrived",
		questions: []askQ{{options: []string{"Alpha", "Beta", "Gamma"}}},
		picks:     [][]int{{1, 2}},
	}, {
		name:      "more answers than questions",
		questions: []askQ{{options: []string{"Alpha"}}},
		picks:     [][]int{{1}, {1}},
	}, {
		name: "the tenth item cannot be picked with a single digit",
		questions: []askQ{{options: []string{
			"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}}},
		picks: [][]int{{10}},
	}, {
		name:      "both a pick and own words for one question",
		questions: []askQ{{options: []string{"Alpha", "Beta"}}},
		picks:     [][]int{{1}},
		texts:     []string{"can it be done another way"},
	}, {
		name: "the own-words item does not fit into a digit",
		questions: []askQ{{options: []string{
			"1", "2", "3", "4", "5", "6", "7", "8", "9"}}},
		picks: [][]int{{}},
		texts: []string{"in my own words"},
	}, {
		name:      "more own answers than questions",
		questions: []askQ{{options: []string{"Alpha"}}},
		picks:     [][]int{{1}},
		texts:     []string{"", "one too many"},
	}, {
		name:      "own words in a multiSelect",
		questions: []askQ{{multi: true, options: []string{"One", "Two"}}},
		picks:     [][]int{{}},
		texts:     []string{"none of these"},
	}, {
		name:      "own words next to a preview",
		questions: []askQ{{options: []string{"Side menu", "Top bar"}, preview: "+---+"}},
		picks:     [][]int{{}},
		texts:     []string{"put it at the bottom, like a tab bar"},
	}, {
		name:      "a lone question with a preview and nothing picked: Enter would pick the first option blind",
		questions: []askQ{{options: []string{"Side menu", "Top bar"}, preview: "+---+"}},
		picks:     [][]int{{}},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if keys, err := answerKeys(storedFrom(tt.questions...), tt.picks, tt.texts); err == nil {
				t.Errorf("keystrokes %q came together where it had to refuse", stepNames(keys))
			}
		})
	}
}

func TestSessionAnswerRefusesStaleQuestion(t *testing.T) {
	procFS(t,
		fakeProc{pid: 700, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 701, comm: "claude", args: []string{"claude"}, ppid: 700, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 701, name: "aacpanel", start: "77", status: "waiting"})
	askFile(t, "s-701", "toolu_new", askQ{options: []string{"Alpha", "Beta"}})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 701})

	e := &Executor{}
	_, err := e.sessionAnswer(t.Context(), "aacpanel", &action.Answer{AskID: "toolu_old", Picks: [][]int{{1}}})
	if err == nil {
		t.Fatal("an answer to a withdrawn question went through")
	}
	if _, err := os.ReadFile(log); err == nil {
		t.Error("keystrokes went out to konsole though the question is no longer the same")
	}
}

func TestSessionAnswerPressesKeys(t *testing.T) {
	procFS(t,
		fakeProc{pid: 800, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 801, comm: "claude", args: []string{"claude"}, ppid: 800, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 801, name: "aacpanel", start: "77", status: "waiting"})
	askFile(t, "s-801", "toolu_42", askQ{options: []string{"Alpha", "Beta", "Gamma"}})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 999, "/Sessions/2": 801})

	e := &Executor{}
	detail, err := e.sessionAnswer(t.Context(), "aacpanel", &action.Answer{AskID: "toolu_42", Picks: [][]int{{2}}})
	if err != nil {
		t.Fatalf("the answer did not go out: %v", err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	if string(raw) != "2" {
		t.Errorf("%q went into the dialog, while a single digit of choice was needed", raw)
	}
	if !strings.Contains(detail, "Beta") {
		t.Errorf("the reply does not name the picked option: %q", detail)
	}
}

func TestKonsoleFindsRightBus(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 900, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 901, comm: "claude", args: []string{"claude"}, ppid: 900, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 901, name: "aacpanel", start: "77", socket: socket, status: "idle"})

	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/bus")
	want := "unix:path=" + filepath.Join(runtime, "bus")
	log := fakeBusctlOnBus(t, want, map[string]int{"/Sessions/1": 901})

	e := &Executor{}
	detail, err := e.sessionSend(t.Context(), "aacpanel", "probe")
	if err != nil {
		t.Fatalf("sending failed: %v", err)
	}
	if !strings.HasPrefix(detail, "typed into") {
		t.Fatalf("the window was not found, the reply went through the fallback channel: %q", detail)
	}
	raw, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(raw), "probe") {
		t.Errorf("nothing landed in the konsole on the right bus: %v %q", err, raw)
	}
}

func TestSessionAnswerTypesOwnWords(t *testing.T) {
	procFS(t,
		fakeProc{pid: 820, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 821, comm: "claude", args: []string{"claude"}, ppid: 820, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 821, name: "aacpanel", start: "77", status: "waiting"})
	askFile(t, "s-821", "toolu_42", askQ{options: []string{"Alpha", "Beta", "Gamma"}})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 821})

	const own = "none of these fit:\nlet us take a third way"
	e := &Executor{}
	detail, err := e.sessionAnswer(t.Context(), "aacpanel",
		&action.Answer{AskID: "toolu_42", Picks: [][]int{{}}, Texts: []string{own}})
	if err != nil {
		t.Fatalf("the own-words answer did not go out: %v", err)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	want := "4" + pasteStart + own + pasteEnd + enterKey
	if string(raw) != want {
		t.Errorf("%q went into the dialog, expected %q", raw, want)
	}
	if strings.Contains(detail, "third way") {
		t.Errorf("the own-words answer came back in detail, and from there into the permanent action log: %q", detail)
	}
	if !strings.Contains(detail, "characters") {
		t.Errorf("the reply does not say the answer was given in words: %q", detail)
	}
}

func TestSessionAnswerRefusesBlindEnterWhenFieldStaysShut(t *testing.T) {
	procFS(t,
		fakeProc{pid: 830, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 831, comm: "claude", args: []string{"claude"}, ppid: 830, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 831, name: "aacpanel", start: "77", status: "waiting"})
	askFile(t, "s-831", "toolu_42", askQ{options: []string{"Alpha", "Beta"}})
	log := fakeBusctlAs(t, fakeBus{
		tabs: map[string]int{"/Sessions/1": 831}, swallow: 1, renderAfter: 1 << 20})

	e := &Executor{}
	_, err := e.sessionAnswer(t.Context(), "aacpanel",
		&action.Answer{AskID: "toolu_42", Picks: [][]int{{}}, Texts: []string{"my own words"}})
	if err == nil {
		t.Fatal("the executor reported success without seeing its own words on the screen")
	}
	raw, _ := os.ReadFile(log)
	if strings.Contains(string(raw), enterKey) {
		t.Errorf("Enter went out blind: %q", raw)
	}
}

func TestSessionDismissPressesChatAbout(t *testing.T) {
	procFS(t,
		fakeProc{pid: 840, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 841, comm: "claude", args: []string{"claude"}, ppid: 840, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 841, name: "aacpanel", start: "77", status: "waiting"})
	askFile(t, "s-841", "toolu_42", askQ{options: []string{"Alpha", "Beta", "Gamma"}})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 841})

	e := &Executor{}
	if _, err := e.sessionDismiss(t.Context(), "aacpanel", &action.Answer{AskID: "toolu_42"}); err != nil {
		t.Fatalf("the question was not dismissed: %v", err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	if string(raw) != "5" {
		t.Errorf("%q went into the dialog, while «chat about it» is the fifth item when there are three options", raw)
	}
}

func TestSessionDismissRefusesQuestionWithPreview(t *testing.T) {
	procFS(t,
		fakeProc{pid: 860, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 861, comm: "claude", args: []string{"claude"}, ppid: 860, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 861, name: "aacpanel", start: "77", status: "waiting"})
	askFileWithPreview(t, "s-861", "toolu_42",
		askQ{options: []string{"Side menu", "Top bar"}, preview: "+---+"})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 861})

	e := &Executor{}
	if _, err := e.sessionDismiss(t.Context(), "aacpanel", &action.Answer{AskID: "toolu_42"}); err == nil {
		t.Fatal("dismissing a question with a preview went through — a digit presses the wrong thing there")
	}
	if _, err := os.ReadFile(log); err == nil {
		t.Error("keystrokes went out to konsole though there was nothing to press")
	}
}

func TestSessionDismissRefusesStaleQuestion(t *testing.T) {
	procFS(t,
		fakeProc{pid: 850, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 851, comm: "claude", args: []string{"claude"}, ppid: 850, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 851, name: "aacpanel", start: "77", status: "waiting"})
	askFile(t, "s-851", "toolu_new", askQ{options: []string{"Alpha", "Beta"}})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 851})

	e := &Executor{}
	if _, err := e.sessionDismiss(t.Context(), "aacpanel", &action.Answer{AskID: "toolu_old"}); err == nil {
		t.Fatal("dismissing a question that belongs to another ask went through")
	}
	if _, err := os.ReadFile(log); err == nil {
		t.Error("keystrokes went out to konsole though the question is no longer the same")
	}
}

func TestAskStoreDefaultStaysPut(t *testing.T) {
	t.Setenv(askStoreEnv, "")
	os.Unsetenv(askStoreEnv)
	t.Setenv("AACP_STATE_DIR", "")
	os.Unsetenv("AACP_STATE_DIR")
	t.Setenv(hostcfg.PathEnv, filepath.Join(t.TempDir(), "no-description.env"))

	if got := askStorePath(); got != "/var/lib/aacpanel/asked.json" {
		t.Errorf("the question store slid to %s — session questions will stop reaching the panel", got)
	}

	t.Setenv("AACP_STATE_DIR", "/srv/state/aacpanel")
	if got := askStorePath(); got != "/srv/state/aacpanel/asked.json" {
		t.Errorf("the store did not follow the state directory: %s", got)
	}

	t.Setenv(askStoreEnv, "/srv/state/own.json")
	if got := askStorePath(); got != "/srv/state/own.json" {
		t.Errorf("the explicit variable lost to the machine description: %s", got)
	}
}

func TestSessionAnswerNamesTheLostQuestion(t *testing.T) {
	procFS(t,
		fakeProc{pid: 880, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 881, comm: "claude", args: []string{"claude"}, ppid: 880, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 881, name: "aacpanel", start: "77", status: "waiting"})
	t.Setenv(askStoreEnv, filepath.Join(t.TempDir(), "asked.json"))
	if err := os.WriteFile(os.Getenv(askStoreEnv), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	e := &Executor{}
	_, err := e.sessionAnswer(t.Context(), "aacpanel", &action.Answer{AskID: "toolu_1", Picks: [][]int{{1}}})
	if err == nil {
		t.Fatal("the answer went through with an empty store")
	}
	if !strings.Contains(err.Error(), "standing on a dialog") {
		t.Errorf("the refusal did not name the reason: %v", err)
	}
}

func TestSessionAnswerStaysShortWhenNothingIsAsked(t *testing.T) {
	procFS(t,
		fakeProc{pid: 890, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 891, comm: "claude", args: []string{"claude"}, ppid: 890, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 891, name: "aacpanel", start: "77"})
	t.Setenv(askStoreEnv, filepath.Join(t.TempDir(), "asked.json"))
	if err := os.WriteFile(os.Getenv(askStoreEnv), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	e := &Executor{}
	_, err := e.sessionAnswer(t.Context(), "aacpanel", &action.Answer{AskID: "toolu_1", Picks: [][]int{{1}}})
	if err == nil || !strings.Contains(err.Error(), "is not asking anything right now") {
		t.Fatalf("the wrong refusal: %v", err)
	}
	if strings.Contains(err.Error(), "standing on a dialog") {
		t.Error("the refusal invented a lost question where there is no dialog")
	}
}
