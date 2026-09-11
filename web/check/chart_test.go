package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"aacpanel/internal/webbuild"
)

func TestBarsAreNotALine(t *testing.T) {
	var got struct {
		Paths  string `json:"paths"`
		Points bool   `json:"points"`
		Fill   string `json:"fill"`
		XTime  any    `json:"xtime"`
		XPad   []any  `json:"xpad"`
		Labels []any  `json:"labels"`
		Incrs  []any  `json:"incrs"`
	}
	runChartJS(t, `
const bar = chart.bars("--accent", "#63a8ff");
const options = chart.chartOptions({
    series: [{}, bar],
    width: 600,
    height: 180,
    xFormat: (h) => String(h).padStart(2, "0"),
});
const plot = { data: [[0, 1, 2, 3]] };
out({
    paths: typeof bar.paths,
    points: bar.points.show,
    fill: typeof bar.fill,
    xtime: options.scales.x.time,
    xpad: options.scales.x.range(plot, 0, 23),
    labels: options.axes[0].values(plot, [0, 6, 18]),
    incrs: options.axes[0].incrs,
});
`, &got)

	if got.Paths != "function" {
		t.Errorf("the bar series has no path builder (paths: %q) — uPlot will draw a line, "+
			"and a line between Sunday and Monday lies about a link that does not exist", got.Paths)
	}
	if got.Points {
		t.Error("points are on for the bars — a circle will hang over every bar")
	}
	if got.Fill != "function" {
		t.Errorf("the bar fill is not a function (%q) — the color is computed once when the "+
			"module loads and does not change with the theme", got.Fill)
	}
	if got.XTime != false {
		t.Errorf("the X axis is still a time axis (time: %v) — bucket labels print as dates "+
			"counted from the start of the epoch", got.XTime)
	}
	if len(got.XPad) != 2 || got.XPad[0] != -0.5 || got.XPad[1] != 23.5 {
		t.Errorf("the X axis runs exactly along the outer buckets (%v instead of [-0.5 23.5]) — "+
			"the outer bars are cut in half by the canvas edge", got.XPad)
	}
	want := []any{"00", "06", "18"}
	if len(got.Labels) != 3 || got.Labels[0] != want[0] || got.Labels[1] != want[1] || got.Labels[2] != want[2] {
		t.Errorf("X axis labels %v instead of %v — the bucket formatter was not asked", got.Labels, want)
	}
	if len(got.Incrs) == 0 {
		t.Fatal("the step of the bucket axis is left to auto-pick — it puts ticks every 2.5, " +
			"and there is no half hour of the day")
	}
	for _, incr := range got.Incrs {
		n, ok := incr.(float64)
		if !ok || n != float64(int(n)) {
			t.Errorf("a non-integer step %v on the bucket axis", incr)
		}
	}
}

func TestTokenAxisFitsItsLabels(t *testing.T) {
	var got struct {
		Kind   string  `json:"kind"`
		Wide   float64 `json:"wide"`
		Narrow float64 `json:"narrow"`
		Settle float64 `json:"settle"`
		Grow   float64 `json:"grow"`
		Right  string  `json:"right"`
	}
	runChartJS(t, `
const options = chart.chartOptions({
    series: [{}], width: 600, height: 180,
    yFormat: format.tokens,
    y2: { format: format.tokens },
});
globalThis.devicePixelRatio = 1;
const plot = {
    axes: [{}, { ticks: { size: 4 }, gap: 5, font: ["12px sans-serif"], _size: 40 }],
    ctx: { measureText: (s) => ({ width: s.length * 8 }) },
};
const size = typeof options.axes[1].size === "function" ? options.axes[1].size : () => -1;
out({
    kind: typeof options.axes[1].size,
    wide: size(plot, ["5 M", format.tokens(75.128e9)], 1, 0),
    narrow: size(plot, ["15%"], 1, 0),
    settle: size(plot, ["5 M"], 1, 2),
    grow: size(plot, [format.tokens(75.128e9)], 1, 2),
    right: typeof options.axes[2].size,
});
`, &got)

	if got.Kind != "function" {
		t.Fatalf("the axis width is given as %s — that is a number, and token labels get cut off", got.Kind)
	}
	if got.Wide != 65 {
		t.Errorf("the axis for 75.1 bn is %v wide instead of 65 — the longest label was not the one measured", got.Wide)
	}
	if got.Narrow >= got.Wide {
		t.Errorf("the axis for 15%% (%v) is no narrower than the one for tokens (%v) — the width "+
			"is not measured from the label but guessed", got.Narrow, got.Wide)
	}
	if got.Settle != 40 {
		t.Errorf("on the third pass the axis returned %v instead of the previous 40 — the axis "+
			"width changes the chart width, and that changes the axis width again, and the chart loops", got.Settle)
	}
	if got.Grow != 65 {
		t.Errorf("on the second pass with the label 75.1 bn the axis stayed %v instead of 65 — "+
			"the width froze on the previous set of ticks and cuts the new one", got.Grow)
	}
	if got.Right != "function" {
		t.Errorf("the right axis width is given as %s — the labels of the second scale get cut off the same way", got.Right)
	}
}

