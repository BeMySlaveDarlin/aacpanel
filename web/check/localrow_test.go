package check

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A message just sent is drawn by the screen until the transcript echoes it.
// The rows still on their way are read at render time: the echo and the
// local row never share a frame, and the ones that failed or are held stay.
// A message sent with files reaches the session as its words and one path a
// line after them; the local row carries only the words. The echo is still
// its own, and the row leaves — or it hangs as queued under the delivered one.
func TestARowSentWithFilesLeavesOnItsEcho(t *testing.T) {
	local := []any{
		map[string]any{"key": "a", "text": "look at these", "sent": "look at these", "state": "queued",
			"from": map[string]any{"files": 2}},
		map[string]any{"key": "b", "text": "one.png", "sent": "", "state": "queued", "from": map[string]any{"files": 1}},
		map[string]any{"key": "c", "text": "/opt/x", "sent": "/opt/x", "state": "queued"},
	}
	items := []any{
		map[string]any{"role": "me", "pos": 10, "text": "look at these\n/home/u/.local/share/aacpanel-exec/files/1-a.png\n" +
			"/home/u/.local/share/aacpanel-exec/files/1-b.png"},
		map[string]any{"role": "me", "pos": 20, "text": "/home/u/.local/share/aacpanel-exec/files/2-one.png"},
		map[string]any{"role": "me", "pos": 30, "text": "something else"},
	}
	got := runModuleJS(t, "src/screens/chat/feed.js", "unarrived", [][]any{{local, items}})
	var left []string
	for _, row := range got[0].([]any) {
		left = append(left, row.(map[string]any)["key"].(string))
	}
	if strings.Join(left, ",") != "c" {
		t.Errorf("rows left waiting: %v — the ones sent with files did not recognise their echo, "+
			"or a path the person typed was taken for a file", left)
	}
}

func TestUnarrivedRowsAreTheOnesTheFeedHasNotEchoed(t *testing.T) {
	local := []any{
		map[string]any{"key": "a", "text": "hello", "sent": "hello", "state": "queued"},
		map[string]any{"key": "b", "text": "! git status", "sent": "! git status", "state": "queued"},
		map[string]any{"key": "c", "text": "hello", "sent": "hello", "state": "failed", "error": "no"},
		map[string]any{"key": "d", "text": "later", "sent": "later", "state": "held"},
		map[string]any{"key": "e", "text": "not yet", "sent": "not yet", "state": "sending"},
	}
	items := []any{
		map[string]any{"role": "me", "text": "hello", "pos": 10},
		map[string]any{"role": "shell", "text": "git status", "pos": 20},
		map[string]any{"role": "me", "text": "later", "pos": 30},
		map[string]any{"role": "ai", "text": "not yet", "pos": 40},
	}
	got := runModuleJS(t, "src/screens/chat/feed.js", "unarrived", [][]any{
		{local, items},
		{local, []any{}},
	})
	keys := func(rows any) string {
		var out []string
		for _, row := range rows.([]any) {
			out = append(out, row.(map[string]any)["key"].(string))
		}
		return strings.Join(out, ",")
	}
	if want := "c,d,e"; keys(got[0]) != want {
		t.Errorf("unarrived against the feed = %s, expected %s: the echoed message and the command the console ran leave, "+
			"a failed one, a held one and one the answer merely repeats stay", keys(got[0]), want)
	}
	if want := "a,b,c,d,e"; keys(got[1]) != want {
		t.Errorf("unarrived against an empty feed = %s, expected every row in the order it was sent", keys(got[1]))
	}

	screen := screenSrc(t, "src/screens/chat.js")
	for _, want := range []struct{ code, harm string }{
		{"const pending = unarrived(local, state.items);", "the rows to draw are not read at render time"},
		{"${pending.map((row) => html`",
			"the feed draws the rows straight from the state — a row the transcript echoed stays on screen for a frame, twice"},
		{"<${Row} key=${`local-${row.key}`} item=${row} />",
			"a pending row is drawn under a key of its own kind, or not as a row of the feed at all"},
		{"setLocal((was) => unarrived(was, state.items));", "the echoed rows never leave the state"},
	} {
		if !strings.Contains(screen, want.code) {
			t.Errorf("chat.js has no %q — %s", want.code, want.harm)
		}
	}
	if strings.Contains(screen, "${local.map(") {
		t.Error("chat.js draws local rows straight from the state, next to the ones already read as pending")
	}
	if !strings.Contains(screen, "if (box && atEnd) box.scrollTop = box.scrollHeight;\n    }, [pending.length]);") {
		t.Error("chat.js does not take the feed to its end when a row is sent — the message stands under the edge until the echo comes, and the feed jumps to it then")
	}

	rows := screenSrc(t, "src/screens/chat/rows.js")
	if !strings.Contains(rows, "${mine && (wait || item.at) && html`<div class=\"mstamp\">${wait || stampText(item.at)}</div>`}") {
		t.Error("rows.js does not put the state of a message on its way where its stamp goes — the bubble changes height when the transcript echoes it, and the feed jumps by that")
	}
	for _, line := range []string{`<p class="mwait">queued`, `<p class="mwait"><span class="mclock">`, `<p class="mwait">will go out`} {
		if strings.Contains(rows, line) {
			t.Errorf("rows.js still draws %q inside the bubble — that line is the height the bubble loses on the echo", line)
		}
	}
}

