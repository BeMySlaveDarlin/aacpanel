package executor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
)

// choice is one item a person presses for a request that is a choice among a
// few: its words, whether it holds past this request, and the reply it sends.
type choice struct {
	text    string
	lasting bool
	result  any
}

// codexChoices are the items a request offers. An approval offers the
// decisions of the daemon; a grant of permissions is for the turn, for the
// session, or none, as codex's own terminal offers it; an MCP server is
// answered yes, no, or with the request cancelled — and a check only codex
// can make is only refused here.
func codexChoices(r codex.Request) []choice {
	switch {
	case r.Grants():
		return []choice{
			{text: "Yes, grant these permissions for this turn", result: r.Grant("turn")},
			{text: "Yes, grant these permissions for this session", lasting: true, result: r.Grant("session")},
			{text: "No, go on without them", result: r.Deny()},
		}
	case r.Elicits():
		out := []choice{
			{text: "No", result: codex.Elicit("decline", nil)},
			{text: "Cancel the request", result: codex.Elicit("cancel", nil)},
		}
		if _, err := r.Fields(); !r.Known() || err != nil {
			return out
		}
		// A page is accepted with no content: the person opens it, and the
		// server learns the rest from the page.
		if r.URL != "" {
			return append([]choice{{text: "Yes, I open the page", result: codex.Elicit("accept", nil)}}, out...)
		}
		return append([]choice{{text: "Yes", result: codex.Elicit("accept", map[string]any{})}}, out...)
	}
	var out []choice
	for _, d := range r.Decisions() {
		text, lasting := decisionText(d)
		out = append(out, choice{text: text, lasting: lasting, result: map[string]any{"decision": d}})
	}
	return out
}

// firstChoice is the oldest request that is a choice rather than a question.
func firstChoice(pending []codex.Request) (codex.Request, bool) {
	for _, r := range pending {
		if !r.Asks() {
			return r, true
		}
	}
	return codex.Request{}, false
}

// elicitLines are what an MCP server says it asks, and the page it asks to
// open.
func elicitLines(r codex.Request) []string {
	var lines []string
	for _, text := range []string{r.Title, r.Message, r.Description} {
		if strings.TrimSpace(text) != "" {
			lines = append(lines, strings.Split(strings.TrimRight(text, "\n"), "\n")...)
		}
	}
	if r.URL != "" {
		lines = append(lines, r.URL)
	}
	return lines
}

// elicitNotes say who asks, and what the panel cannot do of it.
func elicitNotes(r codex.Request) []string {
	notes := []string{"asked by the MCP server " + r.Server}
	switch {
	case r.Verification():
		notes = append(notes, "codex asks for a check of the person only codex itself makes: answer it in codex, "+
			"or refuse it here")
	case !r.Known():
		notes = append(notes, fmt.Sprintf("the server asks in a way the panel does not know (%q): answer it in codex, "+
			"or refuse it here", r.Mode))
	}
	if _, err := r.Fields(); err != nil {
		notes = append(notes, "the panel cannot fill this form ("+err.Error()+"): fill it in codex, or refuse it here")
	}
	return notes
}

// pendingAsk is the question or the form a codex session waits on under the
// id the person answers.
func pendingAsk(th codex.Thread, askID string) (codex.Request, error) {
	for _, r := range th.Link.Pending(th.ID) {
		if r.AskID() == askID && r.Asks() {
			return r, nil
		}
	}
	return codex.Request{}, fmt.Errorf("session %s is asking something else now: the previous question was answered "+
		"outside the panel", th.Name)
}

