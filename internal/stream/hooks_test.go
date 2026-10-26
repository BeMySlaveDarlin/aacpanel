package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/hook"
)

func fakeHook() int {
	switch os.Getenv("FAKE_HOOK_MODE") {
	case "empty":
		return 0
	case "error":
		fmt.Print(`{"decision":"block","reason":"must be discarded"}`)
		return 1
	case "timeout":
		time.Sleep(time.Hour)
	case "invalid":
		fmt.Print("not JSON")
		return 0
	case "null":
		fmt.Print("null")
		return 0
	}
	input, _ := io.ReadAll(os.Stdin)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"input": json.RawMessage(input), "args": os.Args[1:],
		"pid": os.Getenv(hook.PIDEnv), "held": os.Getenv(hook.HeldEnv),
		"config": os.Getenv("CLAUDE_CONFIG_DIR"), "state": os.Getenv("AACP_STATE_DIR"),
		"path": os.Getenv("PATH"),
	})
	return 0
}

func TestTheHandshakeRegistersTheHeldHooks(t *testing.T) {
	r := start(t, func(spec *Spec) {
		spec.Env = []string{"STREAM_FAKE_CLAUDE=1", "FAKE_LOG=" + os.Getenv("FAKE_LOG"), "CLAUDE_CONFIG_DIR=/prepared/contour", hook.HeldEnv + "=0", hook.PIDEnv + "=123"}
	})
	s := r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	var init struct {
		Held   string `json:"held"`
		PID    string `json:"hook_pid"`
		Config string `json:"config"`
	}
	if err := json.Unmarshal(s.Init, &init); err != nil || init.Held != "1" || init.PID != "" || init.Config != "/prepared/contour" {
		t.Fatalf("claude did not get the holder's hook environment: %s (%v)", s.Init, err)
	}
	for _, line := range strings.Split(r.received(), "\n") {
		var msg struct {
			Request struct {
				Subtype string                   `json:"subtype"`
				Hooks   map[string][]hookMatcher `json:"hooks"`
			} `json:"request"`
		}
		if json.Unmarshal([]byte(line), &msg) != nil || msg.Request.Subtype != "initialize" {
			continue
		}
		want := map[string][]hookMatcher{
			"UserPromptSubmit": {{CallbackIDs: []string{"prompt-stamp"}}},
			"Stop":             {{CallbackIDs: []string{"checklist-reminder", "background-reminder"}}},
		}
		if !reflect.DeepEqual(msg.Request.Hooks, want) {
			t.Fatalf("the handshake registered %v, want %v", msg.Request.Hooks, want)
		}
		return
	}
	t.Fatal("claude never received the handshake")
}

type hookOutput chan []byte

func (out hookOutput) Write(p []byte) (int, error) {
	out <- append([]byte(nil), p...)
	return len(p), nil
}
func (out hookOutput) Close() error { return nil }

func hookHolder(t *testing.T, mode string) (*Holder, hookOutput) {
	t.Helper()
	out := make(hookOutput, 4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Holder{
		spec: Spec{Dir: t.TempDir()}, hookCtx: ctx, hookPath: os.Args[0], stdin: out,
		cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}, Env: []string{
			"CLAUDE_CONFIG_DIR=/session/contour", "AACP_STATE_DIR=/session/state", "PATH=/session/bin",
			hook.HeldEnv + "=1", hook.PIDEnv + "=wrong", "FAKE_HOOK_MODE=" + mode,
		}},
		waiters: map[string]chan json.RawMessage{},
	}, out
}

