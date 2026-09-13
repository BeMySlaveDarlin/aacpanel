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
)

// A file the session sent to the human through SendUserFile is a card in the
// feed: the caption, then one row per file with its type, name and size. The
// card is drawn from the receipt the harness writes into the answer, so a
// call that sent nothing has no card — it stays a call in the run.
func TestSentFilesAreACardInTheFeed(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)

	row := jsBlock(t, chatFile, body, "export function Row(")
	if !strings.Contains(row, `item.role === "sent"`) {
		t.Fatalf("%s: the feed does not know the row of sent files — it falls through to a plain "+
			"message and the files are lost", chatFile)
	}
	if !regexp.MustCompile(`item\.role === "sent"\)\s*\{\s*return html` + "`" + `<\$\{SentCard\} item=\$\{item\} onOpen=\$\{onFile\}`).MatchString(row) {
		t.Errorf("%s: the row of sent files opens them with something other than the file handler "+
			"of the feed — a second road to the disk with a boundary of its own", chatFile)
	}

	card := jsBlock(t, chatFile, body, "export function SentCard(")
	if !strings.Contains(card, "<${FileCard}") {
		t.Errorf("%s: the sent card draws a file with a row other than the one an attachment "+
			"named in a reply gets — one and the same thing will look like two", chatFile)
	}
	if !strings.Contains(card, "tag=${fileTag(file)}") {
		t.Errorf("%s: the sent card does not show the type of a file — a PDF and a text file "+
			"under one icon are told apart by nothing", chatFile)
	}
	if !strings.Contains(card, "render(item.text)") {
		t.Errorf("%s: the caption of a delivery is not drawn as a message — the session wrote it "+
			"for the human", chatFile)
	}

	list := regexp.MustCompile(`const HIDDEN = new Set\(\[([^\]]*)\]\)`).FindStringSubmatch(body)
	if list == nil {
		t.Fatal("the feed has no HIDDEN list — the test reads the wrong place")
	}
	if strings.Contains(list[1], `"sent"`) {
		t.Errorf("%s: the row of sent files is hidden from the feed", chatFile)
	}
}

// The chip under the composer counts a sent file among the artifacts, and the
// list behind it shows the files as a group of their own, opened by the same
// file handler as a document.
func TestSentFilesCountOnTheArtifactChip(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := screenSrc(t, chatFile)

	list := jsBlock(t, chatFile, body, "export function WorkList(")
	if !strings.Contains(list, "sent.map(") {
		t.Fatalf("%s: the artifact list does not show the sent files — the chip counts what "+
			"the list does not show", chatFile)
	}
	group := list[strings.Index(list, "sent.map("):]
	if end := strings.Index(group, "`)}"); end > 0 {
		group = group[:end]
	}
	if !strings.Contains(group, `kind: "file"`) || !strings.Contains(group, "path: file.path") {
		t.Errorf("%s: a sent file from the list opens by something other than the file handler — "+
			"then the disk has a second road with a boundary of its own", chatFile)
	}
	if !strings.Contains(group, "fileTag({ name: file.file, media: file.media })") {
		t.Errorf("%s: the list does not say what kind of file was sent, or asks by a name the "+
			"row does not carry — a text file is then tagged by its media type, PLAIN", chatFile)
	}
	if !strings.Contains(list, `<div class="callcap">sent to you</div>`) {
		t.Errorf("%s: the sent files are not a group of their own in the list — they are read "+
			"as documents the session wrote", chatFile)
	}
	if !strings.Contains(list, "The session sent no files.") {
		t.Errorf("%s: an empty group says nothing — a list that skips a group looks as if the "+
			"panel does not know sent files at all", chatFile)
	}
}