// codexAnswer answers a question of plan mode with the labels picked, the
// words of the person's own and a note beside a pick; and a form of an MCP
// server with a value a field, of the kind each field is of.
func codexAnswer(ctx context.Context, th codex.Thread, ans *action.Answer) (string, error) {
	if ans == nil {
		return "", fmt.Errorf("answer with no options picked")
	}
	r, err := pendingAsk(th, ans.AskID)
	if err != nil {
		return "", err
	}
	asked := r.Ask()
	if len(ans.Picks) != len(asked.Questions) {
		return "", fmt.Errorf("there are %d questions, but %d answers arrived", len(asked.Questions), len(ans.Picks))
	}
	var reply any
	if r.Elicits() {
		if slices.ContainsFunc(ans.Notes, func(n string) bool { return n != "" }) {
			return "", fmt.Errorf("a form of an MCP server has no field for a note: the answer was not sent — " +
				"send it without the note")
		}
		content, err := r.Content(ans.Picks, ans.Texts)
		if err != nil {
			return "", fmt.Errorf("the form of session %s was not sent: %w", th.Name, err)
		}
		reply = codex.Elicit("accept", content)
	} else if reply, err = r.Answers(ans.Picks, ans.Texts, ans.Notes); err != nil {
		return "", fmt.Errorf("the answer to session %s was not sent: %w", th.Name, err)
	}
	if err := th.Link.Reply(ctx, th.ID, r.Key(), reply); err != nil {
		if errors.Is(err, codex.ErrAnswered) {
			return "", fmt.Errorf("session %s is asking nothing — the question is already answered", th.Name)
		}
		return "", fmt.Errorf("session %s: %w", th.Name, err)
	}
	if r.Elicits() {
		return fmt.Sprintf("the form of %s sent to session %s", r.Server, th.Name), nil
	}
	return fmt.Sprintf("answer sent to %s: %s", th.Name, plural(len(asked.Questions), "question", "questions")), nil
}

// codexDismiss puts a question away: plan mode reads that the person will
// answer in the conversation, and an MCP server that its form is declined.
func codexDismiss(ctx context.Context, th codex.Thread, ans *action.Answer) (string, error) {
	if ans == nil {
		return "", fmt.Errorf("it is not said which question to dismiss")
	}
	r, err := pendingAsk(th, ans.AskID)
	if err != nil {
		return "", err
	}
	reply := any(r.Dismiss())
	if r.Elicits() {
		reply = codex.Elicit("decline", nil)
	}
	if err := th.Link.Reply(ctx, th.ID, r.Key(), reply); err != nil {
		if errors.Is(err, codex.ErrAnswered) {
			return "", fmt.Errorf("session %s is asking nothing — the question is already answered", th.Name)
		}
		return "", fmt.Errorf("session %s: %w", th.Name, err)
	}
	if r.Elicits() {
		return fmt.Sprintf("the form of %s declined in session %s", r.Server, th.Name), nil
	}
	return fmt.Sprintf("question dismissed in %s: the session is waiting for an ordinary message", th.Name), nil
}

// reviewTargets are the targets of a review as codex names them.
var reviewTargets = map[string]string{
	action.ReviewUncommitted: codex.ReviewUncommitted,
	action.ReviewBranch:      codex.ReviewBranch,
	action.ReviewCommit:      codex.ReviewCommit,
	action.ReviewCustom:      codex.ReviewCustom,
}

// codexCommand runs a command of codex's own terminal client through the
// protocol. The commands of claude are refused by name: a codex session has
// none of them.
func codexCommand(ctx context.Context, th codex.Thread, cmd *action.Command) (string, error) {
	if cmd == nil || !action.CodexCommands[cmd.Name] {
		name := ""
		if cmd != nil {
			name = cmd.Name
		}
		return "", fmt.Errorf("/%s is a command of claude's: %s is a codex session, and the panel sends it "+
			"/compact, /review, /goal and /stop", name, th.Name)
	}
	switch cmd.Name {
	case "compact":
		if err := th.Link.Compact(ctx, th.ID); err != nil {
			return "", fmt.Errorf("session %s was not compacted: %w", th.Name, err)
		}
		return fmt.Sprintf("%s compacts its context: codex summarises the conversation and goes on from the summary",
			th.Name), nil
	case "review":
		return codexReview(ctx, th, cmd.Review)
	case "goal":
		return codexGoal(ctx, th, cmd.Goal)
	case "stop":
		if err := th.Link.Clean(ctx, th.ID); err != nil {
			return "", fmt.Errorf("the background terminals of %s were not stopped: %w", th.Name, err)
		}
		return fmt.Sprintf("every background terminal of %s is stopped", th.Name), nil
	}
	return "", fmt.Errorf("/%s is not sent to codex from the panel", cmd.Name)
}

