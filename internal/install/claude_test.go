package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// claudeMachine answers claude mcp the way claude does: add writes the
// entry into the file of the account its environment names — .claude.json
// in CLAUDE_CONFIG_DIR, or beside ~/.claude without it — and remove takes
// it out.
func (g *rig) claudeMachine() {
	g.says([]string{"bash", filepath.Join(g.clone, "agent/rate-snapshot.sh")}, "")
	fileOf := func(c Cmd) string {
		for _, kv := range c.Env {
			if dir, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok {
				return filepath.Join(dir, ".claude.json")
			}
		}
		return filepath.Join(g.home, ".claude.json")
	}
	add := g.mcpAdd()
	g.says(add, "Added stdio MCP server aacpanel with command: "+g.in.execPath()+" -mcp to user config\n")
	g.m.Effects[Command(add[0], add[1:]...)] = func(c Cmd) error { return g.mcpEntry(fileOf(c), g.in.mcpWant()) }
	remove := []string{"/usr/bin/claude", "mcp", "remove", "aacpanel", "-s", "user"}
	g.says(remove, "Removed MCP server aacpanel from user config\n")
	g.m.Effects[Command(remove[0], remove[1:]...)] = func(c Cmd) error { return g.mcpEntry(fileOf(c), mcpServer{}) }
}

func (g *rig) mcpAdd() []string {
	return []string{"/usr/bin/claude", "mcp", "add", "--scope", "user", "aacpanel", "-e", "AACP_STATE_DIR=" + g.state,
		"--", filepath.Join(g.home, "bin", "aacpanel-exec"), "-mcp"}
}

// mcpEntry writes the panel's server into a .claude.json, or takes it out
// for an empty one, keeping what else the file holds.
func (g *rig) mcpEntry(file string, s mcpServer) error {
	cfg := map[string]any{}
	if raw, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if s.Command == "" {
		delete(servers, "aacpanel")
	} else {
		servers["aacpanel"] = s
	}
	cfg["mcpServers"] = servers
	raw, _ := json.Marshal(cfg)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, raw, 0o600)
}

// undoAll walks the manifest back, as uninstall does, through the undo of
// the step of each line.
func (g *rig) undoAll(s *Step) {
	g.t.Helper()
	es, err := ReadManifest(g.r.Manifest.Path)
	if err != nil {
		g.t.Fatal(err)
	}
	for i := len(es) - 1; i >= 1; i-- {
		if err := g.r.Undo(s, es[i]); err != nil {
			g.t.Fatalf("undo %v: %v", es[i], err)
		}
	}
}

func TestClaudeSettingsAreWiredInEveryAccount(t *testing.T) {
	personal := "{\n  \"model\": \"opus\",\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"bash ~/bin/mine.sh\"\n          }\n        ]\n      }\n    ]\n  },\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"~/bin/line.sh '$x'\"\n  }\n}\n"
	g := newRig(t, homeFile(".claude/settings.json", personal), answer("--account", "~/.claude,~/.claude-work"))
	mine, work := filepath.Join(g.home, ".claude"), filepath.Join(g.home, ".claude-work")
	g.claudeMachine()
	if !slices.ContainsFunc(g.in.Steps(), func(s *Step) bool { return s.ID == "claude" }) {
		t.Fatal("the install has no step for claude's settings")
	}
	step := g.step("claude")
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{mine, work} {
		raw := g.read(filepath.Join(dir, "settings.json"))
		if n, _ := wiredHooks([]byte(raw), "PreToolUse", "agent/ask-hook.py"); n != 1 {
			t.Errorf("%s: the question hook %d times:\n%s", dir, n, raw)
		}
		if !strings.Contains(raw, `"command": "AACP_STATE_DIR=`+g.state+` python3 `+g.clone+`/deploy/claude/context-guard.py"`) {
			t.Errorf("%s: the context guard is not told the state directory:\n%s", dir, raw)
		}
	}
	raw := g.read(filepath.Join(mine, "settings.json"))
	if !strings.Contains(raw, "bash ~/bin/mine.sh") || !strings.HasPrefix(raw, "{\n  \"model\": \"opus\",") {
		t.Errorf("the person's settings did not stay:\n%s", raw)
	}
	if next, ok := statusNext(statusLineOf([]byte(raw))); !ok || next != "~/bin/line.sh '$x'" {
		t.Errorf("the person's status line is %q in the chain", next)
	}
	lines := g.lines()
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "json "+mine+"/settings.json orig=") || !strings.HasPrefix(lines[1], "mcp "+mine+" claude=/usr/bin/claude file="+g.home+"/.claude.json") ||
		lines[2] != "dir "+work+" created" || !strings.HasPrefix(lines[3], "json "+work+"/settings.json created sha=") || !strings.HasPrefix(lines[4], "mcp "+work+" ") {
		t.Errorf("the manifest holds %q", lines)
	}
	// ~/.claude is claude's own without CLAUDE_CONFIG_DIR, and a second
	// account is named by it; neither inherits the installer's.
	var adds [][]string
	for _, c := range g.m.Given {
		if slices.Equal(c.Argv, g.mcpAdd()) {
			adds = append(adds, append(slices.Clone(c.Env), "unset:"+strings.Join(c.Unset, ",")))
		}
	}
	if want := [][]string{{"unset:CLAUDE_CONFIG_DIR"}, {"CLAUDE_CONFIG_DIR=" + work, "unset:"}}; !slices.EqualFunc(adds, want, slices.Equal) {
		t.Errorf("claude mcp add ran with %q, want %q", adds, want)
	}
	if have, ok := g.in.mcpHas(work); !ok || !have.same(g.in.mcpWant()) || !strings.HasSuffix(g.in.mcpFile(work), ".claude-work/.claude.json") {
		t.Errorf("the second account's server: %+v %v", have, ok)
	}

	// Again: nothing to do, nothing run.
	ran := len(g.m.Ran)
	if !g.already(step) {
		t.Error("claude's settings in place were wired again")
	}
	if len(g.m.Ran) != ran {
		t.Errorf("the check of a wired machine ran %q", g.m.Ran[ran:])
	}

	// The undo gives the file back byte for byte, takes away the file the
	// installer made, and the server out of both accounts.
	g.undoAll(step)
	if got := g.read(filepath.Join(mine, "settings.json")); got != personal {
		t.Errorf("the undo left:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(work, "settings.json")); !os.IsNotExist(err) {
		t.Error("the settings the installer made are still there")
	}
	if _, ok := g.in.mcpHas(mine); ok {
		t.Error("the server is still in ~/.claude.json")
	}
}

