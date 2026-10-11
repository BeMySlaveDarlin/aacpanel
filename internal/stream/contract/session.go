package contract

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	turnWait    = 150 * time.Second
	requestWait = 30 * time.Second
)

var passedEnv = []string{"HOME", "PATH", "USER", "LOGNAME", "LANG", "XDG_RUNTIME_DIR", "TMPDIR",
	"CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_TMPDIR", "ANTHROPIC_API_KEY", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY"}
var streamFlags = []string{"--input-format", "--output-format", "--permission-prompt-tool", "--session-id", "--replay-user-messages", "--include-partial-messages"}

func cleanEnv(env []string) []string {
	var out []string
	values := map[string]string{}
	for _, pair := range env {
		k, v, _ := strings.Cut(pair, "=")
		values[k] = v
	}
	if values["LANG"] == "" {
		values["LANG"] = "C.UTF-8"
	}
	for _, k := range passedEnv {
		if v := values[k]; v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}
func envValue(env []string, key string) string {
	for _, pair := range env {
		if v, ok := strings.CutPrefix(pair, key+"="); ok {
			return v
		}
	}
	return ""
}
func configDir(env []string) string {
	if v := envValue(env, "CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(envValue(env, "HOME"), ".claude")
}
func findTranscript(config, id string) string {
	paths, _ := filepath.Glob(filepath.Join(config, "projects", "*", id+".jsonl"))
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}
func directConnect(help string) []string {
	var out []string
	for _, mark := range []string{"cc://", "cc+unix://", "claude connect", "claude server"} {
		if strings.Contains(help, mark) {
			out = append(out, mark)
		}
	}
	return out
}
func uuid() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type eventRead struct {
	event object
	err   error
}
type session struct {
	sid, cwd, manual string
	seen             []object
	events           <-chan eventRead
	input            io.WriteCloser
	output           io.ReadCloser
	cmd              *exec.Cmd
	done             chan struct{}
	readDone         chan struct{}
	stop             chan struct{}
	log, stderr      *os.File
	counter          int
	err              error
}

func startSession(claude, cwd string, env []string, model string) (*session, error) {
	s := &session{sid: uuid(), cwd: cwd, manual: "default", done: make(chan struct{}), readDone: make(chan struct{}), stop: make(chan struct{})}
	var err error
	s.log, err = os.OpenFile(filepath.Join(cwd, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	s.stderr, err = os.OpenFile(filepath.Join(cwd, "events.stderr"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		s.log.Close()
		return nil, err
	}
	s.cmd = exec.Command(claude, "-p", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--replay-user-messages", "--model", model, "--permission-prompt-tool", "stdio",
		"--setting-sources", "project", "--session-id", s.sid, "--allow-dangerously-skip-permissions")
	s.cmd.Dir = cwd
	s.cmd.Env = env
	s.cmd.Stderr = s.stderr
	s.input, err = s.cmd.StdinPipe()
	if err != nil {
		s.log.Close()
		s.stderr.Close()
		return nil, err
	}
	output, err := s.cmd.StdoutPipe()
	if err != nil {
		s.input.Close()
		s.log.Close()
		s.stderr.Close()
		return nil, err
	}
	if err = s.cmd.Start(); err != nil {
		output.Close()
		s.input.Close()
		s.log.Close()
		s.stderr.Close()
		return nil, err
	}
	s.output = output
	events := make(chan eventRead, 256)
	s.events = events
	go func() {
		defer close(s.readDone)
		defer close(events)
		readEvents(io.TeeReader(output, s.log), func(e eventRead) bool {
			select {
			case events <- e:
				return true
			case <-s.stop:
				return true
			}
		})
	}()
	go func() { <-s.readDone; s.cmd.Wait(); close(s.done) }()
	return s, nil
}

// Stream output is JSONL. Non-JSON diagnostics do not take a turn down; a
// broken pipe does. There is no Scanner token limit on a large tool result.
func readEvents(r io.Reader, emit func(eventRead) bool) {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		var ev object
		if json.Unmarshal(line, &ev) == nil && ev != nil {
			if !emit(eventRead{event: ev}) {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				emit(eventRead{err: err})
			}
			return
		}
	}
}
func (s *session) send(msg object) {
	if s.err != nil {
		return
	}
	s.err = json.NewEncoder(s.input).Encode(msg)
}
func (s *session) user(text, id string) {
	m := object{"type": "user", "session_id": s.sid, "parent_tool_use_id": nil, "message": object{"role": "user", "content": text}}
	if id != "" {
		m["uuid"] = id
	}
	s.send(m)
}
func (s *session) pump(until func(object) bool, wait time.Duration, on func(object)) object {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for s.err == nil {
		select {
		case item, ok := <-s.events:
			if !ok {
				s.err = io.EOF
				return nil
			}
			if item.err != nil {
				s.err = item.err
				return nil
			}
			ev := item.event
			s.seen = append(s.seen, ev)
			if on != nil {
				on(ev)
			}
			if until(ev) {
				return ev
			}
		case <-timer.C:
			return nil
		}
	}
	return nil
}
func (s *session) request(subtype string, fields object) object {
	s.counter++
	id := fmt.Sprintf("contract-%d", s.counter)
	req := object{"subtype": subtype}
	for k, v := range fields {
		req[k] = v
	}
	s.send(object{"type": "control_request", "request_id": id, "request": req})
	ev := s.pump(func(e object) bool {
		return e.text("type") == "control_response" && e.obj("response").text("request_id") == id
	}, requestWait, nil)
	return ev.obj("response")
}
func (s *session) answer(ev, body object) {
	s.send(object{"type": "control_response", "response": object{"subtype": "success", "request_id": ev.text("request_id"), "response": body}})
}
func (s *session) allow(ev, patch object) {
	body := object{}
	for k, v := range ev.obj("request").obj("input") {
		body[k] = v
	}
	for k, v := range patch {
		body[k] = v
	}
	s.answer(ev, object{"behavior": "allow", "updatedInput": body})
}
func (s *session) onAsk(ev object) {
	if isAsk(ev) {
		s.allow(ev, nil)
	}
}
func (s *session) turn(text string, onAsk func(object)) (object, []object) {
	start := len(s.seen)
	s.user(text, "")
	if onAsk == nil {
		onAsk = func(ev object) { s.allow(ev, nil) }
	}
	result := s.pump(isResult, turnWait, func(ev object) {
		if isAsk(ev) {
			onAsk(ev)
		}
	})
	return result, s.seen[start:]
}
func (s *session) close() {
	s.input.Close()
	// Keep draining stdout while claude flushes its transcript. No reader of
	// stderr can block it: stderr has its own file for the whole lifetime.
	close(s.stop)
	select {
	case <-s.done:
	case <-time.After(30 * time.Second):
		s.cmd.Process.Kill()
		s.output.Close()
		<-s.done
	}
	s.log.Close()
	s.stderr.Close()
}

func (s *session) gone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
