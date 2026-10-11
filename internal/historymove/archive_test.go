package historymove

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// The collector's real archive reader is the contract: copying a transcript
// to a plausible directory is not proof that the new contour can resume it.
func TestMovedConversationsAppearInTheDestinationArchive(t *testing.T) {
	project, from, to, files := fixture(t)
	put(t, filepath.Join(from, files[0]), `{"type":"user","cwd":"/srv/proj/shop","timestamp":"2026-01-01T00:00:00Z","message":{"content":"Continue the shop"}}`+"\n")
	m, err := Prepare("archive", project, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Finish("archive", project, from, to, true); err != nil {
		t.Fatal(err)
	}
	agent, err := filepath.Abs("../../agent")
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	t.Setenv("PYTHONPATH", agent)
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	t.Setenv("HOME", state)
	t.Setenv("AACP_STATE_DIR", state)
	t.Setenv("AACP_SESSIONS_INDEX", filepath.Join(state, "index.json"))
	t.Setenv("AACP_CLAUDE_HOME", from+":"+to)
	t.Setenv("AACP_CLAUDE_REGISTRY", "")
	t.Setenv("AACP_CLAUDE_PROJECTS", "")
	t.Setenv("AACP_CODEX_HOMES", filepath.Join(state, ".codex"))
	cmd := exec.CommandContext(t.Context(), "python3", "-c", `import archive,json,sys
idx=archive.Index()
print(json.dumps({"old":idx.page(profile=sys.argv[1],under=sys.argv[3]),"new":idx.page(profile=sys.argv[2],under=sys.argv[3])}))`, from, to, project)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("archive reader: %v: %s", err, raw)
	}
	var got struct {
		Old, New struct {
			Rows []struct {
				SessionID string `json:"sessionId"`
				Profile   string `json:"profile"`
			}
		}
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if len(got.Old.Rows) != 0 || len(got.New.Rows) != 1 || got.New.Rows[0].SessionID != sid || got.New.Rows[0].Profile != "new" {
		t.Fatalf("archive after move: %s", raw)
	}
}
