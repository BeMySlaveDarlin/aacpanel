package check

import (
	"fmt"
	"strings"
	"testing"
)

type railFeed struct {
	Rows   int `json:"rows"`
	Stacks []struct {
		Said   string `json:"said"`
		Badges []struct {
			Kind  string `json:"kind"`
			Count string `json:"count"`
			Said  string `json:"said"`
		} `json:"badges"`
	} `json:"stacks"`
}

// counts is a feed's stacks in short: the kinds and numbers of each, top down.
func (f railFeed) counts() string {
	var out []string
	for _, s := range f.Stacks {
		var b []string
		for _, x := range s.Badges {
			b = append(b, x.Kind+x.Count)
		}
		out = append(out, strings.Join(b, ","))
	}
	return strings.Join(out, " ")
}

// Works close together on a phone are one stack: five works in a row, each
// between replies of one line, would each want a stack of three badges
// taller than its row. They are summed into one stack that counts every kind
// of what they did, and its words say the sum. Tapping a badge of it opens
// the calls of its kind of all five works, in order; the ends of two turns in
// one stack open the calls of both turns. Works with room enough beside their
// replies keep a stack each.
func TestWorksCloseTogetherAreOneStackOnAPhone(t *testing.T) {
	var got struct {
		Crowd      railFeed `json:"crowd"`
		Apart      railFeed `json:"apart"`
		BashOpens  []string `json:"bashOpens"`
		FilesOpens []string `json:"filesOpens"`
		SheetHead  string   `json:"sheetHead"`
		Turns      struct {
			Stacks int      `json:"stacks"`
			Said   string   `json:"said"`
			Count  int      `json:"count"`
			List   []string `json:"list"`
			Head   string   `json:"head"`
		} `json:"turns"`
	}
	runFixture(t, "railmerge.html", &got)

	crowd := got.Crowd
	if crowd.Rows != 6 {
		t.Fatalf("the first feed has %d rows, want the person and five replies", crowd.Rows)
	}
	if c := crowd.counts(); c != "think11,bash13,files12" {
		t.Errorf("five works between one-line replies stand as %q, want one stack summing them: think11,bash13,files12", c)
	}
	if len(crowd.Stacks) > 0 {
		s := crowd.Stacks[0]
		if s.Said != "13 commands · 12 files" {
			t.Errorf("the summed stack says %q, want the sum: 13 commands · 12 files", s.Said)
		}
		var said []string
		for _, b := range s.Badges {
			said = append(said, b.Said)
		}
		want := "thinking: 11 — open the calls | 13 commands — open the calls | 12 files — open the calls"
		if strings.Join(said, " | ") != want {
			t.Errorf("the badges of the summed stack say\n  %s\nwant\n  %s", strings.Join(said, " | "), want)
		}
	}

	var bash, files []string
	for n, c := range [][2]int{{3, 3}, {3, 3}, {2, 2}, {3, 3}, {2, 1}} {
		for i := 0; i < c[0]; i++ {
			bash = append(bash, fmt.Sprintf("Bash make step%d.%d", n, i))
		}
		for i := 0; i < c[1]; i++ {
			files = append(files, fmt.Sprintf("Read /srv/proj/web/src/w%df%d.js", n, i))
		}
	}
	if strings.Join(got.BashOpens, "\n") != strings.Join(bash, "\n") {
		t.Errorf("tapping the commands of the summed stack opened\n  %s\nwant the commands of all five works in order\n  %s",
			strings.Join(got.BashOpens, "\n  "), strings.Join(bash, "\n  "))
	}
	if strings.Join(got.FilesOpens, "\n") != strings.Join(files, "\n") {
		t.Errorf("tapping the files of the summed stack opened\n  %s\nwant the files of all five works in order\n  %s",
			strings.Join(got.FilesOpens, "\n  "), strings.Join(files, "\n  "))
	}
	if got.SheetHead != "12 calls" {
		t.Errorf("the calls sheet of the files is headed %q, want 12 calls", got.SheetHead)
	}

	turns := got.Turns
	if turns.Stacks != 1 || turns.Count != 3 ||
		turns.Said != "3 commands · 2 turns · worked 3s · 3 calls · 1 background agent was still at work" {
		t.Errorf("two turns summed into %d stacks, the badge of their ends counting %d, saying %q: "+
			"want one stack, 3 calls, the two turns in words", turns.Stacks, turns.Count, turns.Said)
	}
	if strings.Join(turns.List, " | ") != "Bash make a | Bash make b | Bash make c" || !strings.HasPrefix(turns.Head, "3 calls·worked 3s") {
		t.Errorf("tapping the ends of two turns opened %v headed %q: want the calls of both turns, 3 calls, worked 3s",
			turns.List, turns.Head)
	}

	apart := got.Apart
	if c := apart.counts(); c != "think1,bash2,files1 think2,bash1,files3 think1,bash1,files1" {
		t.Errorf("works with room beside their replies stand as %q, want a stack each", c)
	}
}
