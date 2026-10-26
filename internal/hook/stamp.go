package hook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/guards"
	"aacpanel/internal/hostcfg"
)

// The prompt stamp: before every prompt the model is told the date, the
// context of its session with where the cap of its project falls, the limits
// of its account and the load of the machine, from the collector's snapshot.
// The role of the session comes last, only when the conversation needs to be
// told it (lineRole). Each prompt sweeps the stamp's state files and the role
// marks after stampKeep untouched, so a closed conversation leaves no files
// behind indefinitely.
//
// The cases in deploy/claude/testdata/prompt-stamp.json hold the text to the
// byte. The snapshot follows the collector's python JSON conventions: true
// and false count as 1 and 0 where a number is looked for, a number is
// rounded half to even, and a value said as it is — the limits of an
// account — is said as python's str says it: 41 as 41, 41.0 as 41.0, null as
// None. A value of an unexpected type leaves out only the line it is for.

const (
	stampKeep = 7 * 86400 // seconds a stamp state file stays untouched before it goes
	staleSec  = 120       // seconds a snapshot is old before the stamp says so
	diskMin   = 10 << 30  // bytes a disk holds at least to be listed
	gib       = 1 << 30
)

// stampDate is the date line, as time.strftime("[%a %d.%m.%Y %H:%M %z]")
// writes it in the C locale.
const stampDate = "[Mon 02.01.2006 15:04 -0700]"

func stampPrompt(_ string, payload []byte, now time.Time) Reply {
	var p any
	_ = json.Unmarshal(payload, &p)
	fields, _ := p.(map[string]any)
	id, _ := fields["session_id"].(string)
	g := capOf(fields)
	dir := hostcfg.Load().StateDir
	state := readState(filepath.Join(dir, "state.json"))
	lines := stampLines(state, id, os.Getenv("CLAUDE_CONFIG_DIR"), g, now)
	// The role belongs to the main conversation. A payload naming a subagent
	// must not mark it as told: that would keep it from the one it is for.
	role := ""
	if agent, _ := fields["agent_id"].(string); agent == "" {
		role = lineRole(dir, id, sessionOf(state, id))
	}
	if role != "" {
		lines = append(lines, role)
	}
	sweepStamps(dir, float64(now.Unix()-stampKeep))
	return Reply{Context: strings.Join(lines, "\n")}
}

// lineRole is the role of the session, said once a conversation and again
// when it changes or the conversation is compacted, nothing otherwise. Claude
// keeps the word of the panel's server a conversation started with: a resume
// or a reconnect does not read it again, so a role set or changed after the
// start reaches the model only here. What a conversation was last told — a
// hash of the role and the count of its compactions, from the collector's row
// — lies in role-<conversation>.state and goes with the stamp's sweep. A role
// cleared after it was told is said to be gone, once. A conversation the
// snapshot does not know is told nothing, and nothing is written.
func lineRole(dir, id string, row map[string]any) string {
	if id == "" || row == nil {
		return ""
	}
	path := dir + string(filepath.Separator) + "role-" + id + ".state"
	said, err := os.ReadFile(path)
	told := err == nil
	text, _ := row["role"].(string)
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		if !told {
			return ""
		}
		_ = os.Remove(path)
		return "Role:     none now: the role this session was told is cleared"
	}
	compacts, _ := number(row["compacts"])
	sum := sha256.Sum256([]byte(text))
	mark := hex.EncodeToString(sum[:8]) + ":" + whole(compacts)
	if told && string(said) == mark {
		return ""
	}
	if os.WriteFile(path+".tmp", []byte(mark), 0o666) == nil {
		_ = os.Rename(path+".tmp", path)
	}
	return "Role:     " + text
}

// capOf is the guard of the place the session works in, as the panel's map
// says it, or the default cap with no restart where the map names none.
func capOf(payload map[string]any) action.Guard {
	if g, ok := guards.Of(where(payload)); ok {
		return g
	}
	return action.Guard{Cap: guards.CapDefault}
}

// where is the directory a session belongs to: the project directory claude
// names for its hooks, not the working directory, which follows every cd the
// agent makes — the cap would jump to whatever project the agent is looking
// into.
func where(payload map[string]any) string {
	if dir := os.Getenv("CLAUDE_PROJECT_DIR"); dir != "" {
		return dir
	}
	if cwd, _ := payload["cwd"].(string); cwd != "" {
		return cwd
	}
	cwd, _ := os.Getwd()
	return cwd
}

// readState is the collector's snapshot, the parts the hooks read, or nil
// when there is none: no file, a file that is not JSON, or not an object.
func readState(path string) map[string]any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || top == nil {
		return nil
	}
	state := map[string]any{}
	for _, key := range []string{"at", "sessions", "limits", "host"} {
		if part, ok := top[key]; ok {
			state[key] = loose(part)
		}
	}
	return state
}

// loose is a part of the snapshot with its numbers as they are written.
func loose(raw json.RawMessage) any {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	_ = dec.Decode(&v)
	return v
}