func TestPickGivesTheIndexUnderCursor(t *testing.T) {
	var got struct {
		Wired       bool  `json:"wired"`
		Picked      []int `json:"picked"`
		Bare        bool  `json:"bare"`
		Compact     any   `json:"compact"`
		CompactDraw any   `json:"compactDraw"`
		CompactBare any   `json:"compactBare"`
	}
	runChartJS(t, `
const picked = [];
const options = chart.chartOptions({
    series: [{}], width: 600, height: 180,
    onPick: (idx) => picked.push(idx),
});

const handlers = {};
const plot = {
    over: { addEventListener: (name, fn) => { handlers[name] = fn; } },
    cursor: { idx: 7 },
};
const ready = options.hooks && options.hooks.ready ? options.hooks.ready[0] : null;
if (ready) ready(plot);
const click = () => { if (handlers.click) handlers.click(); };
click();
plot.cursor.idx = null;
click();
plot.cursor.idx = 0;
click();

const compact = (onPick) => chart.chartOptions({ series: [{}], width: 90, height: 44, compact: true, onPick }).cursor;
out({
    wired: typeof ready === "function",
    picked,
    bare: chart.chartOptions({ series: [{}], width: 600, height: 180 }).hooks === undefined,
    compact: compact(() => {}).show,
    compactDraw: compact(() => {}).x,
    compactBare: compact(undefined).show,
});
`, &got)

	if !got.Wired {
		t.Fatal("the click on the plot area is not wired at all: with onPick the chart must get " +
			"a ready hook that puts a listener on the area")
	}
	if len(got.Picked) != 2 || got.Picked[0] != 7 || got.Picked[1] != 0 {
		t.Errorf("the clicks returned %v instead of [7 0] — either the index is wrong, or a miss "+
			"over the empty area counts as a pick, or the zero point was lost along with it", got.Picked)
	}
	if !got.Bare {
		t.Error("a chart with no onPick got uPlot hooks — the click is wired where nobody asked for it")
	}
	if got.Compact != true {
		t.Errorf("the cursor is off on a sparkline with onPick (show: %v) — uPlot then does not "+
			"attach the mouse at all and does not know the point under it, and the click stays silent", got.Compact)
	}
	if got.CompactDraw != false {
		t.Errorf("the cursor is drawn on a sparkline with onPick (x: %v) — a cursor line across "+
			"the metrics tile is out of place there", got.CompactDraw)
	}
	if got.CompactBare != false {
		t.Errorf("a sparkline with no onPick follows the mouse (show: %v) — it has no use for a cursor", got.CompactBare)
	}
}

func TestSecondScaleShowsBothSeries(t *testing.T) {
	var got struct {
		Scales []string `json:"scales"`
		Series []any    `json:"series"`
		Axes   int      `json:"axes"`
		Right  struct {
			Scale  string `json:"scale"`
			Side   int    `json:"side"`
			Grid   any    `json:"grid"`
			Labels []any  `json:"labels"`
		} `json:"right"`
		Left []any `json:"left"`
		Solo struct {
			Axes   int      `json:"axes"`
			Scales []string `json:"scales"`
		} `json:"solo"`
	}
	runChartJS(t, `
const inp = chart.area("--accent", "#63a8ff");
const outp = chart.bars("--accent-2", "#7fe3d4", { scale: "y2" });
const options = chart.chartOptions({
    series: [{}, inp, outp],
    width: 600,
    height: 180,
    range: () => [0, 75e9],
    yFormat: (v) => "L" + v,
    y2: { range: () => [0, 260e6], format: (v) => "R" + v },
});
const plot = { data: [[0, 1]] };
const solo = chart.chartOptions({ series: [{}, inp], width: 600, height: 180, range: () => [0, 1] });
out({
    scales: Object.keys(options.scales),
    series: [inp.scale ?? null, outp.scale ?? null],
    axes: options.axes.length,
    right: {
        scale: options.axes[2].scale,
        side: options.axes[2].side,
        grid: options.axes[2].grid.show,
        labels: options.axes[2].values(plot, [7]),
    },
    left: options.axes[1].values(plot, [7]),
    solo: { axes: solo.axes.length, scales: Object.keys(solo.scales) },
});
`, &got)

	if len(got.Scales) != 2 || got.Scales[0] != "y" || got.Scales[1] != "y2" {
		t.Fatalf("chart scales %v instead of [y y2] — the second series is measured against "+
			"a foreign range and flattens to zero", got.Scales)
	}
	if len(got.Series) != 2 || got.Series[0] != nil || got.Series[1] != "y2" {
		t.Errorf("series scales %v instead of [null y2] — a series cannot ask for the second scale", got.Series)
	}
	if got.Axes != 3 {
		t.Fatalf("%d axes instead of three — there is a second scale but no labels for it: "+
			"the chart carries two series and one step", got.Axes)
	}
	if got.Right.Scale != "y2" {
		t.Errorf("the right axis measures against scale %q, not y2 — it is labelled with the left step", got.Right.Scale)
	}
	if got.Right.Side != 1 {
		t.Errorf("the second axis sits on side %d, not on the right (1) — the two scales overlap on the left", got.Right.Side)
	}
	if got.Right.Grid != false {
		t.Errorf("the second axis has a grid of its own (grid: %v) — its lines follow its own "+
			"ticks and lay a ripple over the first", got.Right.Grid)
	}
	if len(got.Right.Labels) != 1 || got.Right.Labels[0] != "R7" {
		t.Errorf("right axis labels %v instead of [R7] — the formatter of the second scale was not asked", got.Right.Labels)
	}
	if len(got.Left) != 1 || got.Left[0] != "L7" {
		t.Errorf("left axis labels %v instead of [L7] — the second scale overrode the first", got.Left)
	}
	if got.Solo.Axes != 2 || len(got.Solo.Scales) != 1 {
		t.Errorf("a chart with no second scale got %d axes and scales %v — an empty right axis "+
			"eats width from every chart that already exists", got.Solo.Axes, got.Solo.Scales)
	}
}

