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
		{"a clip over the size ceiling", map[string]any{"form": "video", "tooBig": true, "size": 90000000}, "film.mp4", "video"},
		{"a sound over the size ceiling", map[string]any{"form": "audio", "tooBig": true, "size": 10000000}, "song.mp3", "audio"},
		{"a pdf over the size ceiling", map[string]any{"form": "pdf", "tooBig": true, "size": 90000000, "media": "application/pdf"}, "atlas.pdf", "pdf"},
		{"a PNG fetched by address", map[string]any{"form": "image", "tooBig": true, "size": 10 << 20, "media": "image/png"}, "shot.png", "image"},
		{"a PNG at the download cap", map[string]any{"form": "image", "tooBig": true, "size": 128 << 20, "media": "image/png"}, "shot.png", "image"},
		{"a PNG just past the download cap", map[string]any{"form": "image", "tooBig": true, "size": (128 << 20) + 1, "media": "image/png"}, "shot.png", "toobig"},
		{"a PNG past the download cap", map[string]any{"form": "image", "tooBig": true, "size": 200 << 20, "media": "image/png"}, "shot.png", "toobig"},
		{"a PDF at the download cap", map[string]any{"form": "pdf", "tooBig": true, "size": 128 << 20, "media": "application/pdf"}, "atlas.pdf", "pdf"},
		{"a PDF past the download cap", map[string]any{"form": "pdf", "tooBig": true, "size": 200 << 20, "media": "application/pdf"}, "atlas.pdf", "toobig"},
		{"a clip past the download cap", map[string]any{"form": "video", "tooBig": true, "size": 200 << 20, "media": "video/mp4"}, "film.mp4", "video"},
		{"a sound past the download cap", map[string]any{"form": "audio", "tooBig": true, "size": 200 << 20, "media": "audio/mpeg"}, "song.mp3", "audio"},
		{"a photo over the size ceiling", map[string]any{"form": "image", "tooBig": true, "size": 9000000, "media": "image/jpeg"}, "photo.jpg", "image"},
		{"a HEIC over the size ceiling", map[string]any{"form": "image", "tooBig": true, "size": 9000000, "media": "image/heic"}, "photo.heic", "toobig"},
		{"an SVG over the size ceiling", map[string]any{"form": "image", "tooBig": true, "size": 9000000, "media": "image/svg+xml"}, "map.svg", "toobig"},
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

func TestHTMLFilePageURLKeepsTheSessionAndPath(t *testing.T) {
	open := "/api/chat/file/download?session=lab&id=thread-1&path=%2Fsrv%2Flab%2Freview.html&inline=1"
	got := runFileKindJS(t, []map[string]any{{"op": "page", "open": open}})
	want := "/api/chat/file/page?session=lab&id=thread-1&path=%2Fsrv%2Flab%2Freview.html"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("page address %v, expected %s", got, want)
	}
}

var iframeTag = regexp.MustCompile(`(?s)<iframe\b[^>]*>`)

// An inline document inherits the surrounding page's origin: any srcdoc
// therefore keeps an empty sandbox. Scripts are allowed only in frames loaded
// from a route that sandboxes the HTTP response itself. Those addressed pages
// get an opaque origin even when their URL is opened outside the viewer.
func TestForeignMarkupOnlyInsideTheSandbox(t *testing.T) {
	// Never, in either frame: each of these hands back the isolation the
	// sandbox stands there to keep.
	never := []string{"allow-same-origin", "allow-top-navigation"}

	addressed, files := 0, 0
	for path, body := range srcFiles(t) {
		code := stripComments(body)
		for _, bad := range never {
			if strings.Contains(code, bad) {
				t.Errorf("%s contains %q: the sandbox is handed back the very thing "+
					"it stands there to withhold", path, bad)
			}
		}
		for _, tag := range iframeTag.FindAllString(code, -1) {
			switch {
			case strings.Contains(tag, "srcdoc"):
				if !strings.Contains(tag, `sandbox=""`) {
					t.Errorf("%s: foreign markup goes into a frame without an empty sandbox — "+
						"that is foreign code running with the rights of whoever opened the panel:\n%s", path, tag)
				}
			case strings.Contains(tag, "allow-scripts"):
				addressed++
				if strings.Contains(tag, "filePageURL(") {
					files++
					if !strings.Contains(tag, `sandbox="allow-scripts allow-downloads"`) || !strings.Contains(tag, `allow="clipboard-write"`) || strings.Contains(tag, "allow-popups") || strings.Contains(tag, "allow-forms") || !strings.Contains(tag, `referrerpolicy="no-referrer"`) {
						t.Errorf("%s: the HTML file frame must allow isolated scripts, downloads and clipboard writing only: %s", path, tag)
					}
				}
				if strings.Contains(tag, "srcdoc") || !strings.Contains(tag, "src=") {
					t.Errorf("%s: scripts are allowed in a frame that is not loaded by address:\n%s", path, tag)
				}
				if !strings.Contains(tag, "pageURL(") && !strings.Contains(tag, "filePageURL(") {
					t.Errorf("%s: scripts are allowed outside the known routes that sandbox their own answer:\n%s", path, tag)
				}
			}
		}
	}
	if addressed == 0 {
		t.Fatal("no frame loading a page by address found — the test is useless, check the path")
	}
	if files == 0 {
		t.Fatal("no HTML file frame loading its sandboxed route found")
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
import { filePageURL, pickView, sheet } from ` + jsString("file://"+kinds) + `;
import { filePick } from ` + jsString("file://"+clip) + `;
const steps = JSON.parse(readFileSync(0, "utf8"));
const out = steps.map((step) => {
    if (step.op === "view") return pickView(step.state, step.name);
    if (step.op === "copy") return Boolean(filePick(step.state));
    if (step.op === "page") return filePageURL(step.open);
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
