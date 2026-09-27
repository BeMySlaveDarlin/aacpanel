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

func TestMarkdownListSurvivesBlankLinesBetweenItems(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"items through a blank line — one list",
			"## What is missing\n1. **A live session.** There is no profile.\n\n2. **Permissions.** This one is live.\n\n3. **Node 22.** Check it.",
			`<div class="mdh mdh2">What is missing</div>` + plainList(`<ol class="mdlist" start="1"><li><b>A live session.</b> There is no profile.</li>`+
				`<li><b>Permissions.</b> This one is live.</li><li><b>Node 22.</b> Check it.</li></ol>`),
		},
		{
			"a tight list — the same",
			"1. a\n2. b\n3. c",
			plainList(`<ol class="mdlist" start="1"><li>a</li><li>b</li><li>c</li></ol>`),
		},
		{
			"several blank lines in a row — one list",
			"1. a\n\n\n\n2. b\n\n",
			plainList(`<ol class="mdlist" start="1"><li>a</li><li>b</li></ol>`),
		},
		{
			"a list that does not start at one gets start",
			"3. c\n4. d",
			plainList(`<ol class="mdlist" start="3"><li>c</li><li>d</li></ol>`),
		},
		{
			"a bulleted list through a blank line — one list",
			"- a\n\n- b",
			plainList(`<ul class="mdlist"><li>a</li><li>b</li></ul>`),
		},
		{
			"a paragraph between items — two lists, the numbering continues",
			"1. a\n\na paragraph between\n\n2. b",
			plainList(`<ol class="mdlist" start="1"><li>a</li></ol>`) + `<p>a paragraph between</p>` +
				plainList(`<ol class="mdlist" start="2"><li>b</li></ol>`),
		},
		{
			"bullets under a numbered item — a list of their own, the numbering continues",
			"1. Step\n   - detail\n   - detail\n2. Step",
			plainList(`<ol class="mdlist" start="1"><li>Step</li></ol>`) +
				plainList(`<ul class="mdlist"><li>detail</li><li>detail</li></ul>`) +
				plainList(`<ol class="mdlist" start="2"><li>Step</li></ol>`),
		},
		{
			"a list of another kind through a blank line — not the same list",
			"1. a\n\n- b",
			plainList(`<ol class="mdlist" start="1"><li>a</li></ol>`) +
				plainList(`<ul class="mdlist"><li>b</li></ul>`),
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

// A block of code is the code and its copy button, with no bar over it: the
// button lies over the corner (see feedblocks_test.go). The code element is
// drawn by a component, so it is its text that stands in the pre here.
func TestCodeBlockCarriesItsCopyButtonWithoutABar(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"a block with a language: the code and the button",
			"```go\nfmt.Println(1)\nfmt.Println(2)\n```",
			`<div class="mdcode"><pre>fmt.Println(1)` + "\n" + `fmt.Println(2)</pre>` + copyBtn("", "Copy the code") + `</div>`,
		},
		{
			"a block of one line is a command: it says so for the room of its button",
			"```\ndocker compose up -d\n```",
			`<div class="mdcode oneline"><pre>docker compose up -d</pre>` + copyBtn("", "Copy the code") + `</div>`,
		},
		{
			"an unclosed block runs to the end and carries a button too",
			"```sh\ncd /srv",
			`<div class="mdcode oneline"><pre>cd /srv</pre>` + copyBtn("", "Copy the code") + `</div>`,
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

// A table copies its markdown whole; a list is prose, copied by selecting it,
// and carries no button.
func TestATableCopiesItsMarkdownAndAListIsProse(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"table: data-md keeps the header, the separator and the rows",
			"| # | what |\n|---|---|\n| 374 | the map |",
			`<div class="mdtab mdfit"><div class="mdtable"><table><thead><tr><th class="mdnum">#</th><th>what</th></tr></thead>` +
				`<tbody><tr><td class="mdnum">374</td><td>the map</td></tr></tbody></table></div>` +
				copyBtn("| # | what |\n|---|---|\n| 374 | the map |", "Copy the table") + `</div>`,
		},
		{
			"a table of more than three columns keeps its own width",
			"| a | b | c | d |\n|---|---|---|---|\n| w | x | y | z |",
			`<div class="mdtab"><div class="mdtable"><table><thead><tr><th>a</th><th>b</th><th>c</th><th>d</th></tr></thead>` +
				`<tbody><tr><td>w</td><td>x</td><td>y</td><td>z</td></tr></tbody></table></div>` +
				copyBtn("| a | b | c | d |\n|---|---|---|---|\n| w | x | y | z |", "Copy the table") + `</div>`,
		},
		{
			"list: no button, no block around it",
			"- bring up libvirtd\n- unpack the image",
			`<ul class="mdlist"><li>bring up libvirtd</li><li>unpack the image</li></ul>`,
		},
		{
			"a numbered list keeps its own numbers",
			"3. third\n4. fourth",
			`<ol class="mdlist" start="3"><li>third</li><li>fourth</li></ol>`,
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

// A column of numbers is told by what its cells hold, not by its name: every
// cell that says something starts with a number and carries nothing but what
// numbers are written with and short units; a dash is no cell at all. Such a
// column is set right in the code face, and so is one the markdown aligns
// right itself.
func TestANumberColumnIsToldByWhatItHolds(t *testing.T) {
	cases := []struct {
		name  string
		cells []string
		want  bool
	}{
		{"counts", []string{"3261", "**1**"}, true},
		{"a diff of lines and a dash", []string{"—", "`+37 / −3308`"}, true},
		{"durations and sizes with units", []string{"28m 1s", "12 KB", "6.2 ms"}, true},
		{"a unit in another script is a unit too", []string{"345 \u041a\u0411", "7 \u043c\u0441"}, true},
		{"task numbers are names", []string{"#657", "#673"}, false},
		{"a number with words after it is a phrase", []string{"345 dialogs", "12 files"}, false},
		{"one phrase among numbers spoils the column", []string{"12", "3 of 5"}, false},
		{"a column of dashes says nothing", []string{"—", "-"}, false},
	}
	texts := make([]string, 0, len(cases)+1)
	for _, c := range cases {
		md := "| n |\n|---|\n"
		for _, cell := range c.cells {
			md += "| " + cell + " |\n"
		}
		texts = append(texts, md)
	}
	texts = append(texts, "| name | n |\n|---|--:|\n| a | many |")
	got := runMarkdownJS(t, texts)
	for i, c := range cases {
		if numeric := strings.Contains(got[i], `<th class="mdnum">`); numeric != c.want {
			t.Errorf("%s %q: numeric %v, want %v:\n  %s", c.name, c.cells, numeric, c.want, got[i])
		}
	}
	last := got[len(cases)]
	if !strings.Contains(last, `<th class="mdnum">n</th>`) || strings.Contains(last, `<th class="mdnum">name</th>`) {
		t.Errorf("a column the markdown aligns right is not set right, or the one beside it is: %s", last)
	}
}

// A letter a subagent writes arrives two spaces in as a whole, and its tables
// and fences do not start a line; the indent the letter came with is taken
// off before it is read. Its lead is the first paragraph after the headings.
func TestALetterLosesTheIndentItCameWith(t *testing.T) {
	got := runModuleJS(t, "src/md.js", "dedent", [][]any{
		{"  ## Result\n  \n  | a | b |\n  |---|---|\n    nested"},
		{"flush\n  indented"},
		{""},
	})
	want := []string{"## Result\n\n| a | b |\n|---|---|\n  nested", "flush\n  indented", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedent %d: %q, want %q", i, got[i], want[i])
		}
	}
	leads := runModuleJS(t, "src/md.js", "leadOf", [][]any{
		{"## Result\n\nDone, both green.\nNothing skipped.\n\n| a |\n|---|"},
		{"| a |\n|---|\n| 1 |"},
	})
	if leads[0] != "Done, both green.\nNothing skipped." {
		t.Errorf("the lead of a letter under a heading is %q", leads[0])
	}
	if leads[1] != "| a |" {
		t.Errorf("a letter that opens with a table leads with %q", leads[1])
	}
}

func plainList(inner string) string {
	return inner
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
