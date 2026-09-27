package check

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestProbeStateHasOneOwner(t *testing.T) {
	files := srcFiles(t)
	owner := ""
	for path, body := range files {
		if strings.Contains(body, "export function probeState(") {
			owner = path
			break
		}
	}
	if owner == "" {
		t.Fatal("probeState not found — nobody decides the state of a probe")
	}

	for _, path := range sortedKeys(files) {
		if path == owner {
			continue
		}
		if strings.Contains(withoutComments(files[path]), "last.ok") {
			t.Errorf("%s reads last.ok past probeState (%s) — two places decide whether a probe answers, "+
				"and they will diverge silently", path, owner)
		}
	}
}

func TestLogTimeColumnAppearsOnlyWithTime(t *testing.T) {
	const panelFile = "src/desktop/panels/logs.js"
	body := srcFiles(t)[panelFile]
	if body == "" {
		t.Fatalf("%s not found", panelFile)
	}
	code := withoutComments(body)

	if !strings.Contains(code, "lines.some((l) => l.time)") {
		t.Errorf("%s: the pane never asks the feed whether there is any time at all — with a stream "+
			"without stamps an empty column stays and takes width away from the message", panelFile)
	}
	if !strings.Contains(code, "${timed && html`<span class=\"dklogtime\">") {
		t.Errorf("%s: the time column is drawn always — the flag saying the feed has time "+
			"is declared and never used", panelFile)
	}
	row := jsUntil(t, panelFile, code, `class="dklogtime"`, "</span>")
	if !strings.Contains(row, "l.time") {
		t.Errorf("%s: the time column holds something other than the line time — the test is useless, check the anchor", panelFile)
	}

	hook := srcFiles(t)["src/logs.js"]
	if hook == "" {
		t.Fatal("src/logs.js not found")
	}
	if strings.Contains(code, "toLocaleTimeString") {
		t.Errorf("%s: the pane converts time itself while rendering — the work is done on every "+
			"re-render of the feed instead of once per line", panelFile)
	}
	if !strings.Contains(withoutComments(hook), "toLocaleTimeString") {
		t.Error("src/logs.js: the time is not converted to local — docker sends UTC, and the log " +
			"diverges from the action log by the timezone offset")
	}
}

func TestProbeRowSpeaksFromTheHandle(t *testing.T) {
	const file = "src/desktop/machine.js"
	body := srcFiles(t)[file]
	if body == "" {
		t.Fatalf("%s not found", file)
	}
	code := withoutComments(body)
	row := jsUntil(t, file, code, "function ProbeRow(", "\nexport function MachineCenter")

	if strings.Contains(row, "last.ms") {
		t.Errorf("%s: the probe row reads `last.ms` — neither the handle nor the agent snapshot has "+
			"such a field, and the column silently shows a dash", file)
	}
	need := map[string]string{
		"last.latencyMs":          "the latency of the last answer",
		"probe.target":            "what exactly was probed",
		"ago(":                    "when it was last polled",
		"every(probe.intervalSec": "how often the polling runs",
		"OUTCOME[":                "what the answer was — in words, not by the color of a dot",
		"probe.failStreak":        "how many failures in a row",
	}
	for frag, why := range need {
		if !strings.Contains(row, frag) {
			t.Errorf("%s: the probe row has no %q — it never says %s", file, frag, why)
		}
	}

	for _, word := range []string{"disabled", "not checked yet"} {
		if !strings.Contains(row, word) {
			t.Errorf("%s: the row does not tell %q from a failure — on the screen both give a grey dot "+
				"and silence", file, word)
		}
	}
}

