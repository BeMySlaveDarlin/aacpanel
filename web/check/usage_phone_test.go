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

const phoneUsage = "src/screens/usage.js"

func TestUsageOpensFromTheLogoSheet(t *testing.T) {
	files := srcFiles(t)
	shell := files["src/mobile/shell.js"]
	if shell == "" {
		t.Fatal("src/mobile/shell.js not found — the test looks in the wrong place")
	}

	sheet := shellSheet(t, files)
	if !strings.Contains(sheet, `onPage("usage")`) {
		t.Error("the logo sheet has no entry for the usage screen — there is no way into it at all: " +
			"it is kept out of the bottom menu on purpose")
	}
	if strings.Index(sheet, `onPage("devices")`) > strings.Index(sheet, `onPage("usage")`) {
		t.Error("the usage screen sits above devices in the sheet — a rarely used entry landed at the top of the list")
	}

	body := stripComments(shell)
	at := strings.Index(body, `page === "usage"`)
	if at < 0 {
		t.Fatal("the shell has no page branch for the usage screen — the sheet entry is drawn and opens nothing")
	}
	if tail := body[at:min(len(body), at+200)]; !strings.Contains(tail, "<${Usage}") {
		t.Error("the usage branch opens something other than the usage screen")
	}

	if strings.Contains(stripComments(files["src/ui/nav.js"]), "usage") {
		t.Error("the usage screen got into the bottom menu: it has five columns and all are taken, " +
			"and a sixth turns the navigation bar into a list of screens")
	}
}

// shellSheet returns the sheet behind the host name. It lives in the header,
// and the shell opens the page it names: without that hand-over every entry is
// drawn and opens nothing.
func shellSheet(t *testing.T, files map[string]string) string {
	t.Helper()

	shell := stripComments(files["src/mobile/shell.js"])
	if at := strings.Index(shell, "<${HostMenu}"); at < 0 ||
		!strings.Contains(shell[at:min(len(shell), at+600)], "setPage(id)") {
		t.Fatal("the mobile shell does not open the pages of the host menu — the test looks in the wrong place")
	}
	head := files["src/ui/header.js"]
	at := strings.Index(head, "<${Sheet}")
	if at < 0 {
		t.Fatal("the header has no host menu — the test looks in the wrong place")
	}
	end := strings.Index(head[at:], "<//>")
	if end < 0 {
		t.Fatal("the host menu is not closed — the test looks in the wrong place")
	}
	return head[at : at+end]
}

func TestUsageFiltersAreCountedOnTheServer(t *testing.T) {
	body := stripComments(srcFiles(t)[phoneUsage])
	if body == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", phoneUsage)
	}

	for _, want := range []struct{ call, what string }{
		{`useUsageSummary(filter, `, "the summary of the period"},
		{`useUsageSeries(filter, step, `, "the series of the chart"},
		{`useUsageBreakdown(filter, BY_PROJECT, `, "the list of projects"},
		{`useUsageBreakdown(filter, BY_SESSION, TOP_SESSIONS, `, "the top sessions"},
	} {
		if !strings.Contains(body, want.call) {
			t.Errorf("%s does not carry the filter to the server (%s): the numbers next to each other are counted over different selections",
				want.what, want.call)
		}
	}

	if !strings.Contains(body, "useUsageBreakdown(whole, BY_PROJECT, ") {
		t.Error("the project dropdown is not built by a request of its own: with a project in the filter " +
			"it is left with a single entry — the one already selected — and there is no way back out of it")
	}

	for _, narrow := range []string{".filter(", ".sort(", ".slice(", ".splice("} {
		clean := strings.Replace(body, `PERIODS.filter((p) => p.id !== "14d")`, "", 1)
		if strings.Contains(clean, narrow) {
			t.Errorf("the screen narrows what arrived itself (%s): the filter is computed on the server, "+
				"otherwise the rows under the summary answer a different question than it does", narrow)
		}
	}
}

