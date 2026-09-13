package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

// The feed follows new messages while it is near its end, and the jump button
// shows while it is not. Both answers come from one reading of the box, or the
// feed follows a message while the button says it is away from the end.
func TestFeedJumpAndStickinessShareOneEndRule(t *testing.T) {
	files := srcFiles(t)
	src, ok := files["src/screens/chat/feedwindow.js"]
	if !ok {
		t.Fatal("src/screens/chat/feedwindow.js not found — the test is looking in the wrong place")
	}
	code := stripComments(src)

	measure := regexp.MustCompile(`scrollHeight\s*-\s*\w+\.scrollTop\s*-\s*\w+\.clientHeight`)
	if n := len(measure.FindAllString(code, -1)); n != 1 {
		t.Errorf("the distance to the end is measured %d times — the stickiness and the button read different rules", n)
	}

	for _, want := range []struct{ code, harm string }{
		{"stickRef.current = near", "the stickiness has a reading of its own and parts from the button"},
		{"setAtEnd(near)", "the button has a reading of its own and parts from the stickiness"},
		{"const onScroll = (event) => settle(event.currentTarget)", "a scroll settles one side only"},
		{"box.scrollTop = box.scrollHeight;\n        settle(box);", "the jump takes the feed to the end without telling it to follow new messages again"},
		{"else settle(box);", "a box that grows to reach the end keeps the button for an end already in view"},
		{"setAtEnd(true);\n    }, [name, id]);", "a feed opened after a scrolled-up one starts with the button on"},
	} {
		if !strings.Contains(code, want.code) {
			t.Errorf("the feed window has no %q — %s", want.code, want.harm)
		}
	}

	for _, feed := range []string{"src/screens/chat.js", "src/screens/chat/subchat.js"} {
		body, ok := files[feed]
		if !ok {
			t.Fatalf("%s not found — the test is looking in the wrong place", feed)
		}
		feedBox := jsUntil(t, feed, body, `class="chatfeed"`, "\n        </div>")
		if !strings.Contains(feedBox, "${!atEnd && html`<${JumpToEnd} onJump=${toEnd} />`}") {
			t.Errorf("%s: the feed does not draw the jump by the shared end rule, or draws it outside the feed — "+
				"outside, the feed's own edge no longer keeps it above the composer", feed)
		}
	}
}

// The button gives back what it takes in flow, so a feed with the button has
// the scroll height of a feed without it: otherwise the button's own arrival
// moves the feed under the reader.
func TestFeedJumpTakesNoRoomInTheFeed(t *testing.T) {
	css := cssSrc(t)
	jump := cssBlock(t, css, ".feedjump")
	feed := cssBlock(t, css, ".chatfeed")

	gap := regexp.MustCompile(`gap:\s*([^;]+);`).FindStringSubmatch(feed)
	if gap == nil {
		t.Fatal(".chatfeed has no gap — the test does not know what the button has to give back")
	}
	for _, want := range []struct{ rule, harm string }{
		{"position: sticky", "the button scrolls away with the messages instead of staying at the edge"},
		{"align-self: flex-end", "the button stretches across the feed and covers the last line"},
		{"overflow-anchor: none", "scroll anchoring may hold on to the button and move the feed when it goes"},
		{"margin-top: calc(-1 * (var(--feedjump-size) + " + strings.TrimSpace(gap[1]) + "))",
			"the button takes its height and the column gap in flow, and the feed grows by that under the reader"},
	} {
		if !strings.Contains(jump, want.rule) {
			t.Errorf(".feedjump has no %q — %s", want.rule, want.harm)
		}
	}

	raw, err := os.ReadFile(webPath("src/css/chat.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "@media (prefers-reduced-motion: reduce) { .feedjump { animation: none; } }") {
		t.Error("the arrival of the button is not stilled under reduced motion — the one animation the reader asked not to see")
	}
}

type endCase struct {
	ScrollHeight int `json:"scrollHeight"`
	ScrollTop    int `json:"scrollTop"`
	ClientHeight int `json:"clientHeight"`
}

// Within the slack the feed still counts as at its end: a reader who nudged the
// last message a line up is carried along, and the button does not appear.
func TestFeedEndHasALineOfSlack(t *testing.T) {
	cases := []struct {
		name string
		box  endCase
		want bool
	}{
		{"at the very end", endCase{1000, 600, 400}, true},
		{"a line short of the end", endCase{1000, 580, 400}, true},
		{"just inside the slack", endCase{1000, 481, 400}, true},
		{"at the edge of the slack", endCase{1000, 480, 400}, false},
		{"far up", endCase{5000, 0, 400}, false},
		{"a feed shorter than its box", endCase{300, 0, 400}, true},
	}
	boxes := make([]endCase, 0, len(cases))
	for _, c := range cases {
		boxes = append(boxes, c.box)
	}
	got := runNearEndJS(t, boxes)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: nearEnd returned %v, while the feed was supposed to count as %s", c.name, got[i], atEnd(c.want))
		}
	}
}

func atEnd(near bool) string {
	if near {
		return "at its end"
	}
	return "away from its end"
}

func runNearEndJS(t *testing.T, boxes []endCase) []bool {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the end rule is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "feedwindow.js"))
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
		t.Fatalf("feedwindow.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "feedwindow.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { nearEnd } from ` + jsString("file://"+bundle) + `;
const boxes = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(boxes.map((box) => nearEnd(box) === true)));
`
	raw, err := json.Marshal(boxes)
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
	if len(got) != len(boxes) {
		t.Fatalf("%d answers to %d boxes", len(got), len(boxes))
	}
	return got
}
