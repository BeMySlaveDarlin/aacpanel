package check

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cssFiles returns every style file of the frontend by its web-relative path,
// comments stripped so a size quoted in prose does not count as a declaration.
func cssFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(webPath("src/css"), func(path string, d os.DirEntry, err error) error {
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
		files[webRel(path)] = cssWithoutComments(string(raw))
		return nil
	})
	if err != nil {
		t.Fatalf("reading frontend styles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no frontend styles found — the test is useless, check the path")
	}
	return files
}

func lineAt(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

// cssLength matches a size written down in absolute units — the only kind
// that can drift from the scale. Values in em ride on the parent's size.
var cssLength = regexp.MustCompile(`^-?\d*\.?\d+(px|rem|pt)$`)

// The rule for the root element: it sets the pixel the scale is measured in,
// so its size is the unit of the scale, not a step on it.
const rootRule = "html, body"

// TestTypeScaleIsTheOnlyFontSize holds every font-size in the styles to the
// steps of the type scale in tokens.css. A literal size is a ninth step nobody
// declared, and two labels that mean the same thing end up a pixel apart.
func TestTypeScaleIsTheOnlyFontSize(t *testing.T) {
	files := cssFiles(t)
	size := regexp.MustCompile(`font-size:\s*([^;}]+)`)
	shorthand := regexp.MustCompile(`\bfont:\s*([^;}]+)`)
	step := regexp.MustCompile(`^var\(--t-[a-z0-9]+\)$`)
	relative := regexp.MustCompile(`^-?\d*\.?\d+em$`)
	seen := 0

	for _, path := range sortedKeys(files) {
		css := files[path]
		for _, m := range size.FindAllStringSubmatchIndex(css, -1) {
			seen++
			value := strings.TrimSpace(css[m[2]:m[3]])
			if step.MatchString(value) || value == "inherit" || relative.MatchString(value) {
				continue
			}
			t.Errorf("%s:%d: font-size %s is not a step of the type scale — a size off the scale puts two labels of one meaning a pixel apart",
				path, lineAt(css, m[0]), value)
		}
		for _, m := range shorthand.FindAllStringSubmatchIndex(css, -1) {
			selector := css[strings.LastIndex(css[:m[0]], "}")+1 : m[0]]
			if strings.Contains(selector, rootRule) {
				continue
			}
			seen++
			for word := range strings.FieldsSeq(css[m[2]:m[3]]) {
				word = strings.SplitN(word, "/", 2)[0]
				if cssLength.MatchString(word) {
					t.Errorf("%s:%d: the font shorthand carries a literal size %s — it steps off the type scale the same way a font-size would",
						path, lineAt(css, m[0]), word)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no font-size declarations found — the test is useless, check the pattern")
	}
}

// TestIconSizesComeFromTheScale holds every svg sized by the styles to the
// icon tokens. An icon a pixel off its neighbours reads as a different kind of
// button, and the eye stops on it.
func TestIconSizesComeFromTheScale(t *testing.T) {
	files := cssFiles(t)
	rule := regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	dimension := regexp.MustCompile(`\b(width|height):\s*([^;}]+)`)
	token := regexp.MustCompile(`^var\(--i-[a-z]+\)$`)
	seen := 0

	for _, path := range sortedKeys(files) {
		css := files[path]
		for _, m := range rule.FindAllStringSubmatchIndex(css, -1) {
			selector := strings.TrimSpace(css[m[2]:m[3]])
			if !sizesSVG(selector) {
				continue
			}
			at := m[3] - len(strings.TrimLeft(css[m[2]:m[3]], " \t\n"))
			body := css[m[4]:m[5]]
			for _, d := range dimension.FindAllStringSubmatch(body, -1) {
				seen++
				value := strings.TrimSpace(d[2])
				if token.MatchString(value) || !cssLength.MatchString(value) {
					continue
				}
				t.Errorf("%s:%d: the icon under %q is %s %s, not an icon token — icons of one row come out in different sizes",
					path, lineAt(css, at), lastSelector(selector), value, d[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("no svg size rules found — the test is useless, check the pattern")
	}
}

// sizesSVG reports whether any selector in the list addresses an svg itself,
// as opposed to a box that merely holds one.
func sizesSVG(selector string) bool {
	for part := range strings.SplitSeq(selector, ",") {
		if strings.HasSuffix(strings.TrimSpace(part), "svg") {
			return true
		}
	}
	return false
}

func lastSelector(selector string) string {
	parts := strings.Split(selector, ",")
	return strings.TrimSpace(parts[len(parts)-1])
}
