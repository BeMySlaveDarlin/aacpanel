package check

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
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
	// The desk asks either itself or through the shared limit groups, which ask
	// staleLimits for every provider.
	if body := files[footer]; !strings.Contains(body, "staleLimits(") && !strings.Contains(body, "limitGroups(") {
		t.Errorf("%s never asks about the age of the snapshot — the footer again shows yesterday's numbers "+
			"next to fresh ones", footer)
	}
	body := withoutComments(files[owner])
	groups := ""
	if at := strings.Index(body, "function limitGroups("); at >= 0 {
		groups = body[at:]
		if end := strings.Index(groups, "\n}\n"); end >= 0 {
			groups = groups[:end]
		}
	}
	if !strings.Contains(groups, "staleLimits(") {
		t.Errorf("limitGroups in %s does not ask staleLimits — the groups the desk draws would hide stale numbers", owner)
	}
}

type deskReading struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Tip   string `json:"tip"`
}

type deskSection struct {
	Name            string        `json:"name"`
	Head            string        `json:"head"`
	Agents          []string      `json:"agents"`
	Acts            []string      `json:"acts"`
	Readings        []deskReading `json:"readings"`
	Old             bool          `json:"old"`
	Grouped         bool          `json:"grouped"`
	HeadingReadings int           `json:"headingReadings"`
	Live            []string      `json:"live"`
	Closed          int           `json:"closed"`
	Empty           string        `json:"empty"`
}

type deskShelfRow struct {
	Name    string `json:"name"`
	Contour string `json:"contour"`
	About   string `json:"about"`
	When    string `json:"when"`
}

