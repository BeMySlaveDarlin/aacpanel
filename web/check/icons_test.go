package check

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The interface draws one set of icons: every glyph lives in src/ui/icons.js
// and every glyph is drawn with the same stroke. An <svg> written elsewhere
// is a second set, however close its lines look, and a second stroke width
// is a second set inside the first.
func TestIconsAreOneSet(t *testing.T) {
	files := srcFiles(t)

	const set = "src/ui/icons.js"
	if !strings.Contains(files[set], "<svg") {
		t.Fatalf("%s draws no <svg> at all — the test guards the wrong place", set)
	}

	for _, path := range sortedKeys(files) {
		if path == set {
			continue
		}
		if strings.Contains(files[path], "<svg") {
			t.Errorf("%s draws its own <svg> — an icon outside %s is a second set", path, set)
		}
	}

	width := regexp.MustCompile(`stroke-width["']?\s*[:=]\s*["']?([0-9.]+)`)
	seen := map[string][]string{}
	for _, path := range sortedKeys(files) {
		for _, m := range width.FindAllStringSubmatch(files[path], -1) {
			seen[m[1]] = append(seen[m[1]], path)
		}
	}
	if len(seen) == 0 {
		t.Fatalf("no stroke-width anywhere in the sources — the test guards the wrong place")
	}
	if len(seen) != 1 {
		var lines []string
		for value, paths := range seen {
			lines = append(lines, value+" in "+strings.Join(paths, ", "))
		}
		sort.Strings(lines)
		t.Errorf("the icons take %d stroke widths (%s) — one set of icons has one stroke", len(seen), strings.Join(lines, "; "))
	}
}
