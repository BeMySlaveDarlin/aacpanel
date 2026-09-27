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

type deskRing struct {
	Text  string `json:"text"`
	Level string `json:"level"`
}

type deskSection struct {
	Name    string     `json:"name"`
	Head    string     `json:"head"`
	Rings   []deskRing `json:"rings"`
	Old     bool       `json:"old"`
	Fits    bool       `json:"fits"`
	OneLine bool       `json:"oneLine"`
	Live    []string   `json:"live"`
	Closed  int        `json:"closed"`
	Empty   string     `json:"empty"`
	Bottom  float64    `json:"bottom"`
}

type deskShelfRow struct {
	Name    string `json:"name"`
	Contour string `json:"contour"`
	About   string `json:"about"`
	When    string `json:"when"`
}

// Every contour shown in the sessions column at a desk has its section: a
// heading with its name and two rings of its limit — the share of each window
// inside, the window beside, no count of live sessions — which a press opens
// into the details of both windows, each with when it resets; and its live
// sessions, or a word that nothing lives in it. No closed conversation stands
// among them: the closed ones of every contour shown share a shelf below all
// the contours, asked for in one request that leaves the live ones out,
// newest first, each with its contour and what it was about.
func TestDeskColumnShowsEveryContourWithItsLimitAndTheClosedOnAShelf(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: whether the heading holds its rings is the stylesheet's business too — run make front first")
	}
	var got struct {
		Sections  []deskSection  `json:"sections"`
		Asked     []string       `json:"asked"`
		Shelf     []deskShelfRow `json:"shelf"`
		ShelfTop  float64        `json:"shelfTop"`
		ShelfLast bool           `json:"shelfLast"`
		Pop       []string       `json:"pop"`
		Closed    bool           `json:"closed"`
		OldNote   string         `json:"oldNote"`
		NoteOnTop bool           `json:"noteOnTop"`
	}
	runWideFixture(t, "desklimits.html", &got)

	if len(got.Sections) != 3 {
		t.Fatalf("the column shows %d contours, expected all three of the map, with live sessions or without: %+v", len(got.Sections), got.Sections)
	}
	evirma, algo, personal := got.Sections[0], got.Sections[1], got.Sections[2]
	for _, sec := range got.Sections {
		if len(sec.Rings) != 2 || !strings.HasSuffix(sec.Rings[0].Text, "5h") || !strings.HasSuffix(sec.Rings[1].Text, "7d") {
			t.Errorf("%q: the heading holds rings %+v, expected five hours and seven days", sec.Name, sec.Rings)
		}
		if strings.Contains(sec.Head, "live") {
			t.Errorf("%q: the heading still counts live sessions: %q", sec.Name, sec.Head)
		}
		if !sec.Fits || !sec.OneLine {
			t.Errorf("%q: the rings do not fit the heading on one line (fits %v, one line %v)", sec.Name, sec.Fits, sec.OneLine)
		}
		if sec.Closed != 0 {
			t.Errorf("%q holds %d closed conversations among its live sessions — the closed ones stand on the shelf", sec.Name, sec.Closed)
		}
	}
	if len(algo.Rings) == 2 && (algo.Rings[0].Level != "crit" || !strings.HasPrefix(algo.Rings[0].Text, "93")) {
		t.Errorf("a five-hour window at 93%% reads %+v: it has to be marked", algo.Rings[0])
	}
	if !strings.Contains(evirma.Head, "1 h ago") {
		t.Errorf("the heading of numbers an hour old reads %q — how old they are has to show without a press", evirma.Head)
	}
	if len(evirma.Live) != 0 || evirma.Empty != "nothing live" || !evirma.Old {
		t.Errorf("the contour with no live session shows live %v, says %q, dimmed %v: expected a word that nothing lives there and old numbers dimmed",
			evirma.Live, evirma.Empty, evirma.Old)
	}
	if strings.Join(algo.Live, ",") != "lms" || strings.Join(personal.Live, ",") != "aacpanel,atlas,blog" {
		t.Errorf("the contours hold live %v and %v, expected lms and the three of personal", algo.Live, personal.Live)
	}

	if len(got.Asked) != 1 {
		t.Fatalf("the shelf asked the archive %d times (%v), expected one request for every contour shown", len(got.Asked), got.Asked)
	}
	for _, want := range []string{"contour=4", "contour=1", "contour=3", "skip=lms%2Caacpanel%2Catlas%2Cblog"} {
		if !strings.Contains(got.Asked[0], want) {
			t.Errorf("the shelf asked %q without %s — every contour shown goes by the id of its entry, and the live conversations are left out", got.Asked[0], want)
		}
	}
	if !got.ShelfLast || len(got.Sections) > 0 && got.ShelfTop < got.Sections[2].Bottom {
		t.Errorf("the shelf stands at %.0f, last %v: it goes under all the contours", got.ShelfTop, got.ShelfLast)
	}
	var names []string
	for _, row := range got.Shelf {
		names = append(names, row.Name)
	}
	if want := "old-1-0,old-3-0,old-4-0,old-1-1,old-3-1,old-4-1"; strings.Join(names, ",") != want {
		t.Errorf("the shelf shows %v, expected the six newest of all the contours, newest first: %s", names, want)
	}
	if len(got.Shelf) > 0 {
		first := got.Shelf[0]
		if first.Contour != "Algorithmics and every project of it" {
			t.Errorf("a closed conversation of the contour the collector calls algo is labelled %q — the shelf names contours the way the map does", first.Contour)
		}
		if !strings.Contains(first.About, "what conversation 0 of contour 1 was about") || first.When != "1 h ago" {
			t.Errorf("a closed conversation says %q, %q — what it was about, and when in words of the screen", first.About, first.When)
		}
	}

	if len(got.Pop) != 2 || !strings.Contains(got.Pop[0], "Five hours") || !strings.Contains(got.Pop[0], "93%") ||
		!strings.Contains(got.Pop[0], "resets in 1 h") || !strings.Contains(got.Pop[1], "Seven days") || !strings.Contains(got.Pop[1], "resets in 5 d") {
		t.Errorf("the details of the limit read %q: both windows, each with its share and when it resets", got.Pop)
	}
	if !got.Closed {
		t.Error("Escape does not put the details down")
	}
	if !got.NoteOnTop {
		t.Error("the details of a contour drop under the heading of the next one")
	}
	if !strings.Contains(got.OldNote, "from 1 h ago") {
		t.Errorf("the details of numbers an hour old say %q — they have to say how old they are", got.OldNote)
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
