package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
)

// The checklist reminder: at the end of a turn a session that keeps a
// checklist is asked once whether it changed.
//
// The checklist is the model's own list of steps, kept through the panel's
// checklist tool and shown to the person. It belongs to the session: the
// place it works in — the config directory of the account and the directory
// claude runs in — and its name, so a session started again under its name,
// afresh or going on with its conversation, finds it, and another session of
// the directory has its own; a session without a name is told by its place
// alone. A turn that did work — called tools — and left the checklist as it
// was may have left it behind: the end of that turn is held once, and the
// model is told to update the checklist if it changed, to clear it if it no
// longer applies, and otherwise to end the turn. The turn after the hold is
// never held again.
//
// A checklist with nothing left to do asks nothing, and neither does a
// session that has no checklist tool: a claude started by hand in the same
// place cannot update the checklist. A session has the tool when it sent the
// checklist itself, or when it was started with the tool allowed, as the
// panel starts every session — which is how a session started again after a
// restart is asked about the checklist the one before it left.
//
// The claude whose turn ends is named by its holder or found up the chain of
// the hook's parents, by the file in its config directory, sessions/<pid>.json,
// and only while that file is of the live process. The cases live in
// deploy/claude/testdata/checklist-reminder.json.

const (
	// checklistTool is the checklist tool of the panel's server.
	checklistTool = "mcp__aacpanel__checklist"
	// claudeAncestors is how far up the chain of its parents the hook looks
	// for the claude running it: claude may start a hook through a shell.
	claudeAncestors = 6
)

// transcriptTail is how much of the tail of the transcript is read for the
// turn. A turn longer than this is taken from the oldest record read, which
// is all the question needs: whether the turn called tools, and whether the
// checklist is older.
var transcriptTail int64 = 4 << 20

// procDir is where the processes are read; parentPID is the process the
// hook was started by. A test puts a /proc and a parent of its own.
var (
	procDir   = "/proc"
	parentPID = os.Getppid
)

// checklistReason is what the model is told instead of stopping.
const checklistReason = "The panel shows the person the checklist of this session's work, and this turn did work " +
	"without touching it. If the checklist changed — a step started, ended or was dropped, or the " +
	"steps themselves changed — send it with the checklist tool (" + checklistTool + "); if it no longer applies, " +
	"clear it with an empty list. If it did not change, end the turn now, without a word about this."

func remindChecklist(_ string, payload []byte, _ time.Time) Reply {
	p, ok := loadNumbers(payload).(map[string]any)
	if !ok {
		return Reply{}
	}
	if event, ok := p["hook_event_name"]; ok && event != "Stop" {
		return Reply{}
	}
	// The turn that follows a hold is the answer to it: it is never held again.
	if truthy(p["stop_hook_active"]) {
		return Reply{}
	}
	if _, codex := codexHome(nil, p); codex {
		return remindCodexChecklist(p)
	}
	id := p["session_id"]
	if !truthy(id) {
		id = ""
	}
	pid, session := claudeOf(id)
	if session == nil {
		return Reply{}
	}
	list, written, ok := readChecklist(claudeConfigDir(), session["cwd"], sessionName(pid, session), session["sessionId"])
	if !ok || !unfinished(list) {
		return Reply{}
	}
	if !pyEqual(list["pid"], json.Number(strconv.Itoa(pid))) && !hasTool(pid) {
		return Reply{}
	}
	path, _ := p["transcript_path"].(string)
	began, known, called := turnOf(path)
	if !called || !known || written >= began {
		return Reply{}
	}
	return Reply{Block: checklistReason}
}

