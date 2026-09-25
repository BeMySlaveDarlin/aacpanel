package executor

import (
	"os"
	"testing"
	"time"
)

// The fakes the tests drive answer at once or never, so a tenth of every wait
// is as long as any of them needs, and a test of a wait running out does not
// sit out the whole of it.
func TestMain(m *testing.M) {
	for _, wait := range []*time.Duration{
		&arriveWait, &fieldWait, &freeWait, &seenTimeout, &sendWait, &workOpenWait,
		&sendTimeout, &softWait, &pollEvery, &windowWait, &tmuxTimeout,
	} {
		*wait /= 10
	}
	os.Exit(m.Run())
}