func TestPaletteAskedWhenDrawing(t *testing.T) {
	var got struct {
		Stroke []string `json:"stroke"`
		Fill   []string `json:"fill"`
	}
	runChartJS(t, `
let color = "#111111";
globalThis.document = { documentElement: {} };
globalThis.getComputedStyle = () => ({ getPropertyValue: () => color });
const bar = chart.bars("--accent", "#63a8ff");
const first = [bar.stroke(), bar.fill()];
color = "#222222";
out({ stroke: [first[0], bar.stroke()], fill: [first[1], bar.fill()] });
`, &got)

	if len(got.Stroke) != 2 || got.Stroke[0] != "#111111" || got.Stroke[1] != "#222222" {
		t.Errorf("bar stroke %v — the color is taken once, and on a theme change the bars "+
			"keep the night palette", got.Stroke)
	}
	if len(got.Fill) != 2 || got.Fill[0] != "rgba(17, 17, 17, 0.5)" || got.Fill[1] != "rgba(34, 34, 34, 0.5)" {
		t.Errorf("bar fill %v — expected the current palette color at half opacity", got.Fill)
	}
}

func TestTokensReadShort(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0, "0"},
		{842, "842"},
		{1000, "1 k"},
		{1024, "1 k"},
		{1e9, "1 bn"},
		{75_128_000_000, "75.1 bn"},
		{260_138_999, "260 M"},
		{20_000_000_000, "20 bn"},
		{3_667_308_938, "3.7 bn"},
		{1.5e12, "1.5 tn"},
		{-2_500_000, "-2.5 M"},
	}
	values := make([]float64, len(cases))
	for i, c := range cases {
		values[i] = c.value
	}
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	runChartJS(t, "out("+string(raw)+".map(format.tokens));", &got)

	if len(got) != len(cases) {
		t.Fatalf("%d replies for %d values", len(got), len(cases))
	}
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("tokens(%v) = %q, expected %q", c.value, got[i], c.want)
		}
	}
}

func runChartJS(t *testing.T, script string, into any) {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the chart is run through the engine, not read out of the source")
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
	bundles := map[string]string{}
	for name, src := range map[string]string{"chart": "chart.js", "format": "format.js"} {
		entry, err := filepath.Abs(filepath.Join(webDir, "src", src))
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
			t.Fatalf("%s did not build: %v", src, built.Errors[0].Text)
		}
		path := filepath.Join(dir, name+".mjs")
		if err := os.WriteFile(path, built.OutputFiles[0].Contents, 0o600); err != nil {
			t.Fatal(err)
		}
		bundles[name] = "file://" + path
	}

	prelude := `
globalThis.document = { documentElement: {} };
globalThis.getComputedStyle = () => ({ getPropertyValue: () => "" });
const chart = await import(` + jsString(bundles["chart"]) + `);
const format = await import(` + jsString(bundles["format"]) + `);
const out = (value) => process.stdout.write(JSON.stringify(value));
`
	cmd := exec.Command(node, "--input-type=module", "-e", prelude+script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("the node reply did not parse: %v: %s", err, raw)
	}
}
