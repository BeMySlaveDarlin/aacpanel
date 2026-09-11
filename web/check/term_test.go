package check

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

type fitOut struct {
	Fits  map[string]int    `json:"fits"`
	Notes map[string]string `json:"notes"`
	Rooms map[string]int    `json:"rooms"`
}

func TestTermKeepsTheScreenAboveTheKeyboard(t *testing.T) {
	got := runTermFitJS(t)
	for _, c := range []struct {
		name string
		got  int
		want int
	}{
		{"the keyboard squeezed the visible part", got.Rooms["keyboard"], 364},
		{"the browser pushed the page towards the cursor", got.Rooms["panned"], 484},
		{"the whole window is visible — the ceiling is above the block", got.Rooms["whole"], 708},
		{"a pinch gives no number", got.Rooms["pinched"], 0},
		{"less room is left than a row of keys — no number", got.Rooms["tiny"], 0},
		{"the page knows nothing about the visible part — no number", got.Rooms["noView"], 0},
	} {
		if c.got != c.want {
			t.Errorf("%s: room %d, expected %d", c.name, c.got, c.want)
		}
	}
	for _, c := range []struct {
		name, got, want string
	}{
		{"the keyboard sets the ceiling", got.Notes["capResize"], "364px"},
		{"firmware without resize: a lone scroll sets the ceiling too", got.Notes["capScroll"], "364px"},
		{"a ceiling, not a height", got.Notes["capHeight"], ""},
		{"a pinch does not move the previous ceiling", got.Notes["capPinch"], "364px"},
		{"the terminal leaving removes the ceiling", got.Notes["capStopped"], ""},
		{"the terminal leaving removes the subscriptions", got.Notes["capListeners"], "none"},
		{"the room is counted from the bottom edge of the visible strip", got.Notes["capMoved"], "164px"},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %q, expected %q", c.name, c.got, c.want)
		}
	}
	if got.Fits["cap"] != 0 {
		t.Errorf("the ceiling calls the refit itself: %d times — a second place sending the size to the host",
			got.Fits["cap"])
	}
}

func TestTermRefitsWhenTheFontArrives(t *testing.T) {
	got := runTermFitJS(t)
	for _, c := range []struct {
		name string
		got  int
		want int
	}{
		{"the block changed size — a refit", got.Fits["resize"], 1},
		{"the font arrived — a refit", got.Fits["fontLoaded"], 1},
		{"the font did not arrive — a refit anyway", got.Fits["fontFailed"], 1},
		{"both reasons — both refits", got.Fits["both"], 2},
		{"the terminal is gone — no refits", got.Fits["afterStop"], 0},
		{"without a font set the observer remains", got.Fits["noFonts"], 1},
	} {
		if c.got != c.want {
			t.Errorf("%s: %d refits, expected %d", c.name, c.got, c.want)
		}
	}
	for _, c := range []struct {
		name, got, want string
	}{
		{"the font asked for is the one the screen is drawn with", got.Notes["asked"], `13px "JetBrains Mono", monospace`},
		{"the terminal leaving detaches the observer", got.Notes["disconnected"], "yes"},
		{"the observer watches the screen itself", got.Notes["observed"], "screen"},
		{"a throwing refit does not take the terminal down", got.Notes["threw"], "caught"},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %q, expected %q", c.name, c.got, c.want)
		}
	}
}

