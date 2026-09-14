package check

import (
	"strings"
	"testing"
)

// The in-and-out chip of the deck opens the usage screen: the figure is the
// way in to where it comes from, on both shells.
func TestTheDeckCounterOpensUsage(t *testing.T) {
	files := srcFiles(t)
	chat := funcBody(t, files["src/screens/chat.js"], "export function Chat(")
	if !strings.Contains(chat, `class="deckuse"`) || !strings.Contains(chat, "onClick=${onUsage}") {
		t.Error("the deck counter is not a way to the usage screen")
	}
	if !strings.Contains(files["src/mobile/shell.js"], `onUsage=${() => setPage("usage")}`) {
		t.Error("the phone shell does not hand the chat a way to the usage page")
	}
	if !strings.Contains(files["src/desktop/shell.js"], "onUsage=${() =>") || !strings.Contains(files["src/desktop/shell.js"], `goSection("home")`) {
		t.Error("the wide shell does not hand the chat a way to the usage widgets of the home section")
	}
	if !strings.Contains(files["src/screens/sessions.js"], "onUsage=${onUsage}") {
		t.Error("the sessions screen drops the usage callback on its way to the chat")
	}
}
