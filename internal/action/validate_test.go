package action

import (
	"fmt"
	"strings"
	"testing"
)

func TestResumeIdentifier(t *testing.T) {
	const uuid = "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"

	cases := []struct {
		name string
		req  Request
		ok   bool
	}{
		{"resume with a uuid", Request{ID: "1", Kind: SessionResume, Target: "aacpanel", Resume: uuid}, true},
		{"resume without a uuid", Request{ID: "1", Kind: SessionResume, Target: "aacpanel"}, false},
		{"a command instead of a uuid", Request{ID: "1", Kind: SessionResume, Target: "aacpanel", Resume: "$(rm -rf /)"}, false},
		{"uuid without dashes", Request{ID: "1", Kind: SessionResume, Target: "aacpanel", Resume: "e29e01f1748c4a999fd6e3d8827ed5d1"}, false},
		{"uuid of the wrong length", Request{ID: "1", Kind: SessionResume, Target: "aacpanel", Resume: "e29e01f1-748c-4a99-9fd6-e3d8827ed5d"}, false},
		{"not hexadecimal", Request{ID: "1", Kind: SessionResume, Target: "aacpanel", Resume: "z29e01f1-748c-4a99-9fd6-e3d8827ed5d1"}, false},
		{"open with a uuid", Request{ID: "1", Kind: SessionOpen, Target: "aacpanel", Resume: uuid}, false},
		{"open without a uuid", Request{ID: "1", Kind: SessionOpen, Target: "aacpanel"}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if c.ok && err != nil {
				t.Errorf("a sound request is rejected: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("the request is accepted, though it must not be")
			}
		})
	}
}

func TestResumeIsKnown(t *testing.T) {
	if !Valid(SessionResume) {
		t.Fatal("session.resume is missing from Kinds — the executor would reject it as unknown")
	}
}

func TestFileRequestChecksNameAndSize(t *testing.T) {
	ok := func(f *File, text string) Request {
		return Request{ID: "a1", Kind: SessionFile, Target: "aacpanel", Text: text, Files: []File{*f}}
	}

	if err := ok(&File{Name: "shot.png", Data: []byte{1, 2, 3}}, "take a look").Validate(); err != nil {
		t.Fatalf("a plain file is rejected: %v", err)
	}
	if err := ok(&File{Name: "shot.png", Data: []byte{1}}, "").Validate(); err != nil {
		t.Fatalf("a file without a caption is rejected: %v", err)
	}
	if err := ok(&File{Name: "Снимок_экрана_2026-08-31_в_01.05.png", Data: []byte{1}}, "").Validate(); err != nil {
		t.Fatalf("a name in Cyrillic is rejected: %v", err)
	}

	bad := map[string]*File{
		"empty name":             {Name: "", Data: []byte{1}},
		"a way out of the path":  {Name: "..shot.png", Data: []byte{1}},
		"slash in the name":      {Name: "etc/passwd", Data: []byte{1}},
		"space in the name":      {Name: "my photo.png", Data: []byte{1}},
		"right-to-left override": {Name: "shot\u202egnp.exe", Data: []byte{1}},
		"hidden file":            {Name: ".bashrc", Data: []byte{1}},
		"empty content":          {Name: "shot.png", Data: nil},
		"over the size ceiling":  {Name: "shot.png", Data: make([]byte, FileMax+1)},
		"name over the ceiling":  {Name: strings.Repeat("a", fileNameMax+1) + ".png", Data: []byte{1}},
	}
	for name, f := range bad {
		t.Run(name, func(t *testing.T) {
			if err := ok(f, "").Validate(); err == nil {
				t.Errorf("accepted what must not be accepted: %q", f.Name)
			}
		})
	}

	stop := Request{ID: "a1", Kind: ContainerStop, Target: "app", Files: []File{{Name: "a.png", Data: []byte{1}}}}
	if err := stop.Validate(); err == nil {
		t.Error("container.stop accepted a file — the field would reach the executor unnoticed")
	}
}

