package install

import (
	"strings"
	"testing"
)

func TestPanelToolsInstallTaskContextInBothClients(t *testing.T) {
	wiring := Wiring{Exec: executor, Kit: []string{"tools"}}
	raw, _, err := Wire([]byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"my-start"}]}]}}`), wiring)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), executor+" hook task-context") || !strings.Contains(string(raw), "my-start") {
		t.Fatalf("Claude start hook: %s", raw)
	}
	hooks, config, _, err := WireCodex(nil, nil, "/home/u/.codex/hooks.json", wiring)
	if err != nil || !strings.Contains(string(hooks), executor+" hook task-context") || !strings.Contains(string(config), "session_start") {
		t.Fatalf("Codex start hook: %s\n%s\n%v", hooks, config, err)
	}
	wiring.Kit = nil
	raw, _, err = Wire(raw, wiring)
	if err != nil || strings.Contains(string(raw), "task-context") || !strings.Contains(string(raw), "my-start") {
		t.Fatalf("Claude removal: %s %v", raw, err)
	}
	hooks, _, _, err = WireCodex(hooks, config, "/home/u/.codex/hooks.json", wiring)
	if err != nil || strings.Contains(string(hooks), "task-context") {
		t.Fatalf("Codex removal: %s %v", hooks, err)
	}
}
