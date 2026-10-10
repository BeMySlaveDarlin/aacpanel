package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/codex/codextest"
	"aacpanel/internal/codex/contract"
)

func command(c action.Command) action.Request {
	return action.Request{Kind: action.SessionCommand, Target: codexName, Command: &c}
}

// The commands of codex's terminal go to its daemon; claude's are refused by
// name.
func TestCodexCommandsGoThroughTheDaemon(t *testing.T) {
	srv, e := onCodex(t, nil)
	ctx := context.Background()

	detail, err := e.Execute(ctx, command(action.Command{Name: "review", Review: &action.Review{
		Target: action.ReviewBranch, Branch: "main"}}))
	if err != nil || !strings.Contains(detail, "reviews the work against main") {
		t.Fatalf("a review: %q, %v", detail, err)
	}
	if calls := srv.Calls("review/start"); len(calls) != 1 ||
		!strings.Contains(string(calls[0]), `"target":{"branch":"main","type":"baseBranch"}`) {
		t.Errorf("review/start went as %s", calls)
	}
	if _, err := e.Execute(ctx, command(action.Command{Name: "review", Review: &action.Review{
		Target: action.ReviewUncommitted}})); err == nil || !strings.Contains(err.Error(), "a turn runs") {
		t.Errorf("a review into a running turn: %v", err)
	}
	if detail, err := e.Execute(ctx, command(action.Command{Name: "compact"})); err != nil || !strings.Contains(detail, "compacts") {
		t.Errorf("compact: %q, %v", detail, err)
	}

	for _, c := range []struct {
		goal action.Goal
		says string
	}{
		{action.Goal{Do: action.GoalSet, Objective: "ship it", Budget: 9000}, "has a goal now (active)"},
		{action.Goal{Do: action.GoalPause}, "is paused"},
		{action.Goal{Do: action.GoalResume}, "is active again"},
		{action.Goal{Do: action.GoalClear}, "is cleared"},
		{action.Goal{Do: action.GoalClear}, "had no goal"},
	} {
		g := c.goal
		if detail, err := e.Execute(ctx, command(action.Command{Name: "goal", Goal: &g})); err != nil ||
			!strings.Contains(detail, c.says) {
			t.Errorf("%+v: %q, %v", c.goal, detail, err)
		}
	}
	if set := string(srv.Calls("thread/goal/set")[0]); !strings.Contains(set, `"objective":"ship it"`) ||
		!strings.Contains(set, `"tokenBudget":9000`) {
		t.Errorf("the goal went as %s", set)
	}

	if detail, err := e.Execute(ctx, command(action.Command{Name: "stop"})); err != nil || !strings.Contains(detail, "every background terminal") {
		t.Errorf("stop: %q, %v", detail, err)
	}
	if _, err := e.Execute(ctx, command(action.Command{Name: "clear"})); err == nil ||
		!strings.Contains(err.Error(), "a command of claude's") {
		t.Errorf("claude's /clear on a codex session: %v", err)
	}
}

// A rename names the thread and keeps the session's name; a background
// terminal is stopped by the id codex gives it.
func TestCodexRenameAndTaskStop(t *testing.T) {
	srv, e := onCodex(t, func(srv *codextest.Server) {
		srv.Update(codexThread, func(th *codextest.Thread) {
			th.Processes = []map[string]any{{"processId": "71", "command": "sleep 300", "cwd": "/srv/proj"}}
		})
	})
	ctx := context.Background()
	detail, err := e.Execute(ctx, action.Request{Kind: action.SessionRename, Target: codexName, Rename: "login-bug"})
	if err != nil || !strings.Contains(detail, "called login-bug now; the panel still finds it as "+codexName) {
		t.Errorf("a rename: %q, %v", detail, err)
	}
	if len(e.codex.Find(codexName)) != 1 {
		t.Error("the session lost the name the panel finds it by")
	}
	list, err := e.Processes(ctx, codexName)
	if err != nil || len(list) != 1 || list[0].ID != "71" || list[0].Command != "sleep 300" {
		t.Fatalf("the processes are %+v, %v", list, err)
	}
	stop := action.Request{Kind: action.TaskStop, Target: codexName, Work: &action.Work{ID: "71"}}
	if detail, err := e.Execute(ctx, stop); err != nil || !strings.Contains(detail, "71 of "+codexName+" is stopped") {
		t.Errorf("stopping: %q, %v", detail, err)
	}
	if detail, err := e.Execute(ctx, stop); err != nil || !strings.Contains(detail, "not running any more") {
		t.Errorf("stopping again: %q, %v", detail, err)
	}
	if len(srv.Calls("thread/backgroundTerminals/terminate")) != 2 {
		t.Error("terminate did not reach the daemon")
	}
	if _, err := e.Processes(ctx, "aacpanel"); err == nil || !strings.Contains(err.Error(), "not a codex session") {
		t.Errorf("the processes of a claude session: %v", err)
	}
}