func TestFilePackChecksCountAndTotal(t *testing.T) {
	pack := func(files ...File) Request {
		return Request{ID: "a1", Kind: SessionFile, Target: "aacpanel", Files: files}
	}
	one := func(name string, n int) File { return File{Name: name, Data: make([]byte, n)} }

	if err := pack(one("a.png", 3), one("b.png", 5), one("c.log", 7)).Validate(); err != nil {
		t.Fatalf("a batch of three files is rejected: %v", err)
	}

	full := make([]File, 0, FilesMax)
	for i := range FilesMax {
		full = append(full, one(fmt.Sprintf("f%d.png", i), 1))
	}
	if err := pack(full...).Validate(); err != nil {
		t.Fatalf("a batch exactly at the ceiling is rejected: %v", err)
	}
	if err := pack(append(full, one("extra.png", 1))...).Validate(); err == nil {
		t.Error("a batch over the count ceiling is accepted")
	}

	half := FilesBytesMax/2 + 1
	if err := pack(one("a.bin", half), one("b.bin", half)).Validate(); err == nil {
		t.Errorf("a batch weighing %d against a ceiling of %d is accepted — it would break in transit",
			2*half, FilesBytesMax)
	}
	if err := pack(one("a.bin", FilesBytesMax/2), one("b.bin", FilesBytesMax/2)).Validate(); err != nil {
		t.Errorf("a batch exactly at the ceiling is rejected: %v", err)
	}

	if err := pack().Validate(); err == nil {
		t.Error("session.file with no attachments is accepted")
	}
	if err := pack(one("a.png", 3), File{Name: "b.png"}).Validate(); err == nil {
		t.Error("an empty file in the middle of the batch is accepted — the session would go read an empty one")
	}
}

func TestCommandRequestKeepsClosedList(t *testing.T) {
	req := func(c *Command) Request {
		return Request{ID: "a1", Kind: SessionCommand, Target: "aacpanel", Command: c}
	}

	if err := req(&Command{Name: "clear"}).Validate(); err != nil {
		t.Fatalf("/clear is rejected: %v", err)
	}
	if err := req(&Command{Name: "model", Arg: "opus[1m]"}).Validate(); err != nil {
		t.Fatalf("/model opus[1m] is rejected: %v", err)
	}

	bad := map[string]*Command{
		"no command at all":            nil,
		"command outside the list":     {Name: "permissions"},
		"argument where none is taken": {Name: "clear", Arg: "everything"},
		"option outside the list":      {Name: "model", Arg: "gpt"},
		"option not named":             {Name: "effort"},
		"a line of its own":            {Name: "clear && rm -rf ~"},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			if err := req(c).Validate(); err == nil {
				t.Errorf("accepted what must not be accepted: %+v", c)
			}
		})
	}

	send := Request{ID: "a1", Kind: SessionSend, Target: "aacpanel", Text: "hello",
		Command: &Command{Name: "clear"}}
	if err := send.Validate(); err == nil {
		t.Error("session.send accepted a slash command — two actions in one request")
	}
}

func TestProjectLimits(t *testing.T) {
	long := Request{ID: "1", Kind: SessionOpen, Target: "aacpanel",
		Project: &Project{Path: "/opt/" + strings.Repeat("a", pathMax), Session: "aacpanel"}}
	if err := long.Validate(); err == nil {
		t.Error("a path over the ceiling is accepted")
	}
	big := Request{ID: "1", Kind: SessionOpen, Target: "aacpanel",
		Project: &Project{Path: "/opt/x", Session: "aacpanel",
			Launch: []byte(`{"a":"` + strings.Repeat("b", launchMax) + `"}`)}}
	if err := big.Validate(); err == nil {
		t.Error("launch parameters over the ceiling are accepted")
	}
}

