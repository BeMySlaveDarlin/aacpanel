package action

import "strings"

// Refused are the slash commands the panel does not send into a session in any
// form: not as a command, and not as a message that starts with one. The rules
// of permissions are a screen driven by keys on a terminal, awkward even
// there, and the panel neither drives it nor opens it for a person to be stuck
// in. The older name of the same command is refused with it.
var Refused = map[string]string{
	"permissions":   "the rules of permissions are a screen driven by keys, and the panel does not open it",
	"allowed-tools": "the rules of permissions are a screen driven by keys, and the panel does not open it",
}

// refusedCommand says which refused command a text starts with, if any.
func refusedCommand(text string) (string, bool) {
	line := strings.TrimSpace(text)
	if !strings.HasPrefix(line, "/") {
		return "", false
	}
	fields := strings.Fields(line[1:])
	if len(fields) == 0 {
		return "", false
	}
	name := strings.ToLower(fields[0])
	_, refused := Refused[name]
	return name, refused
}
