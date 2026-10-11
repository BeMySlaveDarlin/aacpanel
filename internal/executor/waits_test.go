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
//
// The waits for the text to show on the screen and for Enter to take are left
// whole. The fake terminal of konsole is a python process a call, and a send
// that goes through looks at the screen and presses Enter there several times
// over: a tenth of either wait is a few of those calls on a loaded runner. A
// whole wait costs a send that goes through nothing, and a test that waits
// one out takes a short one with quickWaits.
func TestMain(m *testing.M) {
	for _, wait := range []*time.Duration{
		&fieldWait, &freeWait, &seenTimeout, &workOpenWait,
		&sendTimeout, &softWait, &pollEvery, &windowWait, &tmuxTimeout, &remoteWait, &remoteStepWait,
		&clientGone, &keyPause, &fieldPoll, &transcriptLookup, &pastePoll, &typePause, &workStepWait,
		&upPoll, &windowPoll,
	} {
		*wait /= 10
	}
	os.Exit(m.Run())
}

// quickWaits cuts the waits TestMain leaves whole to a tenth for a test that
// waits them out: a screen that never shows the text, a fake that never lets
// it go or lets it go only past the hundredth Enter, which a whole wait would
// reach.
func quickWaits(t *testing.T, waits ...*time.Duration) {
	t.Helper()
	for _, wait := range waits {
		prev := *wait
		*wait = prev / 10
		t.Cleanup(func() { *wait = prev })
	}
}
