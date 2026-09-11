package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(projectRootsEnv, root)
	return root
}

func createProject(t *testing.T, path string) (string, error) {
	t.Helper()
	return (&Executor{}).Execute(context.Background(), req(action.ProjectCreate, path))
}

func trustedIn(t *testing.T, dir string) bool {
	t.Helper()
	ok, err := projectTrusted(dir)
	if err != nil {
		t.Fatalf("the trust of directory %s could not be read: %v", dir, err)
	}
	return ok
}

func TestProjectCreateMakesTheDirectoryAndTrustsIt(t *testing.T) {
	trustFile(t, map[string]bool{})
	root := projectRoot(t)
	dir := filepath.Join(root, "panel")

	detail, err := createProject(t, dir)
	if err != nil {
		t.Fatalf("the directory was not created: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the directory is not on disk: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s was created as something other than a directory", dir)
	}
	if got := info.Mode().Perm(); got != projectDirMode {
		t.Errorf("directory mode %v, expected %v", got, os.FileMode(projectDirMode))
	}
	if !trustedIn(t, dir) {
		t.Error("trust was not granted — a session in the new project freezes on the question while the panel says the project is ready")
	}
	if !strings.Contains(detail, dir) || !strings.Contains(detail, "created") {
		t.Errorf("the reply %q does not name the directory that was created", detail)
	}
	if !strings.Contains(detail, "trust") {
		t.Errorf("the reply %q says nothing about the panel having granted the trust", detail)
	}
}

func TestProjectCreateMakesTheMissingTail(t *testing.T) {
	trustFile(t, map[string]bool{})
	root := projectRoot(t)
	group := filepath.Join(root, "Beta")
	dir := filepath.Join(group, "panel")

	detail, err := createProject(t, dir)
	if err != nil {
		t.Fatalf("the directory was not created: %v", err)
	}
	for _, path := range []string{group, dir} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("there is no directory %s: %v", path, err)
		}
		if !strings.Contains(detail, path) {
			t.Errorf("the reply %q does not name the created directory %s — a typo in the path cannot be spotted from it", detail, path)
		}
	}
}

func TestProjectCreateLeavesAnExistingDirectoryAlone(t *testing.T) {
	trustFile(t, map[string]bool{})
	root := projectRoot(t)
	dir := filepath.Join(root, "panel")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "README.md")
	if err := os.WriteFile(file, []byte("do not touch"), 0o600); err != nil {
		t.Fatal(err)
	}

	detail, err := createProject(t, dir)
	if err != nil {
		t.Fatalf("an existing directory was rejected: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("the mode of the existing directory became %v — the panel changed it", got)
	}
	if body, err := os.ReadFile(file); err != nil || string(body) != "do not touch" {
		t.Errorf("the contents of the directory changed (%q, %v)", body, err)
	}
	if !trustedIn(t, dir) {
		t.Error("an existing directory was not granted trust — a session in it freezes on the question")
	}
	if !strings.Contains(detail, "already") {
		t.Errorf("the reply %q does not say the directory was on disk before us", detail)
	}
}

func TestProjectCreateTellsGrantedTrustFromTrustAlreadyThere(t *testing.T) {
	root := projectRoot(t)
	dir := filepath.Join(root, "panel")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	trustFile(t, map[string]bool{dir: true})

	detail, err := createProject(t, dir)
	if err != nil {
		t.Fatalf("the directory was rejected: %v", err)
	}
	if !strings.Contains(detail, "already") {
		t.Errorf("the reply %q passes off long-standing trust as trust just granted", detail)
	}
}

func TestProjectCreateKeepsTheOtherProjectsInTheConfig(t *testing.T) {
	root := projectRoot(t)
	neighbours := map[string]bool{
		filepath.Join(root, "task"):    true,
		filepath.Join(root, "shopapp"): false,
	}
	trustFile(t, neighbours)
	dir := filepath.Join(root, "panel")

	if _, err := createProject(t, dir); err != nil {
		t.Fatalf("the directory was not created: %v", err)
	}

	var cfg struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(readFile(t, configPath(t)), &cfg); err != nil {
		t.Fatal(err)
	}
	for path, was := range neighbours {
		entry, ok := cfg.Projects[path]
		if !ok {
			t.Errorf("the record of the neighbouring project %s disappeared from the contour config", path)
			continue
		}
		if entry.Trusted != was {
			t.Errorf("the trust of the neighbouring project %s became %v instead of %v", path, entry.Trusted, was)
		}
	}
	if len(cfg.Projects) != len(neighbours)+1 {
		t.Errorf("%d projects in the config, expected %d", len(cfg.Projects), len(neighbours)+1)
	}
}

func TestProjectCreateRefusesPathOutsideRoots(t *testing.T) {
	trustFile(t, map[string]bool{})
	projectRoot(t)
	outside := filepath.Join(t.TempDir(), "elsewhere")

	if _, err := createProject(t, outside); err == nil {
		t.Fatal("a directory outside the roots was created")
	} else if !strings.Contains(err.Error(), "roots") {
		t.Errorf("the refusal %q does not name the reason", err)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("a directory outside the roots showed up on disk after all")
	}
}

func TestProjectCreateRefusesLinkLeadingOutsideRoots(t *testing.T) {
	trustFile(t, map[string]bool{})
	root := projectRoot(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outward")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "outward", "panel")

	if _, err := createProject(t, dir); err == nil {
		t.Fatal("a directory behind a link pointing outward was created")
	} else if !strings.Contains(err.Error(), "outside the allowed roots") {
		t.Errorf("the refusal %q does not name the reason", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "panel")); err == nil {
		t.Error("a directory showed up outside the roots — nobody resolved the link")
	}
}

func TestProjectCreateRefusesAFileInTheWay(t *testing.T) {
	trustFile(t, map[string]bool{})
	root := projectRoot(t)
	path := filepath.Join(root, "panel")
	if err := os.WriteFile(path, []byte("i am a file"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := createProject(t, path); err == nil {
		t.Fatal("a file was taken for a project directory")
	} else if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("the refusal %q does not name the reason", err)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "i am a file" {
		t.Errorf("the file on the path was changed (%q, %v)", body, err)
	}
}

func TestProjectCreateRefusesRelativePath(t *testing.T) {
	if err := req(action.ProjectCreate, "panel").Validate(); err == nil {
		t.Fatal("a relative path passed the protocol check")
	}
}
