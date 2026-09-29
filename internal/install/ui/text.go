package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// wrap breaks text into lines of at most width cells, at spaces only, and
// inside a word only when the word alone is wider. Not at hyphens, as the
// wrapping of the ansi package does: most of what the installer prints is
// paths and unit names, and aacpanel-agent@ split in two reads as two
// things. A width under one leaves the text whole: there is nothing to fit
// it into.
func wrap(s string, width int) []string { return hang(s, width, width) }

// hang wraps as wrap does, into first cells on the first line and rest
// cells on every line after it: the lines under a bullet hang further in
// than the bullet itself.
func hang(s string, first, rest int) []string {
	if first < 1 || rest < 1 {
		return strings.Split(s, "\n")
	}
	var out []string
	width := first
	for _, para := range strings.Split(s, "\n") {
		line, used, started := "", 0, false
		for _, word := range strings.Split(para, " ") {
			w := ansi.StringWidth(word)
			switch {
			case !started && w <= width:
				line, used, started = word, w, true
			case started && used+1+w <= width:
				line, used = line+" "+word, used+1+w
			default:
				if started {
					out = append(out, line)
					width = rest
				}
				for w > width {
					out = append(out, ansi.Truncate(word, width, ""))
					word = ansi.TruncateLeft(word, width, "")
					w = ansi.StringWidth(word)
					width = rest
				}
				line, used, started = word, w, true
			}
		}
		out = append(out, line)
		width = rest
	}
	return out
}

// fit cuts a line to width cells, marking the cut.
func fit(s string, width int) string {
	if width < 1 || ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// pad fills a line with spaces up to width cells.
func pad(s string, width int) string {
	if gap := width - ansi.StringWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// indent puts prefix before the first line and cont before the rest, wrapping
// the text into what is left of the width.
func indent(s, prefix, cont string, width int) []string {
	out := hang(s, width-ansi.StringWidth(prefix), width-ansi.StringWidth(cont))
	for i, line := range out {
		if i == 0 {
			out[i] = prefix + line
		} else {
			out[i] = cont + line
		}
	}
	return out
}

// Indent wraps text into the width with prefix before its first line and cont
// before the rest.
func Indent(s, prefix, cont string, width int) []string { return indent(s, prefix, cont, width) }

// Fit cuts a line to width cells, marking the cut with an ellipsis.
func Fit(s string, width int) string { return fit(s, width) }

// Width is the width of a line in terminal cells.
func Width(s string) int { return ansi.StringWidth(s) }

// Strip removes the styling from rendered text.
func Strip(s string) string { return ansi.Strip(s) }
