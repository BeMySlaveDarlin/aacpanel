package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"aacpanel/internal/hook"
	"aacpanel/internal/toolset"
)

// claude's settings.json is the person's file: their own hooks, status line
// and rules live in it next to the panel's. The installer edits it as JSON,
// never as text, keeps the order of its keys — an edit that shuffled them
// would read as a rewrite of the whole file — and writes it the way claude
// writes it, two spaces a level. What is the panel's is told by what the
// command runs, not by the whole command: the executor's hook of the name
// from wherever the executor lies, or the script of the clone whose work the
// hook does, from any clone and in any of the forms an install wrote. A clone
// that moved, an install from another clone, or one that ran the script where
// the executor runs the hook left hooks that are the panel's all the same,
// and each is changed where it stands into the form of this install.

// member is a key of a JSON object with its value, in the order of the file.
type member struct {
	Key string
	Val any
}

// object is a JSON object that keeps the order of its keys. The values are
// *object, []any, string, json.Number, bool and nil.
type object struct{ members []member }

func (o *object) get(key string) (any, bool) {
	for _, m := range o.members {
		if m.Key == key {
			return m.Val, true
		}
	}
	return nil, false
}

// set changes the value of key where it stands, or puts key at the end.
func (o *object) set(key string, v any) {
	for i := range o.members {
		if o.members[i].Key == key {
			o.members[i].Val = v
			return
		}
	}
	o.members = append(o.members, member{key, v})
}

func (o *object) del(key string) {
	o.members = slices.DeleteFunc(o.members, func(m member) bool { return m.Key == key })
}

// child is the object under key, made at the end when there is none; a key
// that holds something else is not the settings claude reads, and nil says so.
func (o *object) child(key string) *object {
	v, ok := o.get(key)
	if !ok {
		c := &object{}
		o.set(key, c)
		return c
	}
	c, _ := v.(*object)
	return c
}

func (o *object) str(key string) string {
	v, _ := o.get(key)
	s, _ := v.(string)
	return s
}

func (o *object) list(key string) []any {
	v, _ := o.get(key)
	l, _ := v.([]any)
	return l
}

// parseSettings reads settings.json. A file of nothing but blanks is an
// empty object; anything that is not a JSON object is refused.
func parseSettings(raw []byte) (*object, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return &object{}, nil
	}
	if !json.Valid(raw) {
		var v any
		err := json.Unmarshal(raw, &v)
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := readValue(dec)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*object)
	if !ok {
		return nil, errors.New("the file is JSON but not an object")
	}
	return o, nil
}

func readValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		o := &object{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, _ := k.(string)
			v, err := readValue(dec)
			if err != nil {
				return nil, err
			}
			o.set(key, v)
		}
		_, err := dec.Token()
		return o, err
	case json.Delim('['):
		l := []any{}
		for dec.More() {
			v, err := readValue(dec)
			if err != nil {
				return nil, err
			}
			l = append(l, v)
		}
		_, err := dec.Token()
		return l, err
	}
	return tok, nil
}

// encodeSettings writes settings the way claude does: two spaces a level,
// an empty object or list on one line, a line break at the end.
func encodeSettings(o *object) []byte {
	var b bytes.Buffer
	writeValue(&b, o, 0)
	b.WriteByte('\n')
	return b.Bytes()
}

func writeValue(b *bytes.Buffer, v any, depth int) {
	pad := func(d int) string { return "\n" + strings.Repeat("  ", d) }
	switch v := v.(type) {
	case *object:
		if len(v.members) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, m := range v.members {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(pad(depth + 1))
			writeString(b, m.Key)
			b.WriteString(": ")
			writeValue(b, m.Val, depth+1)
		}
		b.WriteString(pad(depth) + "}")
	case []any:
		if len(v) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(pad(depth + 1))
			writeValue(b, e, depth+1)
		}
		b.WriteString(pad(depth) + "]")
	case string:
		writeString(b, v)
	case json.Number:
		b.WriteString(v.String())
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case nil:
		b.WriteString("null")
	}
}

