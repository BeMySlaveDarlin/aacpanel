package check

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

// gateSent is what the own button of the gate's sheet sent, with what the
// sheet said it does.
type gateSent struct {
	Trouble string `json:"trouble"`
	Effect  string `json:"effect"`
	Sent    []struct {
		Kind   string         `json:"kind"`
		Target string         `json:"target"`
		Params map[string]any `json:"params"`
	} `json:"sent"`
}

// On a phone a codex thread reads by its own title, and the word Thread
// closes its band at the far end. Behind it are compact, review, rename and
// the goal: each a command of codex's own that the gate asks about first,
// a review that waits while a turn runs and refuses a commit named by no
// hash, a title of any printable words, a goal set with a budget and paused
// once it runs. Under the composer the thread counts what it left running in
// place of claude's agents and workflows, and lists it with a stop for each
// and for all; its tools say what it has, read-only; and the models it can
// switch to are asked of the session itself.
func TestTheThreadOfACodexThreadIsDoneFromItsBand(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		Title       string   `json:"title"`
		TitleGap    int      `json:"titleGap"`
		Band        []string `json:"band"`
		ThreadAtEnd bool     `json:"threadAtEnd"`
		Deck        []string `json:"deck"`
		Rows        []string `json:"rows"`
		Compact     gateSent `json:"compact"`
		CommitWhy   string   `json:"commitWhy"`
		CommitOff   bool     `json:"commitOff"`
		Review      gateSent `json:"review"`
		RenameValue string   `json:"renameValue"`
		Rename      []struct {
			Kind   string         `json:"kind"`
			Params map[string]any `json:"params"`
		} `json:"rename"`
		GoalSet    gateSent `json:"goalSet"`
		GoalRow    string   `json:"goalRow"`
		GoalShown  string   `json:"goalShown"`
		GoalPause  gateSent `json:"goalPause"`
		BusyReview struct {
			Off  bool   `json:"off"`
			Desc string `json:"desc"`
		} `json:"busyReview"`
		Procs   []string `json:"procs"`
		StopOne gateSent `json:"stopOne"`
		StopAll gateSent `json:"stopAll"`
		Tools   []string `json:"tools"`
		Mcp     []string `json:"mcp"`
		McpActs bool     `json:"mcpActs"`
		Catalog []string `json:"catalog"`
	}
	runFixture(t, "codexthread.html", &got)

	const name = "codex-5afc361b"
	if got.Title != "Cart flakes" || got.TitleGap <= 0 {
		t.Errorf("the header of a titled thread reads %q with the space between its words %dpx wide, expected its title as written",
			got.Title, got.TitleGap)
	}
	if strings.Join(got.Band, ",") != "clip,think,perm,thread" || !got.ThreadAtEnd {
		t.Errorf("the band holds %v, the thread at its far end %v", got.Band, got.ThreadAtEnd)
	}
	if strings.Join(got.Deck, "|") != "background processes: 2 running|subagents: none" {
		t.Errorf("under the composer stand %v, expected the count of background processes and of the agents", got.Deck)
	}
	wantRows := []string{"Compact: 35% of the context used", "Review: changes, a branch, a commit, your words",
		"Rename: Cart flakes", "Goal: none set"}
	if !reflect.DeepEqual(got.Rows, wantRows) {
		t.Errorf("the thread lists %v", got.Rows)
	}
	sentOne := func(what string, g gateSent, kind string, params map[string]any) {
		t.Helper()
		if g.Trouble != "" || len(g.Sent) != 1 || g.Sent[0].Kind != kind || g.Sent[0].Target != name ||
			!reflect.DeepEqual(g.Sent[0].Params, params) {
			t.Errorf("%s asked through the gate and sent %+v, expected %s %v", what, g, kind, params)
		}
	}
	sentOne("the compaction", got.Compact, "session.command", map[string]any{"command": "compact"})
	if !strings.Contains(got.CommitWhy, "hash") || !got.CommitOff {
		t.Errorf("a commit named by no hash: %q, the press off %v", got.CommitWhy, got.CommitOff)
	}
	sentOne("the review", got.Review, "session.command",
		map[string]any{"command": "review", "review": map[string]any{"target": "branch", "branch": "main"}})
	if !strings.Contains(got.Review.Effect, "changes nothing") {
		t.Errorf("the gate of a review says %q", got.Review.Effect)
	}
	if got.RenameValue != "Cart flakes" || len(got.Rename) != 1 || got.Rename[0].Kind != "session.rename" ||
		got.Rename[0].Params["name"] != "Café ✓ checkout" {
		t.Errorf("the rename starts from %q and sent %+v", got.RenameValue, got.Rename)
	}
	sentOne("the goal", got.GoalSet, "session.command", map[string]any{"command": "goal",
		"goal": map[string]any{"do": "set", "objective": "make CI green", "budget": float64(50000)}})
	if !strings.Contains(got.GoalSet.Effect, "at once") {
		t.Errorf("the gate of a goal says %q, expected that codex starts at once", got.GoalSet.Effect)
	}
	if got.GoalRow != "make CI green · running · 41k of 200k" || !strings.Contains(got.GoalShown, "41k") {
		t.Errorf("a running goal reads %q in the thread and %q in its pane", got.GoalRow, got.GoalShown)
	}
	sentOne("the pause", got.GoalPause, "session.command", map[string]any{"command": "goal", "goal": map[string]any{"do": "pause"}})
	if !got.BusyReview.Off || !strings.Contains(got.BusyReview.Desc, "turn") {
		t.Errorf("the review of a busy thread is off %v and says %q", got.BusyReview.Off, got.BusyReview.Desc)
	}
	if strings.Join(got.Procs, "|") != "npm run dev -- --port 5173|go test ./checkout/... -count=200" {
		t.Errorf("the processes listed are %v", got.Procs)
	}
	sentOne("the stop of one", got.StopOne, "task.stop", map[string]any{"id": "proc-1"})
	sentOne("the stop of all", got.StopAll, "session.command", map[string]any{"command": "stop"})
	if strings.Join(got.Tools, ",") != "Where it lives,What it has,Session" {
		t.Errorf("the tools of the thread have the sections %v", got.Tools)
	}
	if strings.Join(got.Mcp, ",") != "github,docs" || got.McpActs {
		t.Errorf("the MCP servers of the thread read %v, with actions %v — they are only read", got.Mcp, got.McpActs)
	}
	if len(got.Catalog) != 1 || got.Catalog[0] != "/api/session/models?name="+name {
		t.Errorf("the models were asked as %v, expected of the session itself", got.Catalog)
	}
}

// The commands of codex's own the panel sends are the ones the host takes from
// a codex thread: a command the panel knows and the host does not is a button
// that is always refused, and one the host takes with nothing to press it
// with is unfinished work.
func TestCodexCommandListMatchesTheHost(t *testing.T) {
	body := srcFiles(t)[registryFile]
	const head = "export const CODEX_COMMANDS = {"
	at := strings.Index(body, head)
	if at < 0 {
		t.Fatalf("%s has no CODEX_COMMANDS — nothing names the commands of codex", registryFile)
	}
	rest := body[at+len(head):]
	block := rest[:strings.Index(rest, "\n};")]
	onScreen := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s{4}"?([a-z][a-z-]*)"?:\s*\{`).FindAllStringSubmatch(block, -1) {
		onScreen[m[1]] = true
	}
	for name := range action.CodexCommands {
		if !onScreen[name] {
			t.Errorf("a codex thread takes /%s, and the panel has nothing to send it with", name)
		}
	}
	for name := range onScreen {
		if !action.CodexCommands[name] {
			t.Errorf("the panel sends /%s to codex, which the host refuses", name)
		}
	}
}
