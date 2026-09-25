package executor

import "time"

// How long the executor gives a screen, a session or the collector to answer
// before it says they did not. They are variables for the tests alone: a test
// of that saying would otherwise sit each of them out, and the fakes the tests
// drive answer at once or never.
var (
	arriveWait   = 3 * time.Second
	fieldWait    = 3 * time.Second
	freeWait     = 2 * time.Second
	seenTimeout  = time.Second
	sendWait     = 5 * time.Second
	workOpenWait = 3 * time.Second
)
