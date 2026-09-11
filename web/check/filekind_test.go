package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

func TestExecutableIsShownByNameOnly(t *testing.T) {
	cases := []struct {
		name  string
		state map[string]any
		file  string
		want  string
	}{
		{
			"the host called it an executable",
			map[string]any{"form": "exec", "size": 4096, "mode": "-rwxr-xr-x"},
			"aacpanel-exec", "exec",
		},
		{
			"an executable with a picture name is not shown as a picture",
			map[string]any{"form": "exec", "size": 4096, "mode": "-rwxr-xr-x"},
			"logo.png", "exec",
		},
		{
			"a script with an interpreter line is shown as code",
			map[string]any{"form": "text", "text": "#!/usr/bin/env bash\nrm -rf /\n"},
			"deploy.sh", "code",
		},
		{
			"the same script with the x bit is not shown",
			map[string]any{"form": "exec", "size": 31, "mode": "-rwxr-xr-x"},
			"deploy.sh", "exec",
		},
		{
			"an executable with a page name is not rendered as a page",
			map[string]any{"form": "exec", "size": 4096, "mode": "-rwxr-xr-x"},
			"install.html", "exec",
		},
		{
			"a hash at the start of the file is not an executable yet",
			map[string]any{"form": "text", "text": "# heading\n"},
			"notes.md", "doc",
		},
	}
	steps := make([]map[string]any, 0, len(cases)*2)
	for _, c := range cases {
		steps = append(steps, map[string]any{"op": "view", "state": c.state, "name": c.file})
		steps = append(steps, map[string]any{"op": "copy", "state": c.state})
	}
	got := runFileKindJS(t, steps)
	for i, c := range cases {
		if view, _ := got[i*2].(string); view != c.want {
			t.Errorf("%s: view %q, expected %q", c.name, view, c.want)
		}
		offered, _ := got[i*2+1].(bool)
		if c.want == "exec" && offered {
			t.Errorf("%s: copying the contents is offered for an executable", c.name)
		}
		if c.want != "exec" && !offered && c.state["text"] != nil {
			t.Errorf("%s: copying is taken away from a document", c.name)
		}
	}
}

func TestViewIsChosenByKindNotByName(t *testing.T) {
	cases := []struct {
		name  string
		state map[string]any
		file  string
		want  string
	}{
		{"picture", map[string]any{"form": "image", "data": "AAA", "media": "image/webp"}, "shot.webp", "image"},
		{"video", map[string]any{"form": "video", "data": "AAA", "media": "video/mp4"}, "clip.mp4", "video"},
		{"sound", map[string]any{"form": "audio", "data": "AAA", "media": "audio/mpeg"}, "note.mp3", "audio"},
		{"pdf", map[string]any{"form": "pdf", "data": "AAA", "media": "application/pdf"}, "plan.pdf", "pdf"},
		{"binary", map[string]any{"form": "binary", "binary": true}, "core.dump", "binary"},
		{"over the size ceiling", map[string]any{"form": "video", "tooBig": true, "size": 90000000}, "film.mp4", "toobig"},
		{"page", map[string]any{"form": "text", "text": "<b>hi</b>"}, "page.html", "page"},
		{"document", map[string]any{"form": "text", "text": "# hi"}, "README.md", "doc"},
		{"sheet", map[string]any{"form": "text", "text": "a,b"}, "rows.csv", "sheet"},
		{"code", map[string]any{"form": "text", "text": "package main"}, "main.go", "code"},
		{
			"an older collector returned bytes with no form",
			map[string]any{"data": "AAA", "media": "image/png"}, "shot.png", "image",
		},
	}
	steps := make([]map[string]any, 0, len(cases))
	for _, c := range cases {
		steps = append(steps, map[string]any{"op": "view", "state": c.state, "name": c.file})
	}
	got := runFileKindJS(t, steps)
	for i, c := range cases {
		if view, _ := got[i].(string); view != c.want {
			t.Errorf("%s: view %q, expected %q", c.name, view, c.want)
		}
	}
}

