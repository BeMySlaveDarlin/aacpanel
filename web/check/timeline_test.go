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

// Every work stands on the strip as a stack of badges, a badge for each kind
// of what it did — its thinking, its calls kind by kind in the order of the
// labels, the end of its turns — at the row it led to, and a stack is as tall
// as its badges and the gaps between. A work whose stack would run into the
// stack above joins it: the stack above ends where the lowest of its works
// would end alone, or where their sum ends, whichever is further down, and
// the gap after it. The joined stack counts every kind of what its works
// did, the ends of their turns as one, and says so. A work whose row starts
// where the stack above and its gap end keeps a stack of its own, and no
// stack is pushed off its row.
func TestWorksCloseTogetherAreSummedIntoOneStackOnTheStrip(t *testing.T) {
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
	turn := func(pos, calls, ms, agents int) map[string]any {
		return map[string]any{"role": "turn", "pos": pos, "calls": calls, "ms": ms, "agents": agents}
	}
	mark := func(at int, rows ...any) map[string]any { return map[string]any{"at": at, "rows": rows} }
	tops := []int{0, 37, 71, 110, 150, 300, 320, 350, 400, 410, 420, 430, 470, 600, 620, 630, 760}
	bottoms := make([]int, len(tops))
	for i, y := range tops {
		bottoms[i] = y + 20
	}
	got := runModuleJS(t, "src/screens/chat/timeline.js", "railStacks", [][]any{{[]any{
		// The one at 37 runs into the stack at 0, 56px and the gap tall.
		mark(0, thought(run(0, "bash"))),
		mark(1, run(1, "bash")),
		// 71 is where the one at 37 would end alone, and the gap: room.
		mark(2, run(2, "files", "bash")),
		// A chain: each runs into the stack before it; their turns end as one.
		mark(3, run(3, "bash"), turn(90, 3, 1000, 0)),
		mark(4, run(4, "bash"), turn(91, 2, 2000, 1)),
		// Room again, then works of one kind: their sum is one badge, and
		// the one at 350 clears the end of the sum and its gap, but not
		// where the one at 320 would end alone and the gap.
		mark(5, run(5, "bash")),
		mark(6, run(6, "bash")),
		mark(7, run(7, "bash")),
		// Short works of five kinds: the fifth clears where each of the four
		// above would end alone, but not where their sum ends.
		mark(8, run(8, "web")),
		mark(9, run(9, "files")),
		mark(10, run(10, "agents")),
		mark(11, run(11, "mcp")),
		mark(12, run(12, "skill")),
		// Room again. A tall work comes second and a short one after it: the
		// one at 760 clears where the last of them would end alone and where
		// their sum ends, but not where the tall one would end alone.
		mark(13, run(13, "bash")),
		mark(14, run(14, "bash", "files", "web", "agents", "mcp")),
		mark(15, run(15, "bash")),
		mark(16, run(16, "skill")),
	}, map[string]any{"tops": tops, "bottoms": bottoms}}})
	raw, _ := json.Marshal(got[0])
	var stacks []struct {
		Y      float64 `json:"y"`
		H      float64 `json:"h"`
		Sum    any     `json:"sum"`
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
	want := "0+56:think2,bash4 71+86:bash6,files2,turn5 300+26:bash6 400+146:files2,web2,agents2,skill2,mcp2 600+176:bash6,files2,web2,agents2,skill2,mcp2"
	if s := strings.TrimSpace(b.String()); s != want {
		t.Errorf("the works at the rows %v lie on the strip as\n  %s\nwant\n  %s", tops, s, want)
	}
	if len(stacks) > 1 {
		words := runModuleJS(t, "src/screens/chat/timeline.js", "said", [][]any{{stacks[1].Sum}})
		if want := "6 commands · 2 files · 2 turns · worked 3s · 5 calls · 1 background agent was still at work"; words[0] != want {
			t.Errorf("the joined stack says %q, want %q", words[0], want)
		}
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
