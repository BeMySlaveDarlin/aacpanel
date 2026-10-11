package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"aacpanel/internal/codex"
	"aacpanel/internal/contours"
)

// ---- the hooks of codex ----

// codex runs hooks of the person's own from hooks.json in its home, in the
// shape of claude's settings: events, groups with their matchers, entries.
// The kit puts there the hooks codex runs as well — the question relay and
// the stamp before a prompt — in the form it writes for claude, and the file
// is wired as claude's settings are: an entry of the panel's in a former form
// is rewritten where it stands, a retired one goes, the person's stay.
//
// codex runs a hook of the person's own only once it is trusted. config.toml
// of the home keeps the trust by the hook's place,
// [hooks.state."<hooks.json>:<event>:<group>:<entry>"], as trusted_hash: the
// hash of the hook as codex reads it. A hook new or changed since its trust is
// passed over without a word, so the wiring writes the trust of every entry
// it wrote. The trust goes with the place, not with the hook: an entry the
// wiring moves — a hook of the panel's before it in its event taken out —
// would lose its own, so the wiring carries the state of every other hook to
// where it stands after, and the state of a place the file no longer has
// goes. config.toml is codex's own file and the person's: the wiring edits
// the tables of these places line by line and leaves every other line as it
// is; a file that names them in a form other than those tables is not
// touched.

// codexHooksFile is the hooks of a codex home.
func codexHooksFile(home string) string { return filepath.Join(home, "hooks.json") }

// codexConfigFile is the configuration of a codex home.
func codexConfigFile(home string) string { return filepath.Join(home, "config.toml") }

