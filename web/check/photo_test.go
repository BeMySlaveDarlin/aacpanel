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

var (
	phoneBox   = map[string]float64{"w": 360, "h": 600}
	phoneShot  = map[string]float64{"w": 1170, "h": 2532}
	longShot   = map[string]float64{"w": 1170, "h": 6000}
	deskShot   = map[string]float64{"w": 2560, "h": 1440}
	hugeShot   = map[string]float64{"w": 1170, "h": 20000}
	zoomScript = `
import * as zoom from %ZOOM%;
import { shotName } from %PHOTO%;
const steps = JSON.parse(readFileSync(0, "utf8"));
const out = steps.map((step) => {
    if (step.op === "name") return { name: shotName(step.shot, step.pos) };
    if (step.op === "next") return { scale: zoom.nextScale(step.view, step.box, step.image) };
    if (step.op === "zoom") return zoom.zoomAt(step.view, step.scale, step.point, step.box, step.image);
    if (step.op === "pan") return zoom.panBy(step.view, step.dx, step.dy, step.box, step.image);
    if (step.op === "drawn") return zoom.drawn(step.box, step.image);
    throw new Error("unknown step: " + step.op);
});
process.stdout.write(JSON.stringify(out));
`
)

type step struct {
	Op    string             `json:"op"`
	View  map[string]float64 `json:"view,omitempty"`
	Box   map[string]float64 `json:"box,omitempty"`
	Image map[string]float64 `json:"image,omitempty"`
	Point map[string]float64 `json:"point,omitempty"`
	Scale float64            `json:"scale,omitempty"`
	Dx    float64            `json:"dx,omitempty"`
	Dy    float64            `json:"dy,omitempty"`
	Shot  map[string]any     `json:"shot,omitempty"`
	Pos   int64              `json:"pos,omitempty"`
}

