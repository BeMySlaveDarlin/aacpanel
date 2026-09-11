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

	esbuild "github.com/evanw/esbuild/pkg/api"

	"aacpanel/internal/webbuild"
)

func TestSubagentShareComesFromTheServer(t *testing.T) {
	files := usageFront(t)

	models := files["src/desktop/usage/tops.js"]
	if !strings.Contains(models, "subShareIn") {
		t.Error("the models tile does not take the subagent share from the server: computed on screen, " +
			"it shows 95% where the spend is 44%, and it looks plausible")
	}

	own := regexp.MustCompile(`sub\.\w+\s*/|/\s*[\w.]*sub\.\w+`)
	for path, body := range files {
		if own.MatchString(stripComments(body)) {
			t.Errorf("%s divides the subagent numbers itself — that is a second share formula, "+
				"and it drifts from the server one silently", path)
		}
	}
}

func TestInboundAndHitComeFromTheServer(t *testing.T) {
	files := usageFront(t)

	sum := regexp.MustCompile(`cacheRead\s*\|\|\s*0\)?\s*\+|\+\s*\(?\w*\.?cacheCreation`)
	own := regexp.MustCompile(`cacheRead[^;\n]*\)?\s*/|/\s*\(?[\w.]*cacheRead`)
	for _, path := range sortedKeys(files) {
		body := stripComments(files[path])
		if sum.MatchString(body) {
			t.Errorf("%s sums the inbound itself: that is a second place of counting, and how much "+
				"was spent becomes two different numbers", path)
		}
		if own.MatchString(body) {
			t.Errorf("%s divides the cache read itself: that is a second efficiency formula, and with "+
				"an incomplete denominator it shows a hundred percent for nine sessions out of ten", path)
		}
	}

	client := files["src/data/usage.js"]
	for _, mark := range []string{"export function inboundOf", "export function hitOf"} {
		if !strings.Contains(client, mark) {
			t.Fatalf("the usage client has no %q — there will be nowhere to count, and the widgets will count on their own", mark)
		}
	}
}

func TestQueryKeyDoesNotTickWithTheClock(t *testing.T) {
	var got struct {
		First    string `json:"first"`
		Later    string `json:"later"`
		Explicit string `json:"explicit"`
		All      string `json:"later_all"`
	}
	runUsageJS(t, `
const Real = Date;
const freeze = (ms) => {
    globalThis.Date = class extends Real {
        constructor(...args) { super(...(args.length ? args : [ms])); }
        static now() { return ms; }
    };
};
freeze(Real.parse("2026-09-10T14:00:05Z"));
const first = usage.usageQuery({ period: "today", tz: "" }).toString();
const firstAll = usage.usageQuery({ period: "all", tz: "" }).toString();
freeze(Real.parse("2026-09-10T14:01:37Z"));
out({
    first,
    later: usage.usageQuery({ period: "today", tz: "" }).toString(),
    later_all: firstAll + " :: " + usage.usageQuery({ period: "all", tz: "" }).toString(),
    explicit: usage.usageQuery({ period: "today", from: 1000, to: 2000, tz: "" }).toString(),
});
`, &got)

	if got.First != got.Later {
		t.Errorf("the query string ticks along with the clock: %q against %q — this is the hook "+
			"dependency key, and every redraw of the screen re-requests every handler", got.First, got.Later)
	}
	if parts := strings.Split(got.All, " :: "); parts[0] != parts[1] {
		t.Errorf("the all period ticks along with the clock: %q against %q", parts[0], parts[1])
	}
	if !strings.Contains(got.Explicit, "to=2000") {
		t.Errorf("the explicit upper bound did not go out: %s — the hour picked on the chart "+
			"opens up to the current moment", got.Explicit)
	}
}

func TestOutsideTheMapAlwaysStands(t *testing.T) {
	files := usageFront(t)
	tops := stripComments(files["src/desktop/usage/tops.js"])
	if tops == "" {
		t.Fatal("src/desktop/usage/tops.js not found — the test is looking in the wrong place")
	}

	if !strings.Contains(tops, `rows.find((r) => r.outside)`) {
		t.Error("the projects tile does not look for the outside the map row by its flag: it will not " +
			"find it by the label — the label arrives from the server in a wording of its own")
	}
	if !strings.Contains(tops, `${outside && html`) {
		t.Error("the outside the map row is not drawn apart from the list: caught by the shared limit, " +
			"it disappears from the screen together with a tenth of the spend")
	}
	if regexp.MustCompile(`BY_PROJECT,\s*[1-9]`).MatchString(tops) {
		t.Error("the projects breakdown is requested with a limit: outside the map comes back as a zero " +
			"row when it does not fit, and it shows zero instead of a tenth of the spend")
	}

	layer := stripComments(files["src/desktop/usage/breakdown.js"])
	if !strings.Contains(layer, "row.outside") {
		t.Error("the breakdown in the layer does not tell the outside the map row apart by its flag")
	}
}