func TestLimitsStalenessHasOneOwner(t *testing.T) {
	files := srcFiles(t)
	owner := ""
	for _, path := range sortedKeys(files) {
		if strings.Contains(files[path], "export function staleLimits(") {
			owner = path
			break
		}
	}
	if owner == "" {
		t.Fatal("staleLimits not found — nobody decides whether the limits snapshot has gone stale")
	}

	for _, path := range sortedKeys(files) {
		if path == owner {
			continue
		}
		body := withoutComments(files[path])
		if strings.Contains(body, "LIMITS_STALE_SEC") {
			t.Errorf("%s knows the staleness threshold past %s — two places decide whether the numbers are fresh",
				path, owner)
		}
		if regexp.MustCompile(`\.ageSec\s*[<>]=?\s*\d`).MatchString(body) {
			t.Errorf("%s compares the age of the limits snapshot against a number of its own — the threshold has to live in %s",
				path, owner)
		}
	}

	const footer = "src/desktop/sessions.js"
	if body := files[footer]; !strings.Contains(body, "staleLimits(") {
		t.Errorf("%s never asks about the age of the snapshot — the footer again shows yesterday's numbers "+
			"next to fresh ones", footer)
	}
}

type deskMeter struct {
	Text  string `json:"text"`
	Level string `json:"level"`
	Tip   string `json:"tip"`
}

type deskContourHead struct {
	Name    string      `json:"name"`
	Meters  []deskMeter `json:"meters"`
	Fits    bool        `json:"fits"`
	OneLine bool        `json:"oneLine"`
}

type deskQuiet struct {
	Head string `json:"head"`
	Rows []struct {
		Name   string `json:"name"`
		Meters int    `json:"meters"`
		Old    bool   `json:"old"`
		Tip    string `json:"tip"`
	} `json:"rows"`
	Height int `json:"height"`
}

// The limit of a contour stands in its heading in the sessions column, beside
// the sessions spending it, rather than in a block under the list that grows
// by a contour's worth of lines with each contour. When a window resets is
// said on the line only once it runs out; a contour with no live session has
// no heading and keeps one line under the list.
func TestDeskLimitsStandInTheContourHeading(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: whether the heading holds its limits is the stylesheet's business too — run make front first")
	}
	var got struct {
		Heads   []deskContourHead `json:"heads"`
		Quiet   *deskQuiet        `json:"quiet"`
		AllLive struct {
			Heads int        `json:"heads"`
			Quiet *deskQuiet `json:"quiet"`
		} `json:"allLive"`
	}
	runWideFixture(t, "desklimits.html", &got)

	if len(got.Heads) != 2 {
		t.Fatalf("the column shows %d contour headings, expected the two with live sessions: %+v", len(got.Heads), got.Heads)
	}
	for _, head := range got.Heads {
		if len(head.Meters) != 2 {
			t.Errorf("%q: the heading holds %d windows of the limit, expected five hours and seven days", head.Name, len(head.Meters))
			continue
		}
		if !head.Fits || !head.OneLine {
			t.Errorf("%q: the limit does not fit the heading on one line (fits %v, one line %v) — a long name has to give way",
				head.Name, head.Fits, head.OneLine)
		}
	}
	algo, personal := got.Heads[0], got.Heads[1]
	if len(algo.Meters) == 2 {
		five := algo.Meters[0]
		if five.Level != "crit" || !strings.Contains(five.Text, "93%") || !strings.Contains(five.Text, "1 h") {
			t.Errorf("a five-hour window at 93%% reads %q (%s): it has to be marked and say when it resets", five.Text, five.Level)
		}
	}
	if len(personal.Meters) == 2 {
		five := personal.Meters[0]
		if strings.Contains(five.Text, " h") || five.Level != "" {
			t.Errorf("a window at 12%% reads %q (%s): the reset time belongs in the tip until the window runs out", five.Text, five.Level)
		}
		if !strings.Contains(five.Tip, "resets in 4 h") {
			t.Errorf("the tip of a window at 12%% says %q — when it resets is said there always", five.Tip)
		}
	}

	q := got.Quiet
	if q == nil || q.Head != "no live sessions" || len(q.Rows) != 1 || q.Rows[0].Name != "Evirma" {
		t.Fatalf("the contour with no live session is not under the list on a line of its own: %+v", q)
	}
	if q.Rows[0].Meters != 2 {
		t.Errorf("the quiet contour shows %d windows of its limit", q.Rows[0].Meters)
	}
	if !q.Rows[0].Old || !strings.Contains(q.Rows[0].Tip, "from 1 h ago") {
		t.Errorf("numbers an hour old are not dimmed with their age in the tip: old %v, tip %q", q.Rows[0].Old, q.Rows[0].Tip)
	}
	if q.Height > 90 {
		t.Errorf("one quiet contour takes %d px under the list — the block the limits left grew back", q.Height)
	}

	if got.AllLive.Heads != 3 {
		t.Fatalf("with a session in every contour the column shows %d headings — the check looks in the wrong place", got.AllLive.Heads)
	}
	if got.AllLive.Quiet != nil {
		t.Errorf("every contour has a live session, and a block of limits still stands under the list: %+v", got.AllLive.Quiet)
	}
}