// A contour tab keeps its limits, actions and archive scoped to its map id.
// Each reading names its window and opens the shared details independently.
func TestDeskColumnShowsTheContourOfItsTabWithItsLimitAndTheClosedOnAShelf(t *testing.T) {
	type handed struct {
		Tasks    []*int           `json:"tasks"`
		Settings []map[string]int `json:"settings"`
	}
	type tab struct {
		Tab       string         `json:"tab"`
		Sections  []deskSection  `json:"sections"`
		Asked     []string       `json:"asked"`
		Shelf     []deskShelfRow `json:"shelf"`
		ShelfLast bool           `json:"shelfLast"`
		Details   struct {
			Windows []string `json:"windows"`
			Note    string   `json:"note"`
			Closed  bool     `json:"closed"`
		} `json:"details"`
		Handed handed `json:"handed"`
	}
	var got struct {
		Tabs  []tab `json:"tabs"`
		Loose struct {
			Sections []deskSection `json:"sections"`
			Handed   handed        `json:"handed"`
		} `json:"loose"`
		Clock int64 `json:"clock"`
	}
	runWideFixture(t, "desklimits.html", &got)

	want := []struct {
		name, rings, live, shelf string
		id                       int
	}{
		{"Acme", "7:resets in 48 min|41:", "", "old-4-0,old-4-1,old-4-2", 4},
		{"Globex Group and every project of it", "93:resets in 1 h|57:resets in 118 h", "shop", "old-1-0,old-1-1,old-1-2", 1},
		{"personal", "12:resets in 2 h|24:resets in 100 h", "aacpanel,blog,helios", "old-3-0,old-3-1,old-3-2", 3},
	}
	if len(got.Tabs) != len(want) {
		t.Fatalf("the fixture read %d tabs, expected one for each of the three contours of the map: %+v", len(got.Tabs), got.Tabs)
	}
	for i, w := range want {
		tab := got.Tabs[i]
		if len(tab.Sections) != 1 || tab.Sections[0].Name != w.name {
			t.Errorf("the tab of %q shows the sections %+v, expected its contour alone", w.name, tab.Sections)
			continue
		}
		sec := tab.Sections[0]
		if sec.HeadingReadings != 0 {
			t.Errorf("%q: the contour heading still has %d limit readings", sec.Name, sec.HeadingReadings)
		}
		var rings []string
		for _, r := range sec.Readings {
			rings = append(rings, r.Value+":"+r.Label)
		}
		if strings.Join(rings, "|") != w.rings {
			t.Errorf("%q: the readings show %v, expected %s — the share inside, and beside it how long until the window starts over, "+
				"with the same reset labels as on the phone", sec.Name, rings, w.rings)
		}
		if len(sec.Readings) == 2 && (!strings.HasPrefix(sec.Readings[0].Tip, "Claude, 5 hours") || !strings.HasPrefix(sec.Readings[1].Tip, "Claude, 7 days")) {
			t.Errorf("%q: the readings are tipped %q and %q — a label that says the time left has to name its window somewhere",
				sec.Name, sec.Readings[0].Tip, sec.Readings[1].Tip)
		}
		if strings.Contains(sec.Head, "live") {
			t.Errorf("%q: the heading still counts live sessions: %q", sec.Name, sec.Head)
		}
		if sec.Closed != 0 {
			t.Errorf("%q holds %d closed conversations among its live sessions — the closed ones stand on the shelf", sec.Name, sec.Closed)
		}
		if strings.Join(sec.Live, ",") != w.live {
			t.Errorf("%q holds live %v, expected %q", sec.Name, sec.Live, w.live)
		}
		if strings.Join(sec.Acts, ",") != "Tasks,Profile" {
			t.Errorf("%q: the heading holds the actions %v, expected Tasks and Profile, both on", sec.Name, sec.Acts)
		}
		if strings.Join(sec.Agents, ",") != "claude" {
			t.Errorf("%q: the subscription mark identifies %v, expected claude once for both windows", sec.Name, sec.Agents)
		}
		h := tab.Handed
		if len(h.Tasks) != 1 || h.Tasks[0] == nil || *h.Tasks[0] != w.id {
			t.Errorf("%q: Tasks handed on %v, expected the entry of the map with id %d", w.name, h.Tasks, w.id)
		}
		if len(h.Settings) != 1 || h.Settings[0]["contour"] != w.id || len(h.Settings[0]) != 1 {
			t.Errorf("%q: Profile opened the settings at %v, expected the page of contour %d", w.name, h.Settings, w.id)
		}

		if len(tab.Asked) != 1 {
			t.Errorf("%q: the shelf asked the archive %d times (%v), expected one request for the contour shown", w.name, len(tab.Asked), tab.Asked)
		} else {
			for _, other := range want {
				in := strings.Contains(tab.Asked[0], fmt.Sprintf("contour=%d", other.id))
				if in != (other.id == w.id) {
					t.Errorf("%q: the shelf asked %q — the contour shown goes by the id of its entry, and no other contour with it", w.name, tab.Asked[0])
				}
			}
			if !strings.Contains(tab.Asked[0], "skip=shop%2Caacpanel%2Chelios%2Cblog") {
				t.Errorf("%q: the shelf asked %q without leaving the live conversations out", w.name, tab.Asked[0])
			}
		}
		if !tab.ShelfLast {
			t.Errorf("%q: the shelf is not the last of the column: it goes under the contour", w.name)
		}
		var names []string
		for _, row := range tab.Shelf {
			names = append(names, row.Name)
			if row.Contour != "" {
				t.Errorf("%q: a closed conversation on the shelf is tagged %q — the shelf holds the contour of the tab alone, and a tag says nothing", w.name, row.Contour)
			}
		}
		if strings.Join(names, ",") != w.shelf {
			t.Errorf("%q: the shelf shows %v, expected the closed conversations of the contour, newest first: %s", w.name, names, w.shelf)
		}
		if !tab.Details.Closed {
			t.Errorf("%q: Escape does not put the details of the limit down", w.name)
		}
	}

	loose := got.Loose
	if len(loose.Sections) != 1 || loose.Sections[0].Name != "lab" || strings.Join(loose.Sections[0].Acts, ",") != "Tasks off,Profile off" ||
		len(loose.Handed.Tasks) != 0 || len(loose.Handed.Settings) != 0 {
		t.Errorf("the contour the map does not hold shows %+v and its actions handed on %+v — expected its tab with Tasks and Profile off",
			loose.Sections, loose.Handed)
	}

	acme, globex := got.Tabs[0], got.Tabs[1]
	if head := acme.Sections[0]; strings.Contains(head.Head, "ago") || !head.Grouped || len(head.Live) != 0 || head.Empty != "nothing live" || !head.Old {
		t.Errorf("the contour with no live session and numbers an hour old reads %q, shows live %v, says %q, dimmed %v: "+
			"expected an old subscription group without an age line or tracks, and a word that nothing lives there",
			head.Head, head.Live, head.Empty, head.Old)
	}
	if !strings.Contains(acme.Details.Note, "Updated 1 h ago") {
		t.Errorf("the details of numbers an hour old say %q — they have to say how old they are", acme.Details.Note)
	}
	pop := globex.Details.Windows
	if len(pop) != 2 || !strings.Contains(pop[0], "Claude, 5 hours") || !strings.Contains(pop[0], "93%") ||
		!strings.Contains(pop[0], "resets in 1 h") || !strings.Contains(pop[1], "Claude, 7 days") || !strings.Contains(pop[1], "resets in 118 h") {
		t.Errorf("the details of the limit read %q: both windows, each with its share and when it resets", pop)
	}
	if len(globex.Shelf) > 0 {
		first := globex.Shelf[0]
		if !strings.Contains(first.About, "what conversation 0 of contour 1 was about") || !spokeAgo(first.When, time.UnixMilli(got.Clock), 66*time.Minute) {
			t.Errorf("a closed conversation says %q, %q — what it was about, and the local date and time 66 minutes ago", first.About, first.When)
		}
	}
}

