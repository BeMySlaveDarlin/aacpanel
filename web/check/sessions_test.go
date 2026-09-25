package check

import (
	"regexp"
	"strings"
	"testing"

	"aacpanel/internal/store"
)

func TestSessionNotesReachTheScreen(t *testing.T) {
	src := screenSrc(t, "src/screens/sessions.js")
	if src == "" {
		t.Fatal("the sessions screen is not found")
	}

	if !strings.Contains(src, "sessionNotes") {
		t.Error("the screen does not read sessionNotes — the collection is there, the display is not")
	}

	i := strings.Index(src, `<p class="empty">There are no live sessions.</p>`)
	if i < 0 {
		t.Fatal("the empty-list branch is not found — the check is out of date, fix it together with the screen")
	}
	head := src[max(0, i-200):i]
	if !strings.Contains(head, "blind") {
		t.Error("the empty list does not tell no sessions from the collector did not run")
	}
}

func TestScreensKeepDatabaseSideWhenAgentIsDown(t *testing.T) {
	cases := []struct {
		file  string
		guard string
		keep  []string
	}{
		{"resources.js", "if (error || !host)", []string{"Metric", `top="cpu"`, `top="mem"`, "Traffic"}},
		{"sessions.js", "if (error) {", []string{"PastButton"}},
	}

	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			src := srcFiles(t)["src/screens/"+c.file]
			if src == "" {
				t.Fatalf("the %s screen is not found", c.file)
			}

			i := strings.Index(src, c.guard)
			if i < 0 {
				t.Fatalf("%s has no %q branch — the check is out of date, fix it together with the screen", c.file, c.guard)
			}
			rest := src[i:]
			j := strings.Index(rest, "\n    }")
			if j < 0 {
				t.Fatalf("%s: the end of the %q branch is not found", c.file, c.guard)
			}
			branch := rest[:j]

			for _, want := range c.keep {
				if !strings.Contains(branch, want) {
					t.Errorf("%s: the failure branch does not show %s — what lies in the database disappears together with the live numbers", c.file, want)
				}
			}
		})
	}
}

func TestEverySnapshotBlockIsVisibleOnScreen(t *testing.T) {
	files := srcFiles(t)

	names := files["src/ui/trouble.js"]
	if names == "" {
		t.Fatal("src/ui/trouble.js not found")
	}

	for _, block := range store.SnapshotBlocks {
		if !regexp.MustCompile(`(?m)^\s+` + block + `:\s*"`).MatchString(names) {
			t.Errorf("block %q is not put into words in trouble.js: the panel will show it as a json key", block)
		}

		shown := false
		for path, body := range files {
			if !strings.HasPrefix(path, "src/screens/") {
				continue
			}
			if strings.Contains(body, `"`+block+`"`) && strings.Contains(body, "NotRecorded") {
				shown = true
				break
			}
		}
		if !shown {
			t.Errorf("the failure of block %q is not shown on any screen: nobody learns about the hole in the history", block)
		}
	}
}

const sessionsFile = "src/screens/sessions.js"

func TestArchivedSessionCardDiffersFromLive(t *testing.T) {
	css := cssSrc(t)

	cases := []struct {
		selector string
		rule     string
		harm     string
	}{
		{
			selector: ".srow.past",
			rule:     "background: none",
			harm:     "the archived card becomes a live one dimmed down again — the material is the same for both, and in the sky theme the dimming barely reads",
		},
		{
			selector: ".srow.past",
			rule:     "backdrop-filter: none",
			harm:     "the blur carries brightness(1.04): the card lightens by itself and stays a light rectangle even with no fill",
		},
		{
			selector: ".ctxbar.peak i",
			rule:     "background: none",
			harm:     "the peak fills the bar again the way the current fill does — and right now a dead chat has nothing filled in",
		},
		{
			selector: ".rowacts",
			rule:     "min-width",
			harm:     "the room for the buttons stops being reserved, and the percentage on an archived card stands two buttons further left than on the live one next to it",
		},
	}

	for _, c := range cases {
		block := cssBlock(t, css, c.selector)
		if !strings.Contains(block, c.rule) {
			t.Errorf("%s: %s has no %q — %s", cssFile, c.selector, c.rule, c.harm)
		}
	}

	body := screenSrc(t, sessionsFile)
	card := jsBlock(t, sessionsFile, body, "export function PastRow(")
	if !strings.Contains(card, "\n            peak\n") {
		t.Errorf("%s: PastRow does not mark its number as a peak unconditionally — "+
			"on the archive page the peak glows as a fill happening right now again", sessionsFile)
	}
	if strings.Contains(card, "peak=$") {
		t.Errorf("%s: the peak flag in PastRow became conditional — how a value looks cannot depend on "+
			"what stands next to it", sessionsFile)
	}

	shell := jsBlock(t, sessionsFile, body, "function SessionCard(")
	if !strings.Contains(shell, `<div class="rowacts">`) {
		t.Errorf("%s: SessionCard does not reserve the room for actions itself — "+
			"a card with no buttons becomes wider than a card with buttons again", sessionsFile)
	}
}

