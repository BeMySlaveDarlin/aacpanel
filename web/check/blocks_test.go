package check

import (
	"strings"
	"testing"
)

// The sessions of a contour are laid out by project, and the list is read in
// the order of what needs the person: a project whose session asks stands
// above one that works, and that above the quiet ones. A project with a live
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
		t.Errorf("the list reads %v — the project that asks comes first, the working one next, then the quiet", order)
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

// A session on the stream running a claude the stream contract did not pass
// on says so on its row; a console session, and one on the checked version,
// say nothing.
func TestAStreamSessionOnAnUncheckedClaudeSaysSo(t *testing.T) {
	stream := func(v string) map[string]any { return map[string]any{"transport": "stream", "version": v} }
	got := runModuleJS(t, "src/screens/sessions/blocks.js", "unchecked", [][]any{
		{stream("2.1.290"), "2.1.283"},
		{stream("2.1.283"), "2.1.283"},
		{stream("2.1.290"), ""},
		{map[string]any{"version": "2.1.290"}, "2.1.283"},
		{stream(""), "2.1.283"},
	})
	want := []string{"claude 2.1.290 is not checked for the feed", "", "claude 2.1.290 is not checked for the feed", "", ""}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("case %d: the row says %q, expected %q", i, got[i], w)
		}
	}
}
