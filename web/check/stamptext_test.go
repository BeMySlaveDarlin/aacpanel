package check

import (
	"testing"
	"time"
)

// When a message was written is said in numbers, the day before the month,
// the way the charts and the usage card write a date: no month name, which
// would come in the language of the locale rather than the one of the screen.
// A step of the checklist taken today keeps only the time.
func TestTheFeedStampsAMessageInNumbers(t *testing.T) {
	t.Setenv("TZ", "UTC")
	today := time.Now().UTC().Format(time.RFC3339)

	cases := []struct {
		module, fn, iso, want string
	}{
		{"src/screens/chat/labels.js", "stampText", "2026-09-28T10:25:00Z", "28.09 · 10:25"},
		{"src/screens/chat/labels.js", "stampText", "2026-01-05T08:07:00Z", "05.01 · 08:07"},
		{"src/screens/chat/labels.js", "stampText", "", ""},
		{"src/screens/chat/checklist.js", "clockOf", "2026-09-28T10:25:00Z", "28.09 · 10:25"},
		{"src/screens/chat/checklist.js", "clockOf", today, today[11:16]},
	}
	for _, c := range cases {
		got := runModuleJS(t, c.module, c.fn, [][]any{{c.iso}})[0]
		if got != c.want {
			t.Errorf("%s(%q) reads %q, expected %q", c.fn, c.iso, got, c.want)
		}
	}
}
