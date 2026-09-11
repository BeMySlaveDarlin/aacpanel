package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

type keyCase struct {
	Key       string `json:"key"`
	Ctrl      bool   `json:"ctrlKey"`
	Meta      bool   `json:"metaKey"`
	Shift     bool   `json:"shiftKey"`
	Alt       bool   `json:"altKey"`
	Repeat    bool   `json:"repeat"`
	Composing bool   `json:"isComposing"`
}

type askCase struct {
	Event keyCase `json:"e"`
	Wide  bool    `json:"wide"`
}

func TestComposerSendsOnEnterWide(t *testing.T) {
	cases := []struct {
		name string
		in   keyCase
		want bool
	}{
		{"Enter sends", keyCase{Key: "Enter"}, true},
		{"Shift+Enter breaks the line", keyCase{Key: "Enter", Shift: true}, false},
		{"Ctrl+Enter stays the second way", keyCase{Key: "Enter", Ctrl: true}, true},
		{"Cmd+Enter on a mac", keyCase{Key: "Enter", Meta: true}, true},
		{"Alt+Enter is not in the set", keyCase{Key: "Enter", Alt: true}, false},
		{"Ctrl+Shift+Enter", keyCase{Key: "Enter", Ctrl: true, Shift: true}, false},
		{"a held Enter", keyCase{Key: "Enter", Repeat: true}, false},
		{"a held Ctrl+Enter", keyCase{Key: "Enter", Ctrl: true, Repeat: true}, false},
		{"Enter while typing through an IME", keyCase{Key: "Enter", Composing: true}, false},
		{"a letter on its own", keyCase{Key: "a"}, false},
		{"Escape", keyCase{Key: "Escape"}, false},
	}
	asks := make([]askCase, 0, len(cases))
	for _, c := range cases {
		asks = append(asks, askCase{Event: c.in, Wide: true})
	}
	got := runAsksSendJS(t, asks)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("wide layout, %s: asksSend returned %v, while the keystroke was supposed %s",
				c.name, got[i], wanted(c.want))
		}
	}
}

func TestComposerKeepsEnterNarrow(t *testing.T) {
	cases := []struct {
		name string
		in   keyCase
		want bool
	}{
		{"Enter breaks the line", keyCase{Key: "Enter"}, false},
		{"Shift+Enter breaks the line too", keyCase{Key: "Enter", Shift: true}, false},
		{"Ctrl+Enter sends", keyCase{Key: "Enter", Ctrl: true}, true},
		{"Cmd+Enter on a mac", keyCase{Key: "Enter", Meta: true}, true},
		{"Alt+Enter is not ours", keyCase{Key: "Enter", Alt: true}, false},
		{"Ctrl+Shift+Enter", keyCase{Key: "Enter", Ctrl: true, Shift: true}, false},
		{"a held Ctrl+Enter", keyCase{Key: "Enter", Ctrl: true, Repeat: true}, false},
		{"Ctrl with another key", keyCase{Key: "a", Ctrl: true}, false},
		{"a letter on its own", keyCase{Key: "a"}, false},
		{"Escape", keyCase{Key: "Escape", Ctrl: true}, false},
	}
	asks := make([]askCase, 0, len(cases))
	for _, c := range cases {
		asks = append(asks, askCase{Event: c.in, Wide: false})
	}
	got := runAsksSendJS(t, asks)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("narrow layout, %s: asksSend returned %v, while the keystroke was supposed %s",
				c.name, got[i], wanted(c.want))
		}
	}
}

func wanted(send bool) string {
	if send {
		return "to send"
	}
	return "to stay in the field"
}

func TestComposerFieldWiresTheKeys(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)
	composer := jsBlock(t, chatFile, body, "function Composer(")

	if !strings.Contains(composerField(t, composer), "onKeyDown=") {
		t.Fatal("the composer field does not listen to keys — nothing goes out from the keyboard, " +
			"and that is only found out by hand")
	}

	keys := stripComments(composer)
	at := strings.Index(keys, "const keys = ")
	if at < 0 {
		t.Fatal("the composer has no key handler — the test looks in the wrong place")
	}
	keys = keys[at:]
	end := strings.Index(keys, "\n    };")
	if end < 0 {
		t.Fatal("the end of the key handler was not found — the test looks in the wrong place")
	}
	keys = keys[:end]
	for _, want := range []struct{ call, why string }{
		{"asksSend(", "the handler decides about keys itself — that is a second copy of the rule about Enter"},
		{"matchMedia", "nobody asks about the layout — either Enter sends from the phone or it does not send from the monitor"},
		{"send()", "the parsed keystroke leads nowhere — there is no sending from the keyboard"},
		{"cantSend", "the key never asks whether there is anything to send — what the button refuses to send goes out anyway"},
		{"preventDefault", "the browser adds a line break to the field on top of what was sent"},
	} {
		if !strings.Contains(keys, want.call) {
			t.Errorf("the key handler has no %s: %s", want.call, want.why)
		}
	}

	if !strings.Contains(composer, "disabled=${cantSend}") {
		t.Error("the send button dims by a condition of its own instead of the one shared with the key — " +
			"the copies diverge silently")
	}
}

func composerField(t *testing.T, composer string) string {
	t.Helper()
	at := strings.Index(composer, "<textarea")
	if at < 0 {
		t.Fatal("the composer has no input field at all — there is nothing to write into the session with")
	}
	rest := composer[at:]
	end := strings.Index(rest, "></textarea>")
	if end < 0 {
		t.Fatal("the end of the input field was not found — the test looks in the wrong place")
	}
	return rest[:end]
}

func runAsksSendJS(t *testing.T, cases []askCase) []bool {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the key parsing is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "composer.js"))
	if err != nil {
		t.Fatal(err)
	}
	built := esbuild.Build(esbuild.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Format:      esbuild.FormatESModule,
		Platform:    esbuild.PlatformNeutral,
		Alias:       alias,
		Write:       false,
	})
	if len(built.Errors) > 0 {
		t.Fatalf("composer.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "composer.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { asksSend } from ` + jsString("file://"+bundle) + `;
const calls = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(calls.map((c) => asksSend(c.e, c.wide) === true)));
`
	raw, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got []bool
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(cases) {
		t.Fatalf("%d answers to %d keystrokes", len(got), len(cases))
	}
	return got
}
