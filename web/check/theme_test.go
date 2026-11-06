package check

import (
	"regexp"
	"strings"
	"testing"
)

// themeTokens returns the names of the design tokens declared for the dark
// theme and, separately, those the sky theme overrides, out of every style
// file: `:root` and `.deskshell` blocks are dark, `:root.sky …` blocks are sky.
func themeTokens(t *testing.T) (dark, sky map[string]bool) {
	t.Helper()
	dark, sky = map[string]bool{}, map[string]bool{}
	css := cssWithoutComments(cssSrc(t))
	block := regexp.MustCompile(`(?m)^\s*([^{}\n]+)\{([^{}]*)\}`)
	token := regexp.MustCompile(`(--[a-z0-9-]+)\s*:`)
	for _, m := range block.FindAllStringSubmatch(css, -1) {
		selector := strings.TrimSpace(m[1])
		var into map[string]bool
		switch {
		case strings.HasPrefix(selector, ":root.sky"):
			into = sky
		case selector == ":root" || selector == ".deskshell":
			into = dark
		default:
			continue
		}
		for _, d := range token.FindAllStringSubmatch(m[2], -1) {
			into[d[1]] = true
		}
	}
	if len(dark) == 0 || len(sky) == 0 {
		t.Fatalf("theme tokens not found: %d dark, %d sky — the test is useless, check the selectors", len(dark), len(sky))
	}
	return dark, sky
}

// TestThemedSurfacesTakeTheirFillFromASkyToken holds the surfaces that used to
// stay black in the light theme: each draws its fill from a token, and that
// token has a value in the sky block. A literal colour or a token sky does not
// override is a black box on a light screen.
func TestThemedSurfacesTakeTheirFillFromASkyToken(t *testing.T) {
	dark, sky := themeTokens(t)
	cases := []struct {
		file, selector, property string
	}{
		{"src/css/sheet.css", ".toast", "background"},
		{"src/css/sheet.css", ".toast", "box-shadow"},
		{"src/css/markdown.css", ".mdcode", "background"},
		{"src/css/desktop.css", ".dktip", "background"},
	}
	value := func(block, property string) string {
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, property+":") {
				return strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, property+":")), ";")
			}
		}
		return ""
	}
	for _, c := range cases {
		block := cssBlockFile(t, c.file, c.selector)
		if block == "" {
			t.Errorf("%s has no %q rule — the surface is gone or renamed, and this check no longer holds it", c.file, c.selector)
			continue
		}
		v := value(block, c.property)
		m := regexp.MustCompile(`^var\((--[a-z0-9-]+)\)$`).FindStringSubmatch(v)
		if m == nil {
			t.Errorf("%s: %s of %s is %q, not a single theme token — the light theme cannot change it and the box stays black",
				c.file, c.property, c.selector, v)
			continue
		}
		if !dark[m[1]] {
			t.Errorf("%s: %s of %s uses %s, which no dark block declares — the box has no fill at all", c.file, c.property, c.selector, m[1])
		}
		if !sky[m[1]] {
			t.Errorf("%s: %s of %s uses %s, which the sky block does not override — the box stays black on the light theme",
				c.file, c.property, c.selector, m[1])
		}
	}
}

// TestDesktopTipIsOneNodeTheShellAttaches holds the shape of the desktop
// tooltip: one node the shell attaches the script to, which fills it for every
// data-tip. A pseudo-element under the button would be a second tooltip that
// knows nothing of the first.
func TestDesktopTipIsOneNodeTheShellAttaches(t *testing.T) {
	shell := srcFiles(t)["src/desktop/shell.js"]
	if !strings.Contains(shell, "attachTips(") || !strings.Contains(shell, `class="dktip"`) {
		t.Error("the desktop shell does not attach the tooltip script to its .dktip node — every data-tip is silent")
	}
	if strings.Contains(cssWithoutComments(cssSrc(t)), "[data-tip]::after") {
		t.Error("a [data-tip]::after rule is back — a pseudo-element tip is a second tooltip beside the one node")
	}
}