func remindCodexChecklist(p map[string]any) Reply {
	id, _ := p["session_id"].(string)
	file := checklist.ThreadFile(id)
	if file == "" {
		return Reply{}
	}
	path := filepath.Join(checklist.Dir(), file)
	info, err := os.Stat(path)
	if err != nil {
		return Reply{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Reply{}
	}
	list, _ := loadNumbers(raw).(map[string]any)
	if list["sessionId"] != id || !unfinished(list) {
		return Reply{}
	}
	transcript, _ := p["transcript_path"].(string)
	turn, _ := p["turn_id"].(string)
	began, known, called := codexTurnOf(transcript, turn)
	if !known || !called || seconds(info.ModTime()) >= began {
		return Reply{}
	}
	return Reply{Block: checklistReason}
}

// A rollout's tool calls carry no turn id. Only a matching task_started
// boundary proves that they belong to the Stop being answered; a truncated
// tail or another turn is not evidence that this turn worked.
func codexTurnOf(path, turn string) (began float64, known, called bool) {
	if turn == "" {
		return 0, false, false
	}
	for record := range newestFirst(path) {
		p := object(record["payload"])
		switch record["type"] {
		case "event_msg":
			if p["type"] == "task_started" {
				if p["turn_id"] != turn {
					return 0, false, false
				}
				began, known = stampOf(record)
				return began, known, called
			}
		case "turn_context":
			if p["turn_id"] != turn {
				return 0, false, false
			}
		case "response_item":
			switch p["type"] {
			case "function_call", "custom_tool_call", "local_shell_call", "web_search_call", "image_generation_call":
				called = true
			}
		}
	}
	return 0, false, false
}

// loadNumbers is a JSON text read with its numbers as they are written, nil
// for one that does not read or is not UTF-8, which python's reading of a
// file, of stdin and of a line of bytes refuses.
func loadNumbers(raw []byte) any {
	if !utf8.Valid(raw) {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil
	}
	return v
}

// pyEqual is == of python between two values of the JSON: numbers by their
// value, true and false among them as 1 and 0.
func pyEqual(a, b any) bool {
	if x, ok := number(a); ok {
		y, ok := number(b)
		return ok && x == y
	}
	if _, ok := number(b); ok {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// ---- the claude running the hook ----

// claudeConfigDir is the config directory of the claude running the hook:
// the one it passes on, or its default.
func claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// statOf is the fields of /proc/<pid>/stat after the name of the process,
// nil when there are none to read.
func statOf(pid int) []string {
	raw, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "stat"))
	if err != nil || !utf8.Valid(raw) {
		return nil
	}
	i := bytes.LastIndexByte(raw, ')')
	if i < 0 {
		return nil
	}
	return strings.FieldsFunc(string(raw[i+1:]), pySpace)
}

// ancestors are the processes above the hook, its parent first, init left
// out.
func ancestors() []int {
	var out []int
	pid := parentPID()
	for pid > 1 && len(out) < claudeAncestors && !slices.Contains(out, pid) {
		out = append(out, pid)
		fields := statOf(pid)
		if len(fields) < 2 {
			break
		}
		next, err := strconv.Atoi(fields[1])
		if err != nil {
			break
		}
		pid = next
	}
	return out
}

// sessionFile is the file claude keeps of a process of its, or nil when the
// file is not that process's: another kind of claude's, or one a dead
// process with the same pid left.
func sessionFile(pid int) map[string]any {
	raw, err := os.ReadFile(filepath.Join(claudeConfigDir(), "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return nil
	}
	data, ok := loadNumbers(raw).(map[string]any)
	if !ok || !pyEqual(data["pid"], json.Number(strconv.Itoa(pid))) {
		return nil
	}
	if kind := data["kind"]; kind != nil && kind != "" && kind != "interactive" {
		return nil
	}
	fields := statOf(pid)
	if len(fields) < 20 || !truthy(data["procStart"]) || pyStr(data["procStart"]) != fields[19] {
		return nil
	}
	return data
}

// claudeOf is the pid of the claude running the hook and the file it keeps
// of itself, or nothing. The holder names its child explicitly; a settings
// hook takes the nearest process above it with a file. Either must name the
// conversation the hook is for: a
// claude run inside the work of a session has no file of its own, and the
// session above it is not the one whose turn ends.
func claudeOf(id any) (int, map[string]any) {
	if named := os.Getenv(PIDEnv); named != "" {
		pid, err := strconv.Atoi(named)
		if err != nil || pid <= 0 {
			return 0, nil
		}
		if data := sessionFile(pid); data != nil && pyEqual(data["sessionId"], id) {
			return pid, data
		}
		return 0, nil
	}
	for _, pid := range ancestors() {
		if data := sessionFile(pid); data != nil {
			if pyEqual(data["sessionId"], id) {
				return pid, data
			}
			return 0, nil
		}
	}
	return 0, nil
}

// argsOf is the command line of a process, split at its NULs the way python
// splits it: a last empty word after the last NUL.
func argsOf(pid int) []string {
	raw, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil
	}
	return strings.Split(string(raw), "\x00")
}

// hasTool tells whether a claude process was started with the checklist
// tool allowed, as the panel starts every session: a word of its arguments,
// split at white space, commas and equals signs, is the tool.
func hasTool(pid int) bool {
	for _, arg := range argsOf(pid) {
		words := strings.FieldsFunc(arg, func(r rune) bool { return strings.ContainsRune(" \t\n\r\f\v,=", r) })
		if slices.Contains(words, checklistTool) {
			return true
		}
	}
	return false
}

// sessionName is the name of the session of a claude, empty for one started
// without a name: the name claude keeps in the file of the session, or else
// the one the process was started with, in either form claude takes.
func sessionName(pid int, session map[string]any) string {
	if name, ok := session["name"].(string); ok && name != "" {
		return name
	}
	args := argsOf(pid)
	for i, arg := range args {
		for _, flag := range []string{"-n", "--name"} {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
			if after, ok := strings.CutPrefix(arg, flag+"="); ok {
				return after
			}
		}
	}
	return ""
}

// ---- the checklist ----

// readPlaced is the checklist of a place and a name, empty for a place
// alone, and when its file was written, in seconds of the Unix clock as
// python's stat gives them. A file of another place, or that does not read,
// is none.
func readPlaced(configDir string, cwd any, name string) (map[string]any, float64, bool) {
	dir, ok := cwd.(string)
	if !ok {
		return nil, 0, false
	}
	place := mcp.Place{ConfigDir: configDir, Dir: dir}
	file := checklist.File(place, name)
	if file == "" {
		return nil, 0, false
	}
	path := filepath.Join(checklist.Dir(), file)
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false
	}
	found, ok := loadNumbers(raw).(map[string]any)
	where, _ := place.Clean()
	own, _ := found["name"].(string)
	if !ok || found["configDir"] != where.ConfigDir || found["dir"] != where.Dir || own != name {
		return nil, 0, false
	}
	mtime := info.ModTime()
	return found, float64(mtime.Unix()) + float64(mtime.Nanosecond())*1e-9, true
}