// writeString quotes a string as JSON without escaping & < >: a path or a
// command carries them as they are.
func writeString(b *bytes.Buffer, s string) {
	var q bytes.Buffer
	enc := json.NewEncoder(&q)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Write(bytes.TrimSuffix(q.Bytes(), []byte("\n")))
}

// ---- what the kit puts into the settings ----

// kitHook is a hook of the kit: the event and the matcher it stands under,
// and the executor's hook it runs, with the script forms an update replaces.
type kitHook struct {
	Part    string
	Event   string
	Matcher string
	// Hook is the executor's hook the entry runs, aacpanel-exec hook <Hook>;
	// empty only for a retired hook, whose entry is removed.
	Hook string
	// Script is only a migration recognizer. Its path within the clone
	// tells the python forms of the entry — test -f <clone>/<script> &&
	// python3 <clone>/<script>,
	// with the state directory before python3 or without, and the bare
	// python3 <clone>/<script> of a hand — from any clone.
	Script  string
	Args    string // after the script or the hook's name
	Timeout int    // zero writes none: claude's own applies
	// State is a hook that reads the collector's snapshot or keeps files of
	// its own in the state directory: a state directory other than the
	// default is named in its command.
	State bool
	// Codex is a hook codex runs as well, from the hooks.json of its home.
	Codex bool
}

var kitHooks = []kitHook{
	{Part: "relay", Event: "PreToolUse", Matcher: "AskUserQuestion", Hook: "ask-hook", Script: "agent/ask-hook.py", Timeout: 5, Codex: true},
	{Part: "copies", Event: "PostToolUse", Matcher: "Artifact", Hook: "artifact-copy", Script: "deploy/claude/artifact-copy.py", Timeout: 10},
	{Part: "nudge", Event: "Stop", Hook: "checklist-reminder", Script: "deploy/claude/checklist-reminder.py", Timeout: 5},
	{Part: "nudge", Event: "Stop", Hook: "turn-reminder", Script: "deploy/claude/turn-reminder.py", Timeout: 5, State: true},
	{Part: "stamp", Event: "UserPromptSubmit", Hook: "prompt-stamp", Script: "deploy/claude/prompt-stamp.py", Timeout: 5, State: true, Codex: true},
	{Part: "background", Event: "Stop", Hook: "background-reminder", Script: "deploy/claude/background-reminder.py", Timeout: 5, State: true},
}

// retiredHooks are the hooks the kit must not wire. No part of the kit puts
// them in, and the wiring takes their entries out of every file it wires,
// whatever the kit. Each is removed only from its event: the same hook can
// still serve another event.
var retiredHooks = []kitHook{
	{Event: "PostToolBatch", Hook: "prompt-stamp", Script: "deploy/claude/prompt-stamp.py"},
	{Event: "Stop", Hook: "context-guard", Script: "deploy/claude/context-guard.py"},
	{Event: "SessionStart", Script: "deploy/claude/brief-waiting.py"},
	{Event: "Stop", Script: "deploy/claude/cost-snapshot.py"},
	{Event: "SubagentStop", Script: "deploy/claude/cost-snapshot.py"},
}

// owns tells whether the command of an entry is the hook's: it runs the
// executor's hook of the name, or the script, in any of their forms.
func (h kitHook) owns(command string) bool {
	return runs(command, h.Script) || (h.Hook != "" && runsHook(command, h.Hook))
}

// askHook is the hook of the question relay: the one hook every account
// carries, which the check after the wiring counts.
var askHook = kitHooks[0]

// statusScript is the script of the status line: the limits snapshot, first
// in the chain, which passes the payload on to the person's own line.
const statusScript = "agent/rate-snapshot.sh"

// StatusNextEnv is where the snapshot finds the person's status line: in
// the command itself, set by the shell claude runs it with.
const StatusNextEnv = "AACP_STATUSLINE_NEXT"

