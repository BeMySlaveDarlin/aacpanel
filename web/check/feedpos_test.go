package check

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

// The box of the feed is built anew every time the screen comes back from the
// terminal or from an agent, while the window above it lives through both. A
// new box starts at the top of the conversation, so the feed has to be put back
// where the reader left it: at its end if it was following new messages there,
// and at the same offset if it was scrolled up.
func TestFeedComesBackWhereItWasLeft(t *testing.T) {
	got := runFeedWindow(t)

	if got.Opened != 2600 {
		t.Fatalf("the feed opened at %d instead of its end — the drive is not driving the feed", got.Opened)
	}
	if got.AwayButton {
		t.Fatal("the feed reports itself at its end after a scroll up — the drive is reading the wrong box")
	}

	if got.Kept != 1200 {
		t.Errorf("a feed scrolled up came back at %d instead of the 1200 it was left at — "+
			"the reader is thrown to the top of the conversation by a look at the terminal", got.Kept)
	}
	if got.KeptButton {
		t.Error("a feed scrolled up came back without its jump button — the way to the end is gone")
	}

	if got.Followed != 3800 {
		t.Errorf("a feed left at its end came back at %d instead of the end of the messages it has now (3800) — "+
			"following the end was remembered as an offset, and new messages hid below it", got.Followed)
	}
	if !got.FollowedButton {
		t.Error("a feed left at its end came back with the jump button on, as if it stood away from the end")
	}

	if got.SubOpened != 2600 {
		t.Errorf("the feed of an agent opened at %d instead of its own end — it was put where the "+
			"conversation stood, and the two feeds share one position", got.SubOpened)
	}
	if got.ParentBack != 3800 {
		t.Errorf("the conversation came back at %d instead of its own end (3800) after a look into an agent", got.ParentBack)
	}
	if got.SubBack != 700 {
		t.Errorf("the feed of an agent came back at %d instead of the 700 it was left at", got.SubBack)
	}
}

type feedRun struct {
	Opened         int  `json:"opened"`
	AwayButton     bool `json:"awayButton"`
	Kept           int  `json:"kept"`
	KeptButton     bool `json:"keptButton"`
	Followed       int  `json:"followed"`
	FollowedButton bool `json:"followedButton"`
	SubOpened      int  `json:"subOpened"`
	ParentBack     int  `json:"parentBack"`
	SubBack        int  `json:"subBack"`
}

// hookShim stands in for preact/hooks: the renders are made by hand, so that the
// box can be taken away and built anew between them, the way the screen does it.
const hookShim = `
let slots = [];
let cursor = 0;
let layout = [];
let queue = [];

export function __render(fn) {
    cursor = 0;
    layout = [];
    queue = [];
    const out = fn();
    for (const effect of layout) effect();
    for (const effect of queue) effect();
    return out;
}

export function useState(init) {
    const i = cursor++;
    if (!(i in slots)) slots[i] = { value: typeof init === "function" ? init() : init };
    const slot = slots[i];
    return [slot.value, (next) => { slot.value = typeof next === "function" ? next(slot.value) : next; }];
}

export function useRef(init) {
    const i = cursor++;
    if (!(i in slots)) slots[i] = { current: init };
    return slots[i];
}

function run(list, fn, deps) {
    const i = cursor++;
    const prev = slots[i];
    const same = prev && prev.deps && deps && deps.length === prev.deps.length
        && deps.every((d, k) => d === prev.deps[k]);
    slots[i] = { deps };
    if (!same) list.push(fn);
}

export function useEffect(fn, deps) {
    run(queue, fn, deps);
}

export function useLayoutEffect(fn, deps) {
    run(layout, fn, deps);
}
`

// feedProbe walks the feed through a look at the terminal and a look into an
// agent, taking the box away and giving back a new one, as the screen does.
const feedProbe = `
import { useFeedWindow } from %FEED%;
import { __render } from %SHIM%;

const items = [];
for (let pos = 1; pos <= 50; pos += 1) items.push({ role: pos % 2 ? "me" : "ai", pos, text: "line " + pos });

globalThis.fetch = async () => ({
    ok: true,
    json: async () => ({ items, first: 1, last: 50, total: 50, moreBefore: false }),
});
globalThis.EventSource = class {
    constructor() { this.readyState = 1; }
    addEventListener() {}
    close() {}
};
globalThis.document = { visibilityState: "visible", addEventListener() {}, removeEventListener() {} };
globalThis.ResizeObserver = class { observe() {} disconnect() {} };
globalThis.IntersectionObserver = class { observe() {} disconnect() {} };

// box stands for the feed element, and clamps the scroll the way a browser does.
function box(height) {
    let top = 0;
    return {
        scrollHeight: height,
        clientHeight: 400,
        get scrollTop() { return top; },
        set scrollTop(next) {
            const far = Math.max(0, this.scrollHeight - this.clientHeight);
            top = Math.max(0, Math.min(next, far));
        },
    };
}

const live = { status: "idle" };
let at = { name: "shopfront", id: "" };
let win = null;
const draw = () => { win = __render(() => useFeedWindow({ name: at.name, id: at.id, live })); };
const tick = () => new Promise((r) => setTimeout(r, 0));
const scrollTo = (el, top) => { el.scrollTop = top; win.onScroll({ currentTarget: el }); draw(); };
const takeBox = () => { win.feedRef.current = null; draw(); };
const giveBox = (height) => { const el = box(height); win.feedRef.current = el; draw(); return el; };

draw();
await tick();

const opening = giveBox(3000);
const opened = opening.scrollTop;

scrollTo(opening, 1200);
const awayButton = win.atEnd;

takeBox();
const returned = giveBox(3000);
const kept = returned.scrollTop;
const keptButton = win.atEnd;

scrollTo(returned, returned.scrollHeight);
takeBox();
const grown = giveBox(4200);
const followed = grown.scrollTop;
const followedButton = win.atEnd;

takeBox();
at = { name: "shopfront", id: "shopfront:agent-1" };
draw();
await tick();
const agent = giveBox(3000);
const subOpened = agent.scrollTop;
scrollTo(agent, 700);

takeBox();
at = { name: "shopfront", id: "" };
draw();
await tick();
const parent = giveBox(4200);
const parentBack = parent.scrollTop;

takeBox();
at = { name: "shopfront", id: "shopfront:agent-1" };
draw();
await tick();
const again = giveBox(3000);
const subBack = again.scrollTop;

process.stdout.write(JSON.stringify({
    opened, awayButton, kept, keptButton, followed, followedButton, subOpened, parentBack, subBack,
}));
`

func runFeedWindow(t *testing.T) feedRun {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the feed is driven by the engine, not read out of the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "hooks.mjs")
	if err := os.WriteFile(shimPath, []byte(hookShim), 0o600); err != nil {
		t.Fatal(err)
	}
	alias["preact/hooks"] = shimPath

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
		External:    []string{shimPath},
		Write:       false,
	})
	if len(built.Errors) > 0 {
		t.Fatalf("feedwindow.js did not build: %v", built.Errors[0].Text)
	}
	bundle := filepath.Join(dir, "feedwindow.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := strings.NewReplacer(
		"%FEED%", jsString("file://"+bundle),
		"%SHIM%", jsString("file://"+shimPath),
	).Replace(feedProbe)
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got feedRun
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node reply did not parse: %v: %s", err, out)
	}
	return got
}
