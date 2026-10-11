package executor

import (
	"os"
	"testing"
	"time"
)

// The fakes the tests drive answer at once or never, so a tenth of every wait
// is as long as any of them needs, and a test of a wait running out does not
// sit out the whole of it. The pace the executor keeps with a live screen —
// a pause between two keys, between two looks — is a tenth too: a fake takes
// keys at any pace, and a test that types or looks a hundred times would
// otherwise sit out a pace measured for a person's screen.
func TestMain(m *testing.M) {
	for _, wait := range []*time.Duration{
		&arriveWait, &fieldWait, &freeWait, &seenTimeout, &sendWait, &workOpenWait,
		&sendTimeout, &softWait, &pollEvery, &windowWait, &tmuxTimeout, &remoteWait, &remoteStepWait,
		&clientGone, &keyPause, &fieldPoll, &transcriptLookup, &pastePoll, &typePause, &workStepWait,
		&upPoll, &windowPoll,
	} {
		*wait /= 10
	}
	os.Exit(m.Run())
}