func TestArchivedChatHeadShowsPeakNotFill(t *testing.T) {
	src := screenSrc(t, "src/screens/chat.js")
	if !strings.Contains(src, "ContextBar} pct=${pct} peak=${!live}") {
		t.Error("the chat header does not mark the peak: on an archived chat the bar fills the way a current fill does")
	}
	if regexp.MustCompile(`chatpct \$\{`).MatchString(src) {
		t.Error("the number in the chat header is colored by the scale — the bar carries that, not the number")
	}
	if strings.Contains(cssSrc(t), ".chatpct.s1") {
		t.Error("the colored steps of the number are still in the styles: the rule and the markup drift apart silently")
	}
}

func TestProjectRowIsNotASessionCard(t *testing.T) {
	body := screenSrc(t, sessionsFile)
	if body == "" {
		t.Fatalf("%s not found — the test is useless, check the path", sessionsFile)
	}
	groups := jsBlock(t, sessionsFile, body, "function Groups(")

	if strings.Contains(groups, "pct") {
		t.Errorf("%s: the project row shows the session percentage again — "+
			"that is a second carrier of one value and a third look for the session card", sessionsFile)
	}
	if regexp.MustCompile(`\bown\.map\(`).MatchString(groups) {
		t.Errorf("%s: the project row lists sessions by name again — "+
			"their names and numbers live on the card, here a counter takes their place", sessionsFile)
	}
	if !regexp.MustCompile(`\bown\.length}`).MatchString(groups) {
		t.Errorf("%s: the project row has no counter of live sessions — the dot by the name says only "+
			"whether there are any, and a project with three sessions is indistinguishable from one with one", sessionsFile)
	}
}

func TestSessionArtifactsOpenTheirOwnWay(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)

	// The chip of pages counts what this device has not opened, and the chip of
	// briefs counts what still waits for an answer. Each number is counted once
	// and drawn once: two numbers for one thing part on the first kind added.
	work := jsBlock(t, chatFile, body, "function WorkRefs(")
	for _, count := range []string{"const fresh = unopened(", "const wants = waiting("} {
		if !strings.Contains(work, count) {
			t.Errorf("%s: the row under the composer has no %q — a chip that counts for itself "+
				"drifts from the list under it", chatFile, count)
		}
	}
	for _, drawn := range []string{
		"fresh > 0 && html`<span class=\"wnum\">${fresh}</span>`",
		"wants > 0 && html`<span class=\"wnum\">${wants}</span>`",
	} {
		if !strings.Contains(work, drawn) {
			t.Errorf("%s: a chip draws a number of its own instead of the one counted for it, "+
				"or draws a 0 where the other chips draw nothing", chatFile)
		}
	}

	// A document of the project is tapped in the run — on the path in a tool
	// call, or on a file a delivery carried — and it opens by the file handler
	// wherever that happens, so the boundary of the conversation directory
	// stands in one place.
	if !strings.Contains(body, `kind: "file"`) {
		t.Errorf("%s: a document opens by something other than the file handler — "+
			"then the disk has a second road with a boundary of its own", chatFile)
	}
	if n := strings.Count(body, "/api/chat/file?"); n != 1 {
		t.Errorf("%s: %d handlers for reading a file instead of one: the boundary of the chat "+
			"directory stands behind it, and a second address would go around it", chatFile, n)
	}

	list := jsBlock(t, chatFile, body, "function WorkList(")
	if !strings.Contains(list, "<${ArtifactCard}") {
		t.Errorf("%s: the shelf draws a published page with a card other than the one "+
			"the run uses — one and the same thing will look like two", chatFile)
	}
	card := jsBlock(t, chatFile, body, "function ArtifactCard(")
	if !strings.Contains(card, `target="_blank"`) {
		t.Errorf("%s: a page the panel kept no copy of opens by something other than the browser — "+
			"without a copy the panel does not have its content", chatFile)
	}
	if strings.Contains(card, "/api/chat/file") {
		t.Errorf("%s: the artifact card reaches for it on the host disk — what lies there is "+
			"the source file, not what was published", chatFile)
	}

	look := jsBlock(t, chatFile, body, "function FileBody(")
	if !strings.Contains(look, `view === "doc"`) || !strings.Contains(look, "render(") {
		t.Errorf("%s: the document is shown with no markup — on a phone that is a canvas of "+
			"monospaced text, not a document", chatFile)
	}
	if !strings.Contains(look, "highlight(") {
		t.Errorf("%s: the file content is shown with no highlighting — from a phone the code "+
			"is read by eye, not by an editor", chatFile)
	}
}

