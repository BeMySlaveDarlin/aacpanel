package hook

import (
	"os"
	"testing"
)

// A session under a stream holder carries HeldEnv with an empty PIDEnv, and
// Run stays silent there by design. The tests start from no flags and set
// them themselves where the case is about them.
func TestMain(m *testing.M) {
	os.Unsetenv(HeldEnv)
	os.Unsetenv(PIDEnv)
	os.Exit(m.Run())
}
