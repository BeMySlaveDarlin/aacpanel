package check

import (
	"strings"
	"testing"
)

func TestFrontPersonalNameHasOneOwner(t *testing.T) {
	const owner = "src/contour.js"
	files := srcFiles(t)
	if files[owner] == "" {
		t.Fatalf("%s not found — the constant has been moved, and the test guards an empty place", owner)
	}
	if !strings.Contains(files[owner], `= "personal"`) {
		t.Fatalf("%s does not declare the name of the personal contour", owner)
	}

	for _, path := range sortedKeys(files) {
		if path == owner {
			continue
		}
		if strings.Contains(withoutComments(files[path]), `"personal"`) {
			t.Errorf("%s writes \"personal\" as its own literal — the name of the personal contour is "+
				"decided by PERSONAL from %s, otherwise the two sides diverge silently", path, owner)
		}
	}
}