func TestFreshSessionChatIsNotAnError(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	src := screenSrc(t, chatFile)

	fetch := jsBlock(t, chatFile, src, "        const load = () => fetch(")
	if !strings.Contains(fetch, "e.status === 404") {
		t.Errorf("%s: the feed failure is not handled by the response code — there is no transcript yet "+
			"is once again indistinguishable from the collector is down", chatFile)
	}
	if !strings.Contains(fetch, `kind: "fresh"`) {
		t.Errorf("%s: an empty live session has no state of its own — it lands in the shared failure", chatFile)
	}
	if !strings.Contains(fetch, "setInterval(load") {
		t.Errorf("%s: waiting for the first reply ends with nothing — a stream over a transcript "+
			"that does not exist cannot be opened, and the feed stays empty until the screen is reopened", chatFile)
	}

	shown := regexp.MustCompile(`state\.kind === "fresh"[^~]{0,400}?\n            ` + "`" + `\}`).FindString(src)
	if shown == "" {
		t.Fatalf("%s: the state where the chat has not started yet is not shown on the screen", chatFile)
	}
	if strings.Contains(shown, "crit") || strings.Contains(shown, "warn") {
		t.Errorf("%s: an empty fresh session is shown in an alarming color again: %s", chatFile, shown)
	}
	if strings.Contains(shown, "aacpanel-agent") {
		t.Errorf("%s: a fresh session is advised to fix the collector — there is nothing to fix", chatFile)
	}
}

func TestProfileChoiceHasOneOwner(t *testing.T) {
	files := srcFiles(t)

	const home = "src/screens/sessions/pages.js"
	if !strings.Contains(files[home], "export function useProfilePage(") {
		t.Fatalf("%s: the contour choice has moved — the test looks for it in the wrong place", home)
	}

	owners := []string{}
	for _, path := range sortedKeys(files) {
		if path == home {
			continue
		}
		if strings.Contains(stripComments(files[path]), "useProfilePage(") {
			owners = append(owners, path)
		}
	}
	if len(owners) == 0 {
		t.Errorf("%s: nobody calls the contour choice hook — either it is dead or the choice "+
			"was set up past it", home)
	}

	for _, path := range sortedKeys(files) {
		if path == home {
			continue
		}
		body := stripComments(files[path])
		if !strings.Contains(body, "<${Pages}") {
			continue
		}
		if strings.Contains(stripComments(screenSrc(t, path)), "useProfilePage(") {
			continue
		}
		t.Errorf("%s: draws the contour pager but holds the open contour itself — that is a second "+
			"choice, one per screen", path)
	}

	for _, path := range sortedKeys(files) {
		if path == home {
			continue
		}
		body := stripComments(files[path])
		if strings.Contains(body, "aacpanel.sessions.profile") {
			t.Errorf("%s: a storage of its own for the open contour — one contour will be open in "+
				"the sessions and another in the map", path)
		}
		if strings.Contains(body, "pftab") {
			t.Errorf("%s: a contour name row of its own, past Pages — two ways to arrive at one "+
				"place, each with a behaviour of its own", path)
		}
	}

	if !strings.Contains(screenSrc(t, sessionsFile), "<${ProfileLimits}") {
		t.Errorf("%s: the profile page does not show its subscription", sessionsFile)
	}
	head := stripComments(files["src/ui/header.js"])
	for _, mark := range []string{"fiveHour", "sevenDay", "limittab"} {
		if strings.Contains(head, mark) {
			t.Errorf("src/ui/header.js: the subscription limits are in the header again (%s) — "+
				"the row of contour names above them comes back with them", mark)
		}
	}
}

