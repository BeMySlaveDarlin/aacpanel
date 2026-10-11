// Package statusline keeps the limits and session model claude gives its
// status line, then hands the same input to the person's line, if any.
package statusline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const Command = "statusline"

// Run keeps the chain even when a payload cannot be read or a file cannot
// be written. The shell form strips trailing newlines in its command
// substitution; the person's command gets those same bytes here.
func Run(in io.Reader, out, errs io.Writer) int {
	return run(in, out, errs, time.Now())
}

func run(in io.Reader, out, errs io.Writer, now time.Time) int {
	raw, _ := io.ReadAll(in)
	raw = bytes.TrimRight(bytes.ReplaceAll(raw, []byte{0}, nil), "\n")
	snapshot(raw, now)
	if next := os.Getenv("AACP_STATUSLINE_NEXT"); next != "" {
		cmd := exec.Command("sh", "-c", next)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(raw), out, errs
		_ = cmd.Run()
	}
	return 0
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func read(raw []byte) map[string]any {
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

func field(v map[string]any, keys ...string) any {
	var value any = v
	for _, key := range keys {
		obj, _ := value.(map[string]any)
		value = obj[key]
	}
	// jq's alternative operator treats false like null, but keeps zero.
	if value == false {
		return nil
	}
	return value
}

func encoded(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	s := strings.TrimSuffix(b.String(), "\n")
	// JSON permits literal line separators, and jq writes them that way.
	// A doubled backslash belongs to the value and must stay escaped.
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch {
			case strings.HasPrefix(s[i:], `\u2028`):
				out.WriteRune('\u2028')
				i += 5
			case strings.HasPrefix(s[i:], `\u2029`):
				out.WriteRune('\u2029')
				i += 5
			default:
				out.WriteString(s[i : i+2])
				i++
			}
		} else {
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

// rawValue is jq -r: a string is unquoted, all other values are JSON.
func rawValue(v any, fallback string) string {
	if v == nil {
		return fallback
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return fallback
		}
		return s
	}
	return encoded(v)
}

func replace(path, text string) {
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, []byte(text+"\n"), 0o666); err == nil {
		_ = os.Rename(tmp, path)
	}
	_ = os.Remove(tmp)
}

var sessionID = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

func snapshot(raw []byte, now time.Time) {
	v := read(raw)
	if v == nil {
		return
	}
	config := env("CLAUDE_CONFIG_DIR", filepath.Join(os.Getenv("HOME"), ".claude"))
	path := env("AACP_RATE_SNAPSHOT", filepath.Join(config, "rate-limits.json"))
	five := rawValue(field(v, "rate_limits", "five_hour", "used_percentage"), "")
	throttle, err := strconv.ParseInt(env("AACP_RATE_THROTTLE", "20"), 10, 64)
	if err != nil {
		throttle = 20
	}
	var was int64
	if st, err := os.Stat(path); err == nil {
		was = st.ModTime().Unix()
	}
	if five != "" && now.Unix()-was >= throttle {
		value := func(window, key string) string {
			return rawValue(field(v, "rate_limits", window, key), "null")
		}
		replace(path, fmt.Sprintf(`{"at":%d,"fiveHour":{"pct":%s,"resetsAt":%s},"sevenDay":{"pct":%s,"resetsAt":%s}}`,
			now.Unix(), five, value("five_hour", "resets_at"), value("seven_day", "used_percentage"), value("seven_day", "resets_at")))
	}

	sid := rawValue(field(v, "session_id"), "")
	id := rawValue(field(v, "model", "id"), "")
	name := rawValue(field(v, "model", "display_name"), "")
	effort := field(v, "effort")
	if obj, ok := effort.(map[string]any); ok {
		effort = field(obj, "level")
	}
	level := rawValue(effort, "")
	window := rawValue(field(v, "context_window", "context_window_size"), "")
	if !sessionID.MatchString(sid) || id == "" || strings.ContainsAny(id+name+level+window, "\n") {
		return
	}
	models := env("AACP_SESSION_MODELS", filepath.Join(config, "session-models"))
	path = filepath.Join(models, sid+".json")
	if held, err := os.ReadFile(path); err == nil {
		h := read(held)
		if id+"|"+name+"|"+level == rawValue(field(h, "model", "id"), "")+"|"+
			rawValue(field(h, "model", "displayName"), "")+"|"+rawValue(field(h, "effort"), "") {
			return
		}
	}
	if err := os.MkdirAll(models, 0o777); err != nil {
		return
	}
	var size any
	if window != "" {
		n, err := strconv.ParseFloat(window, 64)
		if err != nil {
			return
		}
		size = n
	}
	var savedEffort any
	if level != "" {
		savedEffort = level
	}
	// Field order is part of the file shared with the shell status line.
	replace(path, fmt.Sprintf(`{"at":%d,"sessionId":%s,"model":{"id":%s,"displayName":%s},"effort":%s,"contextWindow":%s}`,
		now.Unix(), encoded(sid), encoded(id), encoded(name), encoded(savedEffort), encoded(size)))
	entries, _ := os.ReadDir(models)
	for _, e := range entries {
		st, err := e.Info()
		// find -mtime +7 counts whole days: a file is swept at eight days.
		if err == nil && st.Mode().IsRegular() && strings.HasSuffix(e.Name(), ".json") && now.Sub(st.ModTime()) >= 8*24*time.Hour {
			_ = os.Remove(filepath.Join(models, e.Name()))
		}
	}
}