type zoomReply struct {
	Scale float64 `json:"scale"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
	Name  string  `json:"name"`
}

func fit() map[string]float64 { return map[string]float64{"scale": 1, "x": 0, "y": 0} }

func TestPhotoZoomKeepsThePointUnderTheFinger(t *testing.T) {
	point := map[string]float64{"x": 40, "y": 50}
	got := runZoomJS(t, []step{{
		Op: "zoom", View: fit(), Scale: 2, Point: point, Box: phoneBox, Image: phoneShot,
	}})[0]

	if got.Scale != 2 {
		t.Fatalf("the scale after zooming in = %v, expected 2", got.Scale)
	}
	was := point["x"]
	now := (point["x"] - got.X) / got.Scale
	if diff := now - was; diff > 0.5 || diff < -0.5 {
		t.Errorf("a different point of the shot ended up under the finger: was %.1f, now %.1f "+
			"(shift %.1f) — zooming drags away from the spot that was tapped", was, now, got.X)
	}
	wasY := point["y"]
	nowY := (point["y"] - got.Y) / got.Scale
	if diff := nowY - wasY; diff > 0.5 || diff < -0.5 {
		t.Errorf("vertically a different point is under the finger: was %.1f, now %.1f", wasY, nowY)
	}
}

func TestPhotoNeverLetsTheImageLeaveTheField(t *testing.T) {
	steps := []step{
		{Op: "pan", View: fit(), Dx: 500, Dy: 500, Box: phoneBox, Image: phoneShot},
		{Op: "pan", View: map[string]float64{"scale": 2, "x": 0, "y": 0},
			Dx: 4000, Dy: 4000, Box: phoneBox, Image: phoneShot},
		{Op: "zoom", View: fit(), Scale: 2, Point: map[string]float64{"x": 180, "y": 300},
			Box: phoneBox, Image: phoneShot},
	}
	got := runZoomJS(t, steps)

	if got[0].X != 0 || got[0].Y != 0 {
		t.Errorf("a fitted shot moved by (%.1f, %.1f) — it has nowhere to go, "+
			"it is fully visible, and any shift opens empty space", got[0].X, got[0].Y)
	}
	wantX, wantY := 97.2, 300.0
	if diff := got[1].X - wantX; diff > 0.1 || diff < -0.1 {
		t.Errorf("the horizontal shift reached %.1f while the edge of the shot is at %.1f: "+
			"past it empty space shows through", got[1].X, wantX)
	}
	if diff := got[1].Y - wantY; diff > 0.1 || diff < -0.1 {
		t.Errorf("the vertical shift reached %.1f while the edge of the shot is at %.1f", got[1].Y, wantY)
	}
	if got[2].X > wantX+0.1 || got[2].X < -wantX-0.1 || got[2].Y > wantY+0.1 || got[2].Y < -wantY-0.1 {
		t.Errorf("zooming at the edge took the shot to (%.1f, %.1f) with bounds (%.1f, %.1f)",
			got[2].X, got[2].Y, wantX, wantY)
	}
}

func TestPhotoDoubleTapFitsTheWidthOfWhatWasSent(t *testing.T) {
	cases := []struct {
		name  string
		view  map[string]float64
		image map[string]float64
		want  float64
	}{
		{"a phone screen", fit(), phoneShot, 2.5},
		{"a whole page", fit(), longShot, 360.0 / 117.0},
		{"a monitor shot", fit(), deskShot, 2.5},
		{"a shot as tall as the whole feed", fit(), hugeShot, 6},
		{"a repeated tap", map[string]float64{"scale": 2.5, "x": 0, "y": 0}, longShot, 1},
	}
	steps := make([]step, 0, len(cases))
	for _, c := range cases {
		steps = append(steps, step{Op: "next", View: c.view, Box: phoneBox, Image: c.image})
	}
	got := runZoomJS(t, steps)
	for i, c := range cases {
		if diff := got[i].Scale - c.want; diff > 0.01 || diff < -0.01 {
			t.Errorf("%s: a double tap gives scale %.3f, expected %.3f",
				c.name, got[i].Scale, c.want)
		}
	}
}

func TestShotFileNameSaysWhatItIs(t *testing.T) {
	cases := []struct {
		name string
		shot map[string]any
		pos  int64
		want string
	}{
		{"a screenshot", map[string]any{"media": "image/png", "index": 0}, 5982507, "attachment-5982507-0.png"},
		{"a photograph", map[string]any{"media": "image/jpeg", "index": 2}, 140, "attachment-140-2.jpg"},
		{"the type was not given", map[string]any{"index": 1}, 140, "attachment-140-1.png"},
		{"the type is unknown", map[string]any{"media": "image/x-strange", "index": 1}, 140, "attachment-140-1.png"},
	}
	steps := make([]step, 0, len(cases))
	for _, c := range cases {
		steps = append(steps, step{Op: "name", Shot: c.shot, Pos: c.pos})
	}
	got := runZoomJS(t, steps)
	for i, c := range cases {
		if got[i].Name != c.want {
			t.Errorf("%s: file name %q, expected %q", c.name, got[i].Name, c.want)
		}
	}
}

func runZoomJS(t *testing.T, steps []step) []zoomReply {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the scale arithmetic is run by the engine, not by reading the source")
	}
	dir := t.TempDir()
	zoomFile := buildModule(t, dir, "zoom", filepath.Join(webDir, "src", "screens", "chat", "zoom.js"))
	photoFile := buildModule(t, dir, "photo", filepath.Join(webDir, "src", "screens", "chat", "photo.js"))

	script := "import { readFileSync } from \"node:fs\";\n" +
		strings.NewReplacer(
			"%ZOOM%", jsString("file://"+zoomFile),
			"%PHOTO%", jsString("file://"+photoFile),
		).Replace(zoomScript)
	raw, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	cmd.Stdin = bytes.NewReader(raw)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node did not finish: %v\n%s", err, errBuf.String())
	}
	var got []zoomReply
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v\n%s", err, out)
	}
	if len(got) != len(steps) {
		t.Fatalf("%d answers to %d steps", len(got), len(steps))
	}
	return got
}

func buildModule(t *testing.T, dir, name, rel string) string {
	t.Helper()
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
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
		t.Fatalf("%s did not build: %v", rel, built.Errors[0].Text)
	}
	file := filepath.Join(dir, name+".mjs")
	if err := os.WriteFile(file, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestPhotoLayerStaysAboveTheShell(t *testing.T) {
	css := cssSrc(t)

	layer := cssBlock(t, css, ".pv")
	for _, rule := range []string{"position: fixed", "z-index: 30"} {
		if !strings.Contains(layer, rule) {
			t.Errorf("%s: .pv has no %q — the preview layer joins the flow of the feed "+
				"and scrolls along with the conversation", cssFile, rule)
		}
	}
	if !strings.Contains(css, ".shell:has(.pv) .body") {
		t.Errorf("%s: the deck is not lifted while the layer is open — the bottom menu "+
			"draws over the picture", cssFile)
	}

	pic := cssBlock(t, css, ".pvpic")
	for _, rule := range []string{"position: absolute", "inset: 0", "object-fit: contain"} {
		if !strings.Contains(pic, rule) {
			t.Errorf("%s: .pvpic has no %q — the shot spills outside the field, and the zoom "+
				"is computed against a rectangle other than the visible one", cssFile, rule)
		}
	}

	stage := cssBlock(t, css, ".pvstage")
	for _, rule := range []string{"touch-action: none", "overflow: hidden", "min-height: 0"} {
		if !strings.Contains(stage, rule) {
			t.Errorf("%s: .pvstage has no %q — the gesture goes to the browser, and a tall "+
				"shot squeezes everything else out of the layer", cssFile, rule)
		}
	}
}