func TestAnswerRequestChecksOwnWords(t *testing.T) {
	ask := func(a *Answer) Request {
		return Request{ID: "a1", Kind: SessionAnswer, Target: "aacpanel", Answer: a}
	}

	good := &Answer{AskID: "toolu_42", Picks: [][]int{{}, {2}}, Texts: []string{"words of my own", ""}}
	if err := ask(good).Validate(); err != nil {
		t.Fatalf("a plain free-form answer is rejected: %v", err)
	}
	multi := &Answer{AskID: "toolu_42", Picks: [][]int{{}}, Texts: []string{"first line\nsecond\twith a tab"}}
	if err := ask(multi).Validate(); err != nil {
		t.Fatalf("a multi-line answer of my own is rejected: %v", err)
	}
	if err := ask(&Answer{AskID: "toolu_42", Picks: [][]int{{1}}}).Validate(); err != nil {
		t.Fatalf("a plain pick is rejected: %v", err)
	}

	bad := map[string]*Answer{
		"nothing but spaces":        {AskID: "toolu_42", Picks: [][]int{{}}, Texts: []string{"   \n  "}},
		"control character":         {AskID: "toolu_42", Picks: [][]int{{}}, Texts: []string{"my \x1b[31mwords"}},
		"over the ceiling":          {AskID: "toolu_42", Picks: [][]int{{}}, Texts: []string{strings.Repeat("a", AskTextMax+1)}},
		"both a pick and words":     {AskID: "toolu_42", Picks: [][]int{{1}}, Texts: []string{"can I answer differently"}},
		"more words than questions": {AskID: "toolu_42", Picks: [][]int{{1}}, Texts: []string{"", "extra"}},
		"without an id":             {AskID: "", Picks: [][]int{{}}, Texts: []string{"words of my own"}},
	}
	for name, a := range bad {
		t.Run(name, func(t *testing.T) {
			if err := ask(a).Validate(); err == nil {
				t.Error("accepted what must not be accepted")
			}
		})
	}
}

func TestAnswerNotesStandBesidePicks(t *testing.T) {
	ask := func(a *Answer) Request {
		return Request{ID: "a1", Kind: SessionAnswer, Target: "aacpanel", Answer: a}
	}
	if err := ask(&Answer{AskID: "toolu_42", Picks: [][]int{{1}, {2}}, Notes: []string{"darker", ""}}).Validate(); err != nil {
		t.Fatalf("a note beside a pick is rejected: %v", err)
	}
	bad := map[string]*Answer{
		"a note beside nothing":     {AskID: "toolu_42", Picks: [][]int{{}}, Texts: []string{"mine"}, Notes: []string{"why"}},
		"more notes than questions": {AskID: "toolu_42", Picks: [][]int{{1}}, Notes: []string{"", "extra"}},
		"nothing but spaces":        {AskID: "toolu_42", Picks: [][]int{{1}}, Notes: []string{"  "}},
		"control character":         {AskID: "toolu_42", Picks: [][]int{{1}}, Notes: []string{"a \x1b[31mnote"}},
		"over the ceiling":          {AskID: "toolu_42", Picks: [][]int{{1}}, Notes: []string{strings.Repeat("a", AskTextMax+1)}},
	}
	for name, a := range bad {
		t.Run(name, func(t *testing.T) {
			if err := ask(a).Validate(); err == nil {
				t.Error("accepted what must not be accepted")
			}
		})
	}
	dismiss := Request{ID: "a1", Kind: SessionDismiss, Target: "aacpanel",
		Answer: &Answer{AskID: "toolu_42", Notes: []string{"why"}}}
	if err := dismiss.Validate(); err == nil {
		t.Error("dismissing a question carried a note")
	}
}

func TestDismissRequestCarriesOnlyQuestion(t *testing.T) {
	drop := func(a *Answer) Request {
		return Request{ID: "a1", Kind: SessionDismiss, Target: "aacpanel", Answer: a}
	}

	if err := drop(&Answer{AskID: "toolu_42"}).Validate(); err != nil {
		t.Fatalf("dismissing a question is rejected: %v", err)
	}

	bad := map[string]*Answer{
		"no question":   nil,
		"without an id": {AskID: ""},
		"with a pick":   {AskID: "toolu_42", Picks: [][]int{{1}}},
		"with words":    {AskID: "toolu_42", Texts: []string{"words of my own"}},
	}
	for name, a := range bad {
		t.Run(name, func(t *testing.T) {
			if err := drop(a).Validate(); err == nil {
				t.Error("accepted what must not be accepted")
			}
		})
	}

	withText := Request{ID: "a1", Kind: SessionDismiss, Target: "aacpanel",
		Answer: &Answer{AskID: "toolu_42"}, Text: "hello"}
	if err := withText.Validate(); err == nil {
		t.Error("dismissing a question accepted reply text")
	}
}

