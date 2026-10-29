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
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"aacpanel/internal/codex"
	"aacpanel/internal/contours"
	"aacpanel/internal/hook"
	"aacpanel/internal/mcp"
	"aacpanel/internal/toolset"
)

// ---- the hooks of codex ----

// codex runs hooks of the person's own from hooks.json in its home, in the
// shape of claude's settings: events, groups with their matchers, entries.
// The kit puts there the hooks codex runs as well — the question relay,
// the stamp before a prompt and the Stop reminders — and the file
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
				command := w.command(h)
				if hook.HeldEvent(h.Hook) != "" {
					command = hook.CodexHomeEnv + "=" + shellQuote(filepath.Dir(path)) + " " + command
				}
				changed = ensureHook(o, h, command) || changed
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
	parts, s, ok := dottedKey(s)
	if !ok {
		return nil, false
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

// arrayHeader tells whether a header is one of an array of tables.
func arrayHeader(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "[[") }

// dottedKey reads the dotted key at the start of s, its parts bare or quoted,
// and gives what follows it.
func dottedKey(s string) (parts []string, rest string, ok bool) {
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
				return nil, "", false
			}
			v, err := strconv.Unquote(s[:end+1])
			if err != nil {
				return nil, "", false
			}
			part, s = v, s[end+1:]
		case strings.HasPrefix(s, "'"):
			end := strings.IndexByte(s[1:], '\'')
			if end < 0 {
				return nil, "", false
			}
			part, s = s[1:end+1], s[end+2:]
		default:
			end := strings.IndexFunc(s, func(r rune) bool { return !bareKey(r) })
			if end <= 0 {
				return nil, "", false
			}
			part, s = s[:end], s[end:]
		}
		parts = append(parts, part)
		s = strings.TrimLeft(s, " \t")
		if strings.HasPrefix(s, ".") {
			s = s[1:]
			continue
		}
		return parts, s, true
	}
}

