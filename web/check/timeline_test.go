package check

import (
	"encoding/json"
	"strings"
	"testing"
)

func roles(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Prose []struct {
			Role    string           `json:"role"`
			Head    *bool            `json:"head"`
			WhoName string           `json:"whoName"`
			List    []map[string]any `json:"list"`
		} `json:"prose"`
		Marks []struct {
			At   int              `json:"at"`
			End  bool             `json:"end"`
			Rows []map[string]any `json:"rows"`
		} `json:"marks"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, p := range got.Prose {
		b.WriteString(p.Role)
		if p.Head != nil && !*p.Head {
			b.WriteString("~")
		}
		if p.WhoName != "" {
			b.WriteString("(" + p.WhoName + ")")
		}
		if len(p.List) > 0 {
			b.WriteString("[" + strings.Repeat("t", len(p.List)) + "]")
		}
		b.WriteString(" ")
	}
	b.WriteString("|")
	for _, m := range got.Marks {
		b.WriteString(" ")
		for _, r := range m.Rows {
			b.WriteString(r["role"].(string)[:1])
		}
		b.WriteString("@")
		b.WriteString(itoa(m.At))
		if m.End {
			b.WriteString("!")
		}
	}
	return b.String()
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

// The feed splits into the column and the timeline: a run of calls and the
// end of a turn leave the column and stand as a mark at the row that follows
// them; tasks that ended side by side share a card, but not across work; a
// thought after a thought carries no word of its own; a letter of a subagent
// is signed with the task it ran.
func TestTheFeedSplitsIntoTheColumnAndTheTimeline(t *testing.T) {
	at := "2026-09-26T10:00:00Z"
	run := func(pos int, lines ...map[string]any) map[string]any {
		r := map[string]any{"role": "toolrow", "run": pos, "pos": pos, "groups": []any{
			map[string]any{"role": "tools", "kind": "bash", "run": pos, "pos": pos,
				"calls": []any{map[string]any{"name": "Bash", "arg": "ls", "pos": pos, "index": 0}}}}}
		if len(lines) > 0 {
			l := []any{}
			for _, x := range lines {
				l = append(l, x)
			}
			r["lines"] = l
		}
		return r
	}
	done := func(pos int, task string) map[string]any {
		return map[string]any{"role": "taskdone", "pos": pos, "task": task, "status": "completed",
			"summary": `Agent "Port the feed" finished`, "tokens": 10, "at": at}
	}
	rows := []any{
		map[string]any{"role": "me", "text": "go", "pos": 1},
		run(2),
		map[string]any{"role": "mind", "text": "a", "pos": 3},
		run(4),
		map[string]any{"role": "mind", "text": "b", "pos": 5},
		map[string]any{"role": "ai", "text": "c", "pos": 6},
		run(7, done(8, "agent1")),
		done(9, "agent2"),
		map[string]any{"role": "mail", "from": "agent1", "source": "agent", "text": "report", "pos": 10},
		run(11),
		done(12, "agent3"),
		map[string]any{"role": "turn", "pos": 13, "ms": 1000},
	}
	got := runModuleJS(t, "src/screens/chat/timeline.js", "book", [][]any{{rows}})
	want := "me mind mind~ ai tasks[tt] mail(Port the feed) tasks[t] | t@1 t@2 t@4 t@6 t@7!"
	if s := roles(t, got[0]); strings.TrimSpace(s) != want {
		t.Errorf("the feed splits into\n  %s\nwant\n  %s", s, want)
	}
}

// Two marks on the strip become one when the lower would stand closer to the
// upper than a mark's height and the gap between marks, and not a pixel
// sooner: a mark at 37px under another joins it, one at 38px stands alone.
func TestMarksThatWouldRunIntoEachOtherBecomeOne(t *testing.T) {
	mark := func(at int) map[string]any {
		return map[string]any{"at": at, "rows": []any{map[string]any{"role": "toolrow", "run": at, "groups": []any{
			map[string]any{"kind": "bash", "calls": []any{map[string]any{"name": "Bash", "pos": at}, map[string]any{"name": "Bash", "pos": at, "index": 1}}}}}}}
	}
	geo := map[string]any{"tops": []int{0, 37, 75, 200}, "bottoms": []int{30, 70, 190, 230}}
	got := runModuleJS(t, "src/screens/chat/timeline.js", "railGroups", [][]any{
		{[]any{mark(0), mark(1), mark(2), mark(3)}, geo},
	})
	raw, _ := json.Marshal(got[0])
	var groups []struct {
		Y      float64 `json:"y"`
		Merged int     `json:"merged"`
		Sum    struct {
			Total int `json:"total"`
		} `json:"sum"`
	}
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, g := range groups {
		b.WriteString(itoa(int(g.Y)) + "x" + itoa(g.Merged) + "=" + itoa(g.Sum.Total) + " ")
	}
	if s := strings.TrimSpace(b.String()); s != "0x2=4 75x1=2 200x1=2" {
		t.Errorf("the marks at 0, 37, 75 and 200px lie on the strip as %s, want 0x2=4 75x1=2 200x1=2", s)
	}
}

// An entry of the desk column stands at the height of its row, or under the
// entry above it with the gap between them when that one is still in the way.
func TestAnEntryOfTheDeskColumnStandsUnderTheOneInItsWay(t *testing.T) {
	got := runModuleJS(t, "src/screens/chat/timeline.js", "stack", [][]any{
		{[]int{0, 10, 100, 104}, []int{50, 20, 10, 10}},
	})
	raw, _ := json.Marshal(got[0])
	if string(raw) != "[0,60,100,120]" {
		t.Errorf("entries at 0, 10, 100 and 104px, 50, 20, 10 and 10px tall, stand at %s, want [0,60,100,120]", raw)
	}
}

// An entry says a call in a few words: the command a shell ran, the file a file
// tool touched, the tool of a server with what it was given.
func TestAnEntrySaysACallInAFewWords(t *testing.T) {
	calls := [][]any{
		{map[string]any{"name": "Bash", "arg": "make check ⏎ echo done"}},
		{map[string]any{"name": "Edit", "arg": "/srv/proj/web/src/md.js"}},
		{map[string]any{"name": "docker: compose_up", "arg": ""}},
		{map[string]any{"name": "Agent", "arg": "Port the feed"}},
	}
	got := runModuleJS(t, "src/screens/chat/timeline.js", "callLabel", calls)
	want := []string{"make check", "md.js", "compose_up", "Agent Port the feed"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d reads %q, want %q", i, got[i], want[i])
		}
	}
}
