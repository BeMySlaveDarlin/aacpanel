package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

func TestClipboardImageGetsItsOwnName(t *testing.T) {
	const at = "2026-09-08T13:42:10.317+00:00"
	cases := []struct {
		name, file, kind, want string
	}{
		{"a screenshot from the clipboard", "image.png", "image/png", "paste-20260908-134210-317.png"},
		{"a picture copied from a page", "image.jpeg", "image/jpeg", "paste-20260908-134210-317.jpeg"},
		{"no name at all", "", "image/webp", "paste-20260908-134210-317.webp"},
		{"a file from the file manager", "README.md", "text/markdown", ""},
		{"a screenshot from the phone", "IMG_20260908_134210.jpg", "image/jpeg", ""},
		{"someone else's file with a similar name", "image-2.png", "image/png", ""},
	}

	var calls []map[string]string
	for _, c := range cases {
		calls = append(calls, map[string]string{"name": c.file, "type": c.kind, "at": at})
	}
	got := runClipNameJS(t, calls)
	for i, c := range cases {
		if got[i] != c.want {
			what := c.want
			if what == "" {
				what = "its own file name (empty)"
			}
			t.Errorf("%s: clipName(%q) = %q, expected %q", c.name, c.file, got[i], what)
		}
	}
}

func runClipNameJS(t *testing.T, calls []map[string]string) []string {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the clipboard name is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "tools.js"))
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
		t.Fatalf("tools.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "tools.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { clipName } from ` + jsString("file://"+bundle) + `;
const calls = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(calls.map((c) =>
    clipName({ name: c.name, type: c.type }, new Date(c.at)) || "")));
`
	raw, err := json.Marshal(calls)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	cmd.Env = append(os.Environ(), "TZ=UTC")
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(calls) {
		t.Fatalf("%d answers to %d calls", len(got), len(calls))
	}
	return got
}
