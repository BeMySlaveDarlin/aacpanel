package check

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func srcFiles(t *testing.T) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.WalkDir(webPath("src"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".js" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[webRel(path)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("reading frontend sources: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no frontend sources found — the test is useless, check the path")
	}
	return files
}

func sortedKeys(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for path := range files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func screenSrc(t *testing.T, hint string) string {
	t.Helper()
	files := srcFiles(t)
	folder := strings.TrimSuffix(hint, ".js") + "/"
	var all strings.Builder
	for _, path := range sortedKeys(files) {
		if path != hint && !strings.HasPrefix(path, folder) {
			continue
		}
		all.WriteString("\n/* ")
		all.WriteString(path)
		all.WriteString(" */\n")
		all.WriteString(files[path])
	}
	if all.Len() == 0 {
		t.Fatalf("the screen has no sources at all — the test is useless (looked for %s and %s)", hint, folder)
	}
	return all.String()
}

func cssSrc(t *testing.T) string {
	t.Helper()
	var all strings.Builder
	err := filepath.WalkDir(webPath("src"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".css" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		all.WriteString("\n/* ")
		all.WriteString(webRel(path))
		all.WriteString(" */\n")
		all.Write(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("reading frontend styles: %v", err)
	}
	if all.Len() == 0 {
		t.Fatal("no frontend styles found — the test is useless, check the path")
	}
	return all.String()
}

func hasPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func contains(value string, list []string) bool {
	for _, item := range list {
		if value == item {
			return true
		}
	}
	return false
}

func after(body, key string) string {
	i := strings.Index(body, key)
	if i < 0 {
		return ""
	}
	rest := body[i+len(key):]
	if j := regexp.MustCompile(`(?m)^\s{4}"[a-z]+\.[a-z]+":`).FindStringIndex(rest); j != nil {
		return rest[:j[0]]
	}
	return rest
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func actionsBlock(t *testing.T, file string) string {
	t.Helper()
	const head = "export const ACTIONS = {"
	i := strings.Index(file, head)
	if i < 0 {
		t.Fatalf("%s has no ACTIONS declaration", registryFile)
	}
	rest := file[i+len(head):]
	j := strings.Index(rest, "\n};")
	if j < 0 {
		t.Fatalf("%s: end of the ACTIONS declaration not found", registryFile)
	}
	return rest[:j]
}

func inMarkupComment(body string, n int) bool {
	lines := strings.Split(body, "\n")
	for i := n; i >= 0; i-- {
		if strings.Contains(lines[i], "-->") && i != n {
			return false
		}
		if strings.Contains(lines[i], "<!--") {
			return !strings.Contains(lines[i], "-->")
		}
	}
	return false
}

func withoutComments(body string) string {
	var out strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if cut := strings.Index(line, "//"); cut >= 0 {
			if strings.TrimSpace(line[:cut]) == "" {
				line = line[:cut]
			}
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

const cssFile = "src/css/"

func funcBody(t *testing.T, src, head string) string {
	t.Helper()
	start := strings.Index(src, head)
	if start < 0 {
		t.Fatalf("%q not found — the test guards the wrong place", head)
	}
	end := strings.Index(src[start:], "\n}")
	if end < 0 {
		t.Fatalf("end of %q not found", head)
	}
	return src[start : start+end]
}

func pyBlock(t *testing.T, file, body, head string) string {
	t.Helper()
	i := strings.Index(body, head)
	if i < 0 {
		t.Fatalf("%s has no %q", file, head)
	}
	rest := body[i:]
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		t.Fatalf("%s: end of the %q header not found", file, head)
	}
	rest = rest[nl+1:]
	j := regexp.MustCompile(`(?m)^\S`).FindStringIndex(rest)
	if j == nil {
		return rest
	}
	return rest[:j[0]]
}

func jsBlock(t *testing.T, file, body, head string) string {
	t.Helper()
	i := strings.Index(body, head)
	if i < 0 {
		t.Fatalf("%s has no %q", file, head)
	}
	rest := body[i:]
	j := strings.Index(rest, "\n}")
	if j < 0 {
		t.Fatalf("%s: end of %q not found", file, head)
	}
	return rest[:j]
}

func paragraphLine(t *testing.T, file, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "out.push(html") && strings.Contains(line, "<p>") {
			return line
		}
	}
	t.Fatalf("%s has no paragraph assembly — the test is useless", file)
	return ""
}

func cssBlock(t *testing.T, css, selector string) string {
	t.Helper()
	key := selector + " {"
	i := strings.Index(css, key)
	if i < 0 {
		t.Fatalf("%s has no %q rule", cssFile, selector)
	}
	rest := css[i+len(key):]
	j := strings.Index(rest, "}")
	if j < 0 {
		t.Fatalf("%s: end of the %q rule not found", cssFile, selector)
	}
	return rest[:j]
}

func actionBlock(src, name string) string {
	start := strings.Index(src, `    "`+name+`":`)
	if start < 0 {
		return ""
	}
	rest := src[start+len(name)+8:]
	if next := regexp.MustCompile(`(?m)^    "[a-z]+\.[a-zA-Z]+":`).FindStringIndex(rest); next != nil {
		return rest[:next[0]]
	}
	return rest
}

func cssWithoutComments(css string) string {
	var b strings.Builder
	for i := 0; i < len(css); i++ {
		if strings.HasPrefix(css[i:], "/*") {
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				break
			}
			b.WriteString(strings.Repeat("\n", strings.Count(css[i:i+2+end], "\n")))
			i += end + 3
			continue
		}
		b.WriteByte(css[i])
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func cssBlockFile(t *testing.T, path, selector string) string {
	t.Helper()
	raw, err := os.ReadFile(webPath(path))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	css := cssWithoutComments(string(raw))
	at := strings.Index(css, selector+" {")
	if at < 0 {
		return ""
	}
	rest := css[at:]
	if end := strings.Index(rest, "}"); end > 0 {
		return rest[:end]
	}
	return rest
}

func jsUntil(t *testing.T, file, body, head, tail string) string {
	t.Helper()
	i := strings.Index(body, head)
	if i < 0 {
		t.Fatalf("%s has no %q — the test is useless, check the anchor", file, head)
	}
	rest := body[i:]
	j := strings.Index(rest, tail)
	if j < 0 {
		t.Fatalf("%s: end of %q not found (looked for %q)", file, head, tail)
	}
	return rest[:j]
}

func stripHTMLComments(page string) string {
	for {
		start := strings.Index(page, "<!--")
		if start < 0 {
			return page
		}
		end := strings.Index(page[start:], "-->")
		if end < 0 {
			return page[:start]
		}
		page = page[:start] + page[start+end+3:]
	}
}

func arrowFn(t *testing.T, file, body, head string) string {
	t.Helper()
	at := strings.Index(body, head)
	if at < 0 {
		t.Fatalf("%s has no %q", file, head)
	}
	rest := body[at:]
	end := strings.Index(rest, "\n    };")
	if end < 0 {
		t.Fatalf("%s: end of %q not found", file, head)
	}
	return rest[:end]
}
