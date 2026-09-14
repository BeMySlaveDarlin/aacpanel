package check

import (
	"strings"
	"testing"
)

// A shell command sent from the phone with its "!" comes back in the transcript
// as the command alone, in a row of its own kind. The local row waiting for it
// has to recognise it, or "queued" hangs under a command the console already ran.
func TestASentShellCommandSettlesItsQueuedRow(t *testing.T) {
	got := runModuleJS(t, "src/screens/chat/feed.js", "sameShell", [][]any{
		{"git status", map[string]any{"text": "! git status"}},
		{"git status", map[string]any{"text": "!git status"}},
		{"git status", map[string]any{"sent": "! git status", "text": "[Image #1] ! git status"}},
		{"git status", map[string]any{"text": "git status"}},
		{"git log", map[string]any{"text": "! git status"}},
		{"", map[string]any{"text": "!"}},
	})
	want := []bool{true, true, true, true, false, false}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("sameShell case %d = %v, expected %v", i, got, want[i])
		}
	}

	screen := screenSrc(t, "src/screens/chat.js")
	settle := jsBlock(t, "src/screens/chat/feed.js", screen, "export function arrived(items, local)")
	if !strings.Contains(settle, "sameShell(") {
		t.Error("the screen settles a queued row only against messages — a shell command the console ran leaves its row queued")
	}
}
