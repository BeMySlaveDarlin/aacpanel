package repo

import (
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// Where a line came from. A review answers for these differently: what is in a
// commit has been written down, what is in the working tree has not.
const (
	LayerCommitted = "committed"
	LayerWorktree  = "worktree"
)

// What a line of a hunk is. The three are kept apart rather than marked with a
// leading character: a screen colours them, counts them and hangs notes on
// them, and reading the first byte of every line to find out which is which is
// work done over and over.
const (
	KindContext = "ctx"
	KindAdd     = "add"
	KindDel     = "del"
)

// Line is one line of a diff, with the numbers it has on each side. A deleted
// line has no number on the new side and an added one none on the old side,
// and that is what a nil says.
type Line struct {
	Kind  string `json:"kind"`
	Old   *int   `json:"old,omitempty"`
	New   *int   `json:"new,omitempty"`
	Text  string `json:"text"`
	Spans []Span `json:"spans,omitempty"`
}

// Hunk is one run of changed lines with its heading.
type Hunk struct {
	Header string `json:"header"`
	Layer  string `json:"layer"`
	Lines  []Line `json:"lines"`
}

// FileDiff is what one file changed, in the order a screen draws it.
type FileDiff struct {
	Path    string `json:"path"`
	Old     string `json:"old,omitempty"`
	Binary  bool   `json:"binary,omitempty"`
	Renamed bool   `json:"renamed,omitempty"`
	// The object ids of the two sides. They are the key a coloured file is
	// cached under: an id changes with the content and with nothing else.
	OldOID string `json:"oldOid,omitempty"`
	NewOID string `json:"newOid,omitempty"`
	Hunks  []Hunk `json:"hunks"`
	Cut    bool   `json:"cut,omitempty"`
}

// MaxHunkLines is how much of one file's diff is carried at a time. Past it
// the reply says it was cut rather than quietly ending.
const MaxHunkLines = 4000

// Cut parses a unified diff of one file, as git wrote it, into hunks.
//
// The parsing lives here and not in the agent because it is reading, not
// running: the agent hands over what git said, and a mistake in the cutting
// costs a redraw instead of a shell command on the host.
func Cut(text, layer string) ([]FileDiff, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	files, _, err := gitdiff.Parse(strings.NewReader(text))
	if err != nil {
		return nil, err
	}

	out := make([]FileDiff, 0, len(files))
	for _, f := range files {
		name := f.NewName
		if name == "" {
			name = f.OldName
		}
		fd := FileDiff{
			Path:    name,
			OldOID:  f.OldOIDPrefix,
			NewOID:  f.NewOIDPrefix,
			Binary:  f.IsBinary,
			Renamed: f.IsRename,
		}
		if f.IsRename && f.OldName != "" && f.OldName != name {
			fd.Old = f.OldName
		}
		left := MaxHunkLines
		for _, frag := range f.TextFragments {
			if left <= 0 {
				fd.Cut = true
				break
			}
			hunk := Hunk{Header: frag.Header(), Layer: layer}
			oldNo := int(frag.OldPosition)
			newNo := int(frag.NewPosition)
			for _, line := range frag.Lines {
				if left <= 0 {
					fd.Cut = true
					break
				}
				left--
				text := strings.TrimSuffix(line.Line, "\n")
				switch line.Op {
				case gitdiff.OpContext:
					o, n := oldNo, newNo
					hunk.Lines = append(hunk.Lines, Line{Kind: KindContext, Old: &o, New: &n, Text: text})
					oldNo++
					newNo++
				case gitdiff.OpAdd:
					n := newNo
					hunk.Lines = append(hunk.Lines, Line{Kind: KindAdd, New: &n, Text: text})
					newNo++
				case gitdiff.OpDelete:
					o := oldNo
					hunk.Lines = append(hunk.Lines, Line{Kind: KindDel, Old: &o, Text: text})
					oldNo++
				}
			}
			if len(hunk.Lines) > 0 {
				fd.Hunks = append(fd.Hunks, hunk)
			}
		}
		out = append(out, fd)
	}
	return out, nil
}

// PaintDiff colours the lines of a diff.
//
// The two sides are coloured as two files rather than line by line: a lexer
// reading one line of a diff sees a string that opens and never closes, and
// paints the rest of the line as one. The old side is built from the context
// and the deletions, the new side from the context and the additions — which
// is what the two files looked like around this change.
func PaintDiff(path string, files []FileDiff) {
	for i := range files {
		name := files[i].Path
		if name == "" {
			name = path
		}
		var oldText, newText strings.Builder
		for _, h := range files[i].Hunks {
			for _, l := range h.Lines {
				switch l.Kind {
				case KindContext:
					oldText.WriteString(l.Text)
					oldText.WriteByte('\n')
					newText.WriteString(l.Text)
					newText.WriteByte('\n')
				case KindDel:
					oldText.WriteString(l.Text)
					oldText.WriteByte('\n')
				case KindAdd:
					newText.WriteString(l.Text)
					newText.WriteByte('\n')
				}
			}
		}
		oldLines, _ := Paint(name, oldText.String())
		newLines, _ := Paint(name, newText.String())
		var o, n int
		for hi := range files[i].Hunks {
			for li := range files[i].Hunks[hi].Lines {
				line := &files[i].Hunks[hi].Lines[li]
				switch line.Kind {
				case KindContext:
					line.Spans = at(newLines, n)
					o++
					n++
				case KindDel:
					line.Spans = at(oldLines, o)
					o++
				case KindAdd:
					line.Spans = at(newLines, n)
					n++
				}
			}
		}
	}
}

func at(lines [][]Span, i int) []Span {
	if i < 0 || i >= len(lines) {
		return nil
	}
	return lines[i]
}
