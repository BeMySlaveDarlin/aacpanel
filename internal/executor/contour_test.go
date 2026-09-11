package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func contourHome(t *testing.T) (home, workConf string) {
	t.Helper()
	home = t.TempDir()
	workConf = filepath.Join(home, ".claude-contours", "work")
	if err := os.MkdirAll(filepath.Join(workConf, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(home, "registry.conf")
	body := "# profile | prefix | config | token\n" +
		"work     | " + filepath.Join(home, "Labs") + "/ | " + workConf + " | ~/.vault/work.age\n" +
		"personal | *                                   | ~/.claude | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv(registryEnv, reg)
	return home, workConf
}

func trustAt(t *testing.T, dir string, trusted map[string]bool) {
	t.Helper()
	type entry struct {
		Trusted bool `json:"hasTrustDialogAccepted"`
	}
	projects := map[string]entry{}
	for d, ok := range trusted {
		projects[d] = entry{Trusted: ok}
	}
	body, err := json.Marshal(map[string]any{"projects": projects})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTrustComesFromContourOfProject(t *testing.T) {
	home, workConf := contourHome(t)
	shop := filepath.Join(home, "Labs", "shop")
	if err := os.MkdirAll(shop, 0o755); err != nil {
		t.Fatal(err)
	}
	trustAt(t, home, map[string]bool{shop: false})
	trustAt(t, workConf, map[string]bool{shop: true})

	ok, err := projectTrusted(shop)
	if err != nil {
		t.Fatalf("the trust could not be read: %v", err)
	}
	if !ok {
		t.Error("the trust was taken from the personal contour — a session in Labs will never open")
	}
}

func TestTrustOutsideContourStaysPersonal(t *testing.T) {
	home, workConf := contourHome(t)
	pet := filepath.Join(home, "Beta", "aacpanel")
	if err := os.MkdirAll(pet, 0o755); err != nil {
		t.Fatal(err)
	}
	trustAt(t, home, map[string]bool{pet: true})
	trustAt(t, workConf, map[string]bool{pet: false})

	ok, err := projectTrusted(pet)
	if err != nil {
		t.Fatalf("the trust could not be read: %v", err)
	}
	if !ok {
		t.Error("a personal directory was judged by the contour of another")
	}
}

func TestTrustFollowsSymlinkLikeWrapper(t *testing.T) {
	home, workConf := contourHome(t)
	real := filepath.Join(home, "Labs", "landing")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "landing")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	trustAt(t, home, map[string]bool{real: false, link: false})
	trustAt(t, workConf, map[string]bool{real: true})

	ok, err := projectTrusted(link)
	if err != nil {
		t.Fatalf("the trust could not be read: %v", err)
	}
	if !ok {
		t.Error("the contour was decided from the link instead of the physical path")
	}
}

func TestContoursWithoutRegistryIsPersonalOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(registryEnv, filepath.Join(home, "no-such-file"))

	list := contours()
	if len(list) != 1 || list[0].Config != filepath.Join(home, ".claude") {
		t.Fatalf("without a registry there must be a single personal contour, got %+v", list)
	}
	path, err := configFileFor(filepath.Join(home, "anything"))
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".claude.json") {
		t.Errorf("the personal config is looked for outside home: %s", path)
	}
}

func TestSessionsDirsSkipsContourWithoutDirectory(t *testing.T) {
	home, workConf := contourHome(t)
	reg := filepath.Join(home, "registry.conf")
	body, err := os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, []byte("acme | "+filepath.Join(home, "Acme")+"/ | "+
		filepath.Join(home, ".claude-contours", "acme")+" | -\n")...)
	if err := os.WriteFile(reg, body, 0o644); err != nil {
		t.Fatal(err)
	}

	dirs := sessionsDirs()
	want := []string{filepath.Join(home, ".claude", "sessions"), filepath.Join(workConf, "sessions")}
	if len(dirs) != len(want) {
		t.Fatalf("session directories: %v, expected %v", dirs, want)
	}
	for i := range want {
		if dirs[i] != want[i] {
			t.Fatalf("session directories: %v, expected %v", dirs, want)
		}
	}
}
