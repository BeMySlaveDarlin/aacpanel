package check

import (
	"encoding/json"
	"strings"
	"testing"
)

// On the stream a clear starts a conversation under a new id, and the session
// drops off the panel. The composer does not offer it there and does not let
// it go, and says why; in the console it stays as it was.
func TestClearIsOffInTheComposerOfAStreamSession(t *testing.T) {
	parsed := runModuleJS(t, "src/actions/registry.js", "parseCommand", [][]any{
		{"/clear", true}, {"/clear", false}, {"/model opus[1m]", true},
	})
	ready := func(v any) bool {
		m, _ := v.(map[string]any)
		r, _ := m["ready"].(bool)
		return r
	}
	if ready(parsed[0]) {
		t.Error("/clear is ready to go to a stream session")
	}
	if !ready(parsed[1]) {
		t.Error("/clear is off in the console as well")
	}
	if !ready(parsed[2]) {
		t.Error("/model is off on the stream: the model is chosen there by the same command")
	}

	hints := runModuleJS(t, "src/actions/registry.js", "commandHints", [][]any{
		{"/", true}, {"/", false}, {"/clear", true},
	})
	text := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	if strings.Contains(text(hints[0]), `"/clear"`) {
		t.Errorf("the stream composer offers /clear: %s", text(hints[0]))
	}
	if !strings.Contains(text(hints[1]), `"/clear"`) {
		t.Errorf("the console composer lost /clear: %s", text(hints[1]))
	}
	if !strings.Contains(text(hints[2]), "console only") {
		t.Errorf("a /clear typed on the stream is not explained: %s", text(hints[2]))
	}
}
