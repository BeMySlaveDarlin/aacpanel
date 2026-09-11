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

func TestMarkdownListSurvivesBlankLinesBetweenItems(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"items through a blank line — one list",
			"## What is missing\n1. **A live session.** There is no profile.\n\n2. **Permissions.** This one is live.\n\n3. **Node 22.** Check it.",
			`<div class="mdh mdh2">What is missing</div>` + listBlock(
				"1. **A live session.** There is no profile.\n2. **Permissions.** This one is live.\n3. **Node 22.** Check it.",
				`<ol class="mdlist" start="1"><li><b>A live session.</b> There is no profile.</li>`+
					`<li><b>Permissions.</b> This one is live.</li><li><b>Node 22.</b> Check it.</li></ol>`),
		},
		{
			"a tight list — the same",
			"1. a\n2. b\n3. c",
			listBlock("1. a\n2. b\n3. c", `<ol class="mdlist" start="1"><li>a</li><li>b</li><li>c</li></ol>`),
		},
		{
			"several blank lines in a row — one list",
			"1. a\n\n\n\n2. b\n\n",
			listBlock("1. a\n2. b", `<ol class="mdlist" start="1"><li>a</li><li>b</li></ol>`),
		},
		{
			"a list that does not start at one gets start",
			"3. c\n4. d",
			listBlock("3. c\n4. d", `<ol class="mdlist" start="3"><li>c</li><li>d</li></ol>`),
		},
		{
			"a bulleted list through a blank line — one list",
			"- a\n\n- b",
			listBlock("- a\n- b", `<ul class="mdlist"><li>a</li><li>b</li></ul>`),
		},
		{
			"a paragraph between items — two lists, the numbering continues",
			"1. a\n\na paragraph between\n\n2. b",
			listBlock("1. a", `<ol class="mdlist" start="1"><li>a</li></ol>`) + `<p>a paragraph between</p>` +
				listBlock("2. b", `<ol class="mdlist" start="2"><li>b</li></ol>`),
		},
		{
			"bullets under a numbered item — a list of their own, the numbering continues",
			"1. Step\n   - detail\n   - detail\n2. Step",
			listBlock("1. Step", `<ol class="mdlist" start="1"><li>Step</li></ol>`) +
				listBlock("   - detail\n   - detail", `<ul class="mdlist"><li>detail</li><li>detail</li></ul>`) +
				listBlock("2. Step", `<ol class="mdlist" start="2"><li>Step</li></ol>`),
		},
		{
			"a list of another kind through a blank line — not the same list",
			"1. a\n\n- b",
			listBlock("1. a", `<ol class="mdlist" start="1"><li>a</li></ol>`) +
				listBlock("- b", `<ul class="mdlist"><li>b</li></ul>`),
		},
	}
	texts := make([]string, 0, len(cases))
	for _, c := range cases {
		texts = append(texts, c.in)
	}
	got := runMarkdownJS(t, texts)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s:\n  input %q\n  got   %s\n  want  %s", c.name, c.in, got[i], c.want)
		}
	}
}

func TestCodeBlockCarriesCopyButton(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"a block with a language: bar, button, code",
			"```go\nfmt.Println(1)\n```",
			`<div class="mdcode">` + bar("go") + `<pre><code>fmt.Println(1)</code></pre></div>`,
		},
		{
			"a block with no language: bar and button all the same",
			"```\ndocker compose up -d\n```",
			`<div class="mdcode">` + bar("") + `<pre><code>docker compose up -d</code></pre></div>`,
		},
		{
			"an unclosed block runs to the end and carries a button too",
			"```sh\ncd /srv",
			`<div class="mdcode">` + bar("sh") + `<pre><code>cd /srv</code></pre></div>`,
		},
	}

	texts := make([]string, 0, len(cases))
	for _, c := range cases {
		texts = append(texts, c.in)
	}
	got := runMarkdownJS(t, texts)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s:\n  input %q\n  got   %s\n  want  %s", c.name, c.in, got[i], c.want)
		}
	}
}

func TestTableAndListCopyTheirMarkdown(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"table: data-md keeps the header, the separator and the rows",
			"| # | what |\n|---|---|\n| 374 | the map |",
			tableBlock("| # | what |\n|---|---|\n| 374 | the map |",
				`<div class="mdtable"><table><thead><tr><th>#</th><th>what</th></tr></thead>`+
					`<tbody><tr><td>374</td><td>the map</td></tr></tbody></table></div>`),
		},
		{
			"list: the bullets stay in place in data-md",
			"- bring up libvirtd\n- unpack the image",
			listBlock("- bring up libvirtd\n- unpack the image",
				`<ul class="mdlist"><li>bring up libvirtd</li><li>unpack the image</li></ul>`),
		},
		{
			"a numbered list is copied with its own numbers",
			"3. third\n4. fourth",
			listBlock("3. third\n4. fourth",
				`<ol class="mdlist" start="3"><li>third</li><li>fourth</li></ol>`),
		},
	}

	texts := make([]string, 0, len(cases))
	for _, c := range cases {
		texts = append(texts, c.in)
	}
	got := runMarkdownJS(t, texts)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s:\n  input %q\n  got   %s\n  want  %s", c.name, c.in, got[i], c.want)
		}
	}
}

func bar(lang string) string {
	return `<div class="mdbar"><span class="mdlang">` + lang + `</span>` +
		copyBtn("", "Copy the code") + `</div>`
}

func listBlock(md, inner string) string {
	return `<div class="mdblock"><div class="mdtools">` +
		copyBtn(md, "Copy the list") + `</div>` + inner + `</div>`
}

func tableBlock(md, inner string) string {
	return `<div class="mdblock"><div class="mdtools">` +
		copyBtn(md, "Copy the table") + `</div>` + inner + `</div>`
}

func copyBtn(md, label string) string {
	attr := ""
	if md != "" {
		attr = ` data-md="` + md + `"`
	}
	return `<button type="button" class="mdcopy"` + attr + ` title="` + label + `" ` +
		`aria-label="` + label + `"><svg fill="none" stroke="currentColor" stroke-width="1.7" ` +
		`stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">` +
		`<rect x="9" y="9" width="11" height="11" rx="2"></rect>` +
		`<path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"></path></svg></button>`
}

func runMarkdownJS(t *testing.T, texts []string) []string {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: list parsing is run through the engine, not read out of the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "md.js"))
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
		t.Fatalf("md.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "md.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { render } from ` + jsString("file://"+bundle) + `;
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
const texts = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(texts.map((text) => toHTML(render(text)).replace(/>\s+</g, "><").trim())));
`
	raw, err := json.Marshal(texts)
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
		t.Fatalf("the node reply did not parse: %v: %s", err, out)
	}
	if len(got) != len(texts) {
		t.Fatalf("%d replies for %d texts", len(got), len(texts))
	}
	return got
}
