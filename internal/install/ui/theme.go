// Package ui draws the installer the way Claude Code draws a session: a feed
// of steps that stays in the scrollback of the terminal, and a live part at
// the bottom — a block of questions, a frame to confirm, a spinner with the
// list of steps. Nothing here knows what the installer asks or does:
// questions arrive as data and output as lines, so the demo and the real
// installer share every piece of the screen.
//
// Everything renders to a string for a given width. The feed is printed once
// and left to the terminal; the live part is drawn again on every change.
// Below MinWidth the frames give way to plain indented lines, since a frame
// that wraps is worse than none.
package ui

import (
	"os"

	"charm.land/lipgloss/v2"
)

// MinWidth is the narrowest terminal the frames and the tab row are laid out
// for. Narrower, a frame is drawn as plain lines.
const MinWidth = 60

// MaxFrame is the widest a frame grows: a wider one on a wide terminal is
// harder to read, not easier.
const MaxFrame = 80

// Theme is the look of every piece. Colours are the sixteen of the terminal's
// own palette, so they follow the person's theme rather than fight it; with
// NO_COLOR they are gone and bold, faint and reverse remain, since those are
// not colour.
type Theme struct {
	Color bool

	Accent lipgloss.Style // the cursor, a checked box, the chosen answer
	Dim    lipgloss.Style // explanations, hints, collapsed output
	Strong lipgloss.Style // a question, a frame's title, the step at work
	Focus  lipgloss.Style // the tab in focus
	OK     lipgloss.Style
	Warn   lipgloss.Style
	Fail   lipgloss.Style
	Spin   lipgloss.Style // the spinner's glyph

	Edge     lipgloss.Style // a frame that informs, in the text's own colour: a faint line is lost at a glance
	PlanEdge lipgloss.Style // a frame to approve
	RootEdge lipgloss.Style // a frame that asks for root
}

// NewTheme builds the look with or without colour.
func NewTheme(color bool) Theme {
	plain := lipgloss.NewStyle()
	t := Theme{
		Color:    color,
		Accent:   plain,
		Dim:      plain.Faint(true),
		Strong:   plain.Bold(true),
		Focus:    plain.Reverse(true),
		OK:       plain,
		Warn:     plain,
		Fail:     plain,
		Spin:     plain,
		Edge:     plain,
		PlanEdge: plain,
		RootEdge: plain,
	}
	if !color {
		return t
	}
	t.Accent = plain.Foreground(lipgloss.BrightBlue)
	t.Dim = plain.Foreground(lipgloss.BrightBlack)
	t.Focus = plain.Reverse(true).Foreground(lipgloss.BrightBlue)
	t.OK = plain.Foreground(lipgloss.Green)
	t.Warn = plain.Foreground(lipgloss.Yellow)
	t.Fail = plain.Foreground(lipgloss.Red)
	t.Spin = plain.Foreground(lipgloss.BrightRed)
	t.PlanEdge = plain.Foreground(lipgloss.Cyan)
	t.RootEdge = plain.Foreground(lipgloss.Yellow)
	return t
}

// ThemeFromEnv honours NO_COLOR: set and not empty, it turns colour off.
func ThemeFromEnv() Theme {
	return NewTheme(os.Getenv("NO_COLOR") == "")
}