// The allow rules of the panel's tools — the close of a session by itself
// among them, which closes only the session calling it and waits out its work,
// and the list, the log and the stop of its observers, which touch only what it
// started, and its alarms, which run nothing — and the one of the restart,
// which lets a session restart itself with nobody at the screen.
var (
	toolRules = []string{"mcp__aacpanel__checklist", "mcp__aacpanel__turn_end", "mcp__aacpanel__role", "mcp__aacpanel__brief_publish", "mcp__aacpanel__brief_delete",
		"mcp__aacpanel__notify", "mcp__aacpanel__secret_ask", "mcp__aacpanel__request", "mcp__aacpanel__session_close", "mcp__aacpanel__session_ready",
		"mcp__aacpanel__observer_list", "mcp__aacpanel__observer_stop", "mcp__aacpanel__observer_tail",
		"mcp__aacpanel__alarm_set", "mcp__aacpanel__alarm_list", "mcp__aacpanel__alarm_cancel"}
	restartRule = "mcp__aacpanel__session_restart"
)

// AllowRules is how many allow rules the kit puts into the permissions of an
// account with the parts has says are checked: the panel's tools with Panel
// tools, the restart with Self-restart. The plan, the line of a wired account
// and the demo count by it, so a tool added to the list is counted on every
// screen that names the number.
func AllowRules(has func(part string) bool) int {
	n := 0
	if has("tools") {
		n += len(toolRules)
	}
	if has("restart") {
		n++
	}
	return n
}

// Wiring is what the settings of an account are to carry: the parts of the
// kit, the clone their scripts run from, the executor their hooks run, the
// state directory.
type Wiring struct {
	Clone string
	Exec  string
	Kit   []string
	State string
}

func (w Wiring) has(part string) bool { return slices.Contains(w.Kit, part) }

// command is the command of a hook in the settings. An executor that is not there
// makes the shell exit 127, an error claude passes over, and the executor
// itself exits 0 on anything it meets.
func (w Wiring) command(h kitHook) string {
	state := ""
	if h.State && w.State != "" && w.State != DefaultStateDir {
		state = "AACP_STATE_DIR=" + shellQuote(w.State) + " "
	}
	args := ""
	if h.Args != "" {
		args = " " + h.Args
	}
	return state + w.Exec + " " + hook.Command + " " + h.Hook + args
}

// runs tells whether a command runs the script: a word of it is the
// script's path, whatever clone it lies in.
func runs(command, script string) bool {
	for _, f := range strings.Fields(command) {
		if f == script || strings.HasSuffix(f, "/"+script) {
			return true
		}
	}
	return false
}

// execName is the executor's binary, wherever an install put it.
const execName = "aacpanel-exec"

// runsHook tells whether a command runs the executor's hook of the name: a
// word of it is the executor, from any directory, and the two after it are
// the hook command and the name.
func runsHook(command, name string) bool {
	f := strings.Fields(command)
	for i := 0; i+2 < len(f); i++ {
		if filepath.Base(f[i]) == execName && f[i+1] == hook.Command && f[i+2] == name {
			return true
		}
	}
	return false
}

// Wire puts the kit into the settings: the hooks of the checked parts — and
// out those of the parts left unchecked — the limits first in the status
// line's chain, and the allow rules of the panel's tools. A hook of the
// panel's already there is changed where it stands, in the group of its
// matcher; there is one of each and never a second. A file that needs
// nothing comes back as it was, byte for byte, and changed says whether
// anything had to change. A file that does not parse is an error and is
// not touched.
func Wire(raw []byte, w Wiring) ([]byte, bool, error) {
	o, err := parseSettings(raw)
	if err != nil {
		return nil, false, err
	}
	changed := false
	for _, h := range kitHooks {
		if w.has(h.Part) {
			changed = ensureHook(o, h, w.command(h)) || changed
		} else {
			changed = dropHook(o, h) || changed
		}
	}
	for _, h := range retiredHooks {
		changed = dropHook(o, h) || changed
	}
	if w.has("limits") {
		changed = chainStatus(o, w.Exec) || changed
	} else {
		changed = unchainStatus(o) || changed
	}
	var want, gone []string
	if w.has("tools") {
		want = append(want, toolRules...)
	} else {
		gone = append(gone, toolRules...)
	}
	if w.has("restart") {
		want = append(want, restartRule)
	} else {
		gone = append(gone, restartRule)
	}
	changed = allow(o, want) || changed
	changed = disallow(o, gone) || changed
	if w.has("tools") {
		changed = liftWord(o) || changed
	}
	if !changed {
		return raw, false, nil
	}
	return encodeSettings(o), true, nil
}

