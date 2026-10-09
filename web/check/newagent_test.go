package check

import (
	"strings"
	"testing"
)

type agentSegment struct {
	Label string `json:"label"`
	On    bool   `json:"on"`
	Off   bool   `json:"off"`
	Mark  string `json:"mark"`
}

type agentSheet struct {
	Trouble  string         `json:"trouble"`
	Segments []agentSegment `json:"segments"`
	Reasons  []struct {
		Text string `json:"text"`
		Seen bool   `json:"seen"`
	} `json:"reasons"`
	Placed  bool     `json:"placed"`
	Seen    bool     `json:"seen"`
	After   []string `json:"after"`
	OK      bool     `json:"ok"`
	Request *struct {
		Kind   string         `json:"kind"`
		Target string         `json:"target"`
		Params map[string]any `json:"params"`
	} `json:"request"`
}

// New asks which agent to start on the sheet that confirms it, above Cancel
// and Open: the project's agent is taken and marked as the project's, so Open
// alone starts what the map says, and the other one is a tap away for this
// start. Codex is offered switched off, with the reason said under the
// switch, where its contour has no codex home or the project is not on the
// map — a phone has no hover to tell why a segment does not press. The pick
// goes with the request every time, the one left as offered too, and an
// action with no choice keeps its sheet as it was.
func TestNewAsksTheAgentOnItsSheet(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Cases map[string]agentSheet `json:"cases"`
				Other struct {
					Title  string         `json:"title"`
					Choice bool           `json:"choice"`
					Params map[string]any `json:"params"`
				} `json:"other"`
			}
			screen.run(t, "newagent.html", &got)
			checkAgentSheets(t, got.Cases)
			if got.Other.Choice || got.Other.Title == "" {
				t.Errorf("the sheet of a resume (%q) carries a choice of agent: %v", got.Other.Title, got.Other.Choice)
			}
			if _, ok := got.Other.Params["agent"]; ok {
				t.Errorf("a resume went out with %v: no choice, nothing laid over its params", got.Other.Params)
			}
		})
	}
}

func checkAgentSheets(t *testing.T, cases map[string]agentSheet) {
	t.Helper()
	const noHome = "Codex is off — contour acme has no codex home: AACP_CODEX_HOMES names none for it"
	const noMap = "Codex is off — the project is not on the map"
	for name, want := range map[string]struct {
		on, mark, reason, sent string
		codexOff               bool
		after                  string
		project                float64
	}{
		"claudePickedCodex": {on: "Claude Code", mark: "Claude Code", after: "Codex", sent: "codex", project: 10},
		"codexLeft":         {on: "Codex", mark: "Codex", after: "Codex", sent: "codex", project: 11},
		"noHome": {on: "Claude Code", mark: "Claude Code", reason: noHome, codexOff: true,
			after: "Claude Code", sent: "claude", project: 20},
		"codexNoHome": {on: "Codex", mark: "Codex", reason: noHome, codexOff: true,
			after: "Codex", sent: "codex", project: 21},
		"offTheMap": {on: "Claude Code", reason: noMap, codexOff: true, after: "Claude Code", sent: "claude"},
		"gone": {on: "Claude Code", reason: noMap, codexOff: true, after: "Claude Code", sent: "claude",
			project: 999},
	} {
		got, ok := cases[name]
		if !ok {
			t.Errorf("%s: the fixture did not run the case", name)
			continue
		}
		if got.Trouble != "" {
			t.Errorf("%s: %s", name, got.Trouble)
			continue
		}
		if len(got.Segments) != 2 || got.Segments[0].Label != "Claude Code" || got.Segments[1].Label != "Codex" {
			t.Errorf("%s: the sheet offers %+v, meant Claude Code and Codex", name, got.Segments)
			continue
		}
		for _, seg := range got.Segments {
			if seg.On != (seg.Label == want.on) {
				t.Errorf("%s: %s taken %v as the sheet opens — the agent of the project, %s, is the one Open "+
					"alone has to start", name, seg.Label, seg.On, want.on)
			}
			if (seg.Mark == "project") != (seg.Label == want.mark) || (seg.Mark != "" && seg.Mark != "project") {
				t.Errorf("%s: %s marked %q — the mark says which agent the project starts, meant on %q",
					name, seg.Label, seg.Mark, want.mark)
			}
		}
		if got.Segments[0].Off {
			t.Errorf("%s: Claude Code is switched off", name)
		}
		if got.Segments[1].Off != want.codexOff {
			t.Errorf("%s: Codex switched off %v, meant %v", name, got.Segments[1].Off, want.codexOff)
		}
		var reasons []string
		for _, r := range got.Reasons {
			reasons = append(reasons, r.Text)
			if !r.Seen {
				t.Errorf("%s: the reason %q is not on the screen — a segment that does not press says why "+
					"in a line a person sees", name, r.Text)
			}
		}
		if strings.Join(reasons, "|") != want.reason {
			t.Errorf("%s: the reasons under the switch are %q, meant %q", name, reasons, want.reason)
		}
		if !got.Placed || !got.Seen {
			t.Errorf("%s: the choice stands between the consequence and the buttons %v, on the screen %v",
				name, got.Placed, got.Seen)
		}
		if len(got.After) != 1 || got.After[0] != want.after {
			t.Errorf("%s: after the press %v is taken, meant %s", name, got.After, want.after)
		}
		if !got.OK || got.Request == nil {
			t.Errorf("%s: Open sent nothing (ok %v)", name, got.OK)
			continue
		}
		params := got.Request.Params
		if got.Request.Kind != "session.open" || params["agent"] != want.sent {
			t.Errorf("%s: Open sent %s with %v — the agent %s has to go by name", name, got.Request.Kind, params, want.sent)
		}
		if id, has := params["project"]; (want.project == 0) == has || (has && id != want.project) {
			t.Errorf("%s: Open sent the project as %v, meant %v: the pick is laid over the params of the press, "+
				"not in place of them", name, params, want.project)
		}
	}
}

// The sheet of New reads the map the snapshot brings, handed over where the
// host's name is: without it every New offers Claude Code alone and says of
// Codex that the project is not on the map.
func TestTheSheetOfNewReadsTheMapOfTheSnapshot(t *testing.T) {
	app := stripComments(srcFiles(t)["src/app.js"])
	if !strings.Contains(app, "setHostName(fresh.hostName);\n            setProfileMap(fresh.profileMap);") {
		t.Error("src/app.js does not hand the map of a fresh snapshot to the registry beside the host's name — " +
			"the sheet of New would not know the agent of a project or the codex home of its contour")
	}
}
