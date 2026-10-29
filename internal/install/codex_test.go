package install

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/codex/codextest"
	"aacpanel/internal/contours"
	"aacpanel/internal/toolset"
)

// codexGolden are hooks of a hooks.json and the hash codex 0.162.1 answers
// hooks/list with for each: its own reading of the entry, against which the
// installer's has to come out the same, or the trust it writes is the trust
// of another hook and codex passes the hook over.
const codexGolden = `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "test -f /home/u/aacpanel/agent/ask-hook.py && python3 /home/u/aacpanel/agent/ask-hook.py", "timeout": 5}]},
      {"matcher": "", "hooks": [{"type": "command", "command": "echo \"a <b> & 'c'\" ü", "timeout": 0, "async": true, "statusMessage": "checks"}]}
    ],
    "UserPromptSubmit": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "AACP_STATE_DIR='/srv/st' /home/u/bin/aacpanel-exec hook prompt-stamp", "timeout": 5}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "bash /home/u/atlas-scripts/cost-snapshot/cost-snapshot.sh"}]}
    ],
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "bash /home/u/atlas-scripts/agent-context/session-context.sh codex", "timeout": 5, "additionalContextLimit": 2500}]},
      {"hooks": [{"type": "command", "command": "bash /home/u/x.sh", "timeout": 5, "additionalContextLimit": 100}]}
    ]
  }
}`

func TestTheTrustIsTheHashCodexComputes(t *testing.T) {
	want := map[string]string{
		"pre_tool_use:0:0":       "sha256:5296f9998ed85701ded4ad93cbaf4476319438b46a4ee2907ad692ca2b09330c",
		"pre_tool_use:1:0":       "sha256:142172ff7a695d23b87c343e96909a8cd3379d706b5afd98e7ee8e65b87cba13",
		"user_prompt_submit:0:0": "sha256:1225baa279130fad49400cb96052e9160b512e8339cbb7d2956e03f8aa345653",
		"stop:0:0":               "sha256:5990e139e8dae8628d55c042badc077f07ff3f30530f9e3a06701df1604d71ef",
		"session_start:0:0":      "sha256:3c5b0f06177dbfc25dd407472dec9423be17feb8187548d0079874e498ddffc8",
		"session_start:1:0":      "sha256:4bdc2bc1616aeae263e65da3e2dfa3da07f5a080c1f558048930209e773f5ba2",
	}
	o, err := parseSettings([]byte(codexGolden))
	if err != nil {
		t.Fatal(err)
	}
	places := codexPlaces(o, "h")
	if len(places) != len(want) {
		t.Fatalf("%d places, want %d", len(places), len(want))
	}
	for _, p := range places {
		key := strings.TrimPrefix(p.Key, "h:")
		if got := codexHash(p.Event, p.Group, p.Entry); got != want[key] {
			t.Errorf("%s: %s, codex says %s", key, got, want[key])
		}
	}
}

const codexHome = "/home/u/.codex"

var codexPath = codexHooksFile(codexHome)

// atlasCodex is a codex home wired by hand: the question relay and the
// stamp as bare python3 of another clone, the machine's own hooks beside
// them.
const atlasCodex = `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "python3 /srv/old/agent/ask-hook.py", "timeout": 5}]}
    ],
    "UserPromptSubmit": [
      {"hooks": [{"type": "command", "command": "python3 /srv/old/deploy/claude/prompt-stamp.py", "timeout": 5}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "bash /home/u/atlas-scripts/cost-snapshot/cost-snapshot.sh"}]}
    ],
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "bash /home/u/atlas-scripts/agent-context/session-context.sh codex", "timeout": 5}]}
    ]
  }
}
`

// atlasConfig is the config.toml of that home: the trust codex wrote for the
// hooks as they were, the bridge of the machine trusted, the rest of the file
// codex's and the person's.
const atlasConfig = `model = "gpt-6"

[mcp_servers.aacpanel]
command = "/home/u/bin/aacpanel-exec"
args = ["-mcp"]

[hooks.state]

[hooks.state."cc-task-manager@x:hooks/hooks.json:session_end:0:0"]
trusted_hash = "sha256:aaa"

[hooks.state."/home/u/.codex/hooks.json:pre_tool_use:0:0"]
trusted_hash = "sha256:old"

[hooks.state."/home/u/.codex/hooks.json:stop:0:0"]
trusted_hash = "sha256:5990e139e8dae8628d55c042badc077f07ff3f30530f9e3a06701df1604d71ef"

[plugins."x@y"]
enabled = true
`

func wireCodex(t *testing.T, hooks, config string, w Wiring) (string, string, bool) {
	t.Helper()
	h, c, changed, err := WireCodex([]byte(hooks), []byte(config), codexPath, w)
	if err != nil {
		t.Fatalf("WireCodex: %v", err)
	}
	return string(h), string(c), changed
}