// Unwire takes the panel out of the settings: every hook of the executor's
// and every one whose script lies in a clone of the panel, retired ones
// among them, the limits out of the status line's chain — the person's line
// goes back where it was — and the allow rules of its tools. What only held
// the panel's goes with it. A file with none of it comes back as it was.
func Unwire(raw []byte) ([]byte, bool, error) {
	o, err := parseSettings(raw)
	if err != nil {
		return nil, false, err
	}
	changed := false
	for _, h := range slices.Concat(kitHooks, retiredHooks) {
		changed = dropHook(o, h) || changed
	}
	changed = unchainStatus(o) || changed
	changed = disallow(o, append(append([]string{}, toolRules...), restartRule)) || changed
	changed = dropWord(o) || changed
	if !changed {
		return raw, false, nil
	}
	return encodeSettings(o), true, nil
}

// emptySettings tells whether a file holds nothing: what is left of one the
// installer made, once the panel is out of it.
func emptySettings(raw []byte) bool {
	o, err := parseSettings(raw)
	return err == nil && len(o.members) == 0
}

// ensureHook keeps one entry of the hook, the first found under its matcher,
// with the command and the timeout of this install: an entry of a former
// form is rewritten where it stands. Any other entry of the hook on the event
// — a second one, one under another matcher — goes, and so does a group left
// empty by that.
func ensureHook(o *object, h kitHook, want string) bool {
	hs := o.child("hooks")
	if hs == nil {
		return false
	}
	groups := hs.list(h.Event)
	changed, found := false, false
	kept := make([]any, 0, len(groups))
	for _, g := range groups {
		group, ok := g.(*object)
		if !ok {
			kept = append(kept, g)
			continue
		}
		entries := group.list("hooks")
		keptEntries := make([]any, 0, len(entries))
		for _, e := range entries {
			entry, ok := e.(*object)
			if !ok || !h.owns(entry.str("command")) {
				keptEntries = append(keptEntries, e)
				continue
			}
			if !found && group.str("matcher") == h.Matcher {
				found = true
				changed = setEntry(entry, want, h.Timeout) || changed
				keptEntries = append(keptEntries, e)
				continue
			}
			changed = true
		}
		if len(keptEntries) == len(entries) {
			kept = append(kept, g)
			continue
		}
		if len(keptEntries) > 0 {
			group.set("hooks", keptEntries)
			kept = append(kept, g)
		}
	}
	if !found {
		kept = append(kept, newGroup(h, want))
		changed = true
	}
	if changed {
		hs.set(h.Event, kept)
	}
	return changed
}

func setEntry(e *object, want string, timeout int) bool {
	changed := false
	if e.str("type") != "command" {
		e.set("type", "command")
		changed = true
	}
	if e.str("command") != want {
		e.set("command", want)
		changed = true
	}
	if timeout > 0 {
		v, _ := e.get("timeout")
		if n, ok := v.(json.Number); !ok || n.String() != strconv.Itoa(timeout) {
			e.set("timeout", json.Number(strconv.Itoa(timeout)))
			changed = true
		}
	}
	return changed
}

func newGroup(h kitHook, command string) *object {
	g := &object{}
	if h.Matcher != "" {
		g.set("matcher", h.Matcher)
	}
	entry := &object{}
	entry.set("type", "command")
	entry.set("command", command)
	if h.Timeout > 0 {
		entry.set("timeout", json.Number(strconv.Itoa(h.Timeout)))
	}
	g.set("hooks", []any{entry})
	return g
}