func codexReview(ctx context.Context, th codex.Thread, r *action.Review) (string, error) {
	if r == nil {
		return "", fmt.Errorf("a review that does not say what to look at")
	}
	target := codex.Review{Target: reviewTargets[r.Target], Branch: r.Branch, SHA: r.Commit, Title: r.Title,
		Instructions: r.Instructions}
	if err := th.Link.StartReview(ctx, th.ID, target); err != nil {
		return "", fmt.Errorf("session %s did not start the review: %w", th.Name, err)
	}
	what := map[string]string{
		action.ReviewUncommitted: "the uncommitted changes",
		action.ReviewBranch:      "the work against " + r.Branch,
		action.ReviewCommit:      "commit " + r.Commit,
		action.ReviewCustom:      "what you described",
	}[r.Target]
	return fmt.Sprintf("%s reviews %s: the findings come as a turn of their own", th.Name, what), nil
}

func codexGoal(ctx context.Context, th codex.Thread, g *action.Goal) (string, error) {
	if g == nil {
		return "", fmt.Errorf("a goal that does not say what to do")
	}
	if g.Do == action.GoalClear {
		had, err := th.Link.ClearGoal(ctx, th.ID)
		switch {
		case err != nil:
			return "", fmt.Errorf("the goal of %s was not cleared: %w", th.Name, err)
		case !had:
			return fmt.Sprintf("%s had no goal", th.Name), nil
		}
		return fmt.Sprintf("the goal of %s is cleared", th.Name), nil
	}
	var objective, status string
	var budget *int64
	switch g.Do {
	case action.GoalSet:
		objective = g.Objective
		if g.Budget > 0 {
			budget = &g.Budget
		}
	case action.GoalPause:
		status = codex.GoalPaused
	case action.GoalResume:
		status = codex.GoalActive
	}
	goal, err := th.Link.SetGoal(ctx, th.ID, objective, status, budget)
	if err != nil {
		return "", fmt.Errorf("the goal of %s was not changed: %w", th.Name, err)
	}
	switch g.Do {
	case action.GoalSet:
		return fmt.Sprintf("%s has a goal now (%s): codex starts working towards it at once, turn after turn, "+
			"until it is met, paused or out of its budget", th.Name, goal.Status), nil
	case action.GoalPause:
		if goal.Status != codex.GoalPaused {
			return fmt.Sprintf("the goal of %s stays %s: codex does not pause it", th.Name, goal.Status), nil
		}
		return fmt.Sprintf("the goal of %s is paused; a turn already running goes on to its end", th.Name), nil
	}
	// A goal codex stopped by itself — out of its budget, or met — stays so,
	// and the daemon answers with how it stands.
	if goal.Status != codex.GoalActive {
		return fmt.Sprintf("the goal of %s stays %s: codex does not resume it", th.Name, goal.Status), nil
	}
	return fmt.Sprintf("the goal of %s is active again: codex goes on with it at once", th.Name), nil
}

// codexRename names the thread. The session keeps the name the panel finds it
// by; the row shows the name beside it.
func codexRename(ctx context.Context, th codex.Thread, name string) (string, error) {
	if err := th.Link.Rename(ctx, th.ID, name); err != nil {
		return "", fmt.Errorf("session %s was not renamed: %w", th.Name, err)
	}
	return fmt.Sprintf("the thread of %s is called %s now; the panel still finds it as %s", th.Name, name, th.Name), nil
}

// codexTaskStop stops one background terminal of a codex session, named by
// the id codex gives it.
func codexTaskStop(ctx context.Context, th codex.Thread, work *action.Work) (string, error) {
	if work == nil {
		return "", fmt.Errorf("it is not said which background terminal to stop")
	}
	stopped, err := th.Link.Terminate(ctx, th.ID, work.ID)
	if err != nil {
		return "", fmt.Errorf("the background terminal %s of %s was not stopped: %w", work.ID, th.Name, err)
	}
	if !stopped {
		return fmt.Sprintf("the background terminal %s of %s was not running any more", work.ID, th.Name), nil
	}
	return fmt.Sprintf("the background terminal %s of %s is stopped", work.ID, th.Name), nil
}