// A temperature line shows up only where a sensor stands behind it: the
// formatter takes the value as it is and adds nothing for a missing one, and
// the screens never spell a degree themselves, so there is no place for a
// zero or a dash to come from.
func TestTemperatureIsSaidOnlyWithASensor(t *testing.T) {
	files := srcFiles(t)
	const formatter = "src/format.js"
	format := withoutComments(files[formatter])
	if format == "" {
		t.Fatalf("%s not found", formatter)
	}
	join := jsUntil(t, formatter, format, "export function withDegrees(", "\n}")
	if !strings.Contains(join, "temp == null ? text") {
		t.Errorf("%s: withDegrees does not hand the line back untouched for a missing sensor — "+
			"a machine without one gets a zero or a dash", formatter)
	}

	for _, path := range sortedKeys(files) {
		if path == formatter {
			continue
		}
		body := withoutComments(files[path])
		if strings.Contains(body, "°C") {
			t.Errorf("%s spells a temperature by itself — the unit lives in %s, one place", path, formatter)
		}
		if regexp.MustCompile(`Temps?\s*\|\|\s*0`).MatchString(body) {
			t.Errorf("%s turns a missing sensor into a zero", path)
		}
	}

	tiles := map[string][]string{
		"src/screens/resources.js": {
			"withDegrees(`load ${host.load.map((n) => n.toFixed(2)).join(\" · \")}`, host.cpuTemp)",
			"withDegrees(`${bytes(host.mem.used)} of ${bytes(host.mem.total)}`, host.memTemp)",
			"withDegrees(`${bytes(root.free)} free`, hottest)",
			"devices.map((d) => html`",
		},
		"src/desktop/machine.js": {
			"h.cpuTemp)",
			"h.memTemp)",
			"h.diskTemp)",
			"h.cpuTemp != null && html`",
			"h.memTemp != null && html`",
			"(h.diskTemps || []).map((d) => html`",
		},
	}
	for path, need := range tiles {
		body := withoutComments(files[path])
		if body == "" {
			t.Fatalf("%s not found", path)
		}
		for _, frag := range need {
			if !strings.Contains(body, frag) {
				t.Errorf("%s: no %q — a tile says nothing about its temperature even with a sensor", path, frag)
			}
		}
	}
}

// The uptime is put into words by one formatter: a second arithmetic on the
// seconds would give the phone "up 3 days 4 h" and the wide screen "3 d".
func TestUptimeIsSaidByOneFormatter(t *testing.T) {
	files := srcFiles(t)
	const formatter = "src/format.js"
	format := withoutComments(files[formatter])
	if !strings.Contains(format, "export function uptime(") {
		t.Fatalf("%s: uptime is not exported — nobody puts the seconds into words", formatter)
	}

	for _, path := range sortedKeys(files) {
		if path == formatter {
			continue
		}
		if regexp.MustCompile(`\buptime\b[^\n]*86400`).MatchString(withoutComments(files[path])) {
			t.Errorf("%s counts days out of the uptime by itself past %s", path, formatter)
		}
	}
	said := map[string][]string{
		"src/screens/machine.js": {"const up = uptime(", "<span class=\"hint\">${up}</span>"},
		"src/desktop/machine.js": {"${uptime(h.uptime)}"},
		"src/desktop/home.js":    {"uptime(h.uptime)"},
	}
	for path, need := range said {
		body := withoutComments(files[path])
		for _, frag := range need {
			if !strings.Contains(body, frag) {
				t.Errorf("%s: no %q — the screen never says how long the machine has been up", path, frag)
			}
		}
	}
}