// dropHook takes every entry of the hook off its event, in any of its forms
// and under any matcher. A group, an event and the hooks themselves go only
// when the drop emptied them: an empty one the person keeps is theirs.
func dropHook(o *object, h kitHook) bool {
	v, _ := o.get("hooks")
	hs, ok := v.(*object)
	if !ok {
		return false
	}
	groups := hs.list(h.Event)
	changed := false
	kept := make([]any, 0, len(groups))
	for _, g := range groups {
		group, ok := g.(*object)
		if !ok {
			kept = append(kept, g)
			continue
		}
		entries := group.list("hooks")
		keptEntries := slices.DeleteFunc(slices.Clone(entries), func(e any) bool {
			entry, ok := e.(*object)
			return ok && h.owns(entry.str("command"))
		})
		switch {
		case len(keptEntries) == len(entries):
			kept = append(kept, g)
		case len(keptEntries) > 0:
			group.set("hooks", keptEntries)
			kept = append(kept, g)
			changed = true
		default:
			changed = true
		}
	}
	if !changed {
		return false
	}
	if len(kept) > 0 {
		hs.set(h.Event, kept)
	} else {
		hs.del(h.Event)
	}
	if len(hs.members) == 0 {
		o.del("hooks")
	}
	return true
}

// ---- the status line ----

// statusCommand is the status line of the panel: the snapshot of the limits
// with the person's line after it, if there is one.
func statusCommand(executor, next string) string {
	base := executor + " statusline"
	if next == "" {
		return base
	}
	return StatusNextEnv + "=" + singleQuote(next) + " " + base
}

// singleQuote quotes any value for the shell: a quote inside closes the
// string, puts an escaped quote, and opens it again.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// statusNext reads the command of the panel's status line: the person's line
// it runs next, and whether the command has the form the panel writes — a
// command that runs the script some other way cannot be taken apart.
func statusNext(command string) (next string, ok bool) {
	next, _, ok = statusParts(command)
	return next, ok
}

// statusParts takes either form of the panel's status line apart: the
// person's line and the command that keeps the snapshot.
func statusParts(command string) (next, base string, ok bool) {
	command = strings.TrimSpace(command)
	rest, prefixed := strings.CutPrefix(command, StatusNextEnv+"=")
	if prefixed {
		next, rest, ok = shellWord(rest)
		if !ok {
			return "", "", false
		}
	}
	base = strings.TrimSpace(rest)
	f := strings.Fields(base)
	if len(f) != 2 || !(f[0] == "bash" && runs(f[1], statusScript) || filepath.Base(f[0]) == execName && f[1] == "statusline") {
		return "", "", false
	}
	return next, base, true
}

// ownsStatus recognises the snapshot in either form, wherever it is installed.
func ownsStatus(command string) bool {
	if runs(command, statusScript) {
		return true
	}
	f := strings.Fields(command)
	for i := 0; i+1 < len(f); i++ {
		if filepath.Base(f[i]) == execName && f[i+1] == "statusline" {
			return true
		}
	}
	return false
}

// shellWord reads the first word of s the way the shell would, for the
// quoting singleQuote and a hand of a person make: single quotes, double
// quotes, backslashes, bare letters. What double quotes would expand stays
// as it is written: the snapshot hands the line to sh, which expands it
// there, as it expands it when the line is the status line itself.
func shellWord(s string) (word, rest string, ok bool) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			return b.String(), s[i:], true
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return "", "", false
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 2
		case c == '"':
			j := i + 1
			for ; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' && j+1 < len(s) && strings.IndexByte("$`\"\\", s[j+1]) >= 0 {
					j++
				}
				b.WriteByte(s[j])
			}
			if j >= len(s) {
				return "", "", false
			}
			i = j + 1
		case c == '\\' && i+1 < len(s):
			b.WriteByte(s[i+1])
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), "", true
}

// peel is the person's line under any number of the panel's snapshots: a
// chain wrapped twice by an older hand calls the snapshot once from now on.
func peel(next string) string {
	for ownsStatus(next) {
		inner, ok := statusNext(next)
		if !ok {
			return next
		}
		next = inner
	}
	return next
}