func TestToolsTopStaysATop(t *testing.T) {
	files := usageFront(t)
	tops := stripComments(files["src/desktop/usage/tops.js"])
	layer := stripComments(files["src/desktop/usage/breakdown.js"])

	widget := regexp.MustCompile(`TOOL_ROWS = (\d+)`).FindStringSubmatch(tops)
	if widget == nil {
		t.Fatal("the tools tile has no row limit — the test is looking in the wrong place")
	}
	if widget[1] != "5" {
		t.Errorf("the tools tile holds %s rows instead of five: the ceiling here is a decision, "+
			"not a matter of how much fitted", widget[1])
	}
	full := regexp.MustCompile(`TOOL_ROWS = (\d+)`).FindStringSubmatch(layer)
	if full == nil {
		t.Fatal("the tools breakdown has no row limit — the test is looking in the wrong place")
	}
	if full[1] != "25" {
		t.Errorf("the tools breakdown holds %s rows instead of twenty-five: the full list "+
			"answers a different question", full[1])
	}
	for path, body := range map[string]string{"tile": tops, "breakdown": layer} {
		if !strings.Contains(body, "restNames") {
			t.Errorf("the tools %s does not show the tail: a list cut silently "+
				"reads as the whole period", path)
		}
	}

	if !strings.Contains(tops, "rate > overall") {
		t.Error("the error share in the tile is colored without comparing against the overall one — every row turns colored")
	}
	if !strings.Contains(layer, "rate > overall") {
		t.Error("the error share in the breakdown is colored without comparing against the overall one")
	}
}

func TestFirstScanNamesItsPrice(t *testing.T) {
	files := usageFront(t)
	today := stripComments(files["src/desktop/usage/today.js"])
	scan := stripComments(files["src/desktop/usage/scan.js"])

	if !strings.Contains(today, "est.files") || !strings.Contains(today, "gb(est.bytes)") {
		t.Error("the button of the first collection does not name its price: how many files and how many " +
			"gigabytes were found is the only thing a person decides by whether to press it")
	}
	if !strings.Contains(today, "scanProgress") {
		t.Error("the tile does not show the progress of the round underway: taking long and stuck " +
			"become indistinguishable")
	}
	if !strings.Contains(scan, "row.filesDone") || !strings.Contains(scan, "row.files") {
		t.Error("the collection layer does not count files by contour: a person thinks in contours, " +
			"not in files, and a shared counter does not say where exactly it stopped")
	}
	if !strings.Contains(scan, "est.contours") {
		t.Error("the collection layer does not show the price by contour before the start")
	}
	client := stripComments(files["src/data/usage.js"])
	if !strings.Contains(client, "estimate: !was.current") {
		t.Error("the collection poll asks for the estimate during the round too — an extra walk over the disk " +
			"takes from the parsing exactly while the parsing runs")
	}
}

func TestWidgetsKeepTheirPlaceInTheGrid(t *testing.T) {
	files := usageFront(t)
	home := stripComments(files["src/desktop/home.js"])
	if home == "" {
		t.Fatal("src/desktop/home.js not found — the test is looking in the wrong place")
	}

	order := regexp.MustCompile(`<\$\{(Today|Cache|Hours|Projects|Models|Tools)\}`).FindAllStringSubmatch(home, -1)
	got := make([]string, 0, len(order))
	for _, m := range order {
		got = append(got, m[1])
	}
	want := []string{"Today", "Cache", "Hours", "Projects", "Models", "Tools"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the dashboard tiles stand in the order %v while the design puts them %v: the order "+
			"goes from how much was spent to what on, and the grid rows are built for it", got, want)
	}

	sizes := map[string]string{
		"today.js|Today":   "w3 h1",
		"today.js|Cache":   "w3 h1",
		"hours.js|Hours":   "w8 h2",
		"tops.js|Projects": "w4 h2",
		"tops.js|Models":   "w6 h1",
		"tops.js|Tools":    "w6 h1",
	}
	for key, size := range sizes {
		parts := strings.SplitN(key, "|", 2)
		body := stripComments(files["src/desktop/usage/"+parts[0]])
		at := strings.Index(body, "export function "+parts[1])
		if at < 0 {
			t.Errorf("tile %s is not found in %s — the test is looking in the wrong place", parts[1], parts[0])
			continue
		}
		tail := body[at:]
		if next := strings.Index(tail[1:], "export function "); next > 0 {
			tail = tail[:next]
		}
		if !strings.Contains(tail, `span="`+size+`"`) {
			t.Errorf("tile %s does not stand at size %q: across twelve columns that changes "+
				"the whole row, not one tile", parts[1], size)
		}
	}
}