// TestTheUndoGivesBackAFileLaidOutByHand: a file claude would lay out
// otherwise comes back from the copy kept aside, not from an unwiring.
func TestTheUndoGivesBackAFileLaidOutByHand(t *testing.T) {
	compact := `{"model":"opus","hooks":{"Stop":[]}}`
	g := newRig(t, homeFile(".claude/settings.json", compact))
	g.claudeMachine()
	step := g.step("claude")
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	// A later run with a smaller kit writes the file again: the undo still
	// goes back to what was there before the first.
	g.in.S.Set(find(t, g.in.S, BlockK, "kit"), "relay,limits,tools", "test")
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	if n := len(slices.DeleteFunc(g.lines(), func(l string) bool { return !strings.HasPrefix(l, "json ") })); n != 2 {
		t.Fatalf("the manifest holds %q", g.lines())
	}
	g.undoAll(step)
	if got := g.read(filepath.Join(g.home, ".claude", "settings.json")); got != compact {
		t.Errorf("the undo left:\n%s", got)
	}
}

// TestAFileChangedSinceLosesOnlyThePanel: after the person's own change the
// undo cannot give the old file back; it takes the panel out and leaves the
// change.
func TestAFileChangedSinceLosesOnlyThePanel(t *testing.T) {
	g := newRig(t, homeFile(".claude/settings.json", "{\n  \"model\": \"opus\"\n}\n"))
	mine := filepath.Join(g.home, ".claude")
	g.claudeMachine()
	step := g.step("claude")
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(mine, "settings.json")
	g.write(path, strings.Replace(g.read(path), "\"model\": \"opus\"", "\"model\": \"sonnet\"", 1), 0o600)
	g.undoAll(step)
	if got := g.read(path); got != "{\n  \"model\": \"sonnet\"\n}\n" {
		t.Errorf("the undo left:\n%s", got)
	}
}

func TestABrokenSettingsFileStopsTheStepAndStaysAsItWas(t *testing.T) {
	broken := "{\n  \"hooks\": {\n"
	g := newRig(t, homeFile(".claude/settings.json", broken))
	g.claudeMachine()
	path := filepath.Join(g.home, ".claude", "settings.json")
	f := failedWith(t, g.do(g.step("claude")), path+" is not valid JSON")
	if len(f.Fix) == 0 || !strings.Contains(f.Fix[0], "python3 -m json.tool "+path) {
		t.Errorf("the fix is %q", f.Fix)
	}
	if g.read(path) != broken || len(g.lines()) != 0 {
		t.Errorf("a broken file was touched: %q, manifest %q", g.read(path), g.lines())
	}
}

// TestAServerOfAnotherPathIsPutInAgain: claude refuses a second server of a
// name, so the old one goes first.
func TestAServerOfAnotherPathIsPutInAgain(t *testing.T) {
	g := newRig(t)
	mine := filepath.Join(g.home, ".claude")
	g.claudeMachine()
	if err := g.mcpEntry(g.in.mcpFile(mine), mcpServer{Command: "/old/bin/aacpanel-exec", Args: []string{"-mcp"}}); err != nil {
		t.Fatal(err)
	}
	if err := g.do(g.step("claude")); err != nil {
		t.Fatal(err)
	}
	var claude []string
	for _, c := range g.m.Ran {
		if strings.HasPrefix(c, "/usr/bin/claude mcp") {
			claude = append(claude, c)
		}
	}
	if len(claude) != 2 || !strings.Contains(claude[0], "mcp remove aacpanel") || !strings.Contains(claude[1], "mcp add") {
		t.Errorf("claude ran %q", claude)
	}
}