func runTermFitJS(t *testing.T) fitOut {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the reasons for a refit are run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	built := esbuild.Build(esbuild.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Format:      esbuild.FormatESModule,
		Platform:    esbuild.PlatformNeutral,
		Alias:       alias,
		External:    []string{"/dist/term.js"},
		Write:       false,
	})
	if len(built.Errors) > 0 {
		t.Fatalf("term.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "term.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
process.on("unhandledRejection", () => {});

import { fitOn, roomFor, capToView } from ` + jsString("file://"+bundle) + `;

function fake(trouble) {
    const state = { fits: 0 };
    state.fit = { fit() { state.fits += 1; if (trouble) throw new Error("the block is not in the layout"); } };
    return state;
}

function watcher() {
    const seen = { observed: null, disconnected: false, fire: null };
    seen.Observer = class {
        constructor(cb) { seen.fire = cb; }
        observe(el) { seen.observed = el; }
        disconnect() { seen.disconnected = true; }
    };
    return seen;
}

function fontset() {
    const set = { asked: [], arrive() {}, fail() {} };
    set.fonts = {
        load(font) {
            set.asked.push(font);
            return new Promise((resolve, reject) => { set.arrive = resolve; set.fail = reject; });
        },
    };
    return set;
}

const FONT = '13px "JetBrains Mono", monospace';
const SCREEN = "screen";
const fits = {};
const notes = {};
const rooms = {};

function box(top) {
    return { style: {}, getBoundingClientRect: () => ({ top }) };
}

function viewport(height, offsetTop = 0, scale = 1) {
    const view = { height, offsetTop, scale, on: {} };
    view.addEventListener = (type, fn) => { view.on[type] = fn; };
    view.removeEventListener = (type) => { delete view.on[type]; };
    view.fire = (type) => view.on[type] && view.on[type]();
    return view;
}
const tick = () => new Promise((r) => setTimeout(r, 0));

{
    const term = fake(), w = watcher();
    fitOn(term.fit, SCREEN, fontset().fonts, FONT, w.Observer);
    w.fire();
    fits.resize = term.fits;
    notes.observed = w.observed;
}

{
    const term = fake(), w = watcher(), f = fontset();
    fitOn(term.fit, SCREEN, f.fonts, FONT, w.Observer);
    f.arrive([]);
    await tick();
    fits.fontLoaded = term.fits;
    notes.asked = f.asked.join(" | ");
}

{
    const term = fake(), w = watcher(), f = fontset();
    fitOn(term.fit, SCREEN, f.fonts, FONT, w.Observer);
    f.fail(new Error("network"));
    await tick();
    fits.fontFailed = term.fits;
}

{
    const term = fake(), w = watcher(), f = fontset();
    fitOn(term.fit, SCREEN, f.fonts, FONT, w.Observer);
    w.fire();
    f.arrive([]);
    await tick();
    fits.both = term.fits;
}

{
    const term = fake(), w = watcher(), f = fontset();
    const stop = fitOn(term.fit, SCREEN, f.fonts, FONT, w.Observer);
    stop();
    w.fire();
    f.arrive([]);
    await tick();
    fits.afterStop = term.fits;
    notes.disconnected = w.disconnected ? "yes" : "no";
}

{
    const term = fake(), w = watcher();
    fitOn(term.fit, SCREEN, undefined, FONT, w.Observer);
    w.fire();
    fits.noFonts = term.fits;
}

{
    const term = fake(true), w = watcher();
    fitOn(term.fit, SCREEN, fontset().fonts, FONT, w.Observer);
    try {
        w.fire();
        notes.threw = "caught";
    } catch (e) {
        notes.threw = "escaped: " + e.message;
    }
}

rooms.keyboard = roomFor(136, viewport(500));
rooms.panned = roomFor(136, viewport(500, 120));
rooms.whole = roomFor(136, viewport(844));
rooms.pinched = roomFor(136, viewport(500, 0, 2));
rooms.tiny = roomFor(136, viewport(200));
rooms.noView = roomFor(136, null);

{
    const wrap = box(136), view = viewport(500);
    capToView(wrap, view);
    view.height = 844;
    wrap.style.maxHeight = "";
    view.height = 500;
    view.fire("resize");
    notes.capResize = wrap.style.maxHeight;
    notes.capHeight = wrap.style.height || "";
}

{
    const wrap = box(136), view = viewport(500);
    capToView(wrap, view);
    wrap.style.maxHeight = "";
    view.fire("scroll");
    notes.capScroll = wrap.style.maxHeight;
}

{
    const wrap = box(136), view = viewport(500);
    capToView(wrap, view);
    view.scale = 2;
    view.height = 250;
    view.fire("resize");
    notes.capPinch = wrap.style.maxHeight;
}

{
    const wrap = box(136), view = viewport(500);
    capToView(wrap, view);
    view.height = 300;
    view.fire("resize");
    notes.capMoved = wrap.style.maxHeight;
}

{
    const wrap = box(136), view = viewport(500);
    const stop = capToView(wrap, view);
    stop();
    notes.capStopped = wrap.style.maxHeight;
    notes.capListeners = Object.keys(view.on).length ? "left" : "none";
}

{
    const term = fake(), w = watcher(), wrap = box(136), view = viewport(500);
    fitOn(term.fit, SCREEN, fontset().fonts, FONT, w.Observer);
    capToView(wrap, view);
    view.fire("resize");
    view.fire("scroll");
    fits.cap = term.fits;
}

console.log(JSON.stringify({ fits, notes, rooms }));
`
	file := filepath.Join(dir, "run.mjs")
	if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, file).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("node did not finish: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("node did not finish: %v", err)
	}
	var got fitOut
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v\n%s", err, out)
	}
	return got
}