// A question of plan mode is answered and dismissed through the daemon by the
// id the person saw; a question answered in the meantime is not.
func TestCodexQuestionIsAnsweredAndDismissed(t *testing.T) {
	ask := func(srv *codextest.Server) int {
		return srv.Ask(codexThread, "item/tool/requestUserInput", map[string]any{"turnId": "turn-x", "itemId": "call_1",
			"isBlocking": true, "questions": []any{map[string]any{"id": "indent", "header": "Indent",
				"question": "Tabs or spaces?", "isOther": true, "isSecret": false, "options": []any{
					map[string]any{"label": "Tabs", "description": ""}, map[string]any{"label": "Spaces", "description": ""}}}}})
	}
	var id int
	srv, e := onCodex(t, func(srv *codextest.Server) { id = ask(srv) })
	ctx := context.Background()
	waitFor(t, "the question to reach the executor", func() bool { return len(e.codex.Find(codexName)[0].Link.Pending(codexThread)) == 1 })
	if d, err := e.Permission(ctx, codexName); err != nil || d != nil {
		t.Errorf("a question offered as a permission: %+v, %v", d, err)
	}
	const key = "call_1"
	_ = id

	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionAnswer, Target: codexName,
		Answer: &action.Answer{AskID: "99", Picks: [][]int{{1}}}}); err == nil || !strings.Contains(err.Error(), "something else") {
		t.Errorf("an answer to a question gone: %v", err)
	}
	detail, err := e.Execute(ctx, action.Request{Kind: action.SessionAnswer, Target: codexName,
		Answer: &action.Answer{AskID: key, Picks: [][]int{{}}, Texts: []string{"two spaces"}}})
	if err != nil || !strings.Contains(detail, "answer sent") {
		t.Fatalf("the answer: %q, %v", detail, err)
	}
	waitFor(t, "the answer to reach the daemon", func() bool { return len(srv.Answers()) == 1 })
	if got := string(srv.Answers()[0].Result); got != `{"answers":{"indent":{"answers":["two spaces"]}}}` {
		t.Errorf("the daemon got %s", got)
	}

	ask(srv)
	waitFor(t, "the second question", func() bool { return len(e.codex.Find(codexName)[0].Link.Pending(codexThread)) == 1 })
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionDismiss, Target: codexName,
		Answer: &action.Answer{AskID: key}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the dismissal to reach the daemon", func() bool { return len(srv.Answers()) == 2 })
	if got := string(srv.Answers()[1].Result); !strings.Contains(got, "will answer in the conversation") {
		t.Errorf("the dismissal went as %s", got)
	}
}

// A form of an MCP server is answered field by field; a note has no place in
// it, and a decline sends no content.
func TestCodexFormOfAnMcpServer(t *testing.T) {
	srv, e := onCodex(t, func(srv *codextest.Server) {
		srv.Ask(codexThread, "mcpServer/elicitation/request", map[string]any{"turnId": "turn-x", "serverName": "tracker",
			"mode": "form", "_meta": nil, "message": "File the bug", "requestedSchema": map[string]any{"type": "object",
				"properties": map[string]any{"severity": map[string]any{"type": "string", "enum": []string{"low", "high"}}},
				"required":   []string{"severity"}}})
	})
	ctx := context.Background()
	waitFor(t, "the form", func() bool { return len(e.codex.Find(codexName)[0].Link.Pending(codexThread)) == 1 })
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionAnswer, Target: codexName,
		Answer: &action.Answer{AskID: "1", Picks: [][]int{{2}}, Notes: []string{"soon"}}}); err == nil ||
		!strings.Contains(err.Error(), "no field for a note") {
		t.Errorf("a note to a form: %v", err)
	}
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionAnswer, Target: codexName,
		Answer: &action.Answer{AskID: "1", Picks: [][]int{{2}}}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the form to reach the daemon", func() bool { return len(srv.Answers()) == 1 })
	if got := string(srv.Answers()[0].Result); got != `{"_meta":null,"action":"accept","content":{"severity":"high"}}` {
		t.Errorf("the daemon got %s", got)
	}
}