// The chat screen under a real engine: a message is typed and sent, the fake
// host answers, the stub stream echoes it. The feed follows the sent row,
// no frame shows the message twice, and the bubble stands where it stood
// with the same height when the echo takes the local row's place.
func TestASentMessageIsEchoedInPlace(t *testing.T) {
	type shot struct {
		Count        int     `json:"count"`
		Unique       int     `json:"unique"`
		Top          float64 `json:"top"`
		Height       float64 `json:"height"`
		Cls          string  `json:"cls"`
		Stamp        string  `json:"stamp"`
		Wait         string  `json:"wait"`
		ScrollTop    float64 `json:"scrollTop"`
		ScrollHeight float64 `json:"scrollHeight"`
		ClientHeight float64 `json:"clientHeight"`
	}
	var got struct {
		Before  shot           `json:"before"`
		Queued  shot           `json:"queued"`
		Frames  []shot         `json:"frames"`
		Settled shot           `json:"settled"`
		Debug   map[string]any `json:"debug"`
	}
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: the geometry of the feed is the stylesheet's — run make front first")
	}
	runFixture(t, "feed.html", &got)

	if got.Queued.Count != got.Before.Count+1 || !strings.Contains(got.Queued.Cls, "queued") {
		t.Fatalf("after the send the feed shows %d own messages against %d, the last one %q — the local row is not drawn (%v)",
			got.Queued.Count, got.Before.Count, got.Queued.Cls, got.Debug)
	}
	if got.Queued.Stamp != "queued" {
		t.Errorf("the sent row carries the stamp %q — its state belongs where the time will be", got.Queued.Stamp)
	}
	if got.Queued.Wait != "" {
		t.Errorf("the queued bubble carries a line of its own %q — that is the height it loses on the echo", got.Queued.Wait)
	}
	if left := got.Queued.ScrollHeight - got.Queued.ScrollTop - got.Queued.ClientHeight; left > 1 {
		t.Errorf("the feed stands %.0f px above its end after the send — the message goes under the edge until the echo comes", left)
	}
	for _, f := range got.Frames {
		if f.Count > got.Queued.Count {
			t.Errorf("a frame shows %d own messages against %d — the echo and the local row were on screen together", f.Count, got.Queued.Count)
			break
		}
	}
	if got.Settled.Count != got.Queued.Count || got.Settled.Unique != got.Settled.Count {
		t.Errorf("after the echo the feed shows %d own messages, %d distinct, against %d before it — a duplicate, or the message is gone",
			got.Settled.Count, got.Settled.Unique, got.Queued.Count)
	}
	if got.Settled.Top != got.Queued.Top || got.Settled.Height != got.Queued.Height {
		t.Errorf("the bubble moved on the echo: top %.2f → %.2f, height %.2f → %.2f", got.Queued.Top, got.Settled.Top, got.Queued.Height, got.Settled.Height)
	}
	for _, f := range got.Frames {
		if f.Top != got.Queued.Top || f.ScrollTop != got.Queued.ScrollTop {
			t.Errorf("a frame on the way had the bubble at %.2f and the feed at %.0f against %.2f and %.0f — the feed jumped", f.Top, f.ScrollTop, got.Queued.Top, got.Queued.ScrollTop)
			break
		}
	}
	if strings.Contains(got.Settled.Cls, "queued") || got.Settled.Stamp == "queued" || got.Settled.Stamp == "" {
		t.Errorf("after the echo the bubble is still %q with the stamp %q — the state and the time did not take over", got.Settled.Cls, got.Settled.Stamp)
	}
	if b, _ := json.Marshal(got.Settled); testing.Verbose() {
		t.Logf("settled: %s", b)
	}
}
