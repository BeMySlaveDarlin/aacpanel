package check

import "testing"

// The strip of recent conversations on the sessions screen shows cards, and a
// card names a talk. A transcript can hold nothing but a snapshot of file
// history or a summary, and a card for one names a conversation that never
// happened. The archive counts and pages every conversation it holds, so which
// of them are worth a card is the screen's own question: it asks for more rows
// than it shows and drops the silent ones itself.
func TestTheRecentStripDropsConversationsThatSaidNothing(t *testing.T) {
	rows := []any{
		map[string]any{"sessionId": "a", "messages": 4},
		map[string]any{"sessionId": "b", "messages": 0},
		map[string]any{"sessionId": "c"},
		map[string]any{"sessionId": "d", "messages": 1},
	}
	got := runModuleJS(t, "src/screens/sessions.js", "spoken", [][]any{
		{rows},
		{[]any{}},
		{nil},
	})

	ids := func(out any) string {
		var s string
		for _, row := range out.([]any) {
			s += row.(map[string]any)["sessionId"].(string)
		}
		return s
	}
	if want := "ad"; ids(got[0]) != want {
		t.Errorf("the strip keeps %q of the archive rows, expected %q: a conversation that said "+
			"nothing gets a card naming a talk that never happened", ids(got[0]), want)
	}
	if len(got[1].([]any)) != 0 {
		t.Errorf("an empty archive came back as %v", got[1])
	}
	if got[2] == nil {
		t.Error("an archive that has not answered yet came back as nothing rather than as no rows — the screen maps over it")
	} else if len(got[2].([]any)) != 0 {
		t.Errorf("an archive that has not answered yet came back as %v", got[2])
	}
}