// Desktop readings share the phone's values, thresholds and reset wording,
// including stale Codex snapshots and accounts without a weekly window.
func TestDeskLimitReadingsShowValuesResetsAndMissingWindows(t *testing.T) {
	var got struct {
		Readings []struct {
			Value, Left, Label, Tone, Agent, Vendor string
			Chip                                    bool
		}
		WrongMode     bool
		Providers     []string
		Groups        []deskLimitGroup
		Details, Past []string
		Stale         struct {
			Vendors, Ages, Notes []string
			Details              string
		}
		NoWeek, NoWeekNote, HalfWeek, Empty string
	}
	runWideFixture(t, "deskrings.html", &got)
	want := []struct{ value, left, reset, tone string }{
		{"20%", "2h", "resets in 2 h", "ok"}, {"60%", "4d", "resets in 99 h", "warn"},
		{"59%", "60m", "resets in 60 min", "ok"}, {"84%", "23h", "resets in 23 h", "warn"},
		{"85%", "1m", "resets in 1 min", "crit"}, {"90%", "1d", "resets in 36 h", "crit"},
		{"99%", "0m", "resets any moment", "crit"}, {"41%", "", "", "ok"},
		{"0%", "", "", "ok"}, {"100%", "46m", "resets in 46 min", "crit"},
		{"99%", "", "", "crit"}, {"105%", "", "", "crit"},
	}
	if len(got.Readings) != len(want) {
		t.Fatalf("readings: %+v, expected %d windows", got.Readings, len(want))
	}
	for i, w := range want {
		r := got.Readings[i]
		if r.Value != w.value || r.Left != w.left || !r.Chip || r.Agent != "claude" || r.Vendor != r.Agent || r.Tone != w.tone || (w.reset != "" && !strings.Contains(r.Label, w.reset)) ||
			(w.reset == "" && strings.Contains(r.Label, "reset")) {
			t.Errorf("reading %d: %+v, expected %+v", i, r, w)
		}
	}
	if got.WrongMode {
		t.Error("the heading contains rings, tracks or an age line instead of grouped window readings")
	}
	if strings.Join(got.Providers, ",") != "claude,codex,gemini,other" {
		t.Errorf("providers are not driven by contour data: %v", got.Providers)
	}
	if len(got.Groups) != 4 {
		t.Fatalf("subscription groups: %+v, expected Claude, Codex, Gemini and an unknown provider", got.Groups)
	}
	for i, agent := range []string{"claude", "codex", "gemini", "other"} {
		checkDeskLimitGroup(t, got.Groups[i], agent, 1)
	}
	for i, want := range []string{"Claude, 5 hours|85% used|resets in 1 min", "Claude, 7 days|90% used|resets in 36 h"} {
		for _, part := range strings.Split(want, "|") {
			if len(got.Details) <= i || !strings.Contains(got.Details[i], part) {
				t.Errorf("details %d: %v, missing %q", i, got.Details, part)
			}
		}
	}
	if len(got.Past) != 2 || !strings.Contains(got.Past[0], "resets any moment") ||
		strings.Contains(got.Past[1], "reset") || !strings.Contains(got.Past[1], "41% used") {
		t.Errorf("past and unknown resets: %v", got.Past)
	}
	if strings.Join(got.Stale.Vendors, ",") != "codex" ||
		len(got.Stale.Ages) != 0 || len(got.Stale.Notes) != 0 ||
		!strings.Contains(got.Stale.Details, "Updated 1 h ago") || !strings.Contains(got.Stale.Details, "last known numbers") ||
		!strings.Contains(got.Stale.Details, "codex says the limit is reached") {
		t.Errorf("stale and reached Codex limits: %+v", got.Stale)
	}
	if got.NoWeek != "no numbers yet" || !strings.Contains(got.NoWeekNote, "codex has told no weekly window yet") ||
		!strings.Contains(got.NoWeekNote, "codex says the limit is reached") || got.Empty != "no numbers yet" {
		t.Errorf("missing windows: %q; empty contour: %q", got.NoWeek, got.Empty)
	}
	if got.HalfWeek != "codex has told no weekly window yet" {
		t.Errorf("a contour with numbers lost the note on codex's missing week: %q", got.HalfWeek)
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