func bareKey(r rune) bool {
	return r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

// stateKey is the key of the hooks' state a header names, if it names one.
func stateKey(line string) (string, bool) {
	parts, ok := tableKey(line)
	if !ok || len(parts) != 3 || parts[0] != "hooks" || parts[1] != "state" || arrayHeader(line) {
		return "", false
	}
	return parts[2], true
}

// table is where the state table of key stands: its header and the line
// after its last.
func (d *tomlDoc) table(key string) (start, end int, ok bool) {
	return d.tableOf([]string{"hooks", "state", key})
}

// tableOf is where the table of the dotted key stands: its header and the
// line after its last. An array of tables is no table of a key.
func (d *tomlDoc) tableOf(key []string) (start, end int, ok bool) {
	for i, l := range d.lines {
		if parts, header := tableKey(l); header && !arrayHeader(l) && slices.Equal(parts, key) {
			return i, d.next(i), true
		}
	}
	return 0, 0, false
}

// next is the line of the first header after line i, or the end.
func (d *tomlDoc) next(i int) int {
	for j := i + 1; j < len(d.lines); j++ {
		if _, header := tableKey(d.lines[j]); header {
			return j
		}
	}
	return len(d.lines)
}

// bodyEnd is the line after the last line of keys of the table at start, end
// the line after the table: the blank lines and comments after it stand with
// what follows.
func (d *tomlDoc) bodyEnd(start, end int) int {
	last := start + 1
	for i := start + 1; i < end; i++ {
		if s := strings.TrimSpace(d.lines[i]); s != "" && !strings.HasPrefix(s, "#") {
			last = i + 1
		}
	}
	return last
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
	last := d.bodyEnd(start, end)
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

// ---- the panel's server in config.toml ----

// codex started by hand reads its MCP servers from config.toml of its home,
// [mcp_servers.<name>], and asks the person before every call of a tool of a
// server unless the tool's table, [mcp_servers.<name>.tools.<tool>], says
// approval_mode = "approve". The wiring puts the panel's server there with
// the command, the arguments and the state directory of claude's, and approves
// the tools a thread the panel starts calls without asking: a terminal of
// codex and a run of codex exec get what such a thread gets. A server of the
// name already there is the panel's: its command, arguments and environment
// are put in place where they stand, the rest of its table stays. The lines
// are edited one by one, as the trust of the hooks is; codex mcp add would
// print the whole file anew. A file that names the server in a form the
// wiring does not edit — an inline table, a dotted key from another table —
// is not touched.

// codexServerKey is the table of the panel's server.
var codexServerKey = []string{"mcp_servers", mcp.ServerName}

// codexServer is the panel's server as the config.toml of a codex home names
// it, and the tools a thread calls without asking, by their names on it.
type codexServer struct {
	Command string
	Args    []string
	Env     map[string]string
	Approve []string
}

// codexServerWant is the server of claude's accounts, with the tools a thread
// the panel starts is allowed.
func (in *Install) codexServerWant() codexServer {
	want := in.mcpWant()
	return codexServer{Command: want.Command, Args: want.Args, Env: want.Env, Approve: toolset.CodexAllowed()}
}

// WireCodexServer puts the panel's server and the approvals of its tools into
// a config.toml. A file that needs nothing comes back as it was, and changed
// says whether anything had to change.
func WireCodexServer(config []byte, want codexServer) ([]byte, bool, error) {
	doc := parseTOML(config)
	if err := doc.serverForm(want.Approve, true); err != nil {
		return nil, false, err
	}
	env := ""
	if len(want.Env) > 0 {
		env = tomlInline(want.Env)
	}
	changed := doc.ensureTable(codexServerKey)
	changed = doc.setKey(codexServerKey, "command", tomlString(want.Command)) || changed
	changed = doc.setKey(codexServerKey, "args", tomlArray(want.Args)) || changed
	changed = doc.setKey(codexServerKey, "env", env) || changed
	for _, tool := range want.Approve {
		table := slices.Concat(codexServerKey, []string{"tools", tool})
		changed = doc.ensureTable(table) || changed
		changed = doc.setKey(table, "approval_mode", tomlString("approve")) || changed
	}
	if !changed {
		return config, false, nil
	}
	return doc.bytes(), true, nil
}

// UnwireCodexServer takes the panel's server out of a config.toml, with every
// table under it, as claude mcp remove takes a server out of an account: a
// table of a tool left without its server would keep codex from reading the
// file at all.
func UnwireCodexServer(config []byte) ([]byte, bool, error) {
	doc := parseTOML(config)
	if err := doc.serverForm(nil, false); err != nil {
		return nil, false, err
	}
	changed := false
	for {
		at := slices.IndexFunc(doc.lines, func(l string) bool {
			parts, header := tableKey(l)
			return header && hasPrefix(parts, codexServerKey)
		})
		if at < 0 {
			break
		}
		doc.dropTable(at)
		changed = true
	}
	if !changed {
		return config, false, nil
	}
	return doc.bytes(), true, nil
}

// codexHasServer tells whether a config.toml holds a table of the panel's
// server.
func codexHasServer(config []byte) bool {
	_, _, ok := parseTOML(config).tableOf(codexServerKey)
	return ok
}

// serverForm finds a line that names the panel's server in a form the wiring
// does not edit, and says what it is. Outside the server's own tables that is
// any line that writes into it, mcp_servers whole as an inline table among
// them, and an array of tables of its name. Inside, wiring edits a key of its
// own in the server's table and approval_mode in the table of a tool it
// approves, each a value on one line; the same tool as an inline table or a
// dotted key, the tools or the environment as tables of another form, and a
// value of those keys that goes on past its line are left to the person.
// Taking the server out takes its tables whole, whatever they hold.
func (d *tomlDoc) serverForm(approve []string, wiring bool) error {
	var table []string
	refuse := func(l string) error {
		return fmt.Errorf("config.toml names the server %s in a line the installer does not edit: %s", mcp.ServerName, strings.TrimSpace(l))
	}
	for _, l := range d.lines {
		if parts, header := tableKey(l); header {
			table = parts
			if arrayHeader(l) && (hasPrefix(parts, codexServerKey) || hasPrefix(codexServerKey, parts)) {
				return refuse(l)
			}
			if wiring && slices.Equal(parts, slices.Concat(codexServerKey, []string{"env"})) {
				return refuse(l)
			}
			continue
		}
		key, value, ok := keyLine(l)
		if !ok {
			continue
		}
		full := slices.Concat(table, key)
		under := hasPrefix(table, codexServerKey)
		_, oneLine := tomlLineValue(value)
		switch {
		case !under && (hasPrefix(full, codexServerKey) || hasPrefix(codexServerKey, full)):
			return refuse(l)
		case !under || !wiring:
		case slices.Equal(table, codexServerKey):
			if len(key) > 1 && key[0] != "tools" || key[0] == "tools" && (len(key) < 2 || slices.Contains(approve, key[1])) ||
				slices.Contains([]string{"command", "args", "env"}, key[0]) && len(key) == 1 && !oneLine {
				return refuse(l)
			}
		case len(table) == 3 && table[2] == "tools":
			if slices.Contains(approve, key[0]) {
				return refuse(l)
			}
		case len(table) == 4 && table[2] == "tools" && slices.Contains(approve, table[3]):
			if len(key) > 1 || key[0] == "approval_mode" && !oneLine {
				return refuse(l)
			}
		}
	}
	return nil
}

// hasPrefix tells whether the dotted key begins with prefix.
func hasPrefix(key, prefix []string) bool {
	return len(key) >= len(prefix) && slices.Equal(key[:len(prefix)], prefix)
}

// keyLine reads a line of a key and its value: the dotted key, and the value
// as written.
func keyLine(line string) (key []string, value string, ok bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "[") {
		return nil, "", false
	}
	key, rest, ok := dottedKey(s)
	if !ok || !strings.HasPrefix(rest, "=") {
		return nil, "", false
	}
	return key, strings.TrimSpace(rest[1:]), true
}

// ensureTable puts an empty table of key after the last table of the panel's
// server, or at the end of the file, when there is none of the key. It says
// whether the file changed.
func (d *tomlDoc) ensureTable(key []string) bool {
	if _, _, ok := d.tableOf(key); ok {
		return false
	}
	at := len(d.lines)
	for i, l := range d.lines {
		if parts, header := tableKey(l); header && !arrayHeader(l) && hasPrefix(parts, codexServerKey) {
			at = d.bodyEnd(i, d.next(i))
		}
	}
	block := []string{"[" + tomlKey(key) + "]"}
	if at > 0 && strings.TrimSpace(d.lines[at-1]) != "" {
		block = append([]string{""}, block...)
	}
	if at < len(d.lines) && strings.TrimSpace(d.lines[at]) != "" {
		block = append(block, "")
	}
	d.lines = slices.Insert(d.lines, at, block...)
	return true
}

// setKey gives the key of a table that is there the value, written as TOML,
// in the line of the key or after the last key of the table; none takes the
// line out. A value that reads the same as the one written stays as it is
// written. It says whether the file changed.
func (d *tomlDoc) setKey(table []string, key, value string) bool {
	start, end, _ := d.tableOf(table)
	for i := start + 1; i < end; i++ {
		k, v, ok := keyLine(d.lines[i])
		if !ok || !slices.Equal(k, []string{key}) {
			continue
		}
		switch {
		case value == "":
			d.lines = slices.Delete(d.lines, i, i+1)
		case sameValue(v, value):
			return false
		default:
			d.lines[i] = key + " = " + value
		}
		return true
	}
	if value == "" {
		return false
	}
	d.lines = slices.Insert(d.lines, d.bodyEnd(start, end), key+" = "+value)
	return true
}

// dropTable takes out the table whose header is at start, to its last line of
// keys, and keeps one blank line where its own and the one before it meet.
func (d *tomlDoc) dropTable(start int) {
	d.lines = slices.Delete(d.lines, start, d.bodyEnd(start, d.next(start)))
	blankBefore := start == 0 || strings.TrimSpace(d.lines[start-1]) == ""
	switch {
	case start < len(d.lines) && blankBefore && strings.TrimSpace(d.lines[start]) == "":
		d.lines = slices.Delete(d.lines, start, start+1)
	case start == len(d.lines) && start > 0 && blankBefore:
		d.lines = d.lines[:start-1]
	}
}

// sameValue tells whether two values as written read the same.
func sameValue(a, b string) bool {
	va, ok := tomlLineValue(a)
	if !ok {
		return false
	}
	vb, ok := tomlLineValue(b)
	return ok && reflect.DeepEqual(va, vb)
}

// tomlLineValue reads the value of a line of a key, with nothing after it but
// a comment.
func tomlLineValue(s string) (any, bool) {
	v, rest, ok := tomlValue(s)
	rest = strings.TrimSpace(rest)
	return v, ok && (rest == "" || strings.HasPrefix(rest, "#"))
}

// tomlValue reads the value at the start of s as far as the server of the
// panel needs it — a string, basic or literal, an array and an inline table
// of values — and gives what follows it. Any other value is kept as it is
// written; a value that goes on past its line is no value here.
func tomlValue(s string) (v any, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t")
	switch {
	case strings.HasPrefix(s, `"""`), strings.HasPrefix(s, "'''"):
		return nil, "", false
	case strings.HasPrefix(s, `"`), strings.HasPrefix(s, "'"):
		// A string reads as a key of one part does.
		parts, rest, ok := dottedKey(s)
		if !ok || len(parts) != 1 {
			return nil, "", false
		}
		return parts[0], rest, true
	case strings.HasPrefix(s, "["):
		out := []any{}
		s = strings.TrimLeft(s[1:], " \t")
		for !strings.HasPrefix(s, "]") {
			v, rest, ok := tomlValue(s)
			if !ok {
				return nil, "", false
			}
			out = append(out, v)
			s = strings.TrimLeft(rest, " \t")
			if strings.HasPrefix(s, ",") {
				s = strings.TrimLeft(s[1:], " \t")
			} else if !strings.HasPrefix(s, "]") {
				return nil, "", false
			}
		}
		return out, s[1:], true
	case strings.HasPrefix(s, "{"):
		out := map[string]any{}
		s = strings.TrimLeft(s[1:], " \t")
		for !strings.HasPrefix(s, "}") {
			key, rest, ok := dottedKey(s)
			if !ok || len(key) != 1 || !strings.HasPrefix(rest, "=") {
				return nil, "", false
			}
			v, rest, ok := tomlValue(rest[1:])
			if !ok {
				return nil, "", false
			}
			out[key[0]] = v
			s = strings.TrimLeft(rest, " \t")
			if strings.HasPrefix(s, ",") {
				s = strings.TrimLeft(s[1:], " \t")
			} else if !strings.HasPrefix(s, "}") {
				return nil, "", false
			}
		}
		return out, s[1:], true
	}
	end := strings.IndexAny(s, ",]} \t#")
	if end < 0 {
		end = len(s)
	}
	if end == 0 {
		return nil, "", false
	}
	return s[:end], s[end:], true
}

// tomlArray writes strings as an array of TOML, on one line.
func tomlArray(vs []string) string {
	quoted := make([]string, len(vs))
	for i, v := range vs {
		quoted[i] = tomlString(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// tomlInline writes strings by their keys as an inline table, keys in order.
func tomlInline(m map[string]string) string {
	var pairs []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		pairs = append(pairs, tomlKey([]string{k})+" = "+tomlString(m[k]))
	}
	return "{ " + strings.Join(pairs, ", ") + " }"
}

// tomlKey writes a dotted key, a part bare where it can be.
func tomlKey(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p
		if p == "" || strings.IndexFunc(p, func(r rune) bool { return !bareKey(r) }) >= 0 {
			out[i] = tomlString(p)
		}
	}
	return strings.Join(out, ".")
}

// ---- the step ----

// codexHomes are the homes of codex the install wires: those the answers
// chose that are there. A home that is not there has no codex to run the
// hooks yet.
func (in *Install) codexHomes() []string {
	return in.S.codexHomes()
}

func (s *Survey) codexHomes() []string {
	return s.codexHomesThere(s.codexHomesChosen())
}

// codexHomesNamed are the homes host.env lists, or ~/.codex, codex's own,
// where it lists none: the panel's processes read the key the same way.
func (s *Survey) codexHomesNamed() []string {
	list := filepath.Join(s.Facts.Account.Home, ".codex")
	if v, ok := s.before(HostEnvFile, contours.CodexHomesEnv); ok && strings.TrimSpace(v) != "" {
		list = v
	}
	return s.codexHomeList(strings.Split(list, ":"))
}

// codexHomesChosen are the homes the answer names, or before it is given the
// named ones. An answer of none leaves the key empty, which the panel reads
// as ~/.codex.
func (s *Survey) codexHomesChosen() []string {
	g, ok := s.Value("codexhomes")
	if !ok {
		return s.codexHomesNamed()
	}
	if out := s.codexHomeList(split(g.Value)); len(out) > 0 {
		return out
	}
	return s.codexHomeList([]string{filepath.Join(s.Facts.Account.Home, ".codex")})
}

func (s *Survey) codexHomeList(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if d = strings.TrimSpace(d); d == "" {
			continue
		}
		if d = filepath.Clean(Expand(d, s.Facts.Account.Home)); !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// codexHomesThere are the homes among dirs that are directories on the
// machine.
func (s *Survey) codexHomesThere(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if st, err := s.m.Stat(d); err == nil && st.Mode.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// codexMind are the homes the answers chose that codex has not made yet:
// nothing goes into them, and the person is told what makes them.
func (s *Survey) codexMind() []string {
	if _, ok := s.Value("codexhomes"); !ok {
		return nil
	}
	var out []string
	there := s.codexHomes()
	for _, d := range s.codexHomesChosen() {
		if slices.Contains(there, d) {
			continue
		}
		start := "CODEX_HOME=" + s.Facts.Short(d) + " codex"
		if d == filepath.Join(s.Facts.Account.Home, ".codex") {
			start = "codex"
		}
		out = append(out, "the codex home "+s.Facts.Short(d)+" is not there yet and gets no hooks or tools: start "+
			start+" once, then ./install.sh again puts them in")
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
	if err != nil || changed {
		return false
	}
	_, changed, err = WireCodexServer(config, in.codexServerWant())
	return err == nil && !changed
}

// CheckCodexHooks verifies the installed entries with the daemon's own
// discovery and trust calculation. The relay is compulsory; optional hooks
// are checked when installed. The same check serves the installer and
// codex-contract, so a changed trust formula
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
	if _, changed, err := WireCodexServer(config, in.codexServerWant()); err != nil || changed {
		return "", &Failed{Diagnosis: codexConfigFile(home) + " holds no " + mcp.ServerName + " server of this install, with the approvals of its tools",
			Fix: []string{"CODEX_HOME=" + in.short(home) + " codex mcp get " + mcp.ServerName + " shows what codex reads; ./install.sh wires the home again."}}
	}
	program := in.S.valueOr("codex", "")
	source, err := CheckCodexHooks(r.context(), program, home, hooks)
	if err != nil {
		return source, &Failed{Diagnosis: err.Error(), Fix: []string{"Inspect /hooks in codex; make codex-contract checks the installed release."}}
	}
	return source, nil
}

func (in *Install) codexStep() *Step {
	return &Step{ID: "codex", Title: "Codex hooks and tools",
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
				r.Say(Pass, in.short(home)+" · "+source+": present, trusted and enabled; the panel's server in config.toml")
			}
			return nil
		},
		Undo: UndoKind,
	}
}

// wireCodex puts the kit into a codex home: the hooks with their trust, and
// the panel's server. The first change of each keeps both files aside, as the
// settings of claude are, and the server has a line of its own, as it has in
// claude's accounts: uninstall takes out the server it wrote, and leaves one
// it found in place as it was.
func (in *Install) wireCodex(r *Run, home string) error {
	hooks, config, err := in.codexRead(home)
	if err != nil {
		return err
	}
	path := codexHooksFile(home)
	hooksOut, configOut, hooksChanged, err := WireCodex(hooks, config, path, in.wiring())
	if err != nil {
		return &Failed{Diagnosis: "the hooks of codex in " + in.short(home) + " are left as they are: " + err.Error(),
			Fix: []string{"Fix the file, or trust the hooks by hand with /hooks in codex, and run ./install.sh again."}}
	}
	configOut, serverChanged, err := WireCodexServer(configOut, in.codexServerWant())
	if err != nil {
		return &Failed{Diagnosis: "the hooks and the server of codex in " + in.short(home) + " are left as they are: " + err.Error(),
			Fix: []string{"Write the server as the tables [mcp_servers." + mcp.ServerName + "] and [mcp_servers." + mcp.ServerName + ".tools.<tool>], or take it out, and run ./install.sh again."}}
	}
	if !hooksChanged && !serverChanged {
		r.Say(Pass, in.short(home)+" · in place already")
		return nil
	}
	files := []struct {
		name, path string
		was, out   []byte
	}{{"hooks", path, hooks, hooksOut}, {"config", codexConfigFile(home), config, configOut}}
	kept := map[string]string{}
	keep := func(name, path string, was []byte) (string, error) {
		if was == nil {
			return name + "=created", nil
		}
		if b, ok := kept[path]; ok {
			return name + "=" + b, nil
		}
		b, err := in.backup(path)
		if err != nil {
			return "", fmt.Errorf("%s was not kept aside before the change: %w", path, err)
		}
		kept[path] = b
		return name + "=" + b, nil
	}
	if hooksChanged && !r.Recorded(Codex, home) {
		var meta []string
		for _, f := range files {
			m, err := keep(f.name, f.path, f.was)
			if err != nil {
				return err
			}
			meta = append(meta, m)
		}
		if err := r.Record(Codex, home, strings.Join(meta, " ")); err != nil {
			return err
		}
	}
	if serverChanged && !r.Recorded(CodexMCP, home) {
		m, err := keep("config", files[1].path, config)
		if err != nil {
			return err
		}
		if err := r.Record(CodexMCP, home, m); err != nil {
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
	var said []string
	if hooksChanged {
		said = append(said, "the hooks codex runs, trusted in config.toml")
	}
	if serverChanged {
		said = append(said, fmt.Sprintf("the panel's server, %s approved", count(len(in.codexServerWant().Approve), "tool")))
	}
	r.Say(Pass, in.short(home)+" · "+strings.Join(said, "; "))
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

// undoCodexMCP takes the panel's server out of the config.toml of a codex
// home, with every table under it, whatever codex and the person wrote there
// since; a config.toml the installer made and that holds nothing else goes.
func undoCodexMCP(r *Run, e Entry) error {
	path := codexConfigFile(e.Target)
	config, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	out, changed, err := UnwireCodexServer(config)
	if err != nil {
		r.Say(Warn, path+": "+err.Error()+"; the panel's server stays in it")
		return nil
	}
	if !changed {
		return nil
	}
	if metaValue(e.Meta, "config") == "created" && strings.TrimSpace(string(out)) == "" {
		return removeIfThere(path)
	}
	return writeKeeping(path, out)
}

// undoCodex takes the panel's hooks out of a codex home and their trust
// out of its config.toml, whatever codex and the person wrote there since; a
// hooks.json or a config.toml the installer made and that holds nothing else
// goes.
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
	switch {
	case bytes.Equal(config, configOut):
	case metaValue(e.Meta, "config") == "created" && strings.TrimSpace(string(configOut)) == "":
		if err := removeIfThere(codexConfigFile(e.Target)); err != nil {
			return err
		}
	default:
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