// A grant of permissions and a page an MCP server asks to open are the
// permission the screen already draws, the page as its own address rather
// than a line of the request.
func TestCodexGrantAndPageAreChoices(t *testing.T) {
	srv, e := onCodex(t, func(srv *codextest.Server) {
		srv.Ask(codexThread, "item/permissions/requestApproval", map[string]any{"turnId": "turn-x", "itemId": "call_p",
			"environmentId": nil, "startedAtMs": 1, "cwd": "/srv/proj", "reason": "it writes the report",
			"permissions": map[string]any{"network": nil, "fileSystem": map[string]any{"read": nil, "write": []string{"/srv/out"},
				"entries": []any{map[string]any{"access": "read",
					"path": map[string]any{"type": "glob_pattern", "pattern": "/srv/proj/**/*.go"}}}}}})
	})
	ctx := context.Background()
	d := askedPermission(t, e)
	var texts []string
	for _, o := range d.Options {
		texts = append(texts, o.Text)
	}
	if d.Tool != "Permissions" || !reflect.DeepEqual(d.Action, []string{"write /srv/out", "read /srv/proj/**/*.go"}) ||
		strings.Join(texts, "|") != "Yes, grant these permissions for this turn|Yes, grant these permissions for this session|No, go on without them" ||
		!d.Options[1].Lasting || len(d.Note) != 1 || !strings.Contains(d.Note[0], "the report") {
		t.Errorf("the grant reads as %+v", d)
	}
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionPermit, Target: codexName,
		Permit: &action.Permit{Option: 2, Fingerprint: d.Fingerprint}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the grant to reach the daemon", func() bool { return len(srv.Answers()) == 1 })
	if got := string(srv.Answers()[0].Result); got != `{"permissions":{"fileSystem":{"entries":[{"access":"read",`+
		`"path":{"pattern":"/srv/proj/**/*.go","type":"glob_pattern"}}],"read":null,"write":["/srv/out"]}},"scope":"session"}` {
		t.Errorf("the daemon got %s", got)
	}

	srv.Ask(codexThread, "mcpServer/elicitation/request", map[string]any{"turnId": nil, "serverName": "tracker",
		"mode": "url", "_meta": nil, "message": "Sign in to the tracker", "url": "https://tracker.test/login",
		"elicitationId": "e1"})
	d = askedPermission(t, e)
	if d.Tool != "MCP" || strings.Join(d.Action, "|") != "Sign in to the tracker" || d.URL != "https://tracker.test/login" ||
		d.Options[0].Text != "Yes, I open the page" || len(d.Options) != 3 || d.Note[0] != "asked by the MCP server tracker" {
		t.Errorf("the page reads as %+v", d)
	}
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionEscape, Target: codexName}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the cancel to reach the daemon", func() bool { return len(srv.Answers()) == 2 })
	if got := string(srv.Answers()[1].Result); got != `{"_meta":null,"action":"cancel","content":null}` {
		t.Errorf("escape answered %s", got)
	}
}

// The models, the MCP servers and the skills of a codex session are codex's
// own lists, in the shapes the screens draw claude's in.
func TestCodexQuestionsAreAnsweredWithCodexsLists(t *testing.T) {
	_, e := onCodex(t, func(srv *codextest.Server) {
		codexCatalogue(srv)
		srv.Mcp(map[string]any{"name": "docs", "runtimeStatus": "authenticationRequired", "authStatus": "notLoggedIn",
			"httpOrigin": "https://docs.test", "pluginId": "docs@market", "toolsError": nil, "tools": map[string]any{},
			"serverInfo": nil})
		srv.Skills(map[string]any{"name": "pdf", "description": "read pdfs", "scope": "repo", "enabled": true, "pluginId": nil})
	})
	ctx := context.Background()
	models, err := e.Models(ctx, codexName)
	if err != nil || models.Agent != "codex" || models.Transport != action.SwitchStream || models.Picked != "gpt-test" ||
		len(models.List) != 2 || models.List[1].Value != "gpt-other" || models.List[1].Effort != "medium" {
		t.Errorf("the models are %+v, %v", models, err)
	}
	mcp, err := e.Mcp(ctx, codexName)
	want := []action.McpServer{{Name: "docs", Status: "needs-auth", Source: "plugin", URL: "https://docs.test",
		Tools: []string{}, Auth: "notLoggedIn"}}
	if err != nil || mcp.Agent != "codex" || !reflect.DeepEqual(mcp.Servers, want) {
		t.Errorf("the servers are %+v, %v", mcp, err)
	}
	setup, err := e.Setup(ctx, codexName, action.SetupSkills)
	if err != nil || setup.Agent != "codex" || !reflect.DeepEqual(setup.Skills, []action.Skill{{Name: "pdf",
		Description: "read pdfs", Source: "repo", State: "on"}}) {
		t.Errorf("the skills are %+v, %v", setup, err)
	}
	if _, err := e.Setup(ctx, codexName, action.SetupHooks); err == nil || !strings.Contains(err.Error(), "its skills") {
		t.Errorf("the hooks of a codex session: %v", err)
	}
}