// readChecklist is the checklist of a session and when its file was written.
// A named session without a file of its own has the checklist of its place —
// the one a server that does not tell the sessions of a place apart keeps
// writing — only while its conversation sent it last.
func readChecklist(configDir string, cwd any, name string, id any) (map[string]any, float64, bool) {
	found, written, ok := readPlaced(configDir, cwd, name)
	if !ok && name != "" {
		found, written, ok = readPlaced(configDir, cwd, "")
		if ok && !pyEqual(found["sessionId"], id) {
			return nil, 0, false
		}
	}
	return found, written, ok
}

// unfinished tells whether a step of the checklist is still to do or at work.
func unfinished(list map[string]any) bool {
	items, _ := list["items"].([]any)
	for _, it := range items {
		if item, ok := it.(map[string]any); ok && (item["status"] == checklist.Pending || item["status"] == checklist.Active) {
			return true
		}
	}
	return false
}

// ---- the turn ----

// turnOf is when the turn whose end the hook answers began, and whether it
// called tools; the time is not known when no record read names one.
func turnOf(path string) (began float64, known, called bool) {
	for record := range newestFirst(path) {
		if at, ok := stampOf(record); ok {
			began, known = at, true
		}
		if calledTools(record) {
			called = true
		}
		if isPrompt(record) {
			break
		}
	}
	return began, known, called
}