func TestFramedPageCannotReachTheNetwork(t *testing.T) {
	page := "<html><body><img src=\"https://tracker.example/pixel.png\"></body></html>"
	got := runFileKindJS(t, []map[string]any{{"op": "frame", "text": page}})
	doc, _ := got[0].(string)

	if !strings.HasPrefix(strings.ToLower(doc), "<!doctype html>") {
		t.Errorf("the document does not start with a doctype — the page will render in quirks mode: %.60s", doc)
	}
	head := doc[:len(doc)-len(page)]
	if !strings.Contains(head, "Content-Security-Policy") {
		t.Fatalf("the document carries no policy: the frame will fetch a resource from a foreign server\n%s", head)
	}
	for _, must := range []string{"default-src 'none'", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(head, must) {
			t.Errorf("the policy has no %q:\n%s", must, head)
		}
	}
	for _, never := range []string{"*", "http:", "https:", "'self'", "'unsafe-eval'"} {
		if strings.Contains(head, never) {
			t.Errorf("the policy lets %q through — the frame again has somewhere to go:\n%s", never, head)
		}
	}
	if !strings.HasSuffix(doc, page) {
		t.Error("the file inside the document is changed: the panel does not rewrite foreign markup, " +
			"safety does not rest on that")
	}
}

var iframeTag = regexp.MustCompile(`(?s)<iframe\b[^>]*>`)

func TestForeignMarkupOnlyInsideTheSandbox(t *testing.T) {
	forbidden := []string{"allow-scripts", "allow-same-origin", "allow-top-navigation",
		"allow-popups", "allow-modals"}

	frames := 0
	for path, body := range srcFiles(t) {
		code := stripComments(body)
		for _, bad := range forbidden {
			if strings.Contains(code, bad) {
				t.Errorf("%s contains %q: the sandbox is handed back the very thing "+
					"it stands there to withhold", path, bad)
			}
		}
		for _, tag := range iframeTag.FindAllString(code, -1) {
			if !strings.Contains(tag, "srcdoc") {
				continue
			}
			frames++
			if !strings.Contains(tag, `sandbox=""`) {
				t.Errorf("%s: foreign markup goes into a frame without an empty sandbox — "+
					"that is foreign code running with the rights of whoever opened the panel:\n%s", path, tag)
			}
		}
	}
	if frames == 0 {
		t.Fatal("no frame with srcdoc found in the frontend — the test is useless, check the path")
	}
}

func TestSheetKeepsQuotedFieldsWhole(t *testing.T) {
	steps := []map[string]any{
		{"op": "sheet", "text": "name,n\n\"Doe, John\",42\n", "name": "rows.csv"},
		{"op": "sheet", "text": "a\tb\n1\t2\n", "name": "rows.tsv"},
		{"op": "sheet", "text": "q\n\"he said \"\"yes\"\"\"\n", "name": "rows.csv"},
	}
	got := runFileKindJS(t, steps)
	want := [][]any{
		{[]any{"name", "n"}, []any{"Doe, John", "42"}},
		{[]any{"a", "b"}, []any{"1", "2"}},
		{[]any{"q"}, []any{"he said \"yes\""}},
	}
	for i, w := range want {
		raw, err := json.Marshal(got[i])
		if err != nil {
			t.Fatal(err)
		}
		expect, err := json.Marshal(w)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != string(expect) {
			t.Errorf("sheet %d: got %s, expected %s", i, raw, expect)
		}
	}
}

func runFileKindJS(t *testing.T, steps []map[string]any) []any {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the viewer decisions are run through the engine, not read out of the source")
	}
	kinds := bundleFront(t, filepath.Join(webDir, "src", "screens", "chat", "kinds.js"))
	clip := bundleFront(t, filepath.Join(webDir, "src", "screens", "chat", "copy.js"))

	script := `
import { readFileSync } from "node:fs";
import { frameDoc, pickView, sheet } from ` + jsString("file://"+kinds) + `;
import { filePick } from ` + jsString("file://"+clip) + `;
const steps = JSON.parse(readFileSync(0, "utf8"));
const out = steps.map((step) => {
    if (step.op === "view") return pickView(step.state, step.name);
    if (step.op === "copy") return Boolean(filePick(step.state));
    if (step.op === "frame") return frameDoc(step.text);
    if (step.op === "sheet") return sheet(step.text, step.name);
    throw new Error("unknown step: " + step.op);
});
process.stdout.write(JSON.stringify(out));
`
	raw, err := json.Marshal(steps)
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
	var got []any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node reply did not parse: %v: %s", err, out)
	}
	if len(got) != len(steps) {
		t.Fatalf("%d replies for %d steps", len(got), len(steps))
	}
	return got
}

func bundleFront(t *testing.T, rel string) string {
	t.Helper()

	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(rel)
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
		t.Fatalf("%s did not build: %v", rel, built.Errors[0].Text)
	}
	out := filepath.Join(t.TempDir(), filepath.Base(rel)+".mjs")
	if err := os.WriteFile(out, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return out
}
