package check

import "testing"

// A restarted session comes back under the same name, so the wait after the
// press cannot be cleared by the name alone: the old row would clear it at
// once, and a wait for a new name would never clear at all. It clears when the
// session under that name is a different run, and neither the gap while the
// old one is gone nor a stale snapshot clears it.
func TestRestartWaitClearsOnANewRunOfTheSameName(t *testing.T) {
	task := func(seen int) map[string]any {
		return map[string]any{
			"kind": "restart", "target": "host", "before": []string{"host", "shop"},
			"seen": seen, "was": "old-id|2025-01-01T10:00:00Z",
		}
	}
	names := func(list ...string) []any {
		out := make([]any, 0, len(list))
		for _, n := range list {
			out = append(out, n)
		}
		return out
	}

	cases := []struct {
		name string
		args []any
		want bool
	}{
		{
			"the same run is still there — not yet",
			[]any{task(0), names("host", "shop"), 0, map[string]any{"host": "old-id|2025-01-01T10:00:00Z"}},
			false,
		},
		{
			"the old one is gone and the new one is not up — not yet",
			[]any{task(0), names("shop"), 0, map[string]any{}},
			false,
		},
		{
			"a new conversation under the same name — done",
			[]any{task(0), names("host", "shop"), 0, map[string]any{"host": "new-id|2025-01-01T10:05:00Z"}},
			true,
		},
		{
			"the same conversation id, but a new start time — done",
			[]any{task(0), names("host"), 0, map[string]any{"host": "old-id|2025-01-01T10:05:00Z"}},
			true,
		},
		{
			"the snapshot is older than the press — not yet, whatever it says",
			[]any{task(1000), names("host"), 995, map[string]any{"host": "new-id|2025-01-01T10:05:00Z"}},
			false,
		},
		{
			"a switched session under the same conversation, now on the stream — done",
			[]any{map[string]any{"kind": "switch", "target": "host", "before": []string{"host"}, "seen": 0,
				"was": "old-id|2025-01-01T10:00:00Z|"},
				names("host"), 0, map[string]any{"host": "old-id|2025-01-01T10:00:00Z|stream"}},
			true,
		},
		{
			"a switched session still where it was — not yet",
			[]any{map[string]any{"kind": "switch", "target": "host", "before": []string{"host"}, "seen": 0,
				"was": "old-id|2025-01-01T10:00:00Z|"},
				names("host"), 0, map[string]any{"host": "old-id|2025-01-01T10:00:00Z|"}},
			false,
		},
		{
			"a close wait is untouched by the identities",
			[]any{map[string]any{"kind": "close", "target": "host", "before": []string{"host"}, "seen": 0},
				names("shop"), 0, map[string]any{"host": "new-id|x"}},
			true,
		},
	}

	calls := make([][]any, 0, len(cases))
	for _, c := range cases {
		calls = append(calls, c.args)
	}
	got := runModuleJS(t, "src/catchup.js", "settled", calls)
	for i, c := range cases {
		if done, _ := got[i].(bool); done != c.want {
			t.Errorf("%s: settled=%v, expected %v", c.name, done, c.want)
		}
	}
}
