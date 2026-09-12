package watchcfg

import (
	"os"
	"testing"

	"aacpanel/internal/testdb"
)

func TestMain(m *testing.M) {
	os.Exit(testdb.Main(m))
}
