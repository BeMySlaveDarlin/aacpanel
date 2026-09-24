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

// The rules of permissions are not sent from the panel in any form: the host
// refuses them, and the composer does not let them go and says why — as a
// command, and as a message that only starts with one.
func TestPermissionsIsOffInTheComposer(t *testing.T) {
	parsed := runModuleJS(t, "src/actions/registry.js", "parseCommand", [][]any{
		{"/permissions", true}, {"/permissions", false}, {"/Permissions allow Bash", false}, {"/allowed-tools", false},
		{"tell me about /permissions", false},
	})
	for i, v := range parsed[:4] {
		m, _ := v.(map[string]any)
		if m == nil || m["ready"] != false || m["refused"] != true {
			t.Errorf("case %d: %v can be sent", i, v)
		}
	}
	if parsed[4] != nil {
		t.Errorf("a message that only mentions the command is taken for it: %v", parsed[4])
	}
	hints := runModuleJS(t, "src/actions/registry.js", "commandHints", [][]any{{"/permissions", false}, {"/p", false}})
	text := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	if !strings.Contains(text(hints[0]), "not sent from the panel") {
		t.Errorf("the refusal is not explained: %s", text(hints[0]))
	}
	if strings.Contains(text(hints[1]), "/permissions") {
		t.Errorf("the composer offers /permissions among the commands: %s", text(hints[1]))
	}
}
