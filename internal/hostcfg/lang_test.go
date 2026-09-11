package hostcfg

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestLocaleFallbackHasOneOwner(t *testing.T) {
	root := filepath.Join("..", "..")
	owner := "internal/hostcfg/hostcfg.go"
	readers := []string{"internal/launcher/env.go", "internal/executor/term.go"}

	literal := regexp.MustCompile(`"[^"]*(?i:utf-?8)[^"]*"`)

	count := func(rel string) (int, []string) {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		n := 0
		var where []string
		for i, line := range codeLines(string(raw), false) {
			if m := literal.FindAllString(line, -1); len(m) > 0 {
				n += len(m)
				where = append(where, rel+":"+strconv.Itoa(i+1)+": "+strings.Join(m, " "))
			}
		}
		return n, where
	}

	if n, where := count(owner); n != 1 {
		t.Errorf("%s: %d locale literals, expected one — the declaration of DefaultLang: %v", owner, n, where)
	}
	for _, rel := range readers {
		if n, where := count(rel); n != 0 {
			t.Errorf("%s: a locale literal of its own instead of hostcfg.DefaultLang — it will part from the description silently: %v", rel, where)
		}
		raw, _ := os.ReadFile(filepath.Join(root, rel))
		if !strings.Contains(strings.Join(codeLines(string(raw), false), "\n"), "hostcfg.DefaultLang") {
			t.Errorf("%s: it does not take the fallback from hostcfg.DefaultLang — where from, then?", rel)
		}
	}
}