func readHookReply(t *testing.T, out hookOutput, requestID string) map[string]json.RawMessage {
	t.Helper()
	select {
	case raw := <-out:
		var reply struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string                     `json:"subtype"`
				RequestID string                     `json:"request_id"`
				Body      map[string]json.RawMessage `json:"response"`
			} `json:"response"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil || reply.Type != "control_response" ||
			reply.Response.Subtype != "success" || reply.Response.RequestID != requestID || reply.Response.Body == nil {
			t.Fatalf("the callback reply lost its request or body: %s (%v)", raw, err)
		}
		return reply.Response.Body
	case <-time.After(2 * time.Second):
		t.Fatal("the hook held the turn without an answer")
		return nil
	}
}

func TestHookCallbacksRunWithTheSessionEnvironment(t *testing.T) {
	for _, name := range []string{"prompt-stamp", "checklist-reminder", "background-reminder"} {
		t.Run(name, func(t *testing.T) {
			h, out := hookHolder(t, "")
			payload := fmt.Sprintf(`{"hook_event_name":%q,"session_id":"conversation","prompt":"hello"}`, hook.HeldEvent(name))
			raw := fmt.Sprintf(`{"type":"control_request","request_id":%q,"request":{"subtype":"hook_callback","callback_id":%q,"input":%s}}`+"\n", name, name, payload)
			h.read(strings.NewReader(raw))
			reply := readHookReply(t, out, name)
			var args []string
			if err := json.Unmarshal(reply["args"], &args); err != nil || !reflect.DeepEqual(args, []string{"hook", name, hook.HeldEvent(name)}) {
				t.Errorf("the hook arguments: %s (%v)", reply["args"], err)
			}
			for key, want := range map[string]string{
				"pid": strconv.Itoa(os.Getpid()), "held": "1", "config": "/session/contour",
				"state": "/session/state", "path": "/session/bin",
			} {
				var got string
				if err := json.Unmarshal(reply[key], &got); err != nil || got != want {
					t.Errorf("the hook's %s is %q, want %q (%v)", key, got, want, err)
				}
			}
			var got, want map[string]any
			_ = json.Unmarshal(reply["input"], &got)
			_ = json.Unmarshal([]byte(payload), &want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the payload changed: %s", reply["input"])
			}
		})
	}
}

func TestFailedHooksAnswerEmptyAndKeepReading(t *testing.T) {
	was := hookGrace
	hookGrace = 80 * time.Millisecond
	t.Cleanup(func() { hookGrace = was })
	for _, mode := range []string{"empty", "error", "timeout", "invalid", "null", "missing"} {
		t.Run(mode, func(t *testing.T) {
			h, out := hookHolder(t, mode)
			if mode == "missing" {
				h.hookPath = filepath.Join(t.TempDir(), "missing")
			}
			read := make(chan json.RawMessage, 1)
			h.waiters["reading"] = read
			done := make(chan struct{})
			go func() {
				h.read(strings.NewReader(`{"type":"control_request","request_id":"callback","request":{"subtype":"hook_callback","callback_id":"prompt-stamp","input":{"hook_event_name":"UserPromptSubmit"}}}` + "\n" +
					`{"type":"control_response","response":{"request_id":"reading"}}` + "\n"))
				close(done)
			}()
			select {
			case <-read:
			case <-time.After(40 * time.Millisecond):
				t.Error("the hook blocked the stream reader")
			}
			if reply := readHookReply(t, out, "callback"); len(reply) != 0 {
				t.Errorf("a failed hook answered %v", reply)
			}
			<-done
		})
	}
}

func TestOnlyRegisteredHookEventsRun(t *testing.T) {
	for _, c := range []struct{ name, event string }{
		{"ask-hook", "PreToolUse"}, {"artifact-copy", "PostToolUse"},
		{"prompt-stamp", "Stop"}, {"unknown", "Stop"},
	} {
		h, out := hookHolder(t, "")
		h.onHook("unregistered", c.name, json.RawMessage(fmt.Sprintf(`{"hook_event_name":%q}`, c.event)))
		if reply := readHookReply(t, out, "unregistered"); len(reply) != 0 {
			t.Errorf("an unregistered hook ran: %v", reply)
		}
	}
}

func TestTheHolderOpensTheInstalledHookAgainAfterReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executor")
	put := func(word string) {
		t.Helper()
		tmp := path + ".next"
		if err := os.WriteFile(tmp, []byte("#!/bin/sh\nprintf '%s' '{\"word\":\""+word+"\"}'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatal(err)
		}
	}
	put("first")
	was := hookExecutable
	hookExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { hookExecutable = was })
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	for _, word := range []string{"first", "replacement"} {
		put(word)
		r.ask(Request{Op: OpSend, Text: "held-hook"})
		r.eventually(`"word":"` + word + `"`)
	}
}
