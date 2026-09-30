package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/contours"
)

// ---- claude itself, when the machine has none ----

// NativeInstaller is Anthropic's installer of claude. The script downloads
// the claude of this machine's platform, checks it against the checksum its
// release publishes and sets it up under the home directory — the launcher
// ~/.local/bin/claude, the versions in ~/.local/share/claude — as the user,
// never as root. The installer fetches the script itself; the script
// downloads claude with curl or wget, and on a machine with neither the root
// step installs curl first.
const NativeInstaller = "https://claude.ai/install.sh"

// nativeCommand is the native install as a person types it.
const nativeCommand = "curl -fsSL " + NativeInstaller + " | bash"

// nativeInstalls tells whether the run installs claude: the check found
// none, and the answer is to install it.
func (in *Install) nativeInstalls() bool {
	return in.f().Claude == "" && in.S.valueOr("pkg:claude", "") == yes
}

// claudeVersion is what claude answers to --version where the install
// expects it, and whether it answered.
func (in *Install) claudeVersion(r *Run) (string, bool) {
	out, err := r.Exec(Cmd{Argv: []string{in.claude(), "--version"}, Limit: 30 * time.Second})
	if err != nil {
		return "", false
	}
	if f := strings.Fields(out); len(f) > 0 {
		return f[0], true
	}
	return "", true
}

func (in *Install) claudeInstallStep() *Step {
	return &Step{ID: "claude-install", Title: "Install claude",
		Done: func(r *Run) (bool, error) {
			_, ok := in.claudeVersion(r)
			return ok, nil
		},
		Apply: func(r *Run) error {
			status, body, err := in.M.Fetch(NativeInstaller)
			switch {
			case err != nil:
				return &Failed{Diagnosis: NativeInstaller + " did not download: " + firstLine(err.Error()),
					Fix: []string{"Check the network or a proxy (HTTPS_PROXY) and run ./install.sh again."}}
			case status != 200 || !bytes.HasPrefix(body, []byte("#!")):
				return &Failed{Diagnosis: fmt.Sprintf("%s gave no installer script (status %d)", NativeInstaller, status),
					Fix: []string{"claude.ai may not serve this region: " + nativeCommand + " by hand says more."}}
			}
			// A program the installer puts on the machine is recorded as the
			// packages of apt are: uninstall names it and leaves it.
			if err := r.Once(Pkg, "claude", "by-installer native"); err != nil {
				return err
			}
			script := filepath.Join(in.f().InstallDir, "claude-install.sh")
			if err := writeFile(script, body, 0o600); err != nil {
				return err
			}
			defer os.Remove(script)
			if _, err := r.Exec(Cmd{Argv: []string{"bash", script}, Limit: 15 * time.Minute}); err != nil {
				return fail("Anthropic's native installer did not install claude", err,
					"The lines above are its own; "+nativeCommand+" runs it by hand, and ./install.sh goes on after it.")
			}
			return nil
		},
		Verify: func(r *Run) error {
			v, ok := in.claudeVersion(r)
			if !ok {
				return &Failed{Diagnosis: in.short(in.claude()) + " --version does not answer after the native installer",
					Fix: []string{"The native installer puts claude into ~/.local/bin; the answer to \"What starts claude?\" names " + in.short(in.claude()) + "."}}
			}
			r.Say(Pass, "claude "+v+" · "+in.short(in.claude()))
			return nil
		},
		Undo: UndoKind,
	}
}

// ---- S12: claude in every account ----

// accounts are the claude accounts the answers chose.
func (in *Install) accounts() []string { return split(in.S.valueOr("accounts", "")) }

// claude is what starts claude: the answer, a wrapper of the person's own
// included, since a wrapper is what knows the accounts.
func (in *Install) claude() string {
	if c := in.S.valueOr("claude", ""); c != "" {
		return c
	}
	return "claude"
}

func (in *Install) wiring() Wiring {
	return Wiring{Clone: in.clone(), Kit: split(in.S.valueOr("kit", "")), State: in.state()}
}

// mcpFile is where claude keeps the servers of an account: .claude.json in
// its directory, and next to the directory for ~/.claude, which claude
// reads without CLAUDE_CONFIG_DIR — naming ~/.claude there would make
// claude keep a second file inside it.
func (in *Install) mcpFile(dir string) string {
	if contours.IsDefaultConfig(dir, in.home()) {
		return filepath.Join(in.home(), ".claude.json")
	}
	return filepath.Join(dir, ".claude.json")
}