// stampLines are the lines of the stamp, top to bottom.
func stampLines(state map[string]any, id, configDir string, g action.Guard, now time.Time) []string {
	lines := []string{now.Format(stampDate)}
	if state == nil {
		lines = append(lines, "The host snapshot is unavailable: the aacpanel collector writes no state")
		return lines
	}
	at, _ := number(state["at"])
	age := seconds(now) - at
	for _, line := range []string{lineContext(state, id, g), lineLimits(state, configDir), lineResources(state),
		lineDisks(state), lineAlarms(state, id, g)} {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if age > staleSec && len(lines) > 1 {
		lines = append(lines, "          the snapshot is "+whole(age/60)+" min old — is the collector stopped?")
	}
	return lines
}

// lineContext is the context fill of the session, with where its cap falls in
// its window. The share is of the window, the hard limit; the cap stands
// beside it as a number of its own — a share of the cap read as a share of the
// window says "wrap up" a quarter too early. Where the project restarts its
// sessions itself, the cap is where the restart comes: the session knows how
// near it is, so as not to start there what the restart would cut, and leaves
// it out of its replies — the guard wraps it up.
func lineContext(state map[string]any, id string, g action.Guard) string {
	s := sessionOf(state, id)
	if s == nil || s["pct"] == nil || !truthy(s["limit"]) {
		return ""
	}
	pct, okPct := number(s["pct"])
	tokens, okTokens := number(s["tokens"])
	limit, okLimit := number(s["limit"])
	if !okPct || !okTokens || !okLimit {
		return ""
	}
	at := whole(limit * float64(g.Cap) / 100 / 1000)
	note := "finalize from " + at + "k"
	if g.Restart {
		note = "auto restart at " + at + "k, automatic: leave it out of replies"
	}
	if known, ok := s["limitKnown"]; ok && !truthy(known) {
		note += " · the model window is not exact"
	}
	return "Context:  " + whole(tokens/1000) + "k/" + window(limit) + " (" + fixed(pct) + "%) · " + note
}

// window names a window: a round million as 1M — "1000k" does not read as
// the model's limit.
func window(tokens float64) string {
	k := pyRound(tokens / 1000)
	if k >= 1000 && math.Mod(k, 1000) == 0 {
		return whole(math.Floor(k/1000)) + "M"
	}
	return whole(k) + "k"
}

// lineLimits is the subscription limits of the account the session runs
// under: the contour whose profile the account's directory ends with, or the
// first.
func lineLimits(state map[string]any, configDir string) string {
	limits := object(state["limits"])
	want := ""
	if configDir != "" {
		dir := strings.TrimRight(configDir, "/")
		want = dir[strings.LastIndex(dir, "/")+1:]
	}
	rows := []any{limits}
	if list, ok := limits["contours"].([]any); ok && len(list) > 0 {
		rows = list
	}
	var row map[string]any
	for _, c := range rows {
		candidate, _ := c.(map[string]any)
		profile, _ := candidate["profile"].(string)
		if want != "" && profile != "" && strings.HasSuffix(want, profile) {
			row = candidate
			break
		}
	}
	if row == nil {
		row, _ = rows[0].(map[string]any)
	}
	five := object(row["fiveHour"])["pct"]
	week := object(row["sevenDay"])["pct"]
	if five == nil && week == nil {
		return ""
	}
	said := "Limits:   5h " + pyStr(five) + "% · week " + pyStr(week) + "%"
	if age, ok := number(row["ageSec"]); ok && age > staleSec {
		said += " (the snapshot is " + whole(age/60) + " min old)"
	}
	return said
}

// lineResources is the load of the machine. The temperature stands in
// brackets by its device: a field of its own beside it read as a second
// measure of the same name.
func lineResources(state map[string]any) string {
	host := object(state["host"])
	var parts []string
	if cpu, ok := number(host["cpuPct"]); ok {
		part := "CPU " + fixed(cpu) + "%"
		if temp, ok := number(host["cpuTemp"]); ok {
			part += " (" + fixed(temp) + "°C)"
		}
		parts = append(parts, part)
	}
	if mem, ok := number(object(host["mem"])["pct"]); ok {
		parts = append(parts, "RAM "+fixed(mem)+"%")
	}
	if len(parts) == 0 {
		return ""
	}
	return "Load:     " + strings.Join(parts, " · ")
}

// lineDisks is the use of the three largest disks of diskMin or more.
func lineDisks(state map[string]any) string {
	type disk struct {
		fields map[string]any
		total  float64
	}
	var disks []disk
	for _, d := range list(object(state["host"])["disks"]) {
		fields, ok := d.(map[string]any)
		if !ok || !truthy(fields["total"]) {
			continue
		}
		if total, ok := number(fields["total"]); ok && total >= diskMin {
			disks = append(disks, disk{fields, total})
		}
	}
	slices.SortStableFunc(disks, func(a, b disk) int {
		switch {
		case a.total > b.total:
			return -1
		case a.total < b.total:
			return 1
		}
		return 0
	})
	var said []string
	for _, d := range disks[:min(3, len(disks))] {
		used := 0.0
		if v, ok := d.fields["used"]; ok {
			var isNumber bool
			if used, isNumber = number(v); !isNumber {
				continue
			}
		}
		said = append(said, mount(d.fields)+" "+fixed(used/gib)+"G/"+fixed(d.total/gib)+"G")
	}
	if len(said) == 0 {
		return ""
	}
	return "          Disks " + strings.Join(said, " · ")
}

// lineAlarms names what is wrong. A session past its cap must leave room
// to save its state when the panel asks, including by starting no background work.
func lineAlarms(state map[string]any, id string, g action.Guard) string {
	var alarms []string
	if id != "" && pastCap(state, id, g.Cap) {
		if g.Restart {
			alarms = append(alarms, "CONTEXT past the cap: start no new work, background included; "+
				"the panel asks this session to save its state before the restart")
		} else {
			alarms = append(alarms, "CONTEXT past the cap")
		}
	}
	host := object(state["host"])
	for _, d := range list(host["disks"]) {
		fields, _ := d.(map[string]any)
		if pct, ok := number(fields["pct"]); ok && pct >= 90 {
			alarms = append(alarms, "DISK "+mount(fields)+" "+fixed(pct)+"%")
		}
	}
	if mem, ok := number(object(host["mem"])["pct"]); ok && mem >= 90 {
		alarms = append(alarms, "MEMORY "+fixed(mem)+"%")
	}
	if len(alarms) == 0 {
		return ""
	}
	return "Alarms:   " + strings.Join(alarms, " · ")
}

// pastCap tells whether the session's context is past the cap of its project.
func pastCap(state map[string]any, id string, limit int) bool {
	s := sessionOf(state, id)
	pct, ok := number(s["pct"])
	return ok && truthy(s["limit"]) && pct >= float64(limit)
}

// sessionOf is the collector's row of the session, or nil.
func sessionOf(state map[string]any, id string) map[string]any {
	for _, r := range list(state["sessions"]) {
		row, _ := r.(map[string]any)
		if sid, ok := row["sessionId"].(string); ok && sid == id {
			return row
		}
	}
	return nil
}

// mount is the mount point a disk is said by, ? when the snapshot names none.
func mount(disk map[string]any) string {
	if m, ok := disk["mount"]; ok {
		return pyStr(m)
	}
	return "?"
}

// sweepStamps takes away the stamp state files and the marks of the role written
// before a moment, in seconds. The state directory is shared with the
// collector: only the stamp's own files go.
func sweepStamps(dir string, before float64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		own := strings.HasPrefix(name, "stamp-") || strings.HasPrefix(name, "role-")
		if !own || !(strings.HasSuffix(name, ".state") || strings.HasSuffix(name, ".state.tmp")) {
			continue
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && seconds(info.ModTime()) < before {
			_ = os.Remove(path)
		}
	}
}

// ---- JSON as python reads it ----

// object is a value that is an object; a value that is not is an empty one.
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// list is a value that is a list; a value that is not is an empty one.
func list(v any) []any {
	l, _ := v.([]any)
	return l
}

// number is a value python takes for a number: a number of the JSON, or true
// and false, which python counts as 1 and 0. A number past the range of a
// float is python's infinity.
func number(v any) (float64, bool) {
	switch v := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		return f, err == nil || errors.Is(err, strconv.ErrRange)
	case float64:
		return v, true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// seconds is a moment in seconds of the Unix clock, as time.time() gives it.
func seconds(t time.Time) float64 {
	return float64(t.UnixNano()) / 1e9
}

// pyRound is round(): to the nearest whole number, half to even.
func pyRound(f float64) float64 {
	r := math.RoundToEven(f)
	if r == 0 {
		return 0 // round(-0.4) is 0, not -0
	}
	return r
}

// whole is round(f) as python writes the int it gives.
func whole(f float64) string {
	return strconv.FormatFloat(pyRound(f), 'f', 0, 64)
}

// fixed is f"{f:.0f}": the nearest whole number, half to even, a minus kept
// on a negative that rounds to nought.
func fixed(f float64) string {
	return strconv.FormatFloat(f, 'f', 0, 64)
}

// pyStr is str() of a value of the JSON.
func pyStr(v any) string {
	switch v := v.(type) {
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case string:
		return v
	case json.Number:
		s := string(v)
		if !strings.ContainsAny(s, ".eE") {
			if s == "-0" {
				return "0"
			}
			return s
		}
		f, _ := number(v)
		return pyFloat(f)
	}
	// A list or an object, which python would write as its repr: no
	// snapshot holds one where a value is said.
	raw, _ := json.Marshal(v)
	return string(raw)
}

// pyFloat is repr() of a float: the shortest digits that read back, in
// positional notation from 1e-4 up to 1e16 with ".0" on a whole number, and
// with an exponent of two digits at least outside that.
func pyFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	mantissa, exp, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	e, _ := strconv.Atoi(exp)
	if e < -4 || e >= 16 {
		return mantissa + "e" + fmt.Sprintf("%+03d", e)
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
