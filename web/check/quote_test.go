package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuoteOfTrimsToTwoLinesAndPrefixes(t *testing.T) {
	long := strings.Repeat("x", 250)
	cases := []struct {
		name, in, want string
	}{
		{"a single line", "no, this part is wrong", "> no, this part is wrong"},
		{"two lines in full", "first\nsecond", "> first\n> second"},
		{"the third line turns into an ellipsis", "first\nsecond\nthird", "> first\n> second…"},
		{"blank lines and the indent of the selection are dropped", "  first  \n\n\n  second\n", "> first\n> second"},
		{"a long line is cut by characters", long, "> " + strings.Repeat("x", 240) + "…"},
		{"the second line does not fit — the ellipsis goes to the first", strings.Repeat("x", 240) + "\nsecond", "> " + strings.Repeat("x", 240) + "…"},
		{"empty in, empty out", "  \n ", ""},
	}
	var calls [][]string
	for _, c := range cases {
		calls = append(calls, []string{c.in})
	}
	got := runQuoteJS(t, "quoteOf", calls)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: quoteOf(%q) = %q, expected %q", c.name, c.in, got[i], c.want)
		}
	}
}

func TestWithQuoteKeepsDraftBelowQuote(t *testing.T) {
	got := runQuoteJS(t, "withQuote", [][]string{
		{"> quote", "already typed"},
		{"> quote", ""},
		{"> quote", "  \n indented"},
	})
	want := []string{"> quote\n\nalready typed", "> quote\n\n", "> quote\n\nindented"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("withQuote #%d = %q, expected %q", i, got[i], want[i])
		}
	}
}

func runQuoteJS(t *testing.T, fn string, calls [][]string) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the quote logic is run by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "quote.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
import * as quote from ` + jsString("file://"+path) + `;
const calls = JSON.parse(readFileSync(0, "utf8"));
const fn = quote[` + jsString(fn) + `];
process.stdout.write(JSON.stringify(calls.map((args) => fn(...args))));
`
	raw, err := json.Marshal(calls)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(calls) {
		t.Fatalf("%d answers to %d calls", len(got), len(calls))
	}
	return got
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