func TestUsageTakesSharesFromTheServer(t *testing.T) {
	body := stripComments(srcFiles(t)[phoneUsage])
	if body == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", phoneUsage)
	}

	for _, want := range []string{"subShareIn", "subShareOut"} {
		if !strings.Contains(body, want) {
			t.Errorf("the subagent share is not taken from the server (no %s): counted over the input token "+
				"column it shows 95%% where 44 was promised", want)
		}
	}
	for _, column := range []string{"cacheRead", "cacheCreation", "sub.input", "sub.output", "sub.inbound"} {
		if strings.Contains(body, column) {
			t.Errorf("the screen counts the inbound or the share itself (%s): both values already have a "+
				"server-side number under the same name, and two roads to it diverge silently", column)
		}
	}
	if !strings.Contains(body, "inboundOf(row)") {
		t.Error("a list row takes the inbound not through the shared reader: the database sorts the rows by " +
			"that value, and a count of its own gives a table where the numbers argue with the order")
	}
}

func TestUsagePlotFillsGapsWithZero(t *testing.T) {
	var got struct {
		T   []int64   `json:"t"`
		V   []float64 `json:"v"`
		Day []int64   `json:"day"`
	}
	runPhoneUsageJS(t, `
const hour = 3600;
const at = (sec) => new Date(sec * 1000).toISOString();
const points = [
    { at: at(hour * 10), inbound: 100 },
    { at: at(hour * 11), inbound: 200 },
    { at: at(hour * 14), inbound: 300 },
];
const [t, v] = usage.plot(points, "hour");
const day = usage.plot([
    { at: at(0), inbound: 1 },
    { at: at(86400 + hour), inbound: 2 },
], "day")[0];
out({ t, v, day });
`, &got)

	want := []int64{3600 * 10, 3600 * 11, 3600 * 12, 3600 * 13, 3600 * 14}
	if len(got.T) != len(want) {
		t.Fatalf("a series of %d points against %d hours in the period: the gap stayed a gap, and the chart "+
			"joins the evening to the morning with a straight line", len(got.T), len(want))
	}
	for i, sec := range want {
		if got.T[i] != sec {
			t.Fatalf("point %d sits at %d instead of %d — the buckets drifted apart", i, got.T[i], sec)
		}
	}
	if got.V[2] != 0 || got.V[3] != 0 {
		t.Errorf("the missing hours arrived as %v and %v instead of zeroes: null here would mean "+
			"the collector was silent, and it was not — there simply was no usage", got.V[2], got.V[3])
	}
	if got.V[0] != 100 || got.V[4] != 300 {
		t.Errorf("the point values slipped: %v instead of 100 and 200, and %v instead of 300", got.V[0], got.V[4])
	}
	if len(got.Day) != 2 {
		t.Errorf("a day with a clock change was split into %d buckets: a spare zero day stood next to "+
			"the real one because that day was an hour longer", len(got.Day))
	}
}

func runPhoneUsageJS(t *testing.T, script string, into any) {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the series arithmetic is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}

	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "usage.js"))
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
		t.Fatalf("the usage screen did not build: %v", built.Errors[0].Text)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.mjs")
	if err := os.WriteFile(path, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	prelude := `
globalThis.document = { documentElement: {}, createElement: () => ({ style: {} }) };
globalThis.getComputedStyle = () => ({ getPropertyValue: () => "" });
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
		t.Fatalf("the node output did not parse: %v: %s", err, raw)
	}
}

func TestUsageChipsStayFour(t *testing.T) {
	body := stripComments(srcFiles(t)[phoneUsage])
	if !strings.Contains(body, "PERIODS.filter(") {
		t.Error("the screen does not take the periods from the shared list: ids of its own diverge from " +
			"the desktop at the day boundary, and they diverge silently")
	}
	if regexp.MustCompile(`SPANS\s*=\s*\[`).MatchString(body) {
		t.Error("the periods are declared as a list of their own — a second dictionary of periods in the panel")
	}
}
