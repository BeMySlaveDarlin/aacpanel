package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

func TestSettledAnswersAboutOurOwnConsole(t *testing.T) {
	type call struct {
		Fn   string        `json:"fn"`
		Args []interface{} `json:"args"`
	}
	task := func(kind, target string, before ...string) map[string]interface{} {
		if before == nil {
			before = []string{}
		}
		return map[string]interface{}{"kind": kind, "target": target, "before": before}
	}

	cases := []struct {
		name string
		call call
		want bool
	}{
		{
			"close: the target is gone from the list",
			call{"settled", []interface{}{task("close", "kiosk"), []string{"aacpanel", "home"}}},
			true,
		},
		{
			"close: the target is still there",
			call{"settled", []interface{}{task("close", "kiosk"), []string{"kiosk", "home"}}},
			false,
		},
		{
			"open: the project name has appeared",
			call{"settled", []interface{}{task("open", "kiosk", "aacpanel"), []string{"aacpanel", "kiosk"}}},
			true,
		},
		{
			"open: a second launch with a suffix has appeared",
			call{"settled", []interface{}{task("open", "kiosk", "aacpanel"), []string{"aacpanel", "kiosk-2"}}},
			true,
		},
		{
			"open: someone else's session came up",
			call{"settled", []interface{}{task("open", "kiosk", "aacpanel"), []string{"aacpanel", "shop"}}},
			false,
		},
		{
			"open: the name was in the list before the tap",
			call{"settled", []interface{}{task("open", "kiosk", "aacpanel", "kiosk"), []string{"aacpanel", "kiosk"}}},
			false,
		},
		{"name: an exact match", call{"ownName", []interface{}{"aacpanel", "aacpanel"}}, true},
		{"name: a second launch", call{"ownName", []interface{}{"aacpanel-2", "aacpanel"}}, true},
		{"name: a neighbouring project with a shared prefix", call{"ownName", []interface{}{"aacpanel-exec", "aacpanel"}}, false},
		{"name: a suffix without a hyphen", call{"ownName", []interface{}{"aacpanel2", "aacpanel"}}, false},
		{"name: someone else's", call{"ownName", []interface{}{"shop", "aacpanel"}}, false},
		{"name: a dot is not a wildcard", call{"ownName", []interface{}{"demoxsite-2", "demo.site"}}, false},
		{"name: a dot matches itself", call{"ownName", []interface{}{"demo.site-3", "demo.site"}}, true},
	}

	calls := make([]call, 0, len(cases))
	for _, c := range cases {
		calls = append(calls, c.call)
	}
	got := runCatchUpJS(t, calls)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: %s = %v, expected %v", c.name, c.call.Fn, got[i], c.want)
		}
	}
}

func runCatchUpJS(t *testing.T, calls interface{}) []bool {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the wait condition is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "catchup.js"))
	if err != nil {
		t.Fatal(err)
	}
	built := esbuild.Build(esbuild.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Format:      esbuild.FormatESModule,
		Platform:    esbuild.PlatformNeutral,
		Alias:       alias,
		Write:       false,
	})
	if len(built.Errors) > 0 {
		t.Fatalf("catchup.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "catchup.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import * as catchup from ` + jsString("file://"+bundle) + `;
const calls = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(calls.map(({ fn, args }) => catchup[fn](...args))));
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
	var got []bool
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	return got
}

func TestStaleSnapshotDoesNotSettleAWait(t *testing.T) {
	type call struct {
		Fn   string        `json:"fn"`
		Args []interface{} `json:"args"`
	}
	task := func(kind string, seen int, before ...string) map[string]interface{} {
		if before == nil {
			before = []string{}
		}
		return map[string]interface{}{
			"kind": kind, "target": "kiosk", "before": before, "seen": seen,
		}
	}

	cases := []struct {
		name string
		call call
		want bool
	}{
		{
			"open: the namesake in the snapshot is older than the tap",
			call{"settled", []interface{}{
				task("open", 1000, "aacpanel"), []string{"aacpanel", "kiosk-2"}, 995,
			}},
			false,
		},
		{
			"open: the snapshot is exactly the one the tap was made on",
			call{"settled", []interface{}{
				task("open", 1000, "aacpanel"), []string{"aacpanel", "kiosk-2"}, 1000,
			}},
			false,
		},
		{
			"open: the snapshot was taken after the tap",
			call{"settled", []interface{}{
				task("open", 1000, "aacpanel"), []string{"aacpanel", "kiosk-2"}, 1005,
			}},
			true,
		},
		{
			"close: the target is missing from a snapshot older than the tap",
			call{"settled", []interface{}{
				task("close", 1000, "kiosk"), []string{"aacpanel"}, 990,
			}},
			false,
		},
		{
			"close: the target is missing from a snapshot after the tap",
			call{"settled", []interface{}{
				task("close", 1000, "kiosk"), []string{"aacpanel"}, 1010,
			}},
			true,
		},
		{
			"a snapshot without a timestamp is judged as before",
			call{"settled", []interface{}{
				task("open", 1000, "aacpanel"), []string{"aacpanel", "kiosk"}, 0,
			}},
			true,
		},
		{
			"the wait was started without a snapshot",
			call{"settled", []interface{}{
				task("open", 0, "aacpanel"), []string{"aacpanel", "kiosk"}, 995,
			}},
			true,
		},
	}

	calls := make([]call, 0, len(cases))
	for _, c := range cases {
		calls = append(calls, c.call)
	}
	got := runCatchUpJS(t, calls)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: settled = %v, expected %v", c.name, got[i], c.want)
		}
	}
}