func TestWorkStopCarriesWhatTheScreenShows(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		ok   bool
	}{
		{"task with a line", Request{ID: "1", Kind: TaskStop, Target: "aacpanel",
			Work: &Work{ID: "bqjjajuiv", Line: "sleep 400"}}, true},
		{"task without work", Request{ID: "1", Kind: TaskStop, Target: "aacpanel"}, false},
		{"task without a line", Request{ID: "1", Kind: TaskStop, Target: "aacpanel",
			Work: &Work{ID: "bqjjajuiv"}}, false},
		{"task without an id", Request{ID: "1", Kind: TaskStop, Target: "aacpanel",
			Work: &Work{Line: "sleep 400"}}, false},
		{"id with a newline", Request{ID: "1", Kind: TaskStop, Target: "aacpanel",
			Work: &Work{ID: "bqjjajuiv\nrm -rf /", Line: "sleep 400"}}, false},
		{"line over the ceiling", Request{ID: "1", Kind: TaskStop, Target: "aacpanel",
			Work: &Work{ID: "bqjjajuiv", Line: strings.Repeat("a", workLineMax+1)}}, false},
		{"line exactly at the ceiling", Request{ID: "1", Kind: TaskStop, Target: "aacpanel",
			Work: &Work{ID: "bqjjajuiv", Line: strings.Repeat("a", workLineMax)}}, true},
		{"line spanning two lines", Request{ID: "1", Kind: TaskStop, Target: "aacpanel",
			Work: &Work{ID: "bqjjajuiv", Line: "sleep 400\nrm -rf /"}}, false},
		{"agent by name", Request{ID: "1", Kind: AgentStop, Target: "aacpanel",
			Work: &Work{ID: "probe-a"}}, true},
		{"agent with a line", Request{ID: "1", Kind: AgentStop, Target: "aacpanel",
			Work: &Work{ID: "probe-a", Line: "@probe-a"}}, false},
		{"agent without a name", Request{ID: "1", Kind: AgentStop, Target: "aacpanel", Work: &Work{}}, false},
		{"work on an action that takes none", Request{ID: "1", Kind: SessionStop, Target: "aacpanel",
			Work: &Work{ID: "bqjjajuiv", Line: "sleep 400"}}, false},
		{"Esc into a session", Request{ID: "1", Kind: SessionEscape, Target: "aacpanel"}, true},
		{"Esc carrying words", Request{ID: "1", Kind: SessionEscape, Target: "aacpanel",
			Text: "restart the router"}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if c.ok && err != nil {
				t.Errorf("a sound request is rejected: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("an unsound request is let through")
			}
		})
	}
}

