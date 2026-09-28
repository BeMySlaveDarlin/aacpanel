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
		run(95, done(96, "agent4")),
		map[string]any{"role": "mail", "from": "agent1", "source": "agent", "text": "report", "pos": 10},
		run(11),
		done(12, "agent3"),
		map[string]any{"role": "turn", "pos": 13, "ms": 1000},
	}
	got := runModuleJS(t, "src/screens/chat/timeline.js", "book", [][]any{{rows}})
	want := "me mind mind~ ai tasks[tt] tasks[t] mail(Port the feed) tasks[t] | t@1 t@2 t@4 t@5 t@7 t@8!"
	if s := roles(t, got[0]); strings.TrimSpace(s) != want {
		t.Errorf("the feed splits into\n  %s\nwant\n  %s", s, want)
	}
}

// Every work stands on the strip as a stack of its own, a badge for each kind
// of what it did — its thinking, its calls kind by kind in the order of the
// labels, the end of its turn — and works close together are never summed
// into one: a stack that would run into the one above stands under it, the gap
// between them, and a stack is as tall as its badges and the gaps between.
func TestEveryWorkIsAStackOfItsOwnOnTheStrip(t *testing.T) {
	run := func(at int, kinds ...string) map[string]any {
		groups := []any{}
		for i, k := range kinds {
			groups = append(groups, map[string]any{"kind": k, "calls": []any{
				map[string]any{"name": k, "pos": at, "index": i}, map[string]any{"name": k, "pos": at, "index": i + 10}}})
		}
		return map[string]any{"role": "toolrow", "run": at, "groups": groups}
	}
	thought := func(r map[string]any) map[string]any {
		r["think"] = map[string]any{"count": 2}
		return r
	}
	mark := func(at int, rows ...any) map[string]any { return map[string]any{"at": at, "rows": rows} }
	geo := map[string]any{"tops": []int{0, 37, 75, 200}, "bottoms": []int{30, 70, 190, 230}}
	got := runModuleJS(t, "src/screens/chat/timeline.js", "railStacks", [][]any{{[]any{
		mark(0, thought(run(0, "bash"))),
		mark(1, run(1, "bash")),
		mark(2, run(2, "files", "bash")),
		mark(3, run(3, "bash"), map[string]any{"role": "turn", "pos": 9, "calls": 3}),
	}, geo}})
	raw, _ := json.Marshal(got[0])
	var stacks []struct {
		Y      float64 `json:"y"`
		H      float64 `json:"h"`
		Badges []struct {
			Kind  string `json:"kind"`
			Count int    `json:"count"`
		} `json:"badges"`
	}
	if err := json.Unmarshal(raw, &stacks); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, s := range stacks {
		b.WriteString(itoa(int(s.Y)) + "+" + itoa(int(s.H)) + ":")
		for i, x := range s.Badges {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(x.Kind + itoa(x.Count))
		}
		b.WriteString(" ")
	}
	want := "0+56:think2,bash2 64+26:bash2 98+56:bash2,files2 200+56:bash2,turn3"
	if s := strings.TrimSpace(b.String()); s != want {
		t.Errorf("the works at 0, 37, 75 and 200px lie on the strip as\n  %s\nwant\n  %s", s, want)
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
