package install

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
	if got := commandsOf(t, hooks, "PreToolUse"); !slices.Equal(got, []string{"test -f " + repo + "/agent/ask-hook.py && python3 " + repo + "/agent/ask-hook.py"}) {
		t.Errorf("the relay: %q", got)
	}
	if got := commandsOf(t, hooks, "UserPromptSubmit"); !slices.Equal(got, []string{executor + " hook prompt-stamp"}) {
		t.Errorf("the stamp: %q", got)
	}
	for e, want := range map[string]string{"Stop": "bash /home/u/atlas-scripts/cost-snapshot/cost-snapshot.sh",
		"SessionStart": "bash /home/u/atlas-scripts/agent-context/session-context.sh codex"} {
		if got := commandsOf(t, hooks, e); !slices.Equal(got, []string{want}) {
			t.Errorf("the machine's %s: %q", e, got)
		}
	}
	for _, place := range []string{"pre_tool_use:0:0", "user_prompt_submit:0:0"} {
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
	g := newRig(t, homeFile(".codex/hooks.json", atlasCodex))
	home := filepath.Join(g.home, ".codex")
	config := strings.ReplaceAll(atlasConfig, codexPath, codexHooksFile(home))
	g.write(codexConfigFile(home), config, 0o600)
	step := g.step("codex")
	if step == nil {
		t.Fatal("no step for the hooks of codex")
	}
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	hooks := g.read(codexHooksFile(home))
	if got := commandsOf(t, hooks, "PreToolUse"); len(got) != 1 || !strings.Contains(got[0], g.clone+"/agent/ask-hook.py") {
		t.Errorf("the relay: %q", got)
	}
	lines := g.lines()
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "codex "+home+" hooks=") || !strings.Contains(last, " config=") {
		t.Errorf("the manifest holds %q", last)
	}
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
