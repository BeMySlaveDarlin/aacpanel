package check

import (
	"strings"
	"testing"
)

// The sessions of a contour are laid out by project, and the list is read in
// the order of what needs the person: a project whose session asks stands
// above the rest, which keep the map's order. A project with a live
// session shows none of its past; one with nothing live closes the list with
// its last conversation, and not more of them than the page is meant to hold.
func TestSessionsAreLaidOutByProjectInTheOrderOfWhatWaits(t *testing.T) {
	now := "2026-09-27T10:00:00Z"
	profile := map[string]any{"groups": []any{
		map[string]any{"name": "Infra", "projects": []any{
			map[string]any{"id": 1, "name": "aacpanel", "session": "aacpanel", "path": "/p/aacpanel"},
			map[string]any{"id": 2, "name": "proxy", "session": "proxy", "path": "/p/proxy"},
			map[string]any{"id": 3, "name": "person", "session": "person", "path": "/p/person"},
		}},
	}}
	sessions := []any{
		map[string]any{"session": "aacpanel", "sessionId": "a1", "status": "busy", "lastRequestAt": now},
		map[string]any{"session": "person", "sessionId": "p1", "status": "idle", "ask": map[string]any{"header": "Screens", "count": 2}},
		map[string]any{"session": "stray", "sessionId": "s1", "status": "idle"},
	}
	recent := []any{
		map[string]any{"sessionId": "a1", "name": "aacpanel", "project": map[string]any{"id": 1, "name": "aacpanel"}, "lastAt": now},
		map[string]any{"sessionId": "a0", "name": "aacpanel", "project": map[string]any{"id": 1, "name": "aacpanel"}, "lastAt": now},
		map[string]any{"sessionId": "x0", "name": "proxy", "project": map[string]any{"id": 2, "name": "proxy", "group": "Infra"}, "lastAt": now},
	}
	for i := 0; i < 8; i++ {
		recent = append(recent, map[string]any{"sessionId": "q" + string(rune('a'+i)), "name": "old", "cwd": "/p/old" + string(rune('a'+i)), "lastAt": now})
	}
	got := runModuleJS(t, "src/screens/sessions/blocks.js", "blocksOf", [][]any{
		{map[string]any{"profile": profile, "sessions": sessions, "recent": recent}},
	})[0].([]any)

	var order []string
	past := map[string]string{}
	quiet := 0
	for _, raw := range got {
		b := raw.(map[string]any)
		order = append(order, b["name"].(string))
		if p, ok := b["past"].(map[string]any); ok {
			past[b["name"].(string)] = p["sessionId"].(string)
		}
		if len(b["live"].([]any)) == 0 {
			quiet++
		}
	}
	if strings.Join(order[:3], ",") != "person,aacpanel,stray" {
		t.Errorf("the list reads %v — the project that asks comes first, then the map and the sessions outside it", order)
	}
	if p, ok := past["aacpanel"]; ok {
		t.Errorf("aacpanel has a live session and still shows its past %q — a closed conversation beside it reads as the session", p)
	}
	if past["proxy"] != "x0" {
		t.Errorf("a project with nothing live lost its last conversation: %v", past)
	}
	if quiet != 5 {
		t.Errorf("%d projects with nothing live are on the list, expected the page's five", quiet)
	}
}

// An archived conversation is named by the last thing the person said in it
// that says something: not a word or two, not a slash command, and not the
// message the project opens with.
func TestAConversationIsNamedByTheLastWordsThatSaySomething(t *testing.T) {
	project := map[string]any{"line": map[string]any{"words": []any{
		map[string]any{"text": "claude"}, map[string]any{"text": "Summary of the day", "key": "intent"},
	}}}
	row := func(prompts ...string) map[string]any {
		out := make([]any, len(prompts))
		for i, p := range prompts {
			out[i] = p
		}
		return map[string]any{"prompts": out}
	}
	got := runModuleJS(t, "src/screens/sessions/blocks.js", "aboutOf", [][]any{
		{row("reshoot the landing scenes", "Continue", "/compact"), project},
		{row("reshoot the landing scenes", "Summary of the day"), project},
		{row("Yes", "ok"), project},
		{map[string]any{}, nil},
	})
	want := []string{"reshoot the landing scenes", "reshoot the landing scenes", "", ""}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("case %d: the conversation is named %q, expected %q", i, got[i], w)
		}
	}
}