// chainStatus puts the snapshot first, rewriting either form where it
// stands. The shell's quoting of the next command stays intact: double
// quotes can expand a variable before the next shell sees the command.
func chainStatus(o *object, executor string) bool {
	v, _ := o.get("statusLine")
	line, _ := v.(*object)
	if line == nil {
		line = &object{}
		line.set("type", "command")
		line.set("command", statusCommand(executor, ""))
		o.set("statusLine", line)
		return true
	}
	current := line.str("command")
	want := statusCommand(executor, current)
	if ownsStatus(current) {
		if next, base, ok := statusParts(current); ok {
			want = statusCommand(executor, peel(next))
			if peel(next) == next {
				at := strings.LastIndex(current, base)
				want = current[:at] + statusCommand(executor, "") + current[at+len(base):]
			}
		} else {
			want = repathStatus(current, executor)
		}
	} else if strings.TrimSpace(current) == "" {
		want = statusCommand(executor, "")
	}
	changed := false
	if line.str("type") != "command" {
		line.set("type", "command")
		changed = true
	}
	if current != want {
		line.set("command", want)
		changed = true
	}
	return changed
}

// repathStatus keeps a line whose shell syntax the installer cannot take
// apart, replacing only the snapshot command, including its interpreter.
func repathStatus(command, executor string) string {
	fields := strings.Fields(command)
	for i := len(fields) - 2; i >= 0; i-- {
		base := fields[i] + " " + fields[i+1]
		if fields[i] == "bash" && runs(fields[i+1], statusScript) || filepath.Base(fields[i]) == execName && fields[i+1] == "statusline" {
			at := strings.LastIndex(command, base)
			if at >= 0 {
				return command[:at] + statusCommand(executor, "") + command[at+len(base):]
			}
		}
	}
	return command
}

// unchainStatus gives the status line back to the person: their command
// where the panel's stood, or no status line when there was none. A line of
// the panel's it cannot take apart stays as it is.
func unchainStatus(o *object) bool {
	v, _ := o.get("statusLine")
	line, _ := v.(*object)
	if line == nil || !ownsStatus(line.str("command")) {
		return false
	}
	next, ok := statusNext(line.str("command"))
	if !ok {
		return false
	}
	if next = peel(next); next == "" {
		o.del("statusLine")
		return true
	}
	line.set("command", next)
	return true
}

// ---- the allow rules ----

// allow puts each rule into permissions.allow once: missing ones at the end,
// a second copy of one of them out.
func allow(o *object, rules []string) bool {
	if len(rules) == 0 {
		return false
	}
	perms := o.child("permissions")
	if perms == nil {
		return false
	}
	v, ok := perms.get("allow")
	list, isList := v.([]any)
	if ok && !isList {
		return false
	}
	changed := !ok
	seen := map[string]bool{}
	kept := make([]any, 0, len(list)+len(rules))
	for _, e := range list {
		s, isStr := e.(string)
		if isStr && slices.Contains(rules, s) {
			if seen[s] {
				changed = true
				continue
			}
			seen[s] = true
		}
		kept = append(kept, e)
	}
	for _, r := range rules {
		if !seen[r] {
			kept = append(kept, r)
			changed = true
		}
	}
	if changed {
		perms.set("allow", kept)
	}
	return changed
}

// disallow takes the rules out of permissions.allow; the list and the
// permissions go only when that emptied them.
func disallow(o *object, rules []string) bool {
	v, _ := o.get("permissions")
	perms, ok := v.(*object)
	if !ok {
		return false
	}
	list := perms.list("allow")
	kept := slices.DeleteFunc(slices.Clone(list), func(e any) bool {
		s, ok := e.(string)
		return ok && slices.Contains(rules, s)
	})
	if len(kept) == len(list) {
		return false
	}
	if len(kept) > 0 {
		perms.set("allow", kept)
		return true
	}
	perms.del("allow")
	if len(perms.members) == 0 {
		o.del("permissions")
	}
	return true
}

// ---- the length of the panel's word ----