// newestFirst are the records of the tail of a transcript, newest first,
// those of the session's agents left out, each read only when it is asked
// for: a turn is a few records at the end of megabytes. A line that is no
// record is passed over, and so is the first line read when the tail starts
// within it.
func newestFirst(path string) iter.Seq[map[string]any] {
	return func(yield func(map[string]any) bool) {
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		size, err := f.Seek(0, io.SeekEnd)
		if err != nil {
			return
		}
		if _, err := f.Seek(max(0, size-transcriptTail), io.SeekStart); err != nil {
			return
		}
		data, err := io.ReadAll(f)
		if err != nil {
			return
		}
		lines := bytes.Split(data, []byte("\n"))
		if size > transcriptTail {
			lines = lines[1:]
		}
		for _, raw := range slices.Backward(lines) {
			if len(bytes.Trim(raw, " \t\n\r\v\f")) == 0 {
				continue
			}
			// python reads a line of bytes past a mark of UTF-8 at its start.
			record, ok := loadNumbers(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))).(map[string]any)
			if ok && !truthy(record["isSidechain"]) && !yield(record) {
				return
			}
		}
	}
}

// stampOf is the time of a record in seconds of the Unix clock, as python's
// datetime.fromisoformat reads it with a Z for +00:00: the date, a character
// between, the time to the second with a fraction of up to six digits read
// and the rest let go, and the offset, Z, ±hh, ±hhmm or ±hh:mm, or none for
// the local time. python reads more forms of ISO 8601, none of which a
// transcript holds.
func stampOf(record map[string]any) (float64, bool) {
	at, _ := record["timestamp"].(string)
	if at == "" {
		return 0, false
	}
	t, ok := parseISO(strings.ReplaceAll(at, "Z", "+00:00"))
	if !ok {
		return 0, false
	}
	return float64(t.UnixMicro()) / 1e6, true
}

func parseISO(s string) (time.Time, bool) {
	if len(s) < 11 {
		return time.Time{}, false
	}
	_, w := utf8.DecodeRuneInString(s[10:])
	date, clock := s[:10], s[10+w:]
	if len(clock) < 8 {
		return time.Time{}, false
	}
	clock, rest := clock[:8], clock[8:]
	frac := ""
	if len(rest) > 0 && (rest[0] == '.' || rest[0] == ',') {
		n := 1
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		frac, rest = rest[1:min(n, 7)], rest[n:]
	}
	loc := time.Local
	if rest != "" {
		sign := 1
		switch rest[0] {
		case '+':
		case '-':
			sign = -1
		default:
			return time.Time{}, false
		}
		digits := strings.Replace(rest[1:], ":", "", 1)
		if len(digits) != 2 && len(digits) != 4 || strings.Trim(digits, "0123456789") != "" {
			return time.Time{}, false
		}
		h, _ := strconv.Atoi(digits[:2])
		m := 0
		if len(digits) == 4 {
			m, _ = strconv.Atoi(digits[2:])
		}
		if h > 23 || m > 59 {
			return time.Time{}, false
		}
		loc = time.FixedZone("", sign*(h*3600+m*60))
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", date+"T"+clock, loc)
	if err != nil {
		return time.Time{}, false
	}
	if frac != "" {
		micro, _ := strconv.Atoi(frac + strings.Repeat("0", 6-len(frac)))
		t = t.Add(time.Duration(micro) * time.Microsecond)
	}
	return t, true
}

// isPrompt tells whether a record starts a turn: a message to the session,
// not a tool's result.
func isPrompt(record map[string]any) bool {
	if record["type"] != "user" || truthy(record["isMeta"]) || truthy(record["isCompactSummary"]) {
		return false
	}
	switch content := contentOf(record).(type) {
	case string:
		return strings.TrimFunc(content, pySpace) != ""
	case []any:
		text, result := false, false
		for _, b := range content {
			if block, ok := b.(map[string]any); ok {
				text = text || block["type"] == "text"
				result = result || block["type"] == "tool_result"
			}
		}
		return text && !result
	}
	return false
}

func calledTools(record map[string]any) bool {
	if record["type"] != "assistant" {
		return false
	}
	blocks, _ := contentOf(record).([]any)
	return slices.ContainsFunc(blocks, func(b any) bool {
		block, ok := b.(map[string]any)
		return ok && block["type"] == "tool_use"
	})
}

func contentOf(record map[string]any) any {
	message, _ := record["message"].(map[string]any)
	return message["content"]
}
