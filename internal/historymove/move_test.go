package historymove

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sid = "12345678-1234-4321-8765-123456789abc"

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) (string, string, string, []string) {
	t.Helper()
	from, to := filepath.Join(t.TempDir(), "old"), filepath.Join(t.TempDir(), "new")
	project := "/srv/proj/shop"
	files := []string{
		"projects/" + Slug(project) + "/" + sid + ".jsonl",
		"projects/" + Slug(project) + "/" + sid + "/subagents/agent-a.jsonl",
		"projects/" + Slug(project) + "/memory/MEMORY.md",
		"file-history/" + sid + "/edited@v1",
		"session-env/" + sid + "/env",
		"tasks/session-12345678/1.json",
		"todos/" + sid + "-agent-a.json",
		"plans/" + sid + ".md",
		"teams/" + sid + "/config.json",
		"session-models/" + sid + ".json",
	}
	for _, rel := range files {
		put(t, filepath.Join(from, rel), rel)
	}
	put(t, filepath.Join(from, "plans/word-named-plan.md"), "unrelated")
	put(t, filepath.Join(from, "projects/"+Slug(project)+"-other/unrelated.jsonl"), "unrelated")
	put(t, filepath.Join(to, files[2]), files[2])
	put(t, filepath.Join(to, "projects/"+Slug(project)+"/old.jsonl"), "already here")
	return project, from, to, files
}
func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, %v; want %q", path, got, err, want)
	}
}
func TestMoveCarriesOwnedFilesAndMergesIdenticalFiles(t *testing.T) {
	project, from, to, files := fixture(t)
	m, err := Prepare("move", project, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, rel := range files {
		assertFile(t, filepath.Join(to, rel), rel)
		if _, err := os.Stat(filepath.Join(from, rel)); !os.IsNotExist(err) {
			t.Fatalf("source remains: %s: %v", rel, err)
		}
	}
	assertFile(t, filepath.Join(from, "plans/word-named-plan.md"), "unrelated")
	assertFile(t, filepath.Join(to, "projects/"+Slug(project)+"/old.jsonl"), "already here")
	if err := Pending([]string{from, to}, project); err == nil {
		t.Fatal("a launch during the map commit was admitted")
	}
	if err := Finish("move", project, from, to, true); err != nil {
		t.Fatal(err)
	}
	if err := Pending([]string{from, to}, project); err != nil {
		t.Fatal(err)
	}
}
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		raw, err := os.ReadFile(path)
		out[rel] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestMoveClashChangesNothing(t *testing.T) {
	project, from, to, files := fixture(t)
	put(t, filepath.Join(to, files[3]), "a different snapshot")
	old, new := snapshot(t, from), snapshot(t, to)
	_, err := Prepare("move", project, from, to)
	if err == nil || !strings.Contains(err.Error(), "clash") {
		t.Fatalf("clash: %v", err)
	}
	if !reflect.DeepEqual(old, snapshot(t, from)) || !reflect.DeepEqual(new, snapshot(t, to)) {
		t.Fatal("a refused merge changed files")
	}
}
func TestMoveRollbackRestoresBothAccounts(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "map failure", true: "late live session"}[late], func(t *testing.T) {
			project, from, to, _ := fixture(t)
			old, new := snapshot(t, from), snapshot(t, to)
			m, err := Prepare("move", project, from, to)
			if err != nil {
				t.Fatal(err)
			}
			err = m.Apply(t.Context(), func() error {
				if late {
					return errors.New("session opened")
				}
				return nil
			})
			if late {
				if err == nil {
					t.Fatal("late refusal ignored")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := Finish("move", project, from, to, false); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(old, snapshot(t, from)) || !reflect.DeepEqual(new, snapshot(t, to)) {
				t.Fatal("rollback did not restore both accounts")
			}
		})
	}
}
func TestMoveRefusesLinksIntoOtherData(t *testing.T) {
	project, from, to, files := fixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(from, "projects/"+Slug(project)+"/linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare("move", project, from, to); err == nil {
		t.Fatal("source link accepted")
	}
	if err := os.Remove(filepath.Join(from, "projects/"+Slug(project)+"/linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(to, "file-history")); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare("move", project, from, to); err == nil {
		t.Fatalf("destination link for %s accepted", files[3])
	}
}

func TestMoveRefusesFilesCreatedDuringCopy(t *testing.T) {
	project, from, to, _ := fixture(t)
	before := snapshot(t, to)
	m, err := Prepare("move", project, from, to)
	if err != nil {
		t.Fatal(err)
	}
	late := filepath.Join(from, "projects", Slug(project), "late.jsonl")
	err = m.Apply(t.Context(), func() error { put(t, late, "a late transcript"); return nil })
	if err == nil {
		t.Fatal("a new source file was silently left only in the disposable backup")
	}
	assertFile(t, late, "a late transcript")
	if !reflect.DeepEqual(before, snapshot(t, to)) {
		t.Fatal("late refusal changed the destination")
	}
}
