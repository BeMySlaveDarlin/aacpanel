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

func TestViewerCopyTellsWhatItCopied(t *testing.T) {
	type pick struct {
		Text  string `json:"text"`
		Title string `json:"title"`
		Sub   string `json:"sub"`
	}
	cases := []struct {
		name  string
		state map[string]any
		want  *pick
	}{
		{
			"a whole file: the name in the caption, the text as it came",
			map[string]any{"name": "plan.md", "text": "# Plan\n- step", "size": 20, "next": 0},
			&pick{Text: "# Plan\n- step", Title: "Copied", Sub: "plan.md"},
		},
		{
			"code travels with its own tabs and quotes",
			map[string]any{
				"name": "main.go",
				"text": "func main() {\n\tfmt.Println(\"hello\")\n}",
				"size": 42, "next": 0,
			},
			&pick{
				Text:  "func main() {\n\tfmt.Println(\"hello\")\n}",
				Title: "Copied", Sub: "main.go",
			},
		},
		{
			"a chunk of a long file is called a chunk",
			map[string]any{"name": "agent.log", "text": "first line", "size": 307200, "next": 65536},
			&pick{Text: "first line", Title: "Copied the shown chunk", Sub: "64 KB of 300 KB"},
		},
		{
			"a picture: nothing to copy, no button",
			map[string]any{"name": "shot.png", "kind": "image", "form": "image",
				"data": "iVBORw0KGgo=", "media": "image/png", "size": 4096},
			nil,
		},
		{
			"binary: nothing to copy, no button",
			map[string]any{"name": "aacpanel", "form": "binary", "binary": true, "text": "", "size": 9000},
			nil,
		},
		{
			"an empty file: the button does not promise an empty clipboard",
			map[string]any{"name": "empty.txt", "text": "", "size": 0, "next": 0},
			nil,
		},
	}

	states := make([]map[string]any, 0, len(cases))
	for _, c := range cases {
		states = append(states, c.state)
	}

	var got []*pick
	if err := json.Unmarshal(runFilePickJS(t, states), &got); err != nil {
		t.Fatalf("the node output did not parse: %v", err)
	}
	if len(got) != len(cases) {
		t.Fatalf("%d answers to %d states", len(got), len(cases))
	}
	for i, c := range cases {
		switch {
		case c.want == nil && got[i] != nil:
			t.Errorf("%s: copying %q (%s) was offered — and there is nothing to copy",
				c.name, got[i].Text, got[i].Title)
		case c.want != nil && got[i] == nil:
			t.Errorf("%s: there is nothing to copy while the content is shown — the button disappears from the screen", c.name)
		case c.want != nil && *got[i] != *c.want:
			t.Errorf("%s:\n  got  %+v\n  want %+v", c.name, *got[i], *c.want)
		}
	}
}

func TestFileViewerCarriesCopyButton(t *testing.T) {
	const lookFile = "src/screens/chat/look.js"
	files := srcFiles(t)
	src, ok := files[lookFile]
	if !ok {
		t.Fatalf("%s not found — the test looks in the wrong place", lookFile)
	}

	if !strings.Contains(src, `class="mdcopy filecopy"`) {
		t.Errorf("%s: the viewer has no copy button — there is again no way to get the file "+
			"content off the screen", lookFile)
	}
	if !strings.Contains(src, "copy.fileCopy(state, toast)") {
		t.Errorf("%s: the copy button calls nothing — drawn and dead", lookFile)
	}
	if !strings.Contains(src, "copy.filePick(state)") {
		t.Errorf("%s: whether the button shows is not decided by `filePick` — the condition diverges from "+
			"what goes into the clipboard, and the button ends up over a picture", lookFile)
	}

	if !strings.Contains(src, "tools=${tools}") {
		t.Errorf("%s: the header of the layer page has no button — a file from the work list "+
			"is left without copying", lookFile)
	}
	if !strings.Contains(src, `class="sheethead">${who}${tools}`) {
		t.Errorf("%s: the sheet header has no button — yet that is how a file opens from the feed, "+
			"and that is the one that was asked about", lookFile)
	}
	if at, body := strings.Index(src, `class="mdcopy filecopy"`), strings.Index(src, `class="callbody"`); at > body {
		t.Errorf("%s: the copy button sits in the body of the viewer instead of the header — "+
			"the body scrolls, and the button leaves together with the content", lookFile)
	}

	if !strings.Contains(src, "copy.fromClick(event, toast)") {
		t.Errorf("%s: the viewer body does not call block copying — the buttons on code "+
			"and tables inside the document are drawn and do nothing", lookFile)
	}

	block := cssBlockFile(t, "src/css/calls.css", ".mdcopy.filecopy")
	if block == "" {
		t.Fatal("src/css/calls.css has no `.mdcopy.filecopy` rule — the button in the header stays " +
			"the size of the strip above code, three times smaller than the close cross next to it")
	}
	if !strings.Contains(block, "margin-left: auto") {
		t.Error("`.mdcopy.filecopy` without margin-left: auto — the button sticks to the title " +
			"instead of the right edge of the header")
	}
}

func runFilePickJS(t *testing.T, states []map[string]any) []byte {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: deciding what can be copied is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "copy.js"))
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
		t.Fatalf("copy.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "copy.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { filePick } from ` + jsString("file://"+bundle) + `;
const states = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(states.map((state) => filePick(state) || null)));
`
	raw, err := json.Marshal(states)
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
	return out
}