// trustOf is the trusted hash config.toml keeps for a place of the hooks.
func trustOf(config, place string) string {
	doc := parseTOML([]byte(config))
	for _, l := range doc.states(codexPath)[codexPath+":"+place] {
		if k, v, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == "trusted_hash" {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

// hashAt is the hash of the entry at a place of a hooks.json, as codex reads
// it.
func hashAt(t *testing.T, hooks, place string) string {
	t.Helper()
	o, err := parseSettings([]byte(hooks))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range codexPlaces(o, codexPath) {
		if p.Key == codexPath+":"+place {
			return codexHash(p.Event, p.Group, p.Entry)
		}
	}
	t.Fatalf("no entry at %s:\n%s", place, hooks)
	return ""
}

// foreignLines are the lines of a config.toml outside the tables of the
// hooks of the home: what the wiring must leave as they are.
func foreignLines(config string) []string {
	doc := parseTOML([]byte(config))
	var out []string
	in := false
	for _, l := range doc.lines {
		if _, header := tableKey(l); header {
			k, ok := stateKey(l)
			in = ok && strings.HasPrefix(k, codexPath+":")
		}
		if !in && strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// The update over a home wired by hand rewrites the relay and the stamp where
// they stand and trusts them as they are now, leaves the machine's hooks and
// their trust, and touches no other line of config.toml.
func TestTheHooksOfCodexAreWiredAndTrusted(t *testing.T) {
	w := Wiring{Clone: repo, Exec: executor, Kit: append(slices.Clone(fullKit.Kit), "stamp")}
	hooks, config, changed := wireCodex(t, atlasCodex, atlasConfig, w)
	if !changed {
		t.Fatal("nothing changed")
	}
	if got := commandsOf(t, hooks, "PreToolUse"); !slices.Equal(got, []string{executor + " hook ask-hook"}) {
		t.Errorf("the relay: %q", got)
	}
	if got := commandsOf(t, hooks, "UserPromptSubmit"); !slices.Equal(got, []string{"AACP_HOOK_CODEX_HOME=" + codexHome + " " + executor + " hook prompt-stamp"}) {
		t.Errorf("the stamp: %q", got)
	}
	for e, want := range map[string][]string{"Stop": {"bash /home/u/atlas-scripts/cost-snapshot/cost-snapshot.sh",
		"AACP_HOOK_CODEX_HOME=" + codexHome + " " + executor + " hook checklist-reminder"},
		"SessionStart": {"bash /home/u/atlas-scripts/agent-context/session-context.sh codex"}} {
		if got := commandsOf(t, hooks, e); !slices.Equal(got, want) {
			t.Errorf("the machine's %s: %q", e, got)
		}
	}
	for _, place := range []string{"pre_tool_use:0:0", "user_prompt_submit:0:0", "stop:1:0"} {
		if got, want := trustOf(config, place), hashAt(t, hooks, place); got != want {
			t.Errorf("%s is trusted as %q, codex reads it as %q:\n%s", place, got, want, config)
		}
	}
	if got := trustOf(config, "stop:0:0"); got != "sha256:5990e139e8dae8628d55c042badc077f07ff3f30530f9e3a06701df1604d71ef" {
		t.Errorf("the trust of the machine's bridge became %q", got)
	}
	if got, want := foreignLines(config), foreignLines(atlasConfig); !slices.Equal(got, want) {
		t.Errorf("the rest of config.toml changed:\n%s", config)
	}
	if strings.Count(config, `[hooks.state."/home/u/.codex/hooks.json:pre_tool_use:0:0"]`) != 1 {
		t.Errorf("the relay's table is not the one it had:\n%s", config)
	}
	again, againConfig, changed := wireCodex(t, hooks, config, w)
	if changed || again != hooks || againConfig != config {
		t.Errorf("a second wiring changed the home:\n%s\n%s", again, againConfig)
	}
}

// The trust goes with the place: an entry of the person's that moves up when
// the panel's before it goes out takes its state along — trusted or not,
// enabled or not — and the place left over loses its table.
func TestTheTrustOfAHookThatMovesGoesWithIt(t *testing.T) {
	hooks := `{"hooks": {
  "PreToolUse": [
    {"matcher": "AskUserQuestion", "hooks": [
      {"type": "command", "command": "python3 /srv/old/agent/ask-hook.py", "timeout": 5},
      {"type": "command", "command": "bash ~/mine.sh"}
    ]},
    {"matcher": "Bash", "hooks": [{"type": "command", "command": "bash ~/guard.sh"}]}
  ]
}}`
	config := `[hooks.state."/home/u/.codex/hooks.json:pre_tool_use:0:0"]
trusted_hash = "sha256:relay"

[hooks.state."/home/u/.codex/hooks.json:pre_tool_use:0:1"]
trusted_hash = "sha256:mine"
enabled = false

[hooks.state."/home/u/.codex/hooks.json:pre_tool_use:1:0"]
trusted_hash = "sha256:guard"
`
	out, outConfig, changed, err := UnwireCodex([]byte(hooks), []byte(config), codexPath)
	if err != nil || !changed {
		t.Fatalf("unwire: %v, changed %v", err, changed)
	}
	if got := commandsOf(t, string(out), "PreToolUse"); !slices.Equal(got, []string{"bash ~/mine.sh", "bash ~/guard.sh"}) {
		t.Errorf("the person's hooks: %q", got)
	}
	doc := parseTOML(outConfig)
	states := doc.states(codexPath)
	want := map[string][]string{
		codexPath + ":pre_tool_use:0:0": {`trusted_hash = "sha256:mine"`, "enabled = false"},
		codexPath + ":pre_tool_use:1:0": {`trusted_hash = "sha256:guard"`},
	}
	if len(states) != len(want) {
		t.Errorf("the tables left: %q", states)
	}
	for k, v := range want {
		if !slices.Equal(states[k], v) {
			t.Errorf("%s holds %q, want %q", k, states[k], v)
		}
	}
}

// A part taken off the kit takes its hook out of codex and its trust with it.
func TestAPartTakenOffTakesItsTrust(t *testing.T) {
	full := Wiring{Clone: repo, Exec: executor, Kit: append(slices.Clone(fullKit.Kit), "stamp")}
	hooks, config, _ := wireCodex(t, atlasCodex, atlasConfig, full)
	hooks, config, changed := wireCodex(t, hooks, config, fullKit)
	if !changed || len(commandsOf(t, hooks, "UserPromptSubmit")) != 0 {
		t.Fatalf("the stamp stayed:\n%s", hooks)
	}
	if trustOf(config, "user_prompt_submit:0:0") != "" || strings.Contains(config, "user_prompt_submit") {
		t.Errorf("the stamp's trust stayed:\n%s", config)
	}
}

func TestCodexStopRemindersFollowTheKitAndKeepTheirTrust(t *testing.T) {
	w := Wiring{Clone: repo, Exec: executor, State: "/srv/panel state", Kit: []string{"relay", "nudge", "background"}}
	hooks, config, _ := wireCodex(t, atlasCodex, atlasConfig, w)
	want := []string{"bash /home/u/atlas-scripts/cost-snapshot/cost-snapshot.sh",
		"AACP_HOOK_CODEX_HOME=/home/u/.codex " + executor + " hook checklist-reminder",
		"AACP_HOOK_CODEX_HOME=/home/u/.codex AACP_STATE_DIR='/srv/panel state' " + executor + " hook background-reminder"}
	if got := commandsOf(t, hooks, "Stop"); !slices.Equal(got, want) {
		t.Fatalf("Stop commands %q, want %q", got, want)
	}
	for i, name := range []string{"checklist-reminder", "background-reminder"} {
		place := "stop:" + strconv.Itoa(i+1) + ":0"
		if trustOf(config, place) != hashAt(t, hooks, place) || timeoutOf(t, hooks, "Stop", want[i+1]) != "5" {
			t.Fatalf("%s is not trusted with its timeout", name)
		}
	}
	again, againConfig, changed := wireCodex(t, hooks, config, w)
	if changed || again != hooks || againConfig != config {
		t.Fatal("a second wiring changed the Stop hooks or their trust")
	}
	w.Kit = []string{"relay"}
	hooks, config, _ = wireCodex(t, hooks, config, w)
	if got := commandsOf(t, hooks, "Stop"); !slices.Equal(got, want[:1]) || trustOf(config, "stop:1:0") != "" || trustOf(config, "stop:2:0") != "" {
		t.Fatalf("unchecked reminders or their trust stayed:\n%s\n%s", hooks, config)
	}
}

// A home with neither file gets both: the hooks and the trust of each.
func TestAFreshHomeGetsTheHooksAndTheirTrust(t *testing.T) {
	w := Wiring{Clone: repo, Exec: executor, Kit: append(slices.Clone(fullKit.Kit), "stamp")}
	hooks, config, changed := wireCodex(t, "", "", w)
	if !changed {
		t.Fatal("nothing changed")
	}
	for _, place := range []string{"pre_tool_use:0:0", "user_prompt_submit:0:0"} {
		if got := trustOf(config, place); got == "" || got != hashAt(t, hooks, place) {
			t.Errorf("%s is trusted as %q:\n%s\n%s", place, got, hooks, config)
		}
	}
	if !strings.HasPrefix(config, "[hooks.state.") || !strings.HasSuffix(config, "\"\n") {
		t.Errorf("config.toml is laid out as:\n%s", config)
	}
	back, backConfig, _, err := UnwireCodex([]byte(hooks), []byte(config), codexPath)
	if err != nil || !emptySettings(back) || strings.TrimSpace(string(backConfig)) != "" {
		t.Errorf("unwired: %v\n%s\n%s", err, back, backConfig)
	}
}

// A config.toml that names the hooks' state in a form the wiring does not
// edit is left as it is, and so is the hooks.json: a table added beside an
// inline one would define the key twice, and codex would read neither file.
func TestATrustWrittenInAnotherFormIsLeftAlone(t *testing.T) {
	config := "[hooks.state]\n\"/home/u/.codex/hooks.json:pre_tool_use:0:0\" = { trusted_hash = \"sha256:x\" }\n"
	if _, _, _, err := WireCodex([]byte(atlasCodex), []byte(config), codexPath, fullKit); err == nil {
		t.Error("an inline table of the trust was taken for editable")
	}
}

// The step of codex: a home host.env names or ~/.codex, wired in place, kept
// aside before the first change, recorded, and taken out by the undo.
func TestTheCodexStepWiresEveryHomeAndTheUndoTakesItOut(t *testing.T) {
	srv := codextest.New(t)
	t.Setenv("TMPDIR", srv.Home)
	g := newRig(t)
	g.m.Disk = srv.Home
	g.in.S.was.host = map[string]string{contours.CodexHomesEnv: srv.Home}
	home := srv.Home
	g.write(codexHooksFile(home), atlasCodex, 0o600)
	config := strings.ReplaceAll(atlasConfig, codexPath, codexHooksFile(home))
	g.write(codexConfigFile(home), config, 0o600)
	srv.Hooks(codexHookListing(home, g.in.execPath(), g.in.state(), "trusted", true, g.in.S.Has("stamp"), 1))
	step := g.step("codex")
	if step == nil {
		t.Fatal("no step for the hooks of codex")
	}
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	hooks := g.read(codexHooksFile(home))
	if got := commandsOf(t, hooks, "PreToolUse"); !slices.Equal(got, []string{g.in.execPath() + " hook ask-hook"}) {
		t.Errorf("the relay: %q", got)
	}
	lines := g.lines()
	at := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "codex "+home+" ") })
	if at < 0 || !strings.HasPrefix(lines[at], "codex "+home+" hooks=") || !strings.Contains(lines[at], " config=") {
		t.Fatalf("the manifest holds %q", lines)
	}
	last := lines[at]
	if strings.Contains(g.read(codexConfigFile(home)), "user_prompt_submit") != g.in.S.Has("stamp") {
		t.Errorf("the trust of the stamp does not follow the kit:\n%s", g.read(codexConfigFile(home)))
	}
	if orig := metaValue(strings.SplitN(last, " ", 3)[2], "config"); g.read(orig) != config {
		t.Errorf("config.toml was not kept aside as it was: %s", orig)
	}
	if !g.already(step) {
		t.Error("the step is not done after it ran")
	}
	if err := UndoKind(g.r, Entry{Kind: string(Codex), Target: home, Meta: strings.SplitN(last, " ", 3)[2]}); err != nil {
		t.Fatal(err)
	}
	if got := commandsOf(t, g.read(codexHooksFile(home)), "PreToolUse"); len(got) != 0 {
		t.Errorf("the undo left %q", got)
	}
	if got := g.read(codexConfigFile(home)); strings.Contains(got, "pre_tool_use") || !strings.Contains(got, "stop:0:0") {
		t.Errorf("the undo left the trust as:\n%s", got)
	}
}

// The daemon answers with its own trust verdict, independent of the hash
// Verify can recompute from config.toml. Both the wiring step and the chain
// used by install.sh check must turn red when that verdict changes.
func TestTheCodexCheckAsksTheDaemonAboutTrust(t *testing.T) {
	srv := codextest.New(t)
	t.Setenv("TMPDIR", srv.Home)
	g := newRig(t, answer("--kit", "+stamp"))
	g.m.Disk = srv.Home
	g.in.S.was.host = map[string]string{contours.CodexHomesEnv: srv.Home}
	g.chainUp()
	step := g.step("codex")
	srv.Hooks(codexHookListing(srv.Home, g.in.execPath(), g.in.state(), "trusted", true, true))
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, trust, reason string
		enabled             bool
	}{
		{"matching formula", "trusted", "", true},
		{"another formula", "modified", "codex does not trust hook prompt-stamp", true},
		{"disabled", "trusted", "is disabled", false},
		{"missing", "", "0 times, want once", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.Hooks(codexHookListing(srv.Home, g.in.execPath(), g.in.state(), tc.trust, tc.enabled, true))
			said := g.said()
			for _, check := range []*Step{step, g.in.CheckStep()} {
				err := check.Verify(g.r)
				if tc.reason == "" {
					if err != nil {
						t.Fatalf("%s: %v; %v", check.ID, err, *said)
					}
					continue
				}
				if err == nil {
					t.Fatalf("%s passed with %s", check.ID, tc.name)
				}
				text := err.Error() + strings.Join(*said, "\n")
				if !strings.Contains(text, tc.reason) {
					t.Fatalf("%s lacks %q: %s", check.ID, tc.reason, text)
				}
				if tc.trust == "modified" && !strings.Contains(text, "the trust formula changed?") {
					t.Fatalf("%s has no diagnosis of a changed formula: %s", check.ID, text)
				}
			}
		})
	}
	if len(srv.Calls("hooks/list")) != 9 {
		t.Fatal("Verify and the check did not ask the daemon each time")
	}
}

