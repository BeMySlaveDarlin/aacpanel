package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTheCommandLine(t *testing.T) {
	never := func() bool {
		t.Fatal("the demo reached the terminal on a command line that should stop before it")
		return false
	}
	noTerminal := func() bool { return false }
	for _, c := range []struct {
		args     []string
		terminal func() bool
		status   int
		says     string
	}{
		{nil, never, 2, "usage: aacpanel-install demo"},
		{[]string{"-h"}, never, 0, "usage: aacpanel-install demo"},
		{[]string{"install"}, never, 2, `unknown command "install"`},
		{[]string{"demo", "-h"}, never, 0, "-speed"},
		{[]string{"demo", "--speed", "0"}, never, 2, "--speed takes a number above zero"},
		{[]string{"demo", "--fail", "tailscale"}, never, 2, `--fail takes compose, not "tailscale"`},
		{[]string{"demo", "extra"}, never, 2, `unexpected "extra"`},
		{[]string{"demo", "--speed", "3", "--fail", "compose"}, noTerminal, 1, "needs a terminal"},
	} {
		var stdout, stderr bytes.Buffer
		status := run(c.args, &stdout, &stderr, c.terminal)
		said := stdout.String() + stderr.String()
		if status != c.status || !strings.Contains(said, c.says) {
			t.Errorf("%q: status %d, said:\n%s\nwant %d and %q", c.args, status, said, c.status, c.says)
		}
	}
}