// Every state codex gives a server of a thread reaches the screen in a word
// it draws: claude's where claude has one, a word of its own where it has
// none, and one for a server codex gives no state, whose configuration
// changed after the thread started it. A word codex adds later goes as codex
// says it, not as "unknown".
func TestCodexServersStandInTheWordsOfTheScreen(t *testing.T) {
	states := []struct {
		codex  any
		screen string
	}{
		{"notStarted", "not-started"}, {"starting", "starting"}, {"connected", "connected"},
		{"authenticationRequired", "needs-auth"}, {"failed", "failed"}, {"cancelled", "cancelled"},
		{"disabled", "disabled"}, {nil, "changed"}, {"reconnecting", "reconnecting"},
	}
	_, e := onCodex(t, func(srv *codextest.Server) {
		servers := []map[string]any{}
		for i, s := range states {
			servers = append(servers, map[string]any{"name": fmt.Sprintf("s%d", i), "runtimeStatus": s.codex,
				"authStatus": "unsupported", "tools": map[string]any{}})
		}
		srv.Mcp(servers...)
	})
	mcp, err := e.Mcp(context.Background(), codexName)
	if err != nil || len(mcp.Servers) != len(states) {
		t.Fatalf("the servers are %+v, %v", mcp, err)
	}
	for i, s := range states {
		if got := mcp.Servers[i].Status; got != s.screen {
			t.Errorf("a server codex calls %v reaches the screen as %q, expected %q", s.codex, got, s.screen)
		}
	}
}

// The words the screen gives the states of a server are the states the
// contract of the protocol holds codex to: a state the map words and the
// contract does not name could go in a release unnoticed.
func TestCodexServerStatesAreTheContracts(t *testing.T) {
	c, err := contract.Panel()
	if err != nil {
		t.Fatal(err)
	}
	named := c.Values(contract.Call, "mcpServerStatus/list", "result", "data[].runtimeStatus")
	var worded []string
	for state := range mcpStatuses {
		if state != "" {
			worded = append(worded, state)
		}
	}
	slices.Sort(worded)
	slices.Sort(named)
	if !slices.Equal(worded, named) {
		t.Errorf("the screen words the states %v, and the contract names %v", worded, named)
	}
}

// A thread codex runs in a tmux session the launcher started is told by the
// command tmux keeps for the pane, the way a close finds it.
func TestCodexTerminalsAreTheLaunchersPanes(t *testing.T) {
	prev := listCodexPanes
	t.Cleanup(func() { listCodexPanes = prev })
	env := "/run/user/1000/aacpanel-launch-1234/env.sh"
	listCodexPanes = func(context.Context) (string, error) {
		return "shop\tenv -i sh " + env + " /opt/codex resume " + codexThread + " -C /srv/shop\n" +
			"mine\tcodex resume 019a1f00-0000-7000-8000-0000000000ef\n" +
			"other\tenv -i sh " + env + " /opt/codex resume 019a1f00-0000-7000-8000-0000000000ef -C /srv/x\n", nil
	}
	other := "019a1f00-0000-7000-8000-0000000000ef"
	got, err := codexTerminals(context.Background(), []string{codexThread, other, "019a1f00-0000-7000-8000-000000000001"})
	if err != nil || !reflect.DeepEqual(got, map[string]string{codexThread: "shop", other: "other"}) {
		t.Errorf("the terminals are %v, %v", got, err)
	}
}