// accountEnv is the environment claude runs in for an account: its
// directory, or none at all for the account whose file lies beside it.
func accountEnv(dir, file string) (env, unset []string) {
	if file != filepath.Join(dir, ".claude.json") {
		return nil, []string{"CLAUDE_CONFIG_DIR"}
	}
	return []string{"CLAUDE_CONFIG_DIR=" + dir}, nil
}

// signInLine is the command that signs an account in by hand.
func (in *Install) signInLine(dir string) string {
	if in.mcpFile(dir) != filepath.Join(dir, ".claude.json") {
		return in.claude()
	}
	return "CLAUDE_CONFIG_DIR=" + in.short(dir) + " " + in.claude()
}

// mcpServer is the entry of a server as claude keeps it.
type mcpServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

func (a mcpServer) same(b mcpServer) bool {
	return a.Command == b.Command && slices.Equal(a.Args, b.Args) && maps.Equal(a.Env, b.Env)
}

// mcpWant is the panel's server: the executor in its MCP mode, told the
// state directory when it is not the default.
func (in *Install) mcpWant() mcpServer {
	env := map[string]string{}
	if in.state() != DefaultStateDir {
		env["AACP_STATE_DIR"] = in.state()
	}
	return mcpServer{Command: in.execPath(), Args: []string{"-mcp"}, Env: env}
}