// A file that comes with its media type is a picture only when the type says
// so. An attachment named in a reply carries a type only when it is a picture;
// a delivered file carries one always, and the presence alone would draw a
// PDF as a photo.
func TestAFileWithATypeIsAPictureOnlyByItsType(t *testing.T) {
	cases := []struct {
		name string
		file map[string]any
		want bool
	}{
		{"a delivered pdf", map[string]any{"name": "resume.pdf", "media": "application/pdf"}, false},
		{"a delivered text file", map[string]any{"name": "notes.txt", "media": "text/plain"}, false},
		{"a delivered picture", map[string]any{"name": "shot", "media": "image/png"}, true},
		{"a named picture", map[string]any{"name": "shot.png", "media": "image/png"}, true},
		{"a named file with no type, by its name", map[string]any{"name": "shot.jpg"}, true},
		{"a named text file with no type", map[string]any{"name": "plan.md"}, false},
	}
	for _, c := range cases {
		got := runFilesJS(t, "isImage", c.file)
		if got != c.want {
			t.Errorf("%s: isImage is %v, expected %v", c.name, got, c.want)
		}
	}
}

// The type on a file row is the extension the human sees; the media type stands
// in only when the name has none.
func TestTheTagOfAFileIsItsExtension(t *testing.T) {
	cases := []struct {
		name string
		file map[string]any
		want string
	}{
		{"by the extension", map[string]any{"name": "resume.pdf", "media": "application/pdf"}, "PDF"},
		{"the extension wins over the type", map[string]any{"name": "notes.txt", "media": "text/plain"}, "TXT"},
		{"no extension, by the type", map[string]any{"name": "shot", "media": "image/png"}, "PNG"},
		{"nothing to go by", map[string]any{"name": "Makefile"}, "FILE"},
		{"a dotfile is not an extension", map[string]any{"name": ".env"}, "FILE"},
		{"a long tail is not an extension", map[string]any{"name": "a.backup-2026"}, "FILE"},
	}
	for _, c := range cases {
		got := runFilesJS(t, "fileTag", c.file)
		if got != c.want {
			t.Errorf("%s: fileTag is %v, expected %q", c.name, got, c.want)
		}
	}
}

// runFilesJS calls one export of files.js in node with the file given, and
// returns what came back. files.js pulls preact in by its bare name, which
// the bundler resolves to web/vendor; here a resolve hook does the same.
func runFilesJS(t *testing.T, fn string, file map[string]any) any {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the file rules are run by the engine, not by reading the source")
	}
	vendor := func(name string) string {
		path, err := filepath.Abs(filepath.Join(webDir, "vendor", name))
		if err != nil {
			t.Fatal(err)
		}
		return "file://" + path
	}
	files, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	aliases, err := json.Marshal(map[string]string{
		"preact":       vendor("preact.mjs"),
		"preact/hooks": vendor("preact-hooks.mjs"),
		"htm":          vendor("htm.mjs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	hook := `
import { register } from "node:module";
const aliases = ` + string(aliases) + `;
register("data:text/javascript," + encodeURIComponent(
  "export async function resolve(spec, ctx, next) {" +
  "  const aliases = " + JSON.stringify(aliases) + ";" +
  "  if (aliases[spec]) return { url: aliases[spec], shortCircuit: true };" +
  "  return next(spec, ctx);" +
  "}"));
`
	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.mjs")
	if err := os.WriteFile(hookPath, []byte(hook), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
import * as files from ` + jsString("file://"+files) + `;
const file = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(files[` + jsString(fn) + `](file)));
`
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--no-warnings", "--import", hookPath, "--input-type=module", "-e", script)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	return got
}

// The row of a tagged file makes room for the tag: the column an icon sits in
// is sixteen pixels wide, and a three-letter type does not fit there.
func TestATaggedFileRowMakesRoomForTheTag(t *testing.T) {
	css := cssSrc(t)
	if !strings.Contains(css, ".mfile.tagged {") {
		t.Fatal("feed.css: the tagged file row has no rule of its own — the tag is squeezed into the icon column")
	}
	if !strings.Contains(cssBlock(t, css, ".mfile.tagged"), "grid-template-columns: auto") {
		t.Error("feed.css: the tagged file row keeps the icon column — the tag is cut at sixteen pixels")
	}
	if !strings.Contains(css, ".sent {") {
		t.Error("feed.css: the sent card has no rule — it is drawn as bare rows with no edge of their own")
	}
}
