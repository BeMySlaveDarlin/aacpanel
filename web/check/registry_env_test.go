package check

import (
	"strings"
	"testing"
)

func TestActionTextsPromiseNoSpecificEnvironment(t *testing.T) {
	forbidden := []string{
		"konsole", "Konsole", "KDE", "plasma", "Plasma", "GNOME", "gnome",
		"your host", "/opt/", "/home/",
	}

	for _, line := range strings.Split(srcFiles(t)[registryFile], "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		for _, bad := range forbidden {
			if strings.Contains(line, bad) {
				t.Errorf("an action text promises %q, and on someone else's machine there is no such thing: %s",
					bad, strings.TrimSpace(line))
			}
		}
	}
}
