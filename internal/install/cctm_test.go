package install

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCCTMShimInstallation(t *testing.T) {
	for _, tc := range []struct {
		name, settings, foreign string
		want                    bool
	}{
		{"enabled", `{"enabledPlugins":{"cc-task-manager@local":true}}`, "", true},
		{"disabled", `{"enabledPlugins":{"cc-task-manager@local":false}}`, "", false},
		{"absent", `{}`, "", false},
		{"another plugin", `{"enabledPlugins":{"cc-task-manager-extra@local":true}}`, "", false},
		{"foreign file", `{"enabledPlugins":{"cc-task-manager@local":true}}`, "file", false},
		{"foreign link", `{"enabledPlugins":{"cc-task-manager@local":true}}`, "link", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newRig(t, homeFile(".claude-extra/settings.json", tc.settings))
			path := g.in.cctmPath()
			if tc.foreign != "" {
				g.write(g.in.execPath(), "executor", 0o755)
				if tc.foreign == "file" {
					g.write(path, "another command", 0o755)
				} else if err := os.Symlink("absent-command", path); err != nil {
					t.Fatal(err)
				}
			}
			if err := g.do(g.step("cctm")); err != nil {
				t.Fatal(err)
			}
			if tc.want {
				to, err := os.Readlink(path)
				if err != nil || to != "aacpanel-exec" {
					t.Fatalf("shim link: %q, %v", to, err)
				}
				if !slices.Contains(g.lines(), "file "+path+" created") {
					t.Fatalf("shim is absent from manifest: %v", g.lines())
				}
			} else if tc.foreign == "file" {
				raw, err := os.ReadFile(path)
				if err != nil || string(raw) != "another command" {
					t.Fatalf("foreign command changed: %q, %v", raw, err)
				}
			} else if tc.foreign == "link" {
				to, err := os.Readlink(path)
				if err != nil || to != "absent-command" {
					t.Fatalf("foreign link changed: %q, %v", to, err)
				}
			} else if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("shim installed without the plugin: %v", err)
			}
			before := slices.Clone(g.lines())
			if !g.already(g.step("cctm")) || !reflect.DeepEqual(before, g.lines()) {
				t.Fatalf("repeat changed the manifest: %v", g.lines())
			}
			if tc.want {
				entries, err := ReadManifest(g.r.Manifest.Path)
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range entries {
					if e.Target == path {
						if err := UndoKind(g.r, e); err != nil {
							t.Fatal(err)
						}
					}
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("uninstall left the shim: %v", err)
				}
			}
		})
	}
}

func TestCCTMShimAcceptsAnExistingExecutorLink(t *testing.T) {
	g := newRig(t, homeFile(".claude/settings.json", `{"enabledPlugins":{"cc-task-manager@local":true}}`))
	g.write(g.in.execPath(), "executor", 0o755)
	if err := os.Symlink(g.in.execPath(), g.in.cctmPath()); err != nil {
		t.Fatal(err)
	}
	if !g.already(g.step("cctm")) || len(g.lines()) != 0 {
		t.Fatalf("existing shim changed: %v", g.lines())
	}
	to, err := os.Readlink(g.in.cctmPath())
	if err != nil || !strings.HasSuffix(to, filepath.Join("bin", "aacpanel-exec")) {
		t.Fatalf("executor link: %q, %v", to, err)
	}
}