func TestRowClickDoesNotReachTheWidget(t *testing.T) {
	parts := stripComments(usageFront(t)["src/desktop/usage/parts.js"])
	if parts == "" {
		t.Fatal("src/desktop/usage/parts.js not found — the test is looking in the wrong place")
	}
	at := strings.Index(parts, "export function Row(")
	if at < 0 {
		t.Fatal("there is no list row — the test is looking in the wrong place")
	}
	if !strings.Contains(parts[at:], "stopPropagation") {
		t.Error("a click on a row bubbles up to the tile: it opens its own breakdown over the " +
			"breakdown of the row, and the row will look dead")
	}
	if !strings.Contains(parts, `onClick=${stop}`) {
		t.Error("the buttons in the tile header do not stop the bubbling: the period and the recollection " +
			"will open the breakdown along the way")
	}
}

func TestPeriodBoundsAreCutByTheBrowser(t *testing.T) {
	var got struct {
		Today    string `json:"today"`
		Week     string `json:"week"`
		All      string `json:"all"`
		Midnight int64  `json:"midnight"`
		From     int64  `json:"from"`
		Step14   string `json:"step14"`
		Step30   string `json:"step30"`
		Explicit string `json:"explicit"`
	}
	runUsageJS(t, `
const now = new Date();
const midnight = new Date(now);
midnight.setHours(0, 0, 0, 0);
const today = usage.usageQuery({ period: "today", tz: "" });
const week = usage.usageQuery({ period: "7d", tz: "" });
const all = usage.usageQuery({ period: "all", tz: "" });
const explicit = usage.usageQuery({ period: "30d", from: 1000, to: 2000, tz: "" });
out({
    today: today.toString(),
    week: week.toString(),
    all: all.toString(),
    midnight: Math.floor(midnight.getTime() / 1000),
    from: Number(today.get("from")),
    step14: usage.stepFor("14d"),
    step30: usage.stepFor("30d"),
    explicit: explicit.toString(),
});
`, &got)

	if strings.Contains(got.Today, "period=today") {
		t.Error("today goes to the server by name: the day is cut there by the zone of the service, " +
			"and a person in another zone sees somebody else's day")
	}
	if got.From != got.Midnight {
		t.Errorf("today starts at %d while local midnight is %d: a period counted by subtracting "+
			"a day from now loses the whole morning at noon", got.From, got.Midnight)
	}
	if !strings.Contains(got.Week, "from=") {
		t.Errorf("the week period goes out with no lower bound: %s — the server will cut the day "+
			"by its own zone, and someone in another zone sees somebody else's week", got.Week)
	}
	if !strings.Contains(got.All, "period=all") {
		t.Errorf("all goes out as a bound rather than as a flag: %s — a ceiling here will one day "+
			"hide a person's own past from them", got.All)
	}
	if !strings.Contains(got.Explicit, "from=1000") || !strings.Contains(got.Explicit, "to=2000") {
		t.Errorf("explicit bounds are not stronger than the period: %s — the hour picked on the chart "+
			"opens for the whole month", got.Explicit)
	}
	if got.Step14 != "hour" {
		t.Errorf("two weeks are drawn with a step of %q instead of an hour", got.Step14)
	}
	if got.Step30 != "day" {
		t.Errorf("thirty days are drawn with a step of %q: seven hundred and twenty points across eight "+
			"grid columns is noise, not a series", got.Step30)
	}
}

