package repo

import (
	"encoding/json"
	"strings"
	"testing"
)

const goDiff = `diff --git a/pkg/env.go b/pkg/env.go
index 1111111111111111111111111111111111111111..2222222222222222222222222222222222222222 100644
--- a/pkg/env.go
+++ b/pkg/env.go
@@ -10,7 +10,8 @@ func childEnv() {
 	// what the caller had
 	warns := []string{}
 	if warn := checkBus(addr); warn != "" {
-		warns = append(warns, warn)
+		delete(env, "DBUS_SESSION_BUS_ADDRESS")
+		warns = append(warns, warn)
 	}
 	return warns
 }
`

// A line of a diff has a number on each side, and only on the side it is on.
// A screen hangs a note on that number, and a note hung on the wrong side of a
// deletion lands on a line the file does not have.
func TestEachLineKeepsTheNumberOfItsOwnSide(t *testing.T) {
	files, err := Cut(goDiff, LayerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(files[0].Hunks) != 1 {
		t.Fatalf("the diff came apart into %d files", len(files))
	}
	if files[0].Path != "pkg/env.go" {
		t.Errorf("the file is named %q", files[0].Path)
	}
	if files[0].OldOID == "" || files[0].NewOID == "" {
		t.Error("the object ids did not survive the parsing — a coloured copy has nothing to be cached under")
	}
	if files[0].Hunks[0].Layer != LayerWorktree {
		t.Errorf("the hunk is labelled %q, and a review answers for the two layers differently", files[0].Hunks[0].Layer)
	}

	var add, del, ctx int
	for _, l := range files[0].Hunks[0].Lines {
		switch l.Kind {
		case KindAdd:
			add++
			if l.New == nil || l.Old != nil {
				t.Errorf("an added line carries old=%v new=%v", l.Old, l.New)
			}
		case KindDel:
			del++
			if l.Old == nil || l.New != nil {
				t.Errorf("a deleted line carries old=%v new=%v", l.Old, l.New)
			}
		case KindContext:
			ctx++
			if l.Old == nil || l.New == nil {
				t.Errorf("a context line is missing a number: old=%v new=%v", l.Old, l.New)
			}
		}
	}
	if add != 2 || del != 1 {
		t.Errorf("the hunk holds %d additions and %d deletions, expected 2 and 1", add, del)
	}
	if ctx == 0 {
		t.Error("the hunk lost its context lines — a diff without them cannot be read")
	}
}

// The line numbers have to run, not repeat: a screen scrolls by them and a
// note is hung on one of them.
func TestTheNumbersRunThroughTheHunk(t *testing.T) {
	files, _ := Cut(goDiff, LayerCommitted)
	lines := files[0].Hunks[0].Lines
	if lines[0].New == nil || *lines[0].New != 10 {
		t.Fatalf("the hunk starts at new line %v, the header says 10", lines[0].New)
	}
	prev := 0
	for _, l := range lines {
		if l.New == nil {
			continue
		}
		if prev != 0 && *l.New != prev+1 {
			t.Errorf("the new side jumps from %d to %d", prev, *l.New)
		}
		prev = *l.New
	}
}

// A lexer reading one line of a diff sees a string that opens and never
// closes. The two sides are coloured as two files, and the lines are taken out
// of the result afterwards.
func TestADeletedLineIsColouredAsPartOfTheOldFile(t *testing.T) {
	const diff = `diff --git a/a.go b/a.go
index 1111111111111111111111111111111111111111..2222222222222222222222222222222222222222 100644
--- a/a.go
+++ b/a.go
@@ -1,4 +1,4 @@
 package main

-// const greeting = "hello"
+const greeting = "hello, world"

`
	files, err := Cut(diff, LayerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	PaintDiff("a.go", files)

	var del, add []Span
	for _, l := range files[0].Hunks[0].Lines {
		switch l.Kind {
		case KindDel:
			del = l.Spans
		case KindAdd:
			add = l.Spans
		}
	}
	if len(del) == 0 || len(add) == 0 {
		t.Fatalf("a changed line came back without colour: del=%v add=%v", del, add)
	}
	// The two sides differ in what they are, not only in what they say: the old
	// line is a comment, the new one is code. Colouring a deleted line against
	// the new side then shows up as the wrong class rather than as the same
	// class with other text.
	if !holds(del, ClassComment) {
		t.Errorf("the deleted line is a comment and came back as %v — it was coloured against the wrong side", del)
	}
	if holds(del, ClassKeyword) {
		t.Errorf("the deleted line holds a keyword span (%v), and a comment has no keywords", del)
	}
	if !holds(add, ClassKeyword) || !holds(add, ClassString) {
		t.Errorf("the added line lost its colouring: %v", add)
	}
}

func holds(spans []Span, class int) bool {
	for _, s := range spans {
		if s.Class == class {
			return true
		}
	}
	return false
}

// Spans are counted the way a browser counts a string. A length in bytes lands
// in the middle of a character as soon as a line holds one that is not ascii,
// and every span after it is off by the same amount.
func TestSpansAreCountedAsABrowserCountsThem(t *testing.T) {
	// The wrench is outside the basic plane: one rune, two utf-16 units, four
	// bytes. All three numbers differ, so a span measured any other way lands
	// in the wrong place and takes every span after it along.
	lines, name := Paint("hello.go", "// \U0001F527\nconst a = 1\n")
	if name == "" {
		t.Fatal("go was not recognised by the name of the file")
	}
	if len(lines) != 2 {
		t.Fatalf("two lines gave %d", len(lines))
	}
	var total int
	for _, s := range lines[0] {
		total += s.Length
	}
	if total != 5 {
		t.Errorf("the first line measures %d, and a browser reads it as 5 (\"// \" plus a surrogate pair)", total)
	}
}

// A span goes on the wire as a pair. A file of six thousand lines carries a
// hundred thousand of them, and an object per span is nine times the size.
func TestASpanTravelsAsAPair(t *testing.T) {
	out, err := json.Marshal([]Span{{Class: ClassKeyword, Length: 5}, {Class: ClassPlain, Length: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[[3,5],[0,1]]" {
		t.Errorf("a pair of spans encodes as %s", out)
	}
}

// Two runs of one class in a row are one span. Chroma joins tokens of the same
// type itself, so what is left to join here is two different types this panel
// paints alike — a builtin next to a function name, a type next to a namespace
// — and those meet on most lines of go.
func TestNeighboursOfOneClassAreJoined(t *testing.T) {
	line := add(nil, ClassKeyword, "const")
	line = add(line, ClassKeyword, " a")
	if len(line) != 1 {
		t.Fatalf("two runs of one class gave %d spans: %v", len(line), line)
	}
	if line[0].Length != 7 {
		t.Errorf("the joined span measures %d, expected 7", line[0].Length)
	}
	line = add(line, ClassString, `"x"`)
	if len(line) != 2 {
		t.Errorf("a run of another class was swallowed: %v", line)
	}
	if got := add(nil, ClassPlain, ""); got != nil {
		t.Errorf("an empty run became a span: %v", got)
	}
}

// What the panel cannot read is sent as text rather than guessed at.
func TestAFileOfNoKnownKindIsLeftUncoloured(t *testing.T) {
	lines, name := Paint("notes.unknownext", "whatever this is\n")
	if lines != nil || name != "" {
		t.Errorf("a file of no known kind was coloured as %q", name)
	}
}

func TestAFileOverTheCeilingIsLeftUncoloured(t *testing.T) {
	big := strings.Repeat("const a = 1\n", MaxPaint/12+10)
	if lines, _ := Paint("big.go", big); lines != nil {
		t.Error("a file past the ceiling was coloured — the colouring costs more there than it gives")
	}
}

// The id of a blob changes with its content and with nothing else, so an entry
// under it never goes stale and nothing has to be invalidated by hand.
func TestASecondReadOfTheSameBlobIsNotRepainted(t *testing.T) {
	c := NewCache(4)
	const text = "package main\n\nfunc main() {}\n"

	first, name := c.Painted("abc123", "main.go", text)
	if name == "" || len(first) == 0 {
		t.Fatal("the first read came back uncoloured")
	}
	second, _ := c.Painted("abc123", "main.go", text)
	if len(second) != len(first) {
		t.Fatal("the second read came back different")
	}
	entries, hits, misses := c.Stat()
	if entries != 1 || hits != 1 || misses != 1 {
		t.Errorf("entries=%d hits=%d misses=%d — the second read painted the file again", entries, hits, misses)
	}
}

func TestAnEditedFileGetsItsOwnEntry(t *testing.T) {
	c := NewCache(4)
	c.Painted("aaa", "main.go", "package main\n")
	c.Painted("bbb", "main.go", "package other\n")
	if entries, _, misses := c.Stat(); entries != 2 || misses != 2 {
		t.Errorf("entries=%d misses=%d — an edited file was served the colouring of the old one", entries, misses)
	}
}

func TestTheOldestEntryLeavesWhenTheCacheIsFull(t *testing.T) {
	c := NewCache(2)
	c.Painted("one", "a.go", "package a\n")
	c.Painted("two", "b.go", "package b\n")
	c.Painted("three", "c.go", "package c\n")
	if entries, _, _ := c.Stat(); entries != 2 {
		t.Errorf("a cache of two holds %d entries", entries)
	}
	// "one" is gone, so reading it again is a miss rather than a hit.
	before := hitsOf(c)
	c.Painted("one", "a.go", "package a\n")
	if hitsOf(c) != before {
		t.Error("the entry that was pushed out came back — the cache grew past its limit")
	}
}

func hitsOf(c *Cache) int {
	_, hits, _ := c.Stat()
	return hits
}
