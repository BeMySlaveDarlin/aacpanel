package check

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNoBacktickInsideMarkup(t *testing.T) {
	for path, body := range srcFiles(t) {
		for n, line := range strings.Split(body, "\n") {
			cut := strings.Index(line, "<!--")
			if cut < 0 {
				cut = 0
			}
			if !strings.Contains(line, "<!--") && !inMarkupComment(body, n) {
				continue
			}
			if strings.Contains(line[cut:], "`") {
				t.Errorf("%s:%d: a backtick inside an html comment closes the template — %s",
					path, n+1, strings.TrimSpace(line))
			}
		}
	}
}

func TestShellNeverNamesOneMachine(t *testing.T) {
	manifest, err := os.ReadFile(webPath("manifest.webmanifest"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(manifest, &meta); err != nil {
		t.Fatalf("the manifest does not parse: %v", err)
	}
	for _, key := range []string{"name", "short_name", "description"} {
		value, _ := meta[key].(string)
		if value == "" {
			t.Errorf("the manifest has no %s", key)
			continue
		}
		if strings.Contains(value, "Atlas") || strings.Contains(value, "your host") {
			t.Errorf("the manifest names the panel after one machine (%s = %q): on a colleague's "+
				"home screen the app carries someone else's host name", key, value)
		}
	}
	if desc, _ := meta["description"].(string); strings.Contains(strings.ToLower(desc), "power") {
		t.Error("the manifest promises power: there is no such screen, and a person goes looking for it")
	}

	page, err := os.ReadFile(webPath("app.html"))
	if err != nil {
		t.Fatalf("app.html: %v", err)
	}
	if markup := stripHTMLComments(string(page)); strings.Contains(markup, "Atlas") || strings.Contains(markup, "your host") {
		t.Error("the tab title is named after one machine: on someone else's install the tab " +
			"and the app window call someone else's host")
	}

	for path, body := range srcFiles(t) {
		code := stripComments(body)
		if strings.Contains(code, "Atlas") || strings.Contains(code, "your host") {
			t.Errorf("%s names the machine in code — on someone else's install that is another host", path)
		}
	}

	app := stripComments(srcFiles(t)["src/app.js"])
	if !strings.Contains(app, "document.title = fresh.hostName") {
		t.Error("the tab does not name the machine being watched: the name arrives in every " +
			"snapshot, and not using it leaves the panel nameless")
	}
	if !strings.Contains(app, "if (fresh.hostName)") {
		t.Error("the title is set without checking the name: a snapshot without hostName would wipe the tab title")
	}
}