// mcpHas is the panel's server as the account has it. The installer only
// reads the file: claude rewrites it all the time, so claude mcp writes it.
func (in *Install) mcpHas(dir string) (mcpServer, bool) {
	raw, err := in.M.ReadFile(in.mcpFile(dir))
	if err != nil {
		return mcpServer{}, false
	}
	var cfg struct {
		Servers map[string]mcpServer `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return mcpServer{}, false
	}
	s, ok := cfg.Servers["aacpanel"]
	return s, ok
}

func (in *Install) settingsPath(dir string) string { return filepath.Join(dir, "settings.json") }

// wired tells whether the settings of the account carry the kit as it is.
func (in *Install) wired(dir string) bool {
	raw, err := in.M.ReadFile(in.settingsPath(dir))
	if err != nil {
		return false
	}
	_, changed, err := Wire(raw, in.wiring())
	return err == nil && !changed
}

// toSignIn tells whether the person asked to sign in to the account now and
// it is not signed in yet.
func (in *Install) toSignIn(dir string) bool {
	signed, _ := in.S.SignedIn(dir)
	return !signed && in.S.valueOr("login:"+dir, "") == "now"
}

func (in *Install) claudeStep() *Step {
	return &Step{ID: "claude", Title: "Claude settings",
		Done: func(*Run) (bool, error) {
			for _, dir := range in.accounts() {
				want := in.mcpWant()
				have, ok := in.mcpHas(dir)
				if !in.wired(dir) || !ok || !have.same(want) || in.toSignIn(dir) {
					return false, nil
				}
			}
			return true, nil
		},
		Apply: func(r *Run) error {
			for _, dir := range in.accounts() {
				if err := in.wireSettings(r, dir); err != nil {
					return err
				}
				if err := in.wireMCP(r, dir); err != nil {
					return err
				}
				in.signIn(r, dir)
			}
			return nil
		},
		Verify: func(r *Run) error {
			for _, dir := range in.accounts() {
				path := in.settingsPath(dir)
				raw, err := in.M.ReadFile(path)
				if err != nil {
					return fail(path+" is not there after the wiring", err)
				}
				if n, err := wiredHooks(raw, "PreToolUse", "agent/ask-hook.py"); err != nil || n != 1 {
					return &Failed{Diagnosis: fmt.Sprintf("%s runs the question hook %d times after the wiring, not once", path, n)}
				}
				if !runs(statusLineOf(raw), statusScript) {
					return &Failed{Diagnosis: path + " has no " + statusScript + " in its status line after the wiring"}
				}
				if have, ok := in.mcpHas(dir); !ok || !have.same(in.mcpWant()) {
					return &Failed{Diagnosis: in.mcpFile(dir) + " holds no aacpanel server of this install after claude mcp add",
						Fix: []string{in.claude() + " --version, and " + in.signInLine(dir) + " mcp get aacpanel"}}
				}
			}
			// The status line runs on every key: a script that fails on
			// nothing fails in every session.
			script := filepath.Join(in.clone(), statusScript)
			if _, err := r.Exec(Cmd{Argv: []string{"bash", script}, Unset: []string{StatusNextEnv}, Limit: 30 * time.Second}); err != nil {
				return fail("the status line script fails on an empty payload: bash "+script+" </dev/null", err)
			}
			return nil
		},
		Undo: UndoKind,
	}
}

// wireSettings puts the kit into the settings of an account. A file that
// does not parse stops the run and is left as it is; the first change keeps
// the file aside, and every write names the sum of what it wrote, so the
// undo knows whether the file is still the installer's.
func (in *Install) wireSettings(r *Run, dir string) error {
	path := in.settingsPath(dir)
	raw, err := in.M.ReadFile(path)
	there := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, changed, err := Wire(raw, in.wiring())
	if err != nil {
		return &Failed{Diagnosis: (&BrokenSettings{Path: path, Err: err}).Error() + ": nothing was written to it",
			Fix: []string{"python3 -m json.tool " + path + " shows where; fix it and run ./install.sh again."}}
	}
	if !changed && there {
		r.Say(Pass, in.short(path)+" · in place already")
		return nil
	}
	if err := in.makeDirs(r, dir); err != nil {
		return err
	}
	orig, err := in.jsonOrig(r, path, there)
	if err != nil {
		return err
	}
	if err := r.Record(JSON, path, orig+" sha="+sha(out)); err != nil {
		return err
	}
	perm := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	if err := writeFile(path, out, perm); err != nil {
		return err
	}
	r.Say(Pass, in.short(path)+" · "+in.wiredSays())
	return nil
}

// jsonOrig is what the settings were before the installer first touched
// them: none, or the copy kept aside then.
func (in *Install) jsonOrig(r *Run, path string, there bool) (string, error) {
	if e, ok := r.First(JSON, path); ok {
		switch {
		case metaHas(e.Meta, "created"):
			return "created", nil
		case metaHas(e.Meta, Adopted):
			// Wired by hand before the installer: no copy of it as it was
			// before the panel exists, so the undo only takes the panel out.
			return Adopted, nil
		}
		return "orig=" + metaValue(e.Meta, "orig"), nil
	}
	if !there {
		return "created", nil
	}
	b, err := in.backup(path)
	if err != nil {
		return "", fmt.Errorf("%s was not kept aside before the change: %w", path, err)
	}
	return "orig=" + b, nil
}

// wiredSays is what the wiring put in, in a line.
func (in *Install) wiredSays() string {
	w := in.wiring()
	hooks := 0
	for _, h := range kitHooks {
		if w.has(h.Part) {
			hooks++
		}
	}
	rules := 0
	if w.has("tools") {
		rules += len(toolRules)
	}
	if w.has("restart") {
		rules++
	}
	return fmt.Sprintf("%s, the status line before yours, %s", count(hooks, "hook"), count(rules, "allow rule"))
}

// wireMCP gives the account the panel's server, through claude's own
// command. An entry of another path or another state directory is taken out
// and put in again: claude refuses a second one of a name.
func (in *Install) wireMCP(r *Run, dir string) error {
	want := in.mcpWant()
	have, there := in.mcpHas(dir)
	if there && have.same(want) {
		return nil
	}
	file := in.mcpFile(dir)
	if err := r.Once(MCP, dir, "claude="+in.claude()+" file="+file); err != nil {
		return err
	}
	env, unset := accountEnv(dir, file)
	if there {
		_, err := r.Exec(Cmd{Argv: []string{in.claude(), "mcp", "remove", "aacpanel", "-s", "user"}, Env: env, Unset: unset, Limit: time.Minute})
		if err != nil && !gone(err, "no mcp server named") {
			return fail("claude mcp remove failed for "+in.short(dir)+": "+in.claude()+" --version and the lines above say more", err)
		}
	}
	argv := []string{in.claude(), "mcp", "add", "--scope", "user", "aacpanel"}
	for _, k := range slices.Sorted(maps.Keys(want.Env)) {
		argv = append(argv, "-e", k+"="+want.Env[k])
	}
	argv = append(append(argv, "--", want.Command), want.Args...)
	if _, err := r.Exec(Cmd{Argv: argv, Env: env, Unset: unset, Limit: time.Minute}); err != nil {
		return fail("claude mcp add failed for "+in.short(dir)+": "+in.claude()+" --version and the lines above say more", err)
	}
	r.Say(Pass, in.short(file)+" · the panel's tools, by claude mcp add")
	return nil
}

// signIn hands the terminal to claude for an account the person chose to
// sign in to now, and looks again once claude is back. A sign-in that did
// not happen stops nothing: the account's sessions start once it does.
func (in *Install) signIn(r *Run, dir string) {
	if !in.toSignIn(dir) {
		return
	}
	later := "claude is not signed in to " + in.short(dir) + ": " + in.signInLine(dir) + " signs in, and its sessions start after that"
	if r.Hand == nil {
		r.Say(Warn, "no terminal to sign in at: "+in.signInLine(dir))
		r.Remind(later)
		return
	}
	env, unset := accountEnv(dir, in.mcpFile(dir))
	err := r.Hand(Handover{Title: "Sign in", Argv: []string{in.claude()}, Direct: true, Env: env, Unset: unset,
		Says: "claude opens here to sign in to " + in.short(dir) + " · /exit brings you back"})
	if signed, how := in.S.SignedIn(dir); signed {
		r.Say(Pass, in.short(dir)+" · "+how)
		return
	}
	why := "claude came back and " + in.short(dir) + " is not signed in"
	if err != nil {
		why = "claude ended: " + firstLine(err.Error())
	}
	r.Say(Warn, why+"; "+in.signInLine(dir)+" signs in later")
	r.Remind(later)
}

// undoJSON takes the panel out of claude's settings. A file as the
// installer last wrote it goes back to what it was, byte for byte: the copy
// kept aside, or no file. One changed since loses the panel's hooks, status
// line and rules by the tails of their paths and keeps the rest.
func undoJSON(r *Run, e Entry) error {
	raw, err := os.ReadFile(e.Target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	st, err := os.Stat(e.Target)
	if err != nil {
		return err
	}
	if sha(raw) == metaValue(e.Meta, "sha") {
		if orig := metaValue(e.Meta, "orig"); orig != "" {
			back, err := os.ReadFile(orig)
			if err != nil {
				return fmt.Errorf("the copy of %s kept aside is gone: %w", e.Target, err)
			}
			return writeFile(e.Target, back, st.Mode().Perm())
		}
		if metaHas(e.Meta, "created") {
			return removeIfThere(e.Target)
		}
	}
	out, changed, err := Unwire(raw)
	if err != nil {
		r.Say(Warn, (&BrokenSettings{Path: e.Target, Err: err}).Error()+": the panel's hooks stay in it until it parses")
		return nil
	}
	if !changed {
		return nil
	}
	if metaHas(e.Meta, "created") && emptySettings(out) {
		return removeIfThere(e.Target)
	}
	r.Say(Note, e.Target+": the panel is taken out, the rest stays as you left it, laid out as claude lays it out")
	return writeFile(e.Target, out, st.Mode().Perm())
}

// undoMCP takes the panel's server out of an account, through claude.
func undoMCP(r *Run, e Entry) error {
	claude := metaValue(e.Meta, "claude")
	if claude == "" {
		claude = "claude"
	}
	file := metaValue(e.Meta, "file")
	if file == "" {
		file = filepath.Join(e.Target, ".claude.json")
	}
	env, unset := accountEnv(e.Target, file)
	_, err := r.Exec(Cmd{Argv: []string{claude, "mcp", "remove", "aacpanel", "-s", "user"}, Env: env, Unset: unset, Limit: time.Minute})
	if err != nil && gone(err, "no mcp server named") {
		return nil
	}
	return err
}

// signInMind are the accounts the person left to sign in to later.
func (s *Survey) signInMind() []string {
	var out []string
	for _, dir := range split(s.valueOr("accounts", "")) {
		if s.valueOr("login:"+dir, "") != "later" {
			continue
		}
		if signed, _ := s.SignedIn(dir); signed {
			continue
		}
		out = append(out, "claude is not signed in to "+s.Facts.Short(dir)+": "+s.signInLine(dir)+" signs in, and its sessions start after that")
	}
	return out
}

// signInLine is the command that signs an account in by hand, as the
// answers so far name claude: ~/.claude without CLAUDE_CONFIG_DIR, which
// would make claude keep a second .claude.json inside it.
func (s *Survey) signInLine(dir string) string {
	line := s.valueOr("claude", "claude")
	if !contours.IsDefaultConfig(dir, s.Facts.Account.Home) {
		line = "CLAUDE_CONFIG_DIR=" + s.Facts.Short(dir) + " " + line
	}
	return line
}
