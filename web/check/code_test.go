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

func TestHighlightPaintsOnlyWhatItKnows(t *testing.T) {
	cases := []struct {
		name, file, in, want string
	}{
		{
			"a comment, a string and a keyword in go",
			"main.go",
			"// note\nreturn \"text\"",
			`<span class="cdcm">// note</span>` + "\n" +
				`<span class="cdkw">return</span> <span class="cdstr">&quot;text&quot;</span>`,
		},
		{
			"a keyword inside a string is not painted",
			"main.go",
			`x := "return nil"`,
			`x := <span class="cdstr">&quot;return nil&quot;</span>`,
		},
		{
			"a hash comments out python but not js",
			"a.py",
			"# no\nx = 1",
			`<span class="cdcm"># no</span>` + "\n" + `x = <span class="cdnum">1</span>`,
		},
		{
			"in js a hash is ordinary text",
			"a.js",
			"const x = 1; # no",
			`<span class="cdkw">const</span> x = <span class="cdnum">1</span>; # no`,
		},
		{
			"a yaml key keeps the structure",
			"a.yml",
			"name: aacpanel\n# tail",
			`<span class="cdkw">name</span>: aacpanel` + "\n" + `<span class="cdcm"># tail</span>`,
		},
		{
			"an unknown extension stays plain text",
			"dump.bin7",
			"return \"text\"",
			`return &quot;text&quot;`,
		},
		{
			"markup from a file stays text",
			"page.html",
			"<script>alert(1)</script>",
			`<span class="cdkw">&lt;script</span>&gt;alert(<span class="cdnum">1</span>)` +
				`<span class="cdkw">&lt;/script</span>&gt;`,
		},
	}

	in := make([][2]string, 0, len(cases))
	for _, c := range cases {
		in = append(in, [2]string{c.in, c.file})
	}
	got := runHighlightJS(t, in)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s:\n  in    %q (%s)\n  out   %s\n  want  %s", c.name, c.in, c.file, got[i], c.want)
		}
	}
}

func TestHighlightSkipsHugeFiles(t *testing.T) {
	big := bytes.Repeat([]byte("return 1\n"), 60000)
	got := runHighlightJS(t, [][2]string{{string(big), "big.go"}})
	if bytes.Contains([]byte(got[0]), []byte("cdkw")) {
		t.Error("a file past the ceiling is painted: the parse runs on every chunk load")
	}
}

func runHighlightJS(t *testing.T, pairs [][2]string) []string {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: highlighting is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "code.js"))
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
		t.Fatalf("code.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "code.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { highlight } from ` + jsString("file://"+bundle) + `;
const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
function toHTML(node) {
    if (node == null || typeof node === "boolean") return "";
    if (typeof node !== "object") return esc(node);
    if (Array.isArray(node)) return node.map(toHTML).join("");
    const { type, props } = node;
    const kids = toHTML(props && props.children);
    if (typeof type !== "string") return kids;
    const attrs = Object.entries(props)
        .filter(([k, v]) => k !== "children" && v != null && v !== false && typeof v !== "function")
        .map(([k, v]) => " " + k + '="' + esc(v) + '"')
        .join("");
    return "<" + type + attrs + ">" + kids + "</" + type + ">";
}
const pairs = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(pairs.map(([text, name]) => toHTML(highlight(text, name)))));
`
	raw, err := json.Marshal(pairs)
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
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(pairs) {
		t.Fatalf("%d answers to %d texts", len(got), len(pairs))
	}
	return got
}
