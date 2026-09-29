package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Field is a line being typed. It is small on purpose: a line of one's own
// answer, a path, a key — no history, no completion, no clipboard of its own
// (a paste arrives from the terminal as a PasteMsg).
type Field struct {
	value []rune
	pos   int
}

// Value is the line as typed.
func (f *Field) Value() string { return string(f.value) }

// Set replaces the line and puts the caret at its end.
func (f *Field) Set(s string) {
	f.value = []rune(s)
	f.pos = len(f.value)
}

// Insert types text at the caret. Line breaks and tabs become spaces: the
// field is one line.
func (f *Field) Insert(s string) {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	r := []rune(s)
	f.value = append(f.value[:f.pos], append(r, f.value[f.pos:]...)...)
	f.pos += len(r)
}

// Key edits the line by a key and says whether the key was one it takes. A
// key it does not take — Enter, Esc, Tab — is left to the block.
func (f *Field) Key(k tea.KeyPressMsg) bool {
	switch k.String() {
	case "backspace", "ctrl+h":
		if f.pos > 0 {
			f.value = append(f.value[:f.pos-1], f.value[f.pos:]...)
			f.pos--
		}
	case "delete", "ctrl+d":
		if f.pos < len(f.value) {
			f.value = append(f.value[:f.pos], f.value[f.pos+1:]...)
		}
	case "left", "ctrl+b":
		f.pos = max(0, f.pos-1)
	case "right", "ctrl+f":
		f.pos = min(len(f.value), f.pos+1)
	case "home", "ctrl+a":
		f.pos = 0
	case "end", "ctrl+e":
		f.pos = len(f.value)
	case "ctrl+u":
		f.value = append([]rune(nil), f.value[f.pos:]...)
		f.pos = 0
	case "ctrl+k":
		f.value = f.value[:f.pos]
	case "ctrl+w":
		start := f.pos
		for start > 0 && f.value[start-1] == ' ' {
			start--
		}
		for start > 0 && f.value[start-1] != ' ' {
			start--
		}
		f.value = append(f.value[:start], f.value[f.pos:]...)
		f.pos = start
	default:
		key := k.Key()
		if key.Text == "" || key.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			return false
		}
		f.Insert(key.Text)
	}
	return true
}

// View draws the line with its caret. mask hides every character behind an
// asterisk; a line wider than width scrolls so the caret stays in sight.
func (f *Field) View(t Theme, mask bool, placeholder string, width int) string {
	if len(f.value) == 0 {
		if placeholder == "" {
			return t.Focus.Render(" ")
		}
		r := []rune(placeholder)
		return t.Focus.Render(string(r[0])) + t.Dim.Render(fit(string(r[1:]), width-1))
	}
	shown := f.value
	if mask {
		shown = []rune(strings.Repeat("*", len(f.value)))
	}
	shown = append(shown, ' ')
	from := 0
	if width > 1 && f.pos >= width-1 {
		from = f.pos - width + 2
	}
	to := min(len(shown), from+max(1, width))
	var b strings.Builder
	for i := from; i < to; i++ {
		if i == f.pos {
			b.WriteString(t.Focus.Render(string(shown[i])))
		} else {
			b.WriteRune(shown[i])
		}
	}
	return strings.TrimRight(b.String(), " ")
}
