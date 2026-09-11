package hostcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noDescription(t *testing.T) {
	t.Helper()
	t.Setenv(PathEnv, filepath.Join(t.TempDir(), "no-description.env"))
	t.Setenv(ProjectRootsEnv, "")
}

func TestProjectRootsTakeTheDescriptionThenHome(t *testing.T) {
	t.Run("the default is the home directory alone", func(t *testing.T) {
		noDescription(t)
		if got := ProjectRoots("/home/u"); len(got) != 1 || got[0] != "/home/u" {
			t.Errorf("the default roots are %v, expected the home directory alone", got)
		}
	})

	t.Run("neither a setting nor a home directory", func(t *testing.T) {
		noDescription(t)
		if got := ProjectRoots(""); len(got) != 0 {
			t.Errorf("roots %v where there is nowhere to take them from", got)
		}
	})

	t.Run("the setting outranks the home directory", func(t *testing.T) {
		noDescription(t)
		t.Setenv(ProjectRootsEnv, "/srv/proj: /srv/work :")
		got := ProjectRoots("/home/u")
		if len(got) != 2 || got[0] != "/srv/proj" || got[1] != "/srv/work" {
			t.Errorf("roots %v: expected two, with no empty pieces and no home directory", got)
		}
	})

	t.Run("the description file instead of the variable", func(t *testing.T) {
		noDescription(t)
		file := filepath.Join(t.TempDir(), "host.env")
		if err := os.WriteFile(file, []byte("# the description\n"+ProjectRootsEnv+"=/srv/proj\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv(PathEnv, file)

		if got := ProjectRoots("/home/u"); len(got) != 1 || got[0] != "/srv/proj" {
			t.Errorf("roots %v — the description was not read", got)
		}
		t.Setenv(ProjectRootsEnv, "/srv/work")
		if got := ProjectRoots("/home/u"); len(got) != 1 || got[0] != "/srv/work" {
			t.Errorf("roots %v — the environment variable lost to the file", got)
		}
	})
}

func TestProjectRootsHaveOneOwner(t *testing.T) {
	root := filepath.Join("..", "..")
	users := map[string]string{
		filepath.Join("cmd", "aacpanel", "main.go"):         "the service",
		filepath.Join("internal", "executor", "session.go"): "the executor",
	}
	for rel, who := range users {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "hostcfg.ProjectRoots(") {
			t.Errorf("%s (%s) works the roots out on its own, past hostcfg.ProjectRoots — the defaults will part silently", rel, who)
		}
	}

	literal := `"` + ProjectRootsEnv + `"`
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "dist", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !strings.HasSuffix(d.Name(), ".go") || rel == filepath.Join("internal", "hostcfg", "roots.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for n, line := range codeLines(string(raw), false) {
			if strings.Contains(line, literal) {
				t.Errorf("%s:%d names %s as a literal — the name of the setting lives in internal/hostcfg/roots.go",
					rel, n+1, ProjectRootsEnv)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
