package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"aacpanel/internal/webbuild"
)

func TestProfilePagesFollowContourNumber(t *testing.T) {
	profiles := []any{
		map[string]any{"id": 1, "profile": "mine"},
		map[string]any{"id": 2, "profile": "office"},
	}
	renamed := map[string]any{"contours": []any{
		map[string]any{"profile": "personal", "contour": 1},
		map[string]any{"profile": "work", "contour": 2},
	}}
	outside := map[string]any{"contours": []any{
		map[string]any{"profile": "personal", "contour": 1},
		map[string]any{"profile": "acme"},
	}}
	old := map[string]any{"contours": []any{
		map[string]any{"profile": "mine"},
		map[string]any{"profile": "acme"},
	}}

	got := runSessionsJS(t, "pages.js", "pageNames", [][]any{
		{profiles, renamed},
		{profiles, outside},
		{profiles, old},
		{[]any{}, renamed},
	})
	want := [][]string{
		{"mine", "office"},
		{"mine", "office", "acme"},
		{"mine", "office", "acme"},
		{},
	}
	for i := range want {
		list, _ := got[i].([]any)
		if len(list) != len(want[i]) {
			t.Errorf("pageNames #%d: %v, expected %v", i, got[i], want[i])
			continue
		}
		for j := range want[i] {
			if s, _ := list[j].(string); s != want[i][j] {
				t.Errorf("pageNames #%d: %v, expected %v", i, got[i], want[i])
				break
			}
		}
	}
}

func TestContourLimitsFollowContourNumber(t *testing.T) {
	limits := map[string]any{"contours": []any{
		map[string]any{"profile": "personal", "contour": 1, "fiveHour": map[string]any{"pct": 12}},
		map[string]any{"profile": "work", "contour": 2, "fiveHour": map[string]any{"pct": 3}},
	}}
	old := map[string]any{"contours": []any{
		map[string]any{"profile": "mine", "fiveHour": map[string]any{"pct": 12}},
	}}

	got := runSessionsJS(t, "limits.js", "contourOf", [][]any{
		{limits, "office", 2},
		{limits, "mine", 1},
		{limits, "acme", 9},
		{old, "mine", 1},
	})
	want := []float64{3, 12, -1, 12}
	for i, w := range want {
		row, _ := got[i].(map[string]any)
		if w < 0 {
			if row != nil {
				t.Errorf("contourOf #%d: %v, expected no numbers at all", i, got[i])
			}
			continue
		}
		if row == nil {
			t.Errorf("contourOf #%d: no numbers, expected %v%%", i, w)
			continue
		}
		five, _ := row["fiveHour"].(map[string]any)
		if five == nil || five["pct"] != w {
			t.Errorf("contourOf #%d: %v, expected five hours at %v%%", i, got[i], w)
		}
	}
}

func TestSessionsStayOnTheirContourPage(t *testing.T) {
	profiles := []any{
		map[string]any{"id": 1, "profile": "mine", "groups": []any{}},
		map[string]any{"id": 2, "profile": "acme", "groups": []any{}},
	}
	sessions := []any{
		map[string]any{"session": "shop", "profile": "work", "contour": 2, "cwd": "/srv/proj/shop"},
		map[string]any{"session": "home", "profile": "personal", "contour": 1, "cwd": "/home/u"},
	}
	names := []any{"mine", "acme"}

	got := runSessionsJS(t, "map.js", "pagesOf", [][]any{{profiles, sessions, names}})
	pages, _ := got[0].(map[string]any)
	own, _ := pages["acme"].([]any)
	if len(own) != 1 {
		t.Fatalf("the page of the renamed contour holds %d sessions, expected one: %v", len(own), got[0])
	}
	row, _ := own[0].(map[string]any)
	if row["session"] != "shop" {
		t.Errorf("the contour page holds someone else's session: %v", row["session"])
	}
	mine, _ := pages["mine"].([]any)
	if len(mine) != 1 {
		t.Errorf("the sessions of the personal contour drifted apart: %v", pages["mine"])
	}
}

func TestFeedStreamWakesOnlyWhenNeeded(t *testing.T) {
	cases := []struct {
		name       string
		visibility string
		state      any
		want       bool
	}{
		{"the tab is back and the stream is dead — bring it up", "visible", 2, true},
		{"there is no stream at all — bring it up", "visible", nil, true},
		{"the stream is alive — leave it alone", "visible", 1, false},
		{"the stream is opening — leave it alone", "visible", 0, false},
		{"the tab went to the background — open nothing", "hidden", 2, false},
	}

	calls := make([][]any, 0, len(cases))
	for _, c := range cases {
		calls = append(calls, []any{c.visibility, c.state})
	}
	got := runSessionsJS(t, "chat/feedwindow.js", "wakeNeeded", calls)
	for i, c := range cases {
		if b, _ := got[i].(bool); b != c.want {
			t.Errorf("%s: %v, expected %v", c.name, got[i], c.want)
		}
	}
}

func runSessionsJS(t *testing.T, file, fn string, calls [][]any) []any {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: matching pages against limits is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	rel := filepath.Join(webDir, "src", "screens", "sessions", file)
	if strings.Contains(file, "/") {
		rel = filepath.Join(webDir, "src", "screens", filepath.FromSlash(file))
	}
	entry, err := filepath.Abs(rel)
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
		t.Fatalf("%s did not build: %v", file, built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "screen.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import * as screen from ` + jsString("file://"+bundle) + `;
const calls = JSON.parse(readFileSync(0, "utf8"));
const fn = screen[` + jsString(fn) + `];
if (!fn) throw new Error("no such function: " + ` + jsString(fn) + `);
const plain = (v) => (v instanceof Map ? Object.fromEntries(v) : v);
process.stdout.write(JSON.stringify(calls.map((args) => plain(fn(...args)))));
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
	var got []any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(calls) {
		t.Fatalf("%d answers to %d calls", len(got), len(calls))
	}
	return got
}