func TestSwitchSaysWhereAndCarriesItsProject(t *testing.T) {
	project := &Project{Path: "/opt/x", Session: "aacpanel"}
	cases := []struct {
		name string
		req  Request
		ok   bool
	}{
		{"to the console", Request{ID: "1", Kind: SessionSwitch, Target: "aacpanel",
			Switch: &Switch{To: SwitchConsole}, Project: project}, true},
		{"to the feed, agreeing to what stops", Request{ID: "1", Kind: SessionSwitch, Target: "aacpanel",
			Switch: &Switch{To: SwitchStream, Force: true}, Project: project}, true},
		{"nowhere", Request{ID: "1", Kind: SessionSwitch, Target: "aacpanel", Project: project}, false},
		{"somewhere unknown", Request{ID: "1", Kind: SessionSwitch, Target: "aacpanel",
			Switch: &Switch{To: "tmux"}, Project: project}, false},
		{"without its project", Request{ID: "1", Kind: SessionSwitch, Target: "aacpanel",
			Switch: &Switch{To: SwitchConsole}}, false},
		{"a switch riding another action", Request{ID: "1", Kind: SessionClose, Target: "aacpanel",
			Switch: &Switch{To: SwitchConsole}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if c.ok && err != nil {
				t.Errorf("a sound request is rejected: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("the request is accepted, though it must not be")
			}
		})
	}
	if !Valid(SessionSwitch) {
		t.Fatal("session.switch is missing from Kinds — the executor would reject it as unknown")
	}
}

func TestAMessageIDNamesASentOrQueuedMessage(t *testing.T) {
	const id = "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"
	cases := []struct {
		name string
		req  Request
		ok   bool
	}{
		{"a send that names its message", Request{ID: "1", Kind: SessionSend, Target: "a", Text: "hi", MessageID: id}, true},
		{"a send without one", Request{ID: "1", Kind: SessionSend, Target: "a", Text: "hi"}, true},
		{"taking a message back", Request{ID: "1", Kind: SessionUnqueue, Target: "a", MessageID: id}, true},
		{"taking back nothing", Request{ID: "1", Kind: SessionUnqueue, Target: "a"}, false},
		{"a message id that is not a uuid", Request{ID: "1", Kind: SessionUnqueue, Target: "a", MessageID: "$(rm)"}, false},
		{"a message id riding another action", Request{ID: "1", Kind: SessionClose, Target: "a", MessageID: id}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if c.ok && err != nil {
				t.Errorf("a sound request is rejected: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("the request is accepted, though it must not be")
			}
		})
	}
}

// The older models of a catalogue have no alias, and /model takes them by id;
// the id goes to the session as a line of text, so its shape is all it may be.
func TestAModelIsTakenByAliasOrByItsID(t *testing.T) {
	req := func(arg string) Request {
		return Request{ID: "a1", Kind: SessionCommand, Target: "aacpanel", Command: &Command{Name: "model", Arg: arg}}
	}
	for _, arg := range []string{"sonnet", "claude-opus-4-8", "claude-fable-5-1[1m]", "claude-haiku-4-5-20251001"} {
		if err := req(arg).Validate(); err != nil {
			t.Errorf("/model %s is rejected: %v", arg, err)
		}
	}
	for _, arg := range []string{"claude-", "claude-opus", "opus-4-8", "claude-opus 4-8", "claude-opus-4-8\n/clear",
		"claude-opus-4-8;rm", "claude-Opus-4-8", "claude-opus-4-8[2m]"} {
		if err := req(arg).Validate(); err == nil {
			t.Errorf("/model %q is accepted", arg)
		}
	}
	effort := Request{ID: "a1", Kind: SessionCommand, Target: "aacpanel",
		Command: &Command{Name: "effort", Arg: "claude-opus-4-8"}}
	if err := effort.Validate(); err == nil {
		t.Error("a model id passed for an effort")
	}
}

// A pick changes one setting, and a mode is one of four: the two that stop a
// session asking at all are not a tap away.
func TestASettingIsOneAtATime(t *testing.T) {
	req := func(set *Setting) Request {
		return Request{ID: "a1", Kind: SessionSet, Target: "aacpanel", Setting: set}
	}
	for _, set := range []*Setting{{Mode: "default"}, {Mode: "acceptEdits"}, {Mode: "plan"}, {Mode: "auto"},
		{Model: "sonnet"}, {Model: "claude-opus-4-8"}, {Effort: "xhigh"}} {
		if err := req(set).Validate(); err != nil {
			t.Errorf("%+v is rejected: %v", *set, err)
		}
	}
	for name, set := range map[string]*Setting{
		"nothing":          nil,
		"an empty setting": {},
		"two at once":      {Model: "sonnet", Mode: "auto"},
		"no questions":     {Mode: "bypassPermissions"},
		"asking nobody":    {Mode: "dontAsk"},
		"a mode in caps":   {Mode: "Auto"},
		"an unknown model": {Model: "gpt"},
		"a model effort":   {Effort: "claude-opus-4-8"},
	} {
		if err := req(set).Validate(); err == nil {
			t.Errorf("%s is accepted", name)
		}
	}
	send := Request{ID: "a1", Kind: SessionSend, Target: "aacpanel", Text: "hi", Setting: &Setting{Mode: "auto"}}
	if err := send.Validate(); err == nil {
		t.Error("session.send carried a setting — two actions in one request")
	}
	if err := (Request{Ask: AskModels}).Validate(); err == nil {
		t.Error("a question about models without a session is accepted")
	}
	if err := (Request{Ask: AskModels, Target: "aacpanel"}).Validate(); err != nil {
		t.Errorf("a question about the models of a session is rejected: %v", err)
	}
}