// Processes answers which background terminals a codex session's turns left
// running. A claude session has none of codex's: its background work is in
// its feed.
func (e *Executor) Processes(ctx context.Context, target string) ([]action.Process, error) {
	th, err := e.codexSession(target)
	if err != nil {
		return nil, err
	}
	if th == nil {
		return nil, fmt.Errorf("%s is not a codex session: the background work of a claude session is in its feed",
			target)
	}
	list, err := th.Link.Processes(ctx, th.ID)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", th.Name, err)
	}
	out := make([]action.Process, 0, len(list))
	for _, p := range list {
		out = append(out, action.Process{ID: p.ID, Command: p.Command, CWD: p.CWD, PID: p.PID, CPU: p.CPU, RSS: p.RSS})
	}
	return out, nil
}

// codexModels answers what a codex session can be switched to: the catalogue
// of the daemon of its own contour, with the model, the effort and the mode
// the thread runs.
func codexModels(ctx context.Context, th codex.Thread) (*action.Models, error) {
	list, err := th.Link.Models(ctx)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", th.Name, err)
	}
	model, effort, mode := th.Link.Settings(th.ID)
	out := &action.Models{Transport: action.SwitchStream, Agent: codex.Agent, List: []action.Model{},
		Effort: effort, Picked: model, Mode: mode}
	for _, m := range list {
		out.List = append(out.List, action.Model{Value: m.Model, Name: m.Name, Efforts: m.Efforts, Effort: m.Effort})
	}
	return out, nil
}

// mcpStatuses word how a server of codex stands the way the screen words
// claude's.
var mcpStatuses = map[string]string{
	"connected":              "connected",
	"authenticationRequired": "needs-auth",
	"starting":               "pending",
	"notStarted":             "pending",
	"disabled":               "disabled",
	"failed":                 "failed",
	"cancelled":              "failed",
}

// codexMcp answers the MCP servers of a codex session as codex lists them,
// with the thread's connections to them. A server of codex's config.toml is
// grouped under it, one of a plugin under the plugins.
func codexMcp(ctx context.Context, th codex.Thread) (*action.Mcp, error) {
	list, err := th.Link.McpServers(ctx, th.ID)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", th.Name, err)
	}
	out := &action.Mcp{Transport: action.SwitchStream, Agent: codex.Agent, Servers: []action.McpServer{}}
	for _, m := range list {
		status := mcpStatuses[m.Status]
		if status == "" {
			status = "unknown"
		}
		source := "config.toml"
		if m.Plugin != "" {
			source = "plugin"
		}
		out.Servers = append(out.Servers, action.McpServer{Name: m.Name, Status: status, Error: m.Error,
			Source: source, URL: m.Origin, Title: m.Title, Version: m.Version, Description: m.Description,
			Tools: m.Tools, Auth: m.Auth})
	}
	return out, nil
}

// codexSetup answers the skills of a codex session, as codex lists them for
// its directory. The rest of what claude shows — hooks, memory, agents,
// settings — is codex's own to show, in its terminal and its codex home.
func codexSetup(ctx context.Context, th codex.Thread, part string) (*action.Setup, error) {
	if part != action.SetupSkills {
		return nil, fmt.Errorf("%s is a codex session: the panel shows its skills, and its %s are in its codex home "+
			"and in codex itself", th.Name, part)
	}
	list, err := th.Link.Skills(ctx, th.ID)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", th.Name, err)
	}
	out := &action.Setup{Transport: action.SwitchStream, Agent: codex.Agent, Skills: []action.Skill{}}
	for _, s := range list {
		source, state := s.Scope, "on"
		if s.Plugin != "" {
			source = "plugin"
		}
		if !s.Enabled {
			state = "off"
		}
		out.Skills = append(out.Skills, action.Skill{Name: s.Name, Description: s.Description, Source: source,
			State: state})
	}
	return out, nil
}
