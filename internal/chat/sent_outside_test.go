package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

// The collector marks a sent file that lies outside the directory of the
// conversation, and the mark has to reach the screen: without it the list
// offers to open a file the reader will refuse. A file inside the directory
// carries no mark at all, so the screen does not have to tell false from
// absent.
func TestASentFileOutsideTheDirectoryKeepsItsMark(t *testing.T) {
	const fromAgent = `{"sent": [
		{"path": "/srv/proj/cv/resume.pdf", "file": "resume.pdf", "size": 10, "count": 1},
		{"path": "/home/u/.cache/scratch/resume.pdf", "file": "resume.pdf", "size": 10, "count": 1,
		 "outside": true}
	]}`
	var state Work
	if err := json.Unmarshal([]byte(fromAgent), &state); err != nil {
		t.Fatalf("the agent answer does not parse: %v", err)
	}
	if len(state.Sent) != 2 {
		t.Fatalf("the sent files are lost: %+v", state)
	}
	if state.Sent[0].Outside {
		t.Errorf("a file inside the directory arrived marked as outside: %+v", state.Sent[0])
	}
	if !state.Sent[1].Outside {
		t.Errorf("the mark of a file outside the directory is dropped between the collector "+
			"and the screen: %+v", state.Sent[1])
	}

	out, err := json.Marshal(state.Sent)
	if err != nil {
		t.Fatalf("the sent files do not marshal: %v", err)
	}
	if n := strings.Count(string(out), `"outside":true`); n != 1 {
		t.Errorf("the mark reaches the screen %d times, want once — a file inside the directory "+
			"must carry no key: %s", n, out)
	}
}
