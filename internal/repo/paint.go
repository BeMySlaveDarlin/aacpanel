// Package repo turns what the agent read off the disk into what a screen can
// draw: a diff cut into hunks, and code cut into coloured spans.
package repo

import (
	"strings"
	"unicode/utf16"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// The classes a span can carry. They are the ones the panel already paints
// code with elsewhere, plus two the panel had no way to tell apart before: a
// type and the name of a function. A class is sent as its number — the name
// would be four bytes on every span of every line.
const (
	ClassPlain = iota
	ClassComment
	ClassString
	ClassKeyword
	ClassNumber
	ClassType
	ClassFunc
)

// ClassNames maps the numbers above to the css classes of the panel. It is
// exported so the screen and the tests read the same list rather than two.
var ClassNames = []string{"", "cdcm", "cdstr", "cdkw", "cdnum", "cdty", "cdfn"}

// What is coloured at all. Past this a file is sent as text: the colouring
// costs more than it gives on a file nobody reads line by line.
const MaxPaint = 256 * 1024

// Span is a run of characters of one class inside a line. The length is
// counted in utf-16 code units, the way a browser counts a string: a span
// measured in bytes lands in the middle of a character as soon as the line
// holds one that is not ascii.
type Span struct {
	Class  int
	Length int
}

// MarshalJSON writes a span as a pair, not as an object: a file of six
// thousand lines carries a hundred thousand of them, and {"class":3,"len":4}
// is nine times the pair that says the same thing.
func (s Span) MarshalJSON() ([]byte, error) {
	out := make([]byte, 0, 12)
	out = append(out, '[')
	out = appendInt(out, s.Class)
	out = append(out, ',')
	out = appendInt(out, s.Length)
	out = append(out, ']')
	return out, nil
}

func appendInt(dst []byte, n int) []byte {
	if n == 0 {
		return append(dst, '0')
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return append(dst, buf[i:]...)
}

// classOf maps a chroma token to one of the classes above. Chroma tells apart
// far more than a screen needs: what matters to a reader is which of these
// six a run of characters belongs to.
func classOf(t chroma.TokenType) int {
	switch {
	case t.InCategory(chroma.Comment):
		return ClassComment
	case t.InCategory(chroma.String) || t == chroma.LiteralStringEscape:
		return ClassString
	case t.InCategory(chroma.Number):
		return ClassNumber
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic || t == chroma.NameBuiltin:
		return ClassFunc
	case t == chroma.NameClass || t == chroma.KeywordType || t == chroma.NameNamespace:
		return ClassType
	case t.InCategory(chroma.Keyword) || t == chroma.OperatorWord:
		return ClassKeyword
	default:
		return ClassPlain
	}
}

// Lexer returns the name of the lexer a file is read with, or "" when nothing
// fits. The name travels with the coloured lines so a cache entry says what it
// was coloured as.
func Lexer(path string) string {
	l := lexers.Match(path)
	if l == nil {
		return ""
	}
	return l.Config().Name
}

// Paint colours a whole file and returns the spans of every line.
//
// A whole file rather than a hunk at a time: a lexer reads a string that opens
// on one line and closes three lines down, and a hunk handed to it alone
// starts in the middle of a sentence. The lines a screen shows are then cut
// out of the result, which costs nothing, while colouring a hunk in isolation
// costs the truth.
func Paint(path, text string) ([][]Span, string) {
	lexer := lexers.Match(path)
	if lexer == nil || len(text) > MaxPaint {
		return nil, ""
	}
	name := lexer.Config().Name
	it, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return nil, ""
	}

	lines := make([][]Span, 0, strings.Count(text, "\n")+1)
	current := []Span(nil)
	for _, token := range it.Tokens() {
		class := classOf(token.Type)
		for part := token.Value; part != ""; {
			head, rest, split := strings.Cut(part, "\n")
			if head != "" {
				current = add(current, class, head)
			}
			if !split {
				break
			}
			lines = append(lines, current)
			current = nil
			part = rest
		}
	}
	lines = append(lines, current)
	// A file ending in a newline gives a last empty line that is not a line of
	// the file: the text has as many lines as it has, and the screen counts
	// them the same way the agent did.
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 && strings.HasSuffix(text, "\n") {
		lines = lines[:n-1]
	}
	return lines, name
}

// add appends a run to a line, joining it to the one before when the class is
// the same: two spans of one class in a row are one span, and the difference
// is bytes on every line of every file.
func add(line []Span, class int, text string) []Span {
	n := len(utf16.Encode([]rune(text)))
	if n == 0 {
		return line
	}
	if k := len(line); k > 0 && line[k-1].Class == class {
		line[k-1].Length += n
		return line
	}
	return append(line, Span{Class: class, Length: n})
}
