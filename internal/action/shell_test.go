package action

import (
	"strings"
	"testing"
)

// A shell command is the words after "!" and nothing else: it may name its
// run for the feed, and a line of control characters is no command.
func TestAShellCommandIsWordsAndMayNameItsRun(t *testing.T) {
	const id = "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"
	cases := []struct {
		name string
		req  Request
		ok   bool
	}{
		{"a command", Request{ID: "1", Kind: SessionShell, Target: "a", Text: "git status"}, true},
		{"a command that names its run", Request{ID: "1", Kind: SessionShell, Target: "a", Text: "ls -la", MessageID: id}, true},
		{"a command over lines", Request{ID: "1", Kind: SessionShell, Target: "a", Text: "for f in a b; do\n\techo $f\ndone"}, true},
		{"a command that starts with a slash", Request{ID: "1", Kind: SessionShell, Target: "a", Text: "/usr/bin/env"}, true},
		{"no command", Request{ID: "1", Kind: SessionShell, Target: "a", Text: "   "}, false},
		{"a control character", Request{ID: "1", Kind: SessionShell, Target: "a", Text: "ls\x1b[2J"}, false},
		{"a run id that is not a uuid", Request{ID: "1", Kind: SessionShell, Target: "a", Text: "ls", MessageID: "$(rm)"}, false},
		{"a command past the ceiling", Request{ID: "1", Kind: SessionShell, Target: "a", Text: strings.Repeat("x", TextMax+1)}, false},
		{"a command riding another action", Request{ID: "1", Kind: SessionStop, Target: "a", Text: "ls"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if c.ok && err != nil {
				t.Errorf("a sound request is rejected: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("the request is accepted, though it must not be")
			}
		})
	}
	if !Valid(SessionShell) {
		t.Fatal("session.shell is missing from Kinds — the executor would reject it as unknown")
	}
}
