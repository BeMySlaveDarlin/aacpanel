package launcher

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/sessionintent"
	"aacpanel/internal/stream"
)

func TestOpeningMessagesCarryTheirOriginOnBothTransports(t *testing.T) {
	text := "Read \"handoff.md\".\nContinue with "
	text += strings.Repeat("\U0001f680", action.IntentMax-len([]rune(text)))
	for _, transport := range []string{TransportTmux, TransportStream} {
		for _, c := range []struct{ name, key, text, from string }{
			{"first", sessionintent.First, text, "session start"},
			{"restart", sessionintent.Restart, text, "session restart"},
			{"note", "", text, "the session's note"},
			{"empty first", sessionintent.First, "", ""},
			{"empty restart", sessionintent.Restart, "", ""},
			{"empty note", "", "", ""},
			{"blank first", sessionintent.First, " \n\t", ""},
		} {
			t.Run(transport+"/"+c.name, func(t *testing.T) {
				proc := fakeProc(t)
				machineClaude(t)
				t.Setenv(terminalAutoEnv, "0")
				var tmuxLog, specPath string
				if transport == TransportTmux {
					tmuxLog = fakeTmuxLauncher(t, proc, "demo", 200, 4242)
				} else {
					specPath = fakeHolder(t, "sleep 1")
					playHolder(t, proc, specPath)
				}
				raw, err := json.Marshal(map[string]string{"transport": transport, "intent": c.text})
				if err != nil {
					t.Fatal(err)
				}
				rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "demo", IntentKey: c.key, Launch: raw})
				if err != nil {
					t.Fatal(err)
				}
				if rep.Intent != c.text {
					t.Errorf("the report changed the opening text: got %q, want %q", rep.Intent, c.text)
				}
				want := c.text
				if strings.TrimSpace(c.text) != "" {
					want = "<panel-message from=\"" + c.from + "\">\n" + c.text + "\n</panel-message>"
				}
				if transport == TransportTmux {
					args := strings.Split(strings.TrimSuffix(string(readFile(t, tmuxLog+".argv")), "\x00"), "\x00")
					if want != "" && !slices.Contains(args, want) {
						t.Errorf("the panel message did not reach tmux as one argument: %q", args)
					}
					if want == "" && strings.Contains(strings.Join(args, " "), "<panel-message") {
						t.Errorf("an empty opening message sent a panel prompt: %q", args)
					}
					return
				}
				var spec stream.Spec
				if err := json.Unmarshal(readFile(t, specPath), &spec); err != nil {
					t.Fatal(err)
				}
				if spec.Intent != want {
					t.Errorf("the holder got %q, want %q", spec.Intent, want)
				}
				if want != "" && (slices.Contains(spec.Argv, want) || slices.Contains(spec.Argv, c.text)) {
					t.Errorf("the stream opening message went into argv: %q", spec.Argv)
				}
			})
		}
	}
}

func TestTmuxKeepsThePanelOpeningMessageInOneArgument(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	sockets, err := os.MkdirTemp("", "aacp-tmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", sockets)
	t.Setenv(tmuxEnv, bin)
	t.Cleanup(func() {
		exec.Command(bin, "kill-server").Run()
		os.RemoveAll(sockets)
	})
	dir := t.TempDir()
	log := filepath.Join(dir, "arguments")
	claude := filepath.Join(dir, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\nprintf '%s\\0' \"$@\" > "+log+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	drop, _, err := writeSessionEnv([]string{"HOME=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	defer drop.remove()
	text := "Read \"handoff.md\".\nContinue with "
	text += strings.Repeat("\U0001f680", action.IntentMax-len([]rune(text)))
	want := "<panel-message from=\"the session's note\">\n" + text + "\n</panel-message>"
	args := claudeArgs("demo", "", Params{Intent: openingMessage("", text)})
	if err := startSession(dir, "demo", claude, args, drop.path); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(waitFile(t, log), "\x00"), "\x00")
	if !slices.Equal(got, []string{"-n", "demo", want}) {
		t.Fatalf("claude received %q, want the intact multiline panel prompt", got)
	}
}
