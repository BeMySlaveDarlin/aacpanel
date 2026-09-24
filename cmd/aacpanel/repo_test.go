package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/chat"
	"aacpanel/internal/repo"
)

// A Go file of 450 lines with a block comment across the edge of the first
// window: lines 195 to 205 are a comment, and a lexer handed line 201 alone
// reads it as code.
func longGoFile() []string {
	lines := []string{"package main", ""}
	for len(lines) < 450 {
		n := len(lines) + 1
		switch {
		case n == 195:
			lines = append(lines, "/* the start of a long comment")
		case n > 195 && n < 205:
			lines = append(lines, fmt.Sprintf("   still inside the comment %d", n))
		case n == 205:
			lines = append(lines, "*/")
		default:
			lines = append(lines, fmt.Sprintf("var v%d = %d", n, n))
		}
	}
	return lines
}

func windowOf(all []string, first, count int) *chat.RepoOut {
	end := min(first-1+count, len(all))
	return &chat.RepoOut{
		OID: "oid-long", Path: "main.go", Size: int64(len(strings.Join(all, "\n"))),
		First: first, Lines: all[first-1 : end], More: end < len(all),
	}
}

func wholeOf(all []string, reads *int) func() (*chat.RepoOut, error) {
	return func() (*chat.RepoOut, error) {
		*reads++
		return &chat.RepoOut{OID: "oid-long", Path: "main.go", First: 1, Lines: all}, nil
	}
}

func TestAWindowIsColouredAsPartOfTheWholeFile(t *testing.T) {
	all := longGoFile()
	whole, _ := repo.Paint("main.go", strings.Join(all, "\n"))
	cache := repo.NewCache(4)
	reads := 0

	first, _ := paintWindow(cache, windowOf(all, 1, 200), wholeOf(all, &reads))
	if !reflect.DeepEqual(first, whole[0:200]) {
		t.Fatal("the first window is not the first rows of the whole file")
	}
	second, _ := paintWindow(cache, windowOf(all, 201, 200), wholeOf(all, &reads))
	if !reflect.DeepEqual(second, whole[200:400]) {
		t.Fatal("the second window is not rows 201–400 of the whole file — it came from somewhere else")
	}
	if reflect.DeepEqual(second, first) {
		t.Fatal("the second window was handed the colouring of the first")
	}
	if reads != 1 {
		t.Errorf("the whole file was read %d times; once is enough, the second window is a cache hit", reads)
	}
}

func TestAWindowThatIsTheWholeFileIsNotReadAgain(t *testing.T) {
	all := longGoFile()[:120]
	reads := 0
	lines, name := paintWindow(repo.NewCache(4), windowOf(all, 1, 200), wholeOf(all, &reads))
	if name == "" || len(lines) != len(all) {
		t.Fatalf("a short file came back with %d coloured rows of %d, lexer %q", len(lines), len(all), name)
	}
	if reads != 0 {
		t.Error("a file that fits one window was read a second time")
	}
}

func TestAFileThatMovedBetweenTwoReadsIsNotKept(t *testing.T) {
	all := longGoFile()
	cache := repo.NewCache(4)
	reads := 0
	moved := func() (*chat.RepoOut, error) {
		reads++
		return &chat.RepoOut{OID: "oid-newer", Path: "main.go", First: 1, Lines: all}, nil
	}
	if lines, _ := paintWindow(cache, windowOf(all, 201, 200), moved); lines != nil {
		t.Fatal("a window was coloured from a file under another id")
	}
	paintWindow(cache, windowOf(all, 201, 200), moved)
	if reads != 2 {
		t.Errorf("the whole file was read %d times; a failed read must not be kept and must be tried again", reads)
	}
}

func TestAFilePastTheColouringCeilingStaysText(t *testing.T) {
	all := longGoFile()
	out := windowOf(all, 201, 200)
	out.Size = repo.MaxPaint + 1
	reads := 0
	if lines, _ := paintWindow(repo.NewCache(4), out, wholeOf(all, &reads)); lines != nil {
		t.Error("a window of a file past the ceiling came back coloured")
	}
	if reads != 0 {
		t.Error("a file past the ceiling was read whole for nothing")
	}
}