// TestSignInHandsTheTerminalToClaude: the answer "now" gives the terminal to
// claude of the account, and the account is looked at again after it.
func TestSignInHandsTheTerminalToClaude(t *testing.T) {
	g := newRig(t, answer("--claude-login", "now"), homeFile(".claude-work/projects/x/a.jsonl", "{}"),
		answer("--account", "~/.claude-work"))
	work := filepath.Join(g.home, ".claude-work")
	g.claudeMachine()
	var got []Handover
	g.r.Hand = func(h Handover) error {
		got = append(got, h)
		g.write(filepath.Join(work, ".credentials.json"), "{}", 0o600)
		return nil
	}
	if err := g.do(g.step("claude")); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Direct || !slices.Equal(got[0].Argv, []string{"/usr/bin/claude"}) ||
		!slices.Equal(got[0].Env, []string{"CLAUDE_CONFIG_DIR=" + work}) || got[0].Line != nil {
		t.Fatalf("the sign-in went as %+v", got)
	}
	if len(g.r.Reminders()) != 0 {
		t.Errorf("a signed-in account is left to mind: %q", g.r.Reminders())
	}
	if !g.already(g.step("claude")) {
		t.Error("a signed-in account asks to sign in again")
	}
}

func TestASignInThatDidNotHappenIsLeftToThePerson(t *testing.T) {
	g := newRig(t, answer("--claude-login", "now"))
	g.claudeMachine()
	g.r.Hand = func(Handover) error { return nil }
	if err := g.do(g.step("claude")); err != nil {
		t.Fatal(err)
	}
	if r := g.r.Reminders(); len(r) != 1 || !strings.Contains(r[0], "claude is not signed in to ~/.claude: /usr/bin/claude signs in") {
		t.Errorf("the reminders are %q", r)
	}
	// Without a terminal nothing is handed over, and the same is left.
	g = newRig(t, answer("--claude-login", "now"))
	g.claudeMachine()
	if err := g.do(g.step("claude")); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Reminders()) != 1 {
		t.Errorf("a plain run leaves %q", g.r.Reminders())
	}
}

// TestAnAccountTheWrapperSignsInIsSignedIn: an account of the wrapper's
// registry with a token file has no .credentials.json and is signed in all
// the same; one whose token file is gone is not.
func TestAnAccountTheWrapperSignsInIsSignedIn(t *testing.T) {
	m := desktop()
	reg := home + "/wrap/registry.conf"
	m.Files[DefaultStateDir+"/host.env"] = "AACP_REPO=" + clone + "\nAACP_CLAUDE_REGISTRY=" + reg + "\n"
	m.Files[reg] = "# profile | prefix | config | token | codex\n" +
		"globex | /srv/globex/ | ~/.claude-profiles/globex | ~/.vault/globex.age | -\n" +
		"lab | /srv/lab/ | ~/.claude-profiles/lab | ~/.vault/lab.age\n" +
		"personal | * | ~/.claude | -\n"
	m.Files[home+"/.vault/globex.age"] = "sealed"
	m.Files[home+"/.claude-profiles/globex/settings.json"] = "{}"
	m.Files[home+"/.claude-profiles/lab/settings.json"] = "{}"
	m.Files[home+"/.claude-keyed/settings.json"] = `{"apiKeyHelper": "~/bin/key.sh"}`
	s := survey(m, &Run{Yes: true, Answers: map[string]string{"--keep": "review"}})
	for dir, want := range map[string]bool{home + "/.claude-profiles/globex": true, home + "/.claude-profiles/lab": false,
		home + "/.claude": true, home + "/.claude-keyed": true} {
		if got, how := s.SignedIn(dir); got != want {
			t.Errorf("%s: signed in %v (%s), want %v", dir, got, how, want)
		}
	}
	if _, how := s.SignedIn(home + "/.claude-profiles/globex"); how != "signed in by the wrapper, with ~/.vault/globex.age" {
		t.Errorf("the account says %q", how)
	}
	s.Keep(Given{Value: "review"})
	for _, b := range []BlockID{BlockP, BlockA, BlockB, BlockC} {
		if _, err := s.Settle(b); err != nil {
			t.Fatal(err)
		}
	}
	if got := ids(s.Questions(BlockL)); !slices.Equal(got, []string{"login:" + home + "/.claude-profiles/lab"}) {
		t.Errorf("block L asks %v: only the account without a token is not signed in", got)
	}
}
