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

type focusCase struct {
	Wide   bool   `json:"wide"`
	Holder string `json:"holder"`
	Field  bool   `json:"field"`
}

type focusGot struct {
	Said bool `json:"said"`
	Took bool `json:"took"`
}

func TestFocusReturnsOnlyWhenNobodyHoldsIt(t *testing.T) {
	cases := []struct {
		name string
		in   focusCase
		want bool
	}{
		{"focus landed on the page body", focusCase{Wide: true, Holder: "body", Field: true}, true},
		{"there is no focus at all", focusCase{Wide: true, Holder: "none", Field: true}, true},
		{"focus is on the document root", focusCase{Wide: true, Holder: "root", Field: true}, true},
		{"the person moved into another field", focusCase{Wide: true, Holder: "input", Field: true}, false},
		{"the person pressed a button", focusCase{Wide: true, Holder: "button", Field: true}, false},
		{"the person is editing markup", focusCase{Wide: true, Holder: "editable", Field: true}, false},
		{"the field already has the focus", focusCase{Wide: true, Holder: "self", Field: true}, false},
		{"phone: the focus is not returned", focusCase{Wide: false, Holder: "body", Field: true}, false},
		{"the field is gone", focusCase{Wide: true, Holder: "body", Field: false}, false},
	}
	in := make([]focusCase, 0, len(cases))
	for _, c := range cases {
		in = append(in, c.in)
	}
	got := runRegainJS(t, in)
	for i, c := range cases {
		if got[i].Said != c.want {
			t.Errorf("%s: regain answered %v, and it should have been %s", c.name, got[i].Said, focusWanted(c.want))
		}
		if got[i].Took != c.want {
			t.Errorf("%s: the focus %s, and it should have been %s", c.name,
				focusTook(got[i].Took), focusWanted(c.want))
		}
	}
}

func focusWanted(take bool) string {
	if take {
		return "to return the focus to the field"
	}
	return "to leave the focus where it is"
}

func focusTook(took bool) string {
	if took {
		return "moved into the field"
	}
	return "stayed where it was"
}

func TestComposerReturnsFocusAfterSending(t *testing.T) {
	const composerFile = "src/screens/chat/composer.js"
	body := screenSrc(t, composerFile)
	if !strings.Contains(body, `from "../../ui/focus.js"`) || !strings.Contains(body, "regain") {
		t.Fatal("the composer does not take the focus return from the shared place: a rule of its own " +
			"diverges from the shared one silently, and the person on a phone is the one who notices")
	}

	src := stripComments(body)
	at := strings.Index(src, "const wasSending")
	if at < 0 {
		t.Fatal("the composer does not remember that a send was in flight — it has nowhere to take " +
			"the transition \"the field went dead and is alive again\" from")
	}
	effect := src[at:]
	end := strings.Index(effect, "}, [sending]);")
	if end < 0 {
		t.Fatal("the focus return is not wired to the sending state: it fires not when the field " +
			"comes back but when the conversation re-renders")
	}
	effect = effect[:end]
	for _, want := range []struct{ call, why string }{
		{"wasSending.current", "the effect looks at the state, not at the transition: the focus jumps into the field " +
			"on the very first render of a conversation opened just to read"},
		{"!sending", "the effect does not wait for the send to finish: the focus returns into a dead field, " +
			"that is, nowhere"},
		{"regain(", "the effect returns nothing — the cursor stays on the page body"},
	} {
		if !strings.Contains(effect, want.call) {
			t.Errorf("the focus return has no %s: %s", want.call, want.why)
		}
	}
}

func runRegainJS(t *testing.T, cases []focusCase) []focusGot {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the focus return is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "ui", "focus.js"))
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
		t.Fatalf("focus.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "focus.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { regain } from ` + jsString("file://"+bundle) + `;
const out = JSON.parse(readFileSync(0, "utf8")).map((c) => {
    const body = { tagName: "BODY" };
    const root = { tagName: "HTML" };
    let took = false;
    const field = { tagName: "TEXTAREA", focus() { took = true; } };
    const holders = {
        body,
        root,
        none: null,
        self: field,
        input: { tagName: "INPUT" },
        button: { tagName: "BUTTON" },
        editable: { tagName: "DIV", isContentEditable: true },
    };
    globalThis.document = { body, documentElement: root, activeElement: holders[c.holder] };
    globalThis.window = { matchMedia: () => ({ matches: c.wide }) };
    const said = regain(c.field ? field : null) === true;
    return { said, took };
});
process.stdout.write(JSON.stringify(out));
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
	var got []focusGot
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(cases) {
		t.Fatalf("%d answers to %d pages", len(got), len(cases))
	}
	return got
}
