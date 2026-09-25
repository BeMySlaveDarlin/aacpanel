package check

import (
	"strings"
	"testing"
)

// The read-only screens of what a session on the stream is set up with open
// from their commands and show claude's own rows: hooks by event down to one
// hook and the command it runs, the memory files and saved memories, skills
// with a search, the kinds of subagents — not the running ones — and the
// merged settings. Nothing is sent to the session.
func TestSettingsScreensShowTheRowsOfTheSession(t *testing.T) {
	var got struct {
		Hints      []string `json:"hints"`
		HooksReady bool     `json:"hooksReady"`
		Hooks      struct {
			Title  string   `json:"title"`
			Sum    string   `json:"sum"`
			Events []string `json:"events"`
			Event  string   `json:"event"`
			Rows   []string `json:"rows"`
			Off    int      `json:"off"`
			Card   string   `json:"card"`
			Facts  []string `json:"facts"`
			Code   string   `json:"code"`
		} `json:"hooks"`
		Memory struct {
			Sum   string   `json:"sum"`
			Heads []string `json:"heads"`
			Rows  []string `json:"rows"`
		} `json:"memory"`
		Skills struct {
			Sum    string   `json:"sum"`
			Rows   []string `json:"rows"`
			Found  []string `json:"found"`
			Opened []string `json:"opened"`
		} `json:"skills"`
		Agents struct {
			Title    string   `json:"title"`
			Sum      string   `json:"sum"`
			Rows     []string `json:"rows"`
			WorkList bool     `json:"workList"`
		} `json:"agents"`
		Config struct {
			Rows   []string `json:"rows"`
			Blocks []string `json:"blocks"`
		} `json:"config"`
		Asked        []string `json:"asked"`
		Sent         int      `json:"sent"`
		ConsoleReady bool     `json:"consoleReady"`
		Error        string   `json:"error"`
	}
	runFixture(t, "setupsheet.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}

	if !strings.Contains(strings.Join(got.Hints, "\n"), "Hooks, read-only") {
		t.Errorf("typing /ho does not offer /hooks: %v", got.Hints)
	}
	h := got.Hooks
	if !got.HooksReady || h.Title != "Hooks" || h.Sum != "3 hooks configured" || len(h.Events) != 2 ||
		!strings.HasPrefix(h.Events[0], "PreToolUse 2") {
		t.Errorf("the hooks screen is %+v", h)
	}
	if h.Event != "PreToolUse" || len(h.Rows) != 2 || h.Off != 1 || !strings.Contains(h.Rows[0], "AskUserQuestion") {
		t.Errorf("the hooks of an event are %+v", h)
	}
	if h.Card != "Command hook" || h.Code != "node /home/u/.claude/plugins/hookify/rules.js --event pre" ||
		!strings.Contains(strings.Join(h.Facts, "|"), "hookify") {
		t.Errorf("one hook reads %q %v %q", h.Card, h.Facts, h.Code)
	}

	m := got.Memory
	if m.Sum != "Auto-memory: on" || strings.Join(m.Heads, ",") != "Instructions,Folders,Saved memories" || len(m.Rows) != 5 {
		t.Errorf("the memory screen is %+v", m)
	}
	if !strings.Contains(m.Rows[1], "not created") || !strings.HasPrefix(m.Rows[2], "Auto memory") ||
		!strings.Contains(m.Rows[4], "How the stack is rolled out") {
		t.Errorf("the memory rows are %v", m.Rows)
	}

	s := got.Skills
	if s.Sum != "3 skills" || len(s.Rows) != 3 || !strings.Contains(s.Rows[1], "locked by plugin · plugin · ~180 tok") ||
		!strings.Contains(s.Rows[2], "name only") {
		t.Errorf("the skills screen is %+v", s)
	}
	if len(s.Found) != 1 || strings.Join(s.Opened, "") != "Go command line applications." {
		t.Errorf("the search found %v and opened %v", s.Found, s.Opened)
	}

	a := got.Agents
	if a.Title != "Agents" || a.Sum != "2 agents" || a.WorkList || !strings.HasPrefix(a.Rows[0], "Explore") {
		t.Errorf("/agents opened %+v — the kinds of subagents, not the running ones, were wanted", a)
	}

	c := got.Config
	if len(c.Rows) != 4 || !strings.HasPrefix(c.Rows[1], "model") || !strings.HasSuffix(c.Rows[1], "opus[1m]") || len(c.Blocks) != 1 ||
		!strings.Contains(c.Blocks[0], `"command": "~/statusline.sh"`) {
		t.Errorf("the settings screen is %+v", c)
	}

	if strings.Join(got.Asked, ",") != "hooks,memory,skills,agents,config" || got.Sent != 0 {
		t.Errorf("the service was asked %v and %d actions were sent", got.Asked, got.Sent)
	}
	if got.ConsoleReady {
		t.Error("/hooks is ready to send in a console, where it is a screen driven by keys")
	}
}
