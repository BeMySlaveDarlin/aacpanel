package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

const sheetFile = "src/ui/sheet.js"

type swipeCase struct {
	DX float64 `json:"dx"`
	DY float64 `json:"dy"`
}

func TestTheAxisOfTheFingerDecidesWhoGetsTheGesture(t *testing.T) {
	cases := []struct {
		name string
		in   swipeCase
		want string
	}{
		{"a finger that did not move", swipeCase{0, 0}, "tap"},
		{"a tap with the shake of a hand in it", swipeCase{3, -4}, "tap"},
		{"a pull down on the sheet", swipeCase{2, 60}, "drag"},
		{"a pull up on the sheet", swipeCase{-3, -80}, "drag"},
		{"a swipe across a wide table", swipeCase{-140, 4}, "sideways"},
		{"the same swipe with the hand sliding down", swipeCase{90, -24}, "sideways"},
		{"a slanted pull the sheet still wins", swipeCase{30, 45}, "drag"},
		{"the far corner of the square that counts as a tap", swipeCase{6, -6}, "tap"},
		{"one pixel sideways out of that square", swipeCase{7, 6}, "sideways"},
		{"one pixel down out of it", swipeCase{6, 7}, "drag"},
	}
	in := make([]swipeCase, 0, len(cases))
	for _, c := range cases {
		in = append(in, c.in)
	}
	got := runGestureJS(t, in)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s (%.0f across, %.0f down): the sheet reads it as %q, and it is %q",
				c.name, c.in.DX, c.in.DY, got[i], c.want)
		}
	}
}

func TestSheetLeavesASidewaysGestureToTheContent(t *testing.T) {
	src := stripComments(srcFiles(t)[sheetFile])

	down := arrowFn(t, sheetFile, src, "const onDown = (event) => {")
	refusal := strings.Index(down, "scrollsSideways(event.target")
	if refusal < 0 {
		t.Fatal("the sheet starts a drag wherever the finger landed: a wide table is paged by dragging it, " +
			"and every such swipe reaches the sheet")
	}
	if start := strings.Index(down, "drag.current = {"); start >= 0 && refusal > start {
		t.Error("the finger inside a sideways-scrolling box is checked after the drag has begun — " +
			"the sheet already follows the hand across the table")
	}
	if !strings.Contains(down, "x: event.clientX") {
		t.Error("the drag does not remember where the finger started sideways: with no starting point " +
			"there is nothing to measure the sideways travel against, and the swipe reads as a tap again")
	}

	helper := funcBody(t, src, "function scrollsSideways(")
	for _, want := range []struct{ code, why string }{
		{"overflowX", "the box is taken for sideways-scrolling by its content alone — content wider than " +
			"the box overflows visibly too, and dragging the sheet dies over ordinary text"},
		{"scrollWidth", "every box on the way up counts as sideways-scrolling, and the sheet stops answering " +
			"the finger at all"},
	} {
		if !strings.Contains(helper, want.code) {
			t.Errorf("the search for a sideways-scrolling box has no %s: %s", want.code, want.why)
		}
	}

	move := arrowFn(t, sheetFile, src, "const onMove = (event) => {")
	if !strings.Contains(move, `gestureKind(dx, dy) === "sideways"`) {
		t.Error("while the finger moves the sheet never asks which way it went: a swipe that began " +
			"downwards and turned sideways drags the sheet along the whole table")
	}
	if !strings.Contains(move, "state.guard && event.clientY < state.y") {
		t.Error("the guard over content scrolled vertically is gone — the sheet moves instead of the list " +
			"under the finger")
	}

	up := arrowFn(t, sheetFile, src, "const onUp = (event) => {")
	if strings.Contains(up, "Math.abs(state.dy) <= TAP_MAX") {
		t.Error("the end of the gesture is still judged by the vertical travel alone: a swipe across a table " +
			"barely moves the finger down, so the sheet counts it as a tap and changes its height")
	}
	kind := strings.Index(up, "gestureKind(state.dx, state.dy)")
	if kind < 0 {
		t.Fatal("the end of the gesture does not ask which way the finger went")
	}
	for _, want := range []struct{ code, why string }{
		{`if (kind === "sideways") return;`, "a gesture that belongs to the content still reaches the sheet"},
		{`if (kind === "tap") {`, "a tap no longer changes the height of the sheet"},
		{"STOPS[(at + 1) % STOPS.length]", "a tap no longer walks the sheet through its heights"},
		{"onClose();", "a pull down on the lowest height no longer closes the sheet"},
	} {
		at := strings.Index(up, want.code)
		if at < 0 {
			t.Errorf("the end of the gesture has no %q: %s", want.code, want.why)
			continue
		}
		if at < kind {
			t.Errorf("%q is decided before the direction of the finger is known: %s", want.code, want.why)
		}
	}
}

func runGestureJS(t *testing.T, cases []swipeCase) []string {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the gesture is sorted out by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(webPath(sheetFile))
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
		t.Fatalf("%s did not build: %v", sheetFile, built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "sheet.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { gestureKind } from ` + jsString("file://"+bundle) + `;
const out = JSON.parse(readFileSync(0, "utf8")).map((c) => gestureKind(c.dx, c.dy));
process.stdout.write(JSON.stringify(out));
`
	raw, err := json.Marshal(cases)
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
	if len(got) != len(cases) {
		t.Fatalf("%d answers to %d gestures", len(got), len(cases))
	}
	return got
}