// liftWord gives the env of the settings the length of a server's word the
// launcher gives the sessions it starts. claude puts the env of its settings
// into its own environment at its start and reads the length from there
// whenever it takes a tool in, so a session started by hand keeps the panel's
// word whole too. A value of the person's at least as large stays.
func liftWord(o *object) bool {
	env := o.child("env")
	if env == nil {
		return false
	}
	if n, ok := wordOf(env); ok && n >= toolset.WordMax {
		return false
	}
	env.set(toolset.WordEnv, strconv.Itoa(toolset.WordMax))
	return true
}

// wordOf is the length the env names, read as claude reads it: digits alone,
// a number or a string of them.
func wordOf(env *object) (int, bool) {
	v, ok := env.get(toolset.WordEnv)
	if !ok {
		return 0, false
	}
	switch v := v.(type) {
	case string:
		return wordNumber(v)
	case json.Number:
		return wordNumber(v.String())
	}
	return 0, false
}

func wordNumber(s string) (int, bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// wordAtLeast tells whether a length as written keeps the panel's word whole.
func wordAtLeast(s string) bool {
	n, ok := wordNumber(s)
	return ok && n >= toolset.WordMax
}

// wordIn is what the env of the settings holds for the length, as written,
// and whether it holds the key at all.
func wordIn(raw []byte) (string, bool) {
	o, err := parseSettings(raw)
	if err != nil {
		return "", false
	}
	v, _ := o.get("env")
	env, ok := v.(*object)
	if !ok {
		return "", false
	}
	w, ok := env.get(toolset.WordEnv)
	if !ok {
		return "", false
	}
	switch w := w.(type) {
	case string:
		return w, true
	case json.Number:
		return w.String(), true
	}
	return fmt.Sprint(w), true
}

// dropWord takes the length the wiring gives out of the env, and the env
// with it when that emptied it: the panel's value is the panel's, any other is
// the person's and stays.
func dropWord(o *object) bool {
	v, _ := o.get("env")
	env, ok := v.(*object)
	if !ok {
		return false
	}
	if n, ok := wordOf(env); !ok || n != toolset.WordMax {
		return false
	}
	env.del(toolset.WordEnv)
	if len(env.members) == 0 {
		o.del("env")
	}
	return true
}

// restoreWord puts back into the unwired settings out the length the env held
// before the installer raised it, was, when the file raw still holds the
// length the installer wrote: a value the person changed since stays as
// Unwire left it.
func restoreWord(raw, out []byte, was string) ([]byte, bool) {
	if was == "" {
		return out, false
	}
	if v, _ := wordIn(raw); v != strconv.Itoa(toolset.WordMax) {
		return out, false
	}
	o, err := parseSettings(out)
	if err != nil {
		return out, false
	}
	env := o.child("env")
	if env == nil {
		return out, false
	}
	env.set(toolset.WordEnv, was)
	return encodeSettings(o), true
}

// ---- what the check after the wiring reads ----

// wiredHooks counts the entries of the hook in the settings, on its event and
// in any of its forms: one, when the wiring holds.
func wiredHooks(raw []byte, h kitHook) (int, error) {
	o, err := parseSettings(raw)
	if err != nil {
		return 0, err
	}
	v, _ := o.get("hooks")
	hs, ok := v.(*object)
	if !ok {
		return 0, nil
	}
	n := 0
	for _, g := range hs.list(h.Event) {
		if group, ok := g.(*object); ok {
			for _, e := range group.list("hooks") {
				if entry, ok := e.(*object); ok && h.owns(entry.str("command")) {
					n++
				}
			}
		}
	}
	return n, nil
}

// statusLineOf is the command of the status line.
func statusLineOf(raw []byte) string {
	o, err := parseSettings(raw)
	if err != nil {
		return ""
	}
	v, _ := o.get("statusLine")
	if line, ok := v.(*object); ok {
		return line.str("command")
	}
	return ""
}

// BrokenSettings is a settings file that does not parse: it is left as it
// is, and the run stops on it.
type BrokenSettings struct {
	Path string
	Err  error
}

func (b *BrokenSettings) Error() string {
	why := b.Err.Error()
	if errors.Is(b.Err, io.ErrUnexpectedEOF) {
		why = "it ends in the middle"
	}
	return fmt.Sprintf("%s is not valid JSON (%s)", b.Path, why)
}
