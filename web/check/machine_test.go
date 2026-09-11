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

func TestLimitsFooterDimsTheOthers(t *testing.T) {
	const cssPath = "src/css/desktop.css"
	raw, err := os.ReadFile(webPath(cssPath))
	if err != nil {
		t.Fatalf("%s: %v", cssPath, err)
	}
	css := cssWithoutComments(string(raw))

	if n := strings.Count(css, "dkon"); n != 1 {
		t.Errorf("%s: `dkon` is mentioned %d time(s) while it has exactly one place — the rule about "+
			"neighbours. The selected contour gets painted again, and in the desktop palette that "+
			"reads backwards: the accent is darker than the base color, so the highlighted row looks muted", cssPath, n)
	}

	dim := cssBlockFile(t, cssPath, ".dklimits.dkfocus .dklimit:not(.dkon)")
	if !strings.Contains(dim, "opacity: var(--dklim-mute)") {
		t.Errorf("%s: neighbouring contours dim by something other than the shared value — a second number "+
			"diverges from the dimming of a stale snapshot, and the shade stops meaning one thing", cssPath)
	}
	old := cssBlockFile(t, cssPath, ".dklimit.dkold")
	if !strings.Contains(old, "opacity: var(--dklim-mute)") {
		t.Errorf("%s: a stale snapshot dims by a number of its own instead of the shared value of the footer", cssPath)
	}

	const footer = "src/desktop/sessions.js"
	js := stripComments(srcFiles(t)[footer])
	if !strings.Contains(js, "dkfocus") {
		t.Errorf("%s: the footer does not tell the styles that there is a selection — there is nothing to dim the neighbours with", footer)
	}
	if !strings.Contains(js, "shown.includes(active)") {
		t.Errorf("%s: the selection flag is set without asking whether the selected contour is shown. "+
			"The contour filter may hide it — and then the whole footer goes dim", footer)
	}
}
