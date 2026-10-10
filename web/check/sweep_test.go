package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A run sweeps the Chrome profiles that runs before it left behind, and keeps
// the ones a live process still names — a run beside it is at work in them —
// and anything that is not a profile of these fixtures.
func TestARunSweepsTheProfilesOfRunsBeforeIt(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, profilePrefix+"left")
	held := filepath.Join(root, profilePrefix+"held")
	other := filepath.Join(root, "someone-else")
	for _, dir := range []string{left, held, other} {
		if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A process that names the held profile the way Chrome does, by its flag.
	holder := exec.Command("sh", "-c", "sleep 30", "--user-data-dir="+held)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill()
		_ = holder.Wait()
	})

	sweepProfiles(root)

	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Errorf("the profile a run left behind is still there: %v", err)
	}
	if _, err := os.Stat(held); err != nil {
		t.Errorf("the profile a live process names was swept: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a directory that is no profile of the fixtures was swept: %v", err)
	}
}