// An idle session with something of its own still running — an agent, a
// background command, a workflow, an observer of the panel — is at work in
// its background, not quiet; a timer runs nothing, and a session with only a
// wake of its own or an alarm of the panel ahead is idle. A question and a
// permission still come first, and an answer at work stays busy. The project
// of a session at work in its background keeps its place in the map beside
// a quiet one that asked later.
func TestAnIdleSessionWithWorkRunningIsAtWorkInItsBackground(t *testing.T) {
	row := func(status string, work map[string]any, more ...any) map[string]any {
		out := map[string]any{"session": "s", "status": status, "work": work, "lastRequestAt": "2026-09-27T09:00:00Z"}
		for i := 0; i+1 < len(more); i += 2 {
			out[more[i].(string)] = more[i+1]
		}
		return out
	}
	work := func(kv ...any) map[string]any {
		out := map[string]any{"tasks": 0, "agents": 0}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	cases := []struct {
		name, tone, text string
		row              map[string]any
	}{
		{"an agent", "bg", "background", row("idle", work("agents", 1))},
		{"an observer of the panel", "bg", "background", row("idle", work("observers", 1))},
		{"a workflow", "bg", "background", row("idle", work("workflows", 1))},
		{"a background command beside a wake", "bg", "background", row("idle", work("tasks", 2, "wakes", 1))},
		{"only a wake of its own", "idle", "idle", row("idle", work("tasks", 1, "wakes", 1))},
		{"only an alarm of the panel", "idle", "idle", row("idle", work("alarms", 1))},
		{"a question beside an agent", "wait", "", row("idle", work("agents", 1), "ask", map[string]any{"header": "Screens", "count": 1})},
		{"a permission beside an agent", "wait", "", row("waiting", work("agents", 1))},
		{"an answer beside an agent", "busy", "working", row("busy", work("agents", 1))},
	}
	calls := make([][]any, len(cases))
	for i, c := range cases {
		calls[i] = []any{c.row}
	}
	got := runModuleJS(t, "src/screens/sessions/blocks.js", "stateOf", calls)
	for i, c := range cases {
		st := got[i].(map[string]any)
		if st["tone"] != c.tone || (c.text != "" && st["text"] != c.text) {
			t.Errorf("%s: the row reads %v %q, expected %s %q", c.name, st["tone"], st["text"], c.tone, c.text)
		}
	}
	if st := got[0].(map[string]any); st["work"] != "1 agent" || !strings.Contains(st["since"].(string), "1 agent") {
		t.Errorf("a row at work in its background lost the line of what runs: work %q, since %q", st["work"], st["since"])
	}

	profile := map[string]any{"groups": []any{
		map[string]any{"name": "Infra", "projects": []any{
			map[string]any{"id": 1, "name": "quiet", "session": "quiet", "path": "/p/quiet"},
			map[string]any{"id": 2, "name": "runs", "session": "runs", "path": "/p/runs"},
		}},
	}}
	sessions := []any{
		map[string]any{"session": "quiet", "sessionId": "q1", "status": "idle", "work": work(), "lastRequestAt": "2026-09-27T10:00:00Z"},
		map[string]any{"session": "runs", "sessionId": "r1", "status": "idle", "work": work("agents", 1), "lastRequestAt": "2026-09-27T09:00:00Z"},
	}
	blocks := runModuleJS(t, "src/screens/sessions/blocks.js", "blocksOf", [][]any{
		{map[string]any{"profile": profile, "sessions": sessions}},
	})[0].([]any)
	var order []string
	rank := map[string]float64{}
	for _, raw := range blocks {
		b := raw.(map[string]any)
		order = append(order, b["name"].(string))
		rank[b["name"].(string)] = b["rank"].(float64)
	}
	if strings.Join(order, ",") != "quiet,runs" || rank["runs"] != 1 || rank["quiet"] != 2 {
		t.Errorf("the list reads %v with ranks %v — background work is said by the row while the projects keep the map's order", order, rank)
	}
}

// The head of the conversation says background work by the same count as the
// row: an idle session with an agent at work is at work in its background and
// says what runs; one with only an alarm ahead is idle.
func TestTheHeadSaysBackgroundWorkAsTheRowDoes(t *testing.T) {
	got := runModuleJS(t, "src/screens/chat/head.js", "stateOf", [][]any{
		{map[string]any{"session": "s", "status": "idle", "work": map[string]any{"tasks": 0, "agents": 1}}},
		{map[string]any{"session": "s", "status": "idle", "work": map[string]any{"tasks": 0, "agents": 0, "alarms": 1}}},
	})
	bg := got[0].(map[string]any)
	if bg["tone"] != "bg" || bg["word"] != "background" || !strings.Contains(bg["say"].(string), "1 agent") {
		t.Errorf("an idle session with an agent at work reads %v %q (%q) in its head, expected bg \"background\" naming the agent",
			bg["tone"], bg["word"], bg["say"])
	}
	if idle := got[1].(map[string]any); idle["tone"] != "idle" || idle["word"] != "idle" {
		t.Errorf("a session with only an alarm ahead reads %v %q in its head, expected idle", idle["tone"], idle["word"])
	}
}