func TestPollingDoesNotWipeTheNumbers(t *testing.T) {
	var got struct {
		Same    string `json:"same"`
		Stale   bool   `json:"stale"`
		Kept    bool   `json:"kept"`
		Changed string `json:"changed"`
		Cold    string `json:"cold"`
	}
	runUsageJS(t, `
const ready = { kind: "ready", data: { inbound: 42 } };
const same = usage.waiting(ready, true);
const changed = usage.waiting(ready, false);
const cold = usage.waiting({ kind: "loading" }, true);
out({
    same: same.kind,
    stale: Boolean(same.stale),
    kept: same.data === ready.data,
    changed: changed.kind,
    cold: cold.kind,
});
`, &got)

	if got.Same != "ready" || !got.Kept {
		t.Errorf("a repeat poll takes the numbers away (%s, data kept: %v): the screen will blink "+
			"for the whole round, and the end of the round is counted on every tick", got.Same, got.Kept)
	}
	if !got.Stale {
		t.Error("numbers being refreshed carry no mark: there will be nothing to show that they are already on the way")
	}
	if got.Changed != "loading" {
		t.Errorf("a change of query keeps the previous numbers (%s): the screen shows the numbers "+
			"of one period under the label of another", got.Changed)
	}
	if got.Cold != "loading" {
		t.Errorf("the first request shows %q instead of loading", got.Cold)
	}

	client := stripComments(srcFiles(t)["src/data/usage.js"])
	if !strings.Contains(client, "waiting(prev, same)") {
		t.Error("useUsage does not ask waiting before the request: the rule exists and there is nowhere to apply it")
	}
}

func TestFilterRidesEveryQuery(t *testing.T) {
	var got struct {
		Query string `json:"query"`
		Zone  string `json:"zone"`
	}
	runUsageJS(t, `
const q = usage.usageQuery({
    period: "30d",
    contours: ["personal", "client"],
    group: "Beta",
    project: "/srv/proj/panel",
    outside: true,
    tz: "Asia/Tokyo",
});
out({ query: q.toString(), zone: usage.usageQuery({ period: "30d" }).get("tz") || "" });
`, &got)

	for _, want := range []string{"contour=personal", "contour=client", "group=Beta", "outside=1", "tz=Asia%2FTokyo"} {
		if !strings.Contains(got.Query, want) {
			t.Errorf("the dashboard query has no %q: %s", want, got.Query)
		}
	}
	if got.Zone == "" {
		t.Error("the zone does not go out with the query on its own: the server cuts daily buckets by zone, " +
			"and only the browser can name it")
	}
}

func TestUsageAsksInOnePlace(t *testing.T) {
	const client = "src/data/usage.js"
	files := srcFiles(t)

	for _, path := range sortedKeys(files) {
		if path == client {
			continue
		}
		body := stripComments(files[path])
		if strings.Contains(body, `"/api/usage`) {
			t.Errorf("%s calls the usage handler past the client (%s): a second set of calls "+
				"drifts from the first on the very first new filter field", path, client)
		}
		if !strings.Contains(body, client[len("src/"):]) && !strings.Contains(body, "data/usage.js") {
			continue
		}
		if strings.Contains(body, "setInterval(") || strings.Contains(body, "setTimeout(") {
			t.Errorf("%s starts a timer of its own on top of the usage client: the poll rate of the round "+
				"has to be one for both dashboards", path)
		}
		if strings.Contains(body, "status === 503") {
			t.Errorf("%s handles the refusal of the usage handler itself: six slightly different texts "+
				"about one case mean that one day one of them lies", path)
		}
	}

	body := stripComments(files[client])
	for _, mark := range []string{"export function useUsage", "export function useUsageScan", "setInterval("} {
		if !strings.Contains(body, mark) {
			t.Errorf("the usage client has no %q — the machinery moved back out into the screens", mark)
		}
	}
	for _, mark := range []string{"SCAN_POLL_RUNNING_MS =", "SCAN_POLL_IDLE_MS ="} {
		if !strings.Contains(body, mark) {
			t.Errorf("the poll rate of the round is not named by a constant %q: picked in place, "+
				"it drifts from the second screen silently", mark)
		}
	}
}

func usageFront(t *testing.T) map[string]string {
	t.Helper()
	files := srcFiles(t)
	out := map[string]string{}
	for path, body := range files {
		if strings.HasPrefix(path, "src/desktop/usage/") || path == "src/desktop/home.js" || path == "src/data/usage.js" {
			out[path] = body
		}
	}
	if len(out) < 5 {
		t.Fatalf("%d dashboard sources found — the test is looking in the wrong place", len(out))
	}
	return out
}

func runUsageJS(t *testing.T, script string, into any) {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the usage client is run through the engine, not read out of the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}

	entry, err := filepath.Abs(filepath.Join(webDir, "src", "data", "usage.js"))
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
		t.Fatalf("the usage client did not build: %v", built.Errors[0].Text)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.mjs")
	if err := os.WriteFile(path, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	prelude := `
const usage = await import(` + jsString("file://"+path) + `);
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
