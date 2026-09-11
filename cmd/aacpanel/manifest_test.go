package main

import (
	"encoding/json"
	"html/template"
	"os"
	"strings"
	"testing"

	"aacpanel/web"
)

func TestManifestCarriesMachineName(t *testing.T) {
	body, err := web.FS.ReadFile("manifest.webmanifest")
	if err != nil {
		t.Fatalf("the manifest: %v", err)
	}

	named, err := renameManifest(body, "STAND-01")
	if err != nil {
		t.Fatalf("the rename: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(named, &meta); err != nil {
		t.Fatalf("the renamed manifest does not parse: %v", err)
	}
	if meta["name"] != "STAND-01" || meta["short_name"] != "STAND-01" {
		t.Errorf("the application name did not become the machine name: name=%v short_name=%v",
			meta["name"], meta["short_name"])
	}
	if desc, _ := meta["description"].(string); strings.Contains(desc, "STAND-01") {
		t.Error("the machine name went into the description as well")
	}
	if icons, ok := meta["icons"].([]any); !ok || len(icons) == 0 {
		t.Error("the rename lost the icons — the application will install without a badge")
	}
	if meta["display"] != "standalone" || meta["start_url"] != "/app" {
		t.Error("the rename touched what it has no business with")
	}
}

func TestManifestNameSurvivesPunctuation(t *testing.T) {
	body, err := web.FS.ReadFile("manifest.webmanifest")
	if err != nil {
		t.Fatalf("the manifest: %v", err)
	}
	const tricky = `home & office "main"`
	named, err := renameManifest(body, tricky)
	if err != nil {
		t.Fatalf("the rename: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(named, &meta); err != nil {
		t.Fatalf("a manifest with such a name does not parse (%v) — the browser will not read it either", err)
	}
	if meta["name"] != tricky {
		t.Errorf("the name arrived distorted: %q instead of %q", meta["name"], tricky)
	}
}

func TestShellNameFallsBackToPanel(t *testing.T) {
	if got := (&Server{hostName: "STAND-01"}).shellName(); got != "STAND-01" {
		t.Errorf("the machine name did not reach the shell: %q", got)
	}
	for _, empty := range []string{"", "   "} {
		if got := (&Server{hostName: empty}).shellName(); got != panelName {
			t.Errorf("without a machine name the shell is called %q, and it has to be the panel name", got)
		}
	}
}

func TestShellTemplateNamesTheTab(t *testing.T) {
	tmpl, err := template.ParseFS(web.FS, "*.html")
	if err != nil {
		t.Fatalf("the templates did not parse: %v", err)
	}
	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, "app.html", map[string]any{"Host": "STAND-01"}); err != nil {
		t.Fatalf("the shell did not render: %v", err)
	}
	if !strings.Contains(out.String(), "<title>STAND-01</title>") {
		t.Error("the machine name did not get into the tab title")
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		for _, v := range []string{host, strings.ToUpper(host), strings.ToLower(host)} {
			if strings.Contains(out.String(), v) {
				t.Errorf("the shell still carries the name of the machine it was built on: %s", v)
			}
		}
	}

	source, err := os.ReadFile("pages.go")
	if err != nil {
		t.Fatalf("pages.go: %v", err)
	}
	body := string(source)
	calls := strings.Count(body, `render(w, "app.html"`)
	if calls == 0 {
		t.Fatal("nobody renders the shell — there is nothing to check")
	}
	if withHost := strings.Count(body, `render(w, "app.html", map[string]any{"Host": `); withHost != calls {
		t.Errorf("the shell is rendered %d times and the name is passed %d: the tab will stay unnamed "+
			"where the key was forgotten, and a person will see it, not a test run", calls, withHost)
	}
}