// codexEvent is the name codex keys the state of an event's hook by: the
// event in snake case, PreToolUse as pre_tool_use.
func codexEvent(event string) string {
	var b strings.Builder
	for i, r := range event {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// codexUnmatched are the events codex reads no matcher of: a matcher there
// is no part of the hook.
var codexUnmatched = map[string]bool{"UserPromptSubmit": true, "Stop": true, "Interrupt": true}

// codexContextEvents are the events whose hooks codex keeps a limit of
// additional context for; the limit it applies by itself is no part of it.
var codexContextEvents = map[string]bool{"PreToolUse": true, "PostToolUse": true, "SessionStart": true,
	"UserPromptSubmit": true, "SubagentStart": true}

const codexContextLimit = 2500

// codexHash is the hash codex trusts a command hook by: SHA-256 of the JSON
// of the hook's identity — the event in snake case, the matcher where the
// event reads one, the entry as codex reads it — with every key sorted and no
// spaces, in hex after "sha256:". codex reads an entry with its timeout, 600
// when none is given and never under 1, async false unless it says so, the
// status message and the limit of additional context only when given; the
// Windows command is no part of it. (codex-rs/hooks/src/engine/discovery.rs,
// hook_hash, over codex-rs/config/src/fingerprint.rs, version_for_toml.) The
// timeouts codex clamps for SessionEnd and Interrupt are left out: the kit
// hooks neither.
func codexHash(event string, group, entry *object) string {
	h := map[string]any{"type": "command", "command": entry.str("command"), "async": false, "timeout": 600}
	if v, ok := entry.get("timeout"); ok {
		if n, err := strconv.ParseInt(fmt.Sprint(v), 10, 64); err == nil {
			h["timeout"] = max(n, 1)
		}
	}
	if v, ok := entry.get("async"); ok {
		if b, ok := v.(bool); ok {
			h["async"] = b
		}
	}
	if v, ok := entry.get("statusMessage"); ok {
		if s, ok := v.(string); ok {
			h["statusMessage"] = s
		}
	}
	if v, ok := entry.get("additionalContextLimit"); ok && codexContextEvents[event] {
		if n, err := strconv.Atoi(fmt.Sprint(v)); err == nil && n != codexContextLimit {
			h["additionalContextLimit"] = n
		}
	}
	id := map[string]any{"event_name": codexEvent(event), "hooks": []any{h}}
	if m, ok := group.get("matcher"); ok && !codexUnmatched[event] {
		if s, ok := m.(string); ok {
			id["matcher"] = s
		}
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(id)
	sum := sha256.Sum256([]byte(strings.TrimSuffix(b.String(), "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// codexPlace is an entry of a hooks.json at its place: the key codex keeps
// its state by, and whether the entry is the panel's.
type codexPlace struct {
	Key          string
	Event        string
	Group, Entry *object
	Ours         bool
}

// codexPlaces are the entries of a hooks.json in the order codex reads them.
func codexPlaces(o *object, path string) []codexPlace {
	v, _ := o.get("hooks")
	hs, _ := v.(*object)
	if hs == nil {
		return nil
	}
	var out []codexPlace
	for _, m := range hs.members {
		groups, _ := m.Val.([]any)
		for gi, g := range groups {
			group, ok := g.(*object)
			if !ok {
				continue
			}
			for ei, e := range group.list("hooks") {
				entry, ok := e.(*object)
				if !ok {
					continue
				}
				ours := slices.ContainsFunc(slices.Concat(kitHooks, retiredHooks), func(h kitHook) bool {
					return h.owns(entry.str("command"))
				})
				out = append(out, codexPlace{Key: fmt.Sprintf("%s:%s:%d:%d", path, codexEvent(m.Key), gi, ei),
					Event: m.Key, Group: group, Entry: entry, Ours: ours})
			}
		}
	}
	return out
}

// WireCodex puts the kit's hooks that codex runs into the hooks.json of a
// codex home, at path, and the trust of each into config.toml: the hooks of
// the checked parts in, those of the parts left unchecked and the retired
// ones out. Files that need nothing come back as they were, and changed says
// whether anything had to change. A hooks.json that does not parse, or a
// config.toml that names its hooks in a form the wiring does not edit, is
// an error and neither file is touched.
func WireCodex(hooks, config []byte, path string, w Wiring) (hooksOut, configOut []byte, changed bool, err error) {
	return rewireCodex(hooks, config, path, func(o *object) bool {
		changed := false
		for _, h := range kitHooks {
			switch {
			case !h.Codex:
			case w.has(h.Part):
				changed = ensureHook(o, h, w.command(h)) || changed
			default:
				changed = dropHook(o, h) || changed
			}
		}
		for _, h := range retiredHooks {
			changed = dropHook(o, h) || changed
		}
		return changed
	})
}

// UnwireCodex takes the panel's hooks out of the hooks.json of a codex home
// and their trust out of config.toml; the person's hooks keep theirs, where
// they stand after.
func UnwireCodex(hooks, config []byte, path string) (hooksOut, configOut []byte, changed bool, err error) {
	return rewireCodex(hooks, config, path, func(o *object) bool {
		changed := false
		for _, h := range slices.Concat(kitHooks, retiredHooks) {
			changed = dropHook(o, h) || changed
		}
		return changed
	})
}

func rewireCodex(hooks, config []byte, path string, edit func(*object) bool) ([]byte, []byte, bool, error) {
	o, err := parseSettings(hooks)
	if err != nil {
		return nil, nil, false, err
	}
	doc := parseTOML(config)
	if err := doc.editable(path); err != nil {
		return nil, nil, false, err
	}
	before := codexPlaces(o, path)
	hooksChanged := edit(o)
	after := codexPlaces(o, path)

	states := doc.states(path)
	was := map[*object]string{}
	oursAt := map[string]bool{}
	for _, p := range before {
		was[p.Entry] = p.Key
		oursAt[p.Key] = p.Ours
	}
	want := map[string][]string{}
	for _, p := range after {
		switch {
		case p.Ours:
			var body []string
			if oursAt[p.Key] {
				body = states[p.Key]
			}
			want[p.Key] = withTrust(body, codexHash(p.Event, p.Group, p.Entry))
		case was[p.Entry] != p.Key:
			want[p.Key] = states[was[p.Entry]]
		}
	}
	for _, p := range before {
		if _, kept := want[p.Key]; !kept && !slices.ContainsFunc(after, func(a codexPlace) bool { return a.Key == p.Key }) {
			want[p.Key] = nil
		}
	}
	configChanged := false
	for _, key := range slices.Sorted(maps.Keys(want)) {
		configChanged = doc.setTable(key, want[key]) || configChanged
	}
	hooksOut, configOut := hooks, config
	if hooksChanged {
		hooksOut = encodeSettings(o)
	}
	if configChanged {
		configOut = doc.bytes()
	}
	return hooksOut, configOut, hooksChanged || configChanged, nil
}

// withTrust is the body of a state table with the trust of hash: the line
// of trusted_hash put in place, the rest as it was.
func withTrust(body []string, hash string) []string {
	line := "trusted_hash = " + tomlString(hash)
	out := slices.Clone(body)
	for i, l := range out {
		if k, _, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == "trusted_hash" {
			out[i] = line
			return out
		}
	}
	return append(out, line)
}

// ---- config.toml, line by line ----

// tomlDoc is a config.toml as its lines. The wiring finds the tables of the
// hooks' state by their headers and changes only their lines.
type tomlDoc struct {
	lines []string
	// nl is whether the file ends in a line break.
	nl bool
}

func parseTOML(raw []byte) *tomlDoc {
	text := string(raw)
	d := &tomlDoc{nl: strings.HasSuffix(text, "\n")}
	text = strings.TrimSuffix(text, "\n")
	if text != "" {
		d.lines = strings.Split(text, "\n")
	}
	return d
}

func (d *tomlDoc) bytes() []byte {
	text := strings.Join(d.lines, "\n")
	if d.nl || len(d.lines) > 0 {
		text += "\n"
	}
	return []byte(text)
}

// tableKey is the dotted key of a table header, [a."b c".d], and whether the
// line is one; the header of an array of tables counts, as a line where a
// table ends.
func tableKey(line string) ([]string, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "[") {
		return nil, false
	}
	s = strings.TrimPrefix(s, "[")
	array := strings.HasPrefix(s, "[")
	s = strings.TrimPrefix(s, "[")
	var parts []string
	for {
		s = strings.TrimLeft(s, " \t")
		var part string
		switch {
		case strings.HasPrefix(s, `"`):
			end := 1
			for ; end < len(s) && s[end] != '"'; end++ {
				if s[end] == '\\' {
					end++
				}
			}
			if end >= len(s) {
				return nil, false
			}
			v, err := strconv.Unquote(s[:end+1])
			if err != nil {
				return nil, false
			}
			part, s = v, s[end+1:]
		case strings.HasPrefix(s, "'"):
			end := strings.IndexByte(s[1:], '\'')
			if end < 0 {
				return nil, false
			}
			part, s = s[1:end+1], s[end+2:]
		default:
			end := strings.IndexFunc(s, func(r rune) bool {
				return !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
			})
			if end <= 0 {
				return nil, false
			}
			part, s = s[:end], s[end:]
		}
		parts = append(parts, part)
		s = strings.TrimLeft(s, " \t")
		if strings.HasPrefix(s, ".") {
			s = s[1:]
			continue
		}
		break
	}
	close := "]"
	if array {
		close = "]]"
	}
	if !strings.HasPrefix(s, close) {
		return nil, false
	}
	rest := strings.TrimSpace(s[len(close):])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return nil, false
	}
	return parts, true
}

// stateKey is the key of the hooks' state a header names, if it names one.
func stateKey(line string) (string, bool) {
	parts, ok := tableKey(line)
	if !ok || len(parts) != 3 || parts[0] != "hooks" || parts[1] != "state" || strings.HasPrefix(strings.TrimSpace(line), "[[") {
		return "", false
	}
	return parts[2], true
}

// table is where the state table of key stands: its header and the line
// after its last.
func (d *tomlDoc) table(key string) (start, end int, ok bool) {
	start = -1
	for i, l := range d.lines {
		if _, header := tableKey(l); !header {
			continue
		}
		if start >= 0 {
			return start, i, true
		}
		if k, ok := stateKey(l); ok && k == key {
			start = i
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	return start, len(d.lines), true
}

// editable says whether the hooks of the file at path are named only by the
// tables of their state, the one form the wiring edits: a key of theirs
// written inside another table, or in a table of another shape, would be
// defined twice once the wiring adds its table.
func (d *tomlDoc) editable(path string) error {
	for _, l := range d.lines {
		if !strings.Contains(l, path+":") {
			continue
		}
		if k, ok := stateKey(l); ok && strings.HasPrefix(k, path+":") {
			continue
		}
		return fmt.Errorf("config.toml names the hooks of %s in a line the installer does not edit: %s", path, strings.TrimSpace(l))
	}
	return nil
}

// states are the bodies of the state tables of the hooks of the file at
// path: their lines of keys and values, by the key of the place.
func (d *tomlDoc) states(path string) map[string][]string {
	out := map[string][]string{}
	for _, l := range d.lines {
		k, ok := stateKey(l)
		if !ok || !strings.HasPrefix(k, path+":") {
			continue
		}
		start, end, _ := d.table(k)
		out[k] = values(d.lines[start+1 : end])
	}
	return out
}

// values are the lines of keys and values among lines: no blank ones, no
// comments.
func values(lines []string) []string {
	var out []string
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" && !strings.HasPrefix(s, "#") {
			out = append(out, l)
		}
	}
	return out
}

// setTable puts the body in the state table of key — a new table at the end
// of the file — or takes the table out for none. It says whether the file
// changed.
func (d *tomlDoc) setTable(key string, body []string) bool {
	start, end, there := d.table(key)
	if !there {
		if len(body) == 0 {
			return false
		}
		if n := len(d.lines); n > 0 && strings.TrimSpace(d.lines[n-1]) != "" {
			d.lines = append(d.lines, "")
		}
		d.lines = append(d.lines, "[hooks.state."+tomlString(key)+"]")
		d.lines = append(d.lines, body...)
		return true
	}
	if len(body) == 0 {
		d.lines = slices.Delete(d.lines, start, end)
		return true
	}
	// The body ends at its last line of keys: the blank lines and comments
	// after it stay where they are.
	last := start + 1
	for i := start + 1; i < end; i++ {
		if s := strings.TrimSpace(d.lines[i]); s != "" && !strings.HasPrefix(s, "#") {
			last = i + 1
		}
	}
	if slices.Equal(values(d.lines[start+1:last]), body) {
		return false
	}
	d.lines = slices.Concat(d.lines[:start+1], body, d.lines[last:])
	return true
}

// tomlString quotes a string as a basic string of TOML.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ---- the step ----

// codexHomes are the homes of codex on the machine: those host.env lists,
// or ~/.codex. A home that is not there has no codex to run the hooks.
func (in *Install) codexHomes() []string {
	home := in.home()
	list := filepath.Join(home, ".codex")
	if v, ok := in.S.before(HostEnvFile, contours.CodexHomesEnv); ok && strings.TrimSpace(v) != "" {
		list = v
	}
	var out []string
	for _, d := range strings.Split(list, ":") {
		if d = strings.TrimSpace(d); d == "" {
			continue
		}
		d = filepath.Clean(Expand(d, home))
		if st, err := in.M.Stat(d); err == nil && st.Mode.IsDir() && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// codexRead is what the two files of a home hold, none for a file that is
// not there.
func (in *Install) codexRead(home string) (hooks, config []byte, err error) {
	hooks, err = in.M.ReadFile(codexHooksFile(home))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	config, err = in.M.ReadFile(codexConfigFile(home))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	return hooks, config, nil
}

func (in *Install) codexWired(home string) bool {
	hooks, config, err := in.codexRead(home)
	if err != nil {
		return false
	}
	_, _, changed, err := WireCodex(hooks, config, codexHooksFile(home), in.wiring())
	return err == nil && !changed
}

// CheckCodexHooks verifies the installed entries with the daemon's own
// discovery and trust calculation. The relay is compulsory; the stamp is
// checked when installed, since its part of the kit is optional. The same
// check serves the installer and codex-contract, so a changed trust formula
// cannot pass by agreeing with another copy of the installer's calculation.
func CheckCodexHooks(ctx context.Context, program, home string, hooks []byte) (string, error) {
	o, err := parseSettings(hooks)
	if err != nil {
		return "", fmt.Errorf("%s: %w", codexHooksFile(home), err)
	}
	var want []codex.HookCheck
	relay := 0
	for _, p := range codexPlaces(o, codexHooksFile(home)) {
		for _, h := range kitHooks {
			if !h.Codex || !h.owns(p.Entry.str("command")) {
				continue
			}
			if p.Event != h.Event || p.Group.str("matcher") != h.Matcher {
				return "", fmt.Errorf("codex hook %s has the wrong event or matcher in %s", h.Hook, codexHooksFile(home))
			}
			if h.Part == "relay" {
				relay++
			}
			want = append(want, codex.HookCheck{Name: h.Hook, Key: p.Key, Command: p.Entry.str("command")})
		}
	}
	if relay != 1 {
		return "", fmt.Errorf("%s installs hook %s %d times, want once", codexHooksFile(home), askHook.Hook, relay)
	}
	return codex.CheckHooks(ctx, program, home, want)
}

func (in *Install) verifyCodex(r *Run, home string) (string, error) {
	hooks, config, err := in.codexRead(home)
	if err != nil {
		return "", err
	}
	if _, _, changed, err := WireCodex(hooks, config, codexHooksFile(home), in.wiring()); err != nil || changed {
		return "", &Failed{Diagnosis: codexConfigFile(home) + " does not trust the hooks of " + codexHooksFile(home) + " after the wiring",
			Fix: []string{"/hooks in codex shows what it trusts; ./install.sh wires the home again."}}
	}
	program := in.M.Env("AACP_CODEX")
	if program == "" {
		program = "codex"
	}
	source, err := CheckCodexHooks(r.context(), program, home, hooks)
	if err != nil {
		return source, &Failed{Diagnosis: err.Error(), Fix: []string{"Inspect /hooks in codex; make codex-contract checks the installed release."}}
	}
	return source, nil
}

func (in *Install) codexStep() *Step {
	return &Step{ID: "codex", Title: "Codex hooks",
		Done: func(*Run) (bool, error) {
			for _, home := range in.codexHomes() {
				if !in.codexWired(home) {
					return false, nil
				}
			}
			return true, nil
		},
		Apply: func(r *Run) error {
			for _, home := range in.codexHomes() {
				if err := in.wireCodex(r, home); err != nil {
					return err
				}
			}
			return nil
		},
		Verify: func(r *Run) error {
			for _, home := range in.codexHomes() {
				source, err := in.verifyCodex(r, home)
				if err != nil {
					return err
				}
				r.Say(Pass, in.short(home)+" · "+source+": present, trusted and enabled")
			}
			return nil
		},
		Undo: UndoKind,
	}
}

// wireCodex puts the kit into a codex home. The first change keeps both
// files aside, as the settings of claude are.
func (in *Install) wireCodex(r *Run, home string) error {
	hooks, config, err := in.codexRead(home)
	if err != nil {
		return err
	}
	path := codexHooksFile(home)
	hooksOut, configOut, changed, err := WireCodex(hooks, config, path, in.wiring())
	if err != nil {
		return &Failed{Diagnosis: "the hooks of codex in " + in.short(home) + " are left as they are: " + err.Error(),
			Fix: []string{"Fix the file, or trust the hooks by hand with /hooks in codex, and run ./install.sh again."}}
	}
	if !changed {
		r.Say(Pass, in.short(home)+" · in place already")
		return nil
	}
	files := []struct {
		name, path string
		was, out   []byte
	}{{"hooks", path, hooks, hooksOut}, {"config", codexConfigFile(home), config, configOut}}
	if !r.Recorded(Codex, home) {
		var meta []string
		for _, f := range files {
			if f.was == nil {
				meta = append(meta, f.name+"=created")
				continue
			}
			b, err := in.backup(f.path)
			if err != nil {
				return fmt.Errorf("%s was not kept aside before the change: %w", f.path, err)
			}
			meta = append(meta, f.name+"="+b)
		}
		if err := r.Record(Codex, home, strings.Join(meta, " ")); err != nil {
			return err
		}
	}
	for _, f := range files {
		if bytes.Equal(f.was, f.out) {
			continue
		}
		if err := writeKeeping(f.path, f.out); err != nil {
			return err
		}
	}
	r.Say(Pass, in.short(home)+" · the hooks codex runs, trusted in config.toml")
	return nil
}

// writeKeeping writes a file of the person's with the mode it has, 0600 for
// one that is not there yet.
func writeKeeping(path string, raw []byte) error {
	perm := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	return writeFile(path, raw, perm)
}

// undoCodex takes the panel's hooks out of a codex home and their trust
// out of its config.toml, whatever codex and the person wrote there since; a
// hooks.json the installer made and that holds nothing else goes.
func undoCodex(r *Run, e Entry) error {
	path := codexHooksFile(e.Target)
	hooks, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	config, err := os.ReadFile(codexConfigFile(e.Target))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	hooksOut, configOut, changed, err := UnwireCodex(hooks, config, path)
	if err != nil {
		r.Say(Warn, path+": "+err.Error()+"; the panel's hooks stay in it")
		return nil
	}
	if !changed {
		return nil
	}
	if !bytes.Equal(config, configOut) {
		if err := writeKeeping(codexConfigFile(e.Target), configOut); err != nil {
			return err
		}
	}
	if metaValue(e.Meta, "hooks") == "created" && emptySettings(hooksOut) {
		return removeIfThere(path)
	}
	if bytes.Equal(hooks, hooksOut) {
		return nil
	}
	return writeKeeping(path, hooksOut)
}
