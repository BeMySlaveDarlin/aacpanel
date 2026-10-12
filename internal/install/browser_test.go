package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserIsOneQuestionOffByDefaultAndHostOnly(t *testing.T) {
	m := desktop()
	m.Path["google-chrome"] = "/usr/local/bin/google-chrome"
	s := survey(m, nil)
	qs := s.Questions(BlockW)
	if len(qs) != 1 || qs[0].Default != "off" || len(qs[0].Options) != 2 {
		t.Fatalf("browser questions: %+v", qs)
	}
	q := qs[0]
	s.Set(q, "/usr/local/bin/google-chrome", "test")
	env := map[string]string{}
	for _, v := range s.HostEnv() {
		env[v.Key] = v.Value
	}
	if env["AACP_BROWSER"] != "1" || env["AACP_CHROME"] != "/usr/local/bin/google-chrome" {
		t.Fatalf("host browser settings: %+v", env)
	}
	s.Set(q, "off", "test")
	for _, v := range s.HostEnv() {
		if v.Key == "AACP_BROWSER" && v.Value != "0" {
			t.Fatal("off is not persisted")
		}
	}
	for _, target := range q.Writes {
		if target.File != HostEnvFile {
			t.Fatalf("browser setting is not host-only: %+v", target)
		}
	}
	delete(m.Path, "google-chrome")
	missing := survey(m, nil).Questions(BlockW)
	if len(missing) != 1 || missing[0].Default != "off" || len(missing[0].Options) != 1 {
		t.Fatal("missing Chrome changed the off default")
	}
}

func TestBrowserUpgradeKeepsConfiguredWrapper(t *testing.T) {
	m := desktop()
	m.Files[DefaultStateDir+"/host.env"] = "AACP_BROWSER=1\nAACP_CHROME=/custom/chrome-wrapper\n"
	s := survey(m, nil)
	// Earlier settings are read through the same path as the other questions.
	s.was.host = map[string]string{"AACP_BROWSER": "1", "AACP_CHROME": "/custom/chrome-wrapper"}
	q := s.browserQuestion()
	if q.Default != "/custom/chrome-wrapper" {
		t.Fatalf("wrapper lost: %+v", q)
	}
	if !strings.Contains(q.Note, "public websites") {
		t.Fatal("the browser boundary is absent from its question")
	}
}

func TestBrowserStateIsRecordedKeptAndPurgedOnlyAsChosen(t *testing.T) {
	g := newRig(t)
	if err := g.r.Do(g.in.browserStateStep()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(g.home, ".local", "state", "aacpanel", "browser")
	secret := filepath.Join(path, "profile-cookie")
	if err := os.WriteFile(secret, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadManifest(g.r.Manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Target == path && metaHas(e.Meta, "browser") && metaHas(e.Meta, "data") {
			found = true
		}
	}
	if !found {
		t.Fatal("browser data is absent from the manifest")
	}
	rm := NewRemoval(g.m, g.in.f(), entries, "")
	if err := rm.files(g.r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("uninstall erased an unselected profile")
	}
	if err := rm.Choose([]string{"exec"}, rm.Host, false); err != nil {
		t.Fatal(err)
	}
	if err := rm.data(g.r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("selected browser data remains")
	}
}
