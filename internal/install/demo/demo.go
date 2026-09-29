// Package demo walks the whole installer on a made-up machine: the check of
// the machine, every block of questions with the kit and the map, the plan,
// the root command with a real handover of the terminal, the steps with
// their spinner and output, a failure on request, the code for the first
// device and the report. It changes nothing and sends nothing: its only
// process is the shell that asks for the password the way sudo would, and
// that shell forgets it.
//
// It is the screen of the installer before the installer: the owner sees
// the whole run, and the pieces of ui are the ones the real run will use.
package demo

import (
	tea "charm.land/bubbletea/v2"
)

// Run plays the demo in the terminal and returns its exit status.
func Run(opt Options) (int, error) {
	m := New(opt)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return 1, err
	}
	return m.Status(), nil
}
