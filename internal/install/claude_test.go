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

// ---- claude by its native installer ----

// missingClaude is a machine the check found without claude.
var missingClaude = Missing{Name: "claude", Why: "the panel starts and watches claude sessions", Command: nativeCommand}

func stepIDs(in *Install) []string {
	var out []string
	for _, s := range in.Steps() {
		out = append(out, s.ID)
	}
	return out
}

// TestClaudeIsInstalledWhenMissingAndAgreedTo: the native installer runs
// only for a machine without claude whose answer is yes, as the user and
// before the settings of claude are wired, and its program is recorded as
// the packages of apt are.
func TestClaudeIsInstalledWhenMissingAndAgreedTo(t *testing.T) {
	if slices.Contains(stepIDs(newRig(t).in), "claude-install") {
		t.Error("a machine with claude runs the native installer")
	}
	g := newRig(t, lacking(missingClaude), answer("--install-claude", "yes"))
	ids := stepIDs(g.in)
	at := slices.Index(ids, "claude-install")
	if at < 0 || at > slices.Index(ids, "claude") || at < slices.Index(ids, "usermgr") {
		t.Fatalf("the steps are %q: the native installer comes after the root part and before the settings of claude", ids)
	}

	version := []string{"/usr/bin/claude", "--version"}
	g.fails(version, "bash: /usr/bin/claude: No such file or directory")
	body := "#!/bin/bash\nset -e\necho installing\n"
	g.m.Pages = map[string]string{NativeInstaller: body}
	script := filepath.Join(g.in.f().InstallDir, "claude-install.sh")
	run := []string{"bash", script}
	g.says(run, "Setting up Claude Code...\n")
	var ran string
	g.m.Effects[Command(run[0], run[1:]...)] = func(Cmd) error {
		raw, err := os.ReadFile(script)
		ran = string(raw)
		g.says(version, "2.1.283 (Claude Code)\n")
		return err
	}
	lines := g.said()
	if err := g.do(g.step("claude-install")); err != nil {
		t.Fatal(err)
	}
	if ran != body {
		t.Errorf("bash ran %q, want the script as it downloaded", ran)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Error("the script stays in the installer's directory after it ran")
	}
	if want := []string{"pkg claude by-installer native"}; !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	for _, c := range g.m.Ran {
		if strings.Contains(c, "sudo") {
			t.Errorf("the native installer ran as root: %s", c)
		}
	}
	if !slices.Contains(*lines, "✓ claude 2.1.283 · /usr/bin/claude") {
		t.Errorf("the step said %q", *lines)
	}
	if !g.already(g.step("claude-install")) {
		t.Error("claude that answers --version was installed again")
	}

	// Uninstall names it with the command that takes it away, and leaves it.
	es, err := ReadManifest(g.r.Manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	rm := NewRemoval(g.m, g.in.f(), es, "")
	const named = "claude, installed with Anthropic's native installer"
	if !slices.ContainsFunc(rm.Plan(), func(r PlanRow) bool { return r.Text == "  · "+named }) {
		t.Errorf("the plan of uninstall does not name %q", named)
	}
	if left := strings.Join(rm.Left(), "\n"); !strings.Contains(left, named+": rm -f ~/.local/bin/claude && rm -rf ~/.local/share/claude") {
		t.Errorf("what uninstall leaves does not say how claude goes:\n%s", left)
	}
}

func TestTheNativeInstallerIsDiagnosed(t *testing.T) {
	g := newRig(t, lacking(missingClaude), answer("--install-claude", "yes"))
	g.fails([]string{"/usr/bin/claude", "--version"}, "No such file or directory")
	failedWith(t, g.do(g.step("claude-install")), NativeInstaller+" did not download: dial tcp")
	g.m.Pages = map[string]string{NativeInstaller: "<html>not here</html>"}
	failedWith(t, g.do(g.step("claude-install")), NativeInstaller+" gave no installer script")
	if g.lines() != nil {
		t.Errorf("a download that failed recorded %q", g.lines())
	}
	g.m.Pages = map[string]string{NativeInstaller: "#!/bin/bash\n"}
	g.fails([]string{"bash", filepath.Join(g.in.f().InstallDir, "claude-install.sh")}, "Either curl or wget is required but neither is installed")
	f := failedWith(t, g.do(g.step("claude-install")), "Anthropic's native installer did not install claude")
	if !slices.Contains(f.Tail, "Either curl or wget is required but neither is installed") {
		t.Errorf("the stop does not show what the installer said: %q", f.Tail)
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

// TestSignInLaterNamesTheCommandThatSignsIn: the option Later names the
// command a person signs in with, as the reminder at the end does — claude
// as the answers name it, and ~/.claude without CLAUDE_CONFIG_DIR, which
// would make claude keep a second .claude.json inside it.
func TestSignInLaterNamesTheCommandThatSignsIn(t *testing.T) {
	m := desktop()
	delete(m.Files, home+"/.claude/.credentials.json")
	m.Files[home+"/.claude-work/settings.json"] = "{}"
	native := home + "/.local/bin/claude"
	s := survey(m, &Run{Yes: true, Answers: map[string]string{
		"--account": home + "/.claude," + home + "/.claude-work",
		"--claude":  native,
	}})
	for _, b := range []BlockID{BlockP, BlockA, BlockB, BlockC} {
		if _, err := s.Settle(b); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{
		"login:" + home + "/.claude":      ": " + native,
		"login:" + home + "/.claude-work": ": CLAUDE_CONFIG_DIR=~/.claude-work " + native,
	}
	qs := s.Questions(BlockL)
	if len(qs) != len(want) {
		t.Fatalf("block L asks %v", ids(qs))
	}
	for _, q := range qs {
		i := slices.IndexFunc(q.Options, func(o Option) bool { return o.Value == "later" })
		if i < 0 || !strings.HasSuffix(q.Options[i].Detail, want[q.ID]) {
			t.Errorf("%s: Later says %+v, want it to end %q", q.ID, q.Options, want[q.ID])
		}
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