func codexHookListing(home, executor, state, trust string, enabled, stamp bool, stopGroup ...int) map[string]any {
	path := codexHooksFile(home)
	hooks := []any{map[string]any{"key": path + ":pre_tool_use:0:0", "handlerType": "command",
		"command": executor + " hook ask-hook", "enabled": true, "trustStatus": "trusted"}}
	group := 0
	if len(stopGroup) > 0 {
		group = stopGroup[0]
	}
	hooks = append(hooks, map[string]any{"key": path + ":stop:" + strconv.Itoa(group) + ":0", "handlerType": "command",
		"command": "AACP_HOOK_CODEX_HOME=" + shellQuote(home) + " " + executor + " hook checklist-reminder", "enabled": true, "trustStatus": "trusted"})
	if stamp && trust != "" {
		hooks = append(hooks, map[string]any{"key": path + ":user_prompt_submit:0:0", "handlerType": "command",
			"command": "AACP_HOOK_CODEX_HOME=" + shellQuote(home) + " AACP_STATE_DIR=" + shellQuote(state) + " " + executor + " hook prompt-stamp", "enabled": enabled, "trustStatus": trust})
	}
	return map[string]any{"cwd": home, "hooks": hooks, "errors": []any{}, "warnings": []string{}}
}

func TestTheCodexCheckStartsAProgramWithoutADaemon(t *testing.T) {
	g := newRig(t, answer("--kit", "+stamp"))
	home := filepath.Join(g.home, ".codex")
	program := filepath.Join(home, "packages", "standalone", "current", "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(program), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	fixture := codextest.HooksProgram(t, codexHookListing(home, g.in.execPath(), g.in.state(), "trusted", true, true))
	if err := os.Symlink(fixture, program); err != nil {
		t.Fatal(err)
	}
	if _, err := g.in.S.Settle(BlockX); err != nil {
		t.Fatal(err)
	}
	if got, _ := g.in.S.Value("codex"); got.Value != program {
		t.Fatalf("the standalone program off PATH was not found: %+v", got)
	}
	g.write(filepath.Join(g.state, "host.env"), "", 0o644)
	if err := g.do(g.step("hostenv")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.read(filepath.Join(g.state, "host.env")), "AACP_CODEX="+program+"\n") {
		t.Fatal("host.env does not name the standalone program")
	}
	g.chainUp()
	step := g.step("codex")
	// The answer is the program the plan promised, even if the installer's
	// environment changes before the hooks are checked.
	g.m.Vars["AACP_CODEX"] = "/missing/codex"
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	// check and update rebuild the survey from host.env, without inheriting
	// the process environment that happened to find codex during installation.
	dotenv := filepath.Join(g.clone, ".env")
	g.write(dotenv, g.read(dotenv)+"AACP_STATE_DIR="+g.state+"\n", 0o600)
	f := g.in.S.Facts
	f.StateDir, f.Mode = g.state, Upgrade
	g.in.S = Kept(g.m, Inspection{Facts: f}, now)
	if got, _ := g.in.S.Value("codex"); got.Value != program || got.Source != "current setting" {
		t.Fatalf("check and update did not keep the saved program: %+v", got)
	}
	for _, trust := range []string{"trusted", "modified"} {
		fixture := codextest.HooksProgram(t, codexHookListing(home, g.in.execPath(), g.in.state(), trust, true, true))
		t.Run(trust, func(t *testing.T) {
			if trust == "trusted" {
				if err := os.Remove(program); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(fixture, program); err != nil {
					t.Fatal(err)
				}
			} else {
				// An explicit answer must also reach the check rather than the
				// discovered default or the value read from host.env.
				q := find(t, g.in.S, BlockX, "codex")
				g.in.S.Set(q, fixture, "you")
			}
			said := g.said()
			for _, check := range []*Step{step, g.in.CheckStep()} {
				err := check.Verify(g.r)
				if trust == "trusted" && err != nil || trust == "modified" && err == nil {
					t.Fatalf("%s with trust %s: %v; %v", check.ID, trust, err, *said)
				}
				text := strings.Join(*said, "\n")
				if err != nil {
					text += err.Error()
				}
				if !strings.Contains(text, "asked a codex app-server started for the check") {
					t.Fatalf("%s does not name the server: %s", check.ID, text)
				}
				if trust == "modified" && !strings.Contains(text, "the trust formula changed?") {
					t.Fatalf("%s has no diagnosis of a changed formula: %s", check.ID, text)
				}
			}
		})
	}
}

// A machine without codex has no step for it.
func TestNoCodexNoStep(t *testing.T) {
	g := newRig(t)
	if _, err := os.Stat(filepath.Join(g.home, ".codex")); err == nil {
		t.Fatal("the rig has codex")
	}
	if slices.ContainsFunc(g.in.Steps(), func(s *Step) bool { return s.ID == "codex" }) {
		t.Error("a machine without codex has a step for it")
	}
}

// ---- the panel's server in config.toml ----

// serverWant is the panel's server as a run with a state directory of its own
// puts it into a home.
var serverWant = codexServer{Command: executor, Args: []string{"-mcp"},
	Env: map[string]string{"AACP_STATE_DIR": "/srv/state"}, Approve: toolset.CodexAllowed()}

func wireServer(t *testing.T, config string, want codexServer) (string, bool) {
	t.Helper()
	out, changed, err := WireCodexServer([]byte(config), want)
	if err != nil {
		t.Fatalf("WireCodexServer: %v", err)
	}
	return string(out), changed
}

// serverTables are the tables of the panel's server in a config.toml, each
// with its keys and their values as codex reads them.
func serverTables(t *testing.T, config string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	var keys map[string]any
	for _, l := range parseTOML([]byte(config)).lines {
		if parts, header := tableKey(l); header {
			keys = nil
			if hasPrefix(parts, codexServerKey) {
				keys = map[string]any{}
				out[strings.Join(parts[len(codexServerKey):], ".")] = keys
			}
			continue
		}
		key, value, ok := keyLine(l)
		if !ok || keys == nil {
			continue
		}
		v, ok := tomlLineValue(value)
		if !ok {
			t.Fatalf("%q does not read", l)
		}
		keys[strings.Join(key, ".")] = v
	}
	return out
}

// approved are the tools whose tables approve them.
func approved(tables map[string]map[string]any) []string {
	var out []string
	for name, keys := range tables {
		if tool, ok := strings.CutPrefix(name, "tools."); ok && keys["approval_mode"] == "approve" {
			out = append(out, tool)
		}
	}
	slices.Sort(out)
	return out
}

// A home without a config.toml gets the server with the command, the flag and
// the state directory of claude's, and the approval of exactly the tools a
// thread the panel starts calls without asking; taking it out leaves nothing.
func TestThePanelsServerGoesIntoAFreshHome(t *testing.T) {
	config, changed := wireServer(t, "", serverWant)
	if !changed {
		t.Fatal("nothing changed")
	}
	tables := serverTables(t, config)
	server := tables[""]
	if server["command"] != executor || !reflect.DeepEqual(server["args"], []any{"-mcp"}) ||
		!reflect.DeepEqual(server["env"], map[string]any{"AACP_STATE_DIR": "/srv/state"}) {
		t.Errorf("the server is %v:\n%s", server, config)
	}
	want := slices.Sorted(slices.Values(toolset.CodexAllowed()))
	if got := approved(tables); !slices.Equal(got, want) || len(tables) != len(want)+1 {
		t.Errorf("approved %q, want %q:\n%s", got, want, config)
	}
	if again, changed := wireServer(t, config, serverWant); changed || again != config {
		t.Errorf("a second wiring changed the file:\n%s", again)
	}
	back, changed, err := UnwireCodexServer([]byte(config))
	if err != nil || !changed || strings.TrimSpace(string(back)) != "" {
		t.Errorf("unwired: %v, %v:\n%s", err, changed, back)
	}
}

// personServer is a config.toml with a server of the panel's name from
// another install and a table of a tool the person approved for themselves,
// among the person's own tables.
const personServer = `model = "gpt-6"

[mcp_servers.aacpanel]
command = "/old/bin/aacpanel-exec"   # an old clone
args = [ '-mcp' ]
startup_timeout_sec = 20

[mcp_servers.aacpanel.tools.send_to_session]
approval_mode = "approve"

# the person's own servers
[mcp_servers.github]
command = "gh-mcp"

[projects."/home/u/code"]
trust_level = "trusted"
`

// A server of the name is the panel's: its command and environment are put
// in place where they stand, an argument written in another form that reads
// the same stays as it is written, the person's keys and tables stay, and
// the approvals go after its last table; no line outside it changes. Taking
// it out takes every table under it, and the rest of the file is as it was.
func TestAServerOfTheNameIsPutInPlaceWhereItStands(t *testing.T) {
	config, changed := wireServer(t, personServer, serverWant)
	if !changed {
		t.Fatal("nothing changed")
	}
	tables := serverTables(t, config)
	if s := tables[""]; s["command"] != executor || s["startup_timeout_sec"] != "20" || s["env"] == nil {
		t.Errorf("the server is %v:\n%s", s, config)
	}
	if !strings.Contains(config, "args = [ '-mcp' ]\n") {
		t.Errorf("the arguments were rewritten:\n%s", config)
	}
	if tables["tools.send_to_session"]["approval_mode"] != "approve" {
		t.Errorf("the person's approval is gone:\n%s", config)
	}
	if strings.Index(config, "[mcp_servers.aacpanel.tools.checklist]") > strings.Index(config, "# the person's own servers") {
		t.Errorf("the approvals do not follow the server:\n%s", config)
	}
	outside := func(c string) []string {
		var out []string
		var in bool
		for _, l := range parseTOML([]byte(c)).lines {
			if parts, header := tableKey(l); header {
				in = hasPrefix(parts, codexServerKey)
			}
			if !in && strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
		return out
	}
	if got, want := outside(config), outside(personServer); !slices.Equal(got, want) {
		t.Errorf("the lines outside the server changed:\n%s", config)
	}
	if again, changed := wireServer(t, config, serverWant); changed || again != config {
		t.Errorf("a second wiring changed the file:\n%s", again)
	}
	back, _, err := UnwireCodexServer([]byte(config))
	want := "model = \"gpt-6\"\n\n# the person's own servers\n[mcp_servers.github]\ncommand = \"gh-mcp\"\n\n[projects.\"/home/u/code\"]\ntrust_level = \"trusted\"\n"
	if err != nil || string(back) != want {
		t.Errorf("unwired: %v:\n%s", err, back)
	}
}

// The wiring gives a file back as it was once it is taken out: the server
// and its approvals were all it added.
func TestTheServerTakenOutGivesTheFileBack(t *testing.T) {
	for _, raw := range []string{atlasConfig[:strings.Index(atlasConfig, "[mcp_servers")] + atlasConfig[strings.Index(atlasConfig, "[hooks.state]"):],
		"model = \"gpt-6\"\n", "[projects.\"/x\"]\ntrust_level = \"trusted\"\n\n[plugins.\"x@y\"]\nenabled = true\n"} {
		config, _ := wireServer(t, raw, serverWant)
		back, changed, err := UnwireCodexServer([]byte(config))
		if err != nil || !changed || string(back) != raw {
			t.Errorf("unwired: %v, %v:\n%s\nwant:\n%s", err, changed, back, raw)
		}
	}
}

// The state directory is named to the server only where it is not the
// default: a run back at the default takes the environment out.
func TestTheStateDirectoryOfTheServerFollowsTheInstall(t *testing.T) {
	config, _ := wireServer(t, "", serverWant)
	plain := serverWant
	plain.Env = nil
	config, changed := wireServer(t, config, plain)
	if !changed || serverTables(t, config)[""]["env"] != nil {
		t.Errorf("the environment stayed:\n%s", config)
	}
}

// A file that names the server in a form the wiring does not edit is left as
// it is: a table added beside an inline one, or a key written past its line,
// would leave codex a file it does not read.
func TestAServerInAnotherFormIsLeftAlone(t *testing.T) {
	for _, c := range []struct {
		name, config string
		unwires      bool
	}{
		{"inline in mcp_servers", "[mcp_servers]\naacpanel = { command = \"x\" }\n", false},
		{"mcp_servers inline", "mcp_servers = { aacpanel = { command = \"x\" } }\n", false},
		{"dotted from the root", "mcp_servers.aacpanel.command = \"x\"\n", false},
		{"an array of its name", "[[mcp_servers.aacpanel]]\ncommand = \"x\"\n", false},
		{"a tool as a dotted key", "[mcp_servers.aacpanel]\ncommand = \"x\"\ntools.checklist.approval_mode = \"prompt\"\n", true},
		{"a tool as an inline table", "[mcp_servers.aacpanel.tools]\nchecklist = { approval_mode = \"prompt\" }\n", true},
		{"the environment as a table", "[mcp_servers.aacpanel]\ncommand = \"x\"\n\n[mcp_servers.aacpanel.env]\nX = \"y\"\n", true},
		{"arguments past their line", "[mcp_servers.aacpanel]\ncommand = \"x\"\nargs = [\n  \"-mcp\",\n]\n", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := WireCodexServer([]byte(c.config), serverWant); err == nil || !strings.Contains(err.Error(), "does not edit") {
				t.Errorf("wired: %v", err)
			}
			if _, _, err := UnwireCodexServer([]byte(c.config)); (err == nil) != c.unwires {
				t.Errorf("unwired: %v", err)
			}
		})
	}
}

// codexStepRig is a rig with a codex home that the daemon of the fixture
// lists as wired: its hooks trusted and enabled.
func codexStepRig(t *testing.T) (*rig, string) {
	srv := codextest.New(t)
	t.Setenv("TMPDIR", srv.Home)
	g := newRig(t)
	g.m.Disk = srv.Home
	g.in.S.was.host = map[string]string{contours.CodexHomesEnv: srv.Home}
	srv.Hooks(codexHookListing(srv.Home, g.in.execPath(), g.in.state(), "trusted", true, g.in.S.Has("stamp")))
	return g, srv.Home
}

// The step puts the server into a home, records it as a line of its own, and
// the undo of that line takes it out; a config.toml the installer made goes
// once nothing else is left in it.
func TestTheCodexStepPutsTheServerInAndTheUndoTakesItOut(t *testing.T) {
	g, home := codexStepRig(t)
	step := g.step("codex")
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	config := g.read(codexConfigFile(home))
	tables := serverTables(t, config)
	if s := tables[""]; s["command"] != g.in.execPath() || !reflect.DeepEqual(s["env"], map[string]any{"AACP_STATE_DIR": g.state}) {
		t.Errorf("the server is %v:\n%s", s, config)
	}
	if got := approved(tables); len(got) != len(toolset.CodexAllowed()) {
		t.Errorf("approved %q", got)
	}
	var mcpLine string
	for _, l := range g.lines() {
		if strings.HasPrefix(l, "codexmcp ") {
			mcpLine = l
		}
	}
	if mcpLine != "codexmcp "+home+" config=created" {
		t.Fatalf("the manifest holds %q", g.lines())
	}
	if !g.already(step) {
		t.Error("the step is not done after it ran")
	}
	g.undoAll(step)
	if _, err := os.Stat(codexConfigFile(home)); !os.IsNotExist(err) {
		t.Errorf("the config.toml the installer made is left:\n%s", g.read(codexConfigFile(home)))
	}
}

// A server in place as this install writes it is not the installer's to take
// back: nothing is recorded for it, and the undo leaves it, as a claude
// account's server in place is left.
func TestAServerInPlaceIsNotRecorded(t *testing.T) {
	g, home := codexStepRig(t)
	config, _ := wireServer(t, "model = \"gpt-6\"\n", g.in.codexServerWant())
	g.write(codexConfigFile(home), config, 0o600)
	step := g.step("codex")
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(g.lines(), func(l string) bool { return strings.HasPrefix(l, "codexmcp ") }) {
		t.Fatalf("a server in place was recorded: %q", g.lines())
	}
	g.undoAll(step)
	if !codexHasServer([]byte(g.read(codexConfigFile(home)))) {
		t.Errorf("the undo took out a server it did not write:\n%s", g.read(codexConfigFile(home)))
	}
}

// An install by hand whose codex home holds the server is taken over with it,
// as a claude account's server is: uninstall takes it out by its name.
func TestAServerOfAnInstallByHandIsTakenOver(t *testing.T) {
	g, home := codexStepRig(t)
	g.write(codexConfigFile(home), "[mcp_servers.aacpanel]\ncommand = \"/srv/old/aacpanel-exec\"\n", 0o600)
	if !slices.Contains(g.in.traces(g.r), trace{CodexMCP, home, ""}) {
		t.Errorf("the traces are %v", g.in.traces(g.r))
	}
}

// Uninstall names the server it takes out of a codex home in its plan.
func TestUninstallNamesTheServerOfCodex(t *testing.T) {
	g := newRig(t)
	home := filepath.Join(g.home, ".codex")
	rm := NewRemoval(g.m, g.in.f(), []Entry{{Step: "codex", Kind: string(CodexMCP), Target: home, Meta: "config=created"}}, filepath.Join(g.home, ".cache"))
	var plan []string
	for _, r := range rm.Plan() {
		plan = append(plan, r.Text)
	}
	if !slices.Contains(plan, "  · ~/.codex/config.toml: the server aacpanel taken out, with the tables under it") {
		t.Errorf("the plan is %q", plan)
	}
}