func TestLiveListCutByContourNotByMap(t *testing.T) {
	body := screenSrc(t, sessionsFile)
	block := jsBlock(t, sessionsFile, body, "export function contoursOf(")

	at := strings.Index(block, "s.profile")
	if at < 0 {
		t.Fatalf("%s: contoursOf does not look at the contour from the snapshot — the live list is "+
			"split by a guess from the project path again", sessionsFile)
	}
	if guess := strings.LastIndex(block, "byPath.get"); guess > at {
		t.Errorf("%s: the guess by directory is written after the contour from the snapshot and "+
			"overrides it — it has to be the other way round", sessionsFile)
	}
	if !strings.Contains(block, "if (s.profile)") {
		t.Errorf("%s: the contour from the snapshot is written with no check — a session without one "+
			"loses the guess by the map as well", sessionsFile)
	}
}

func TestCatchUpHasOneOwner(t *testing.T) {
	files := srcFiles(t)

	for _, path := range sortedKeys(files) {
		if path == "src/catchup.js" {
			continue
		}
		body := stripComments(files[path])
		for _, mark := range []string{"WAIT_LIMIT", "CATCH_UP_MS =", "CATCH_UP_LIMIT =", "function waitKey", "function settled"} {
			if strings.Contains(body, mark) {
				t.Errorf("%s starts a wait of its own (%s): the deadlines drift from the shared ones, "+
					"and one action behaves differently on a phone and on a monitor", path, mark)
			}
		}
		if strings.Contains(body, "function ownName") {
			t.Errorf("%s starts its own parsing of the console name", path)
		}
	}

	allowed := map[string]string{
		"src/app.js":                     "the ordinary snapshot poll every fifteen seconds and a recheck of the base for requests",
		"src/api.js":                     "reissuing the bearer once an hour for requests through another address of the panel",
		"src/catchup.js":                 "the catch-up poll under a wait",
		"src/alerts.js":                  "alerts at a rate of their own",
		"src/exec.js":                    "whether the executor is alive",
		"src/faults.js":                  "what from the snapshot does not reach the history",
		"src/screens/chat/feedwindow.js": "the feed of a session that has not said anything yet",
		"src/screens/chat/look.js":       "the output of a background command while it writes",
		"src/screens/chat/term.js":       "the key repeat while a finger holds it",
		"src/screens/chat/work.js":       "the clock of a compaction, redrawn by the second and asking nothing",
		"src/data/usage.js":              "the progress of usage collection, one for the monitor and the phone",
	}
	for _, path := range sortedKeys(files) {
		if !strings.Contains(stripComments(files[path]), "setInterval(") {
			continue
		}
		if _, ok := allowed[path]; !ok {
			t.Errorf("%s started a timer: if it waits for the consequences of an action, that is a second "+
				"catch-up poll — and one already lives in src/catchup.js", path)
		}
	}
	if strings.Contains(stripComments(files["src/screens/sessions.js"]), "setInterval(") {
		t.Error("the sessions screen hurries the snapshot itself again")
	}
}

// A session run in a worktree kept beside its repository has a name of its
// own, and the service tells which project it belongs to. The list takes that
// word over the name: null is outside the map even under a matching name, and
// the name is the guess only for a row the service did not place.
func TestSessionGoesToTheProjectTheServiceNamed(t *testing.T) {
	project := map[string]any{"id": 200, "session": "evirma"}
	sessions := []any{
		map[string]any{"session": "evirma-fingerprint-rotation", "project": map[string]any{"id": 200}},
		map[string]any{"session": "evirma-2", "project": nil},
		map[string]any{"session": "evirma", "project": map[string]any{"id": 7}},
		map[string]any{"session": "evirma-3"},
	}
	got := runModuleJS(t, "src/screens/sessions/map.js", "sessionsOf", [][]any{{project, sessions}})

	var names []string
	for _, row := range got[0].([]any) {
		names = append(names, row.(map[string]any)["session"].(string))
	}
	if strings.Join(names, ",") != "evirma-fingerprint-rotation,evirma-3" {
		t.Errorf("the project holds %v, expected the worktree session and the unplaced one by its name", names)
	}
}