// What an MCP server asks with nothing to fill is answered yes, no or with the
// request cancelled; a check only codex can make, and a form the panel cannot
// fill, are only refused.
func TestCodexChoicesOfAnMcpServer(t *testing.T) {
	results := func(r codex.Request) (texts []string, actions []string) {
		for _, c := range codexChoices(r) {
			texts = append(texts, c.text)
			actions = append(actions, c.result.(map[string]any)["action"].(string))
		}
		return texts, actions
	}
	page := codex.Request{Method: "mcpServer/elicitation/request", Params: codex.Params{Elicitation: codex.Elicitation{
		Server: "s", Mode: "url", URL: "https://tracker.test/login"}}}
	texts, actions := results(page)
	if strings.Join(texts, "|") != "Yes, I open the page|No|Cancel the request" ||
		strings.Join(actions, " ") != "accept decline cancel" {
		t.Errorf("a page offers %q answered %q", texts, actions)
	}
	if c := codexChoices(page)[0].result.(map[string]any); c["content"] != nil {
		t.Errorf("a page is accepted with content %v", c["content"])
	}
	verify := codex.Request{Method: "mcpServer/elicitation/request", Params: codex.Params{Elicitation: codex.Elicitation{
		Server: "s", Mode: "openai/userVerification", Title: "Verify"}}}
	if _, actions := results(verify); strings.Join(actions, " ") != "decline cancel" {
		t.Errorf("a verification is answered %q", actions)
	}
	for name, mode := range map[string]string{"no mode": "", "a mode of another release": "openai/wizard"} {
		odd := codex.Request{Method: "mcpServer/elicitation/request", Params: codex.Params{Elicitation: codex.Elicitation{
			Server: "s", Mode: mode, Message: "Fill it", Schema: json.RawMessage(`{"type":"object","properties":{}}`)}}}
		if _, actions := results(odd); strings.Join(actions, " ") != "decline cancel" {
			t.Errorf("%s is answered %q", name, actions)
		}
		if notes := elicitNotes(odd); len(notes) != 2 || !strings.Contains(notes[1], "does not know") {
			t.Errorf("%s says %q", name, notes)
		}
	}
	empty := codex.Request{Method: "mcpServer/elicitation/request", Params: codex.Params{Elicitation: codex.Elicitation{
		Server: "s", Mode: "form", Message: "Go on?", Schema: json.RawMessage(`{"type":"object","properties":{}}`)}}}
	if _, actions := results(empty); strings.Join(actions, " ") != "accept decline cancel" {
		t.Errorf("a form with nothing to fill is answered %q", actions)
	}
}

// A command the switch does not name is refused, never taken for /stop.
func TestCodexCommandRefusesWhatItDoesNotName(t *testing.T) {
	srv, e := onCodex(t, nil)
	action.CodexCommands["ps"] = true
	t.Cleanup(func() { delete(action.CodexCommands, "ps") })
	th := e.codex.Find(codexName)[0]
	if _, err := codexCommand(context.Background(), th, &action.Command{Name: "ps"}); err == nil ||
		!strings.Contains(err.Error(), "not sent to codex") {
		t.Errorf("/ps: %v", err)
	}
	if n := len(srv.Calls("thread/backgroundTerminals/clean")); n != 0 {
		t.Errorf("an unnamed command stopped the background terminals %d times", n)
	}
}

// A codex thread takes a name of any words; a claude session named like one
// keeps the rule of a key.
func TestCodexThreadTakesAnyNameAndClaudeAKey(t *testing.T) {
	_, e := onCodex(t, nil)
	detail, err := e.Execute(context.Background(), action.Request{Kind: action.SessionRename, Target: codexName,
		Rename: "Σφάλμα σύνδεσης — δεύτερη φορά"})
	if err != nil || !strings.Contains(detail, "Σφάλμα σύνδεσης") {
		t.Errorf("a name of words: %q, %v", detail, err)
	}
	if _, err := e.sessionRename(context.Background(), "aacpanel", "Σφάλμα σύνδεσης"); err == nil ||
		!strings.Contains(err.Error(), "latin letters") {
		t.Errorf("a claude session took a name of words: %v", err)
	}
}

// A command of codex's alone never reaches claude, whose /review is another
// thing.
func TestClaudeRefusesTheCommandsOfCodex(t *testing.T) {
	e := New(nil, "")
	for _, name := range []string{"review", "goal", "stop"} {
		if _, err := e.sessionCommand(context.Background(), "aacpanel", &action.Command{Name: name}); err == nil ||
			!strings.Contains(err.Error(), "a command of codex's") {
			t.Errorf("/%s for claude: %v", name, err)
		}
	}
}
