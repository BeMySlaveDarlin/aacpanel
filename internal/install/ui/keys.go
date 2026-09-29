package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Keys are the keys of the installer, the ones Claude Code gives its
// questions: arrows move, a digit picks at once, Tab walks the tabs, space
// checks a box, Enter answers, Esc steps back.
var Keys = struct {
	Up, Down, Next, Prev, Left, Right key.Binding
	Toggle, Pick, Back, Expand, Stop  key.Binding
}{
	Up:     key.NewBinding(key.WithKeys("up", "ctrl+p")),
	Down:   key.NewBinding(key.WithKeys("down", "ctrl+n")),
	Next:   key.NewBinding(key.WithKeys("tab")),
	Prev:   key.NewBinding(key.WithKeys("shift+tab")),
	Left:   key.NewBinding(key.WithKeys("left")),
	Right:  key.NewBinding(key.WithKeys("right")),
	Toggle: key.NewBinding(key.WithKeys("space")),
	Pick:   key.NewBinding(key.WithKeys("enter")),
	Back:   key.NewBinding(key.WithKeys("esc")),
	Expand: key.NewBinding(key.WithKeys("ctrl+o")),
	Stop:   key.NewBinding(key.WithKeys("ctrl+c")),
}

// digit is the number a key stands for, 1 to 9, or 0 for any other key.
func digit(k tea.KeyPressMsg) int {
	s := k.String()
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return int(s[0] - '0')
	}
	return 0
}
