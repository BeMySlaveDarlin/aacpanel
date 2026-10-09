package contours

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWithoutRegistryIsPersonalOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(RegistryEnv, filepath.Join(home, "no-such-file"))

	list := Load()
	if len(list) != 1 || list[0].Profile != "personal" || list[0].Prefix != "*" ||
		list[0].Config != filepath.Join(home, ".claude") {
		t.Fatalf("with no registry one personal contour was expected, got %+v", list)
	}
}

func TestLoadReadsRegistryOnlyFromEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv(RegistryEnv)

	reg := filepath.Join(home, ".claude-contours", "registry.conf")
	if err := os.MkdirAll(filepath.Dir(reg), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "work | " + filepath.Join(home, "Labs") + "/ | " + filepath.Join(home, "work-config") + " | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	list := Load()
	if len(list) != 1 || list[0].Config != filepath.Join(home, ".claude") {
		t.Fatalf("the registry was read past %s — the layout of the contours came from a file nobody named: %+v", RegistryEnv, list)
	}
}

func TestLoadParsesRegistryInFileOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reg := filepath.Join(home, "registry.conf")
	body := "# profile | prefix | config | token\n" +
		"\n" +
		"work     | " + filepath.Join(home, "Labs") + "/ | " + filepath.Join(home, ".claude-contours", "work") + " | ~/.vault/work.age\n" +
		"acme   | " + filepath.Join(home, "Acme") + "/ | ~/.claude-contours/acme | ~/.vault/acme.age\n" +
		"personal | *                                   | ~/.claude | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RegistryEnv, reg)

	list := Load()
	if len(list) != 3 {
		t.Fatalf("%d contours, expected 3: %+v", len(list), list)
	}
	if list[0].Profile != "work" || list[1].Profile != "acme" || list[2].Profile != "personal" {
		t.Fatalf("the order is not kept (the first matching prefix has to win): %+v", list)
	}
	if list[2].Prefix != "*" {
		t.Errorf("the default profile lost %q: %+v", "*", list[2])
	}
	if list[1].Token != filepath.Join(home, ".vault", "acme.age") || list[2].Token != "" {
		t.Errorf("the token files are %q and %q", list[1].Token, list[2].Token)
	}
}

func TestLoadExpandsTildeInConfigOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reg := filepath.Join(home, "registry.conf")
	body := "work | ~/Labs/ | ~/.claude-contours/work | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RegistryEnv, reg)

	list := Load()
	if len(list) != 1 {
		t.Fatalf("%d contours, expected 1: %+v", len(list), list)
	}
	if list[0].Config != filepath.Join(home, ".claude-contours", "work") {
		t.Errorf("the tilde in config is not expanded: %+v", list[0])
	}
	if list[0].Prefix != "~/Labs/" {
		t.Errorf("the tilde in prefix is expanded and should not have been: %+v", list[0])
	}
}

func TestLoadSkipsCommentsBlankAndMalformedLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reg := filepath.Join(home, "registry.conf")
	body := "# a comment\n\n   \na field with no separators\nwork | " +
		filepath.Join(home, "Labs") + "/ | " + filepath.Join(home, ".claude-contours", "work") + " | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RegistryEnv, reg)

	list := Load()
	if len(list) != 1 || list[0].Profile != "work" {
		t.Fatalf("the junk lines are not filtered out: %+v", list)
	}
}

func TestConfigDirsTakeHostEnvFirstThenRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	reg := filepath.Join(home, "registry.conf")
	body := "work   | " + home + "/Labs/ | ~/.claude-contours/work | -\n" +
		"personal | * | ~/.claude | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RegistryEnv, reg)
	t.Setenv(HomeEnv, filepath.Join(home, ".claude")+string(os.PathListSeparator)+
		filepath.Join(home, ".claude-contours", "acme"))

	got := ConfigDirs()
	want := []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".claude-contours", "acme"),
		filepath.Join(home, ".claude-contours", "work"),
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the directories of the contours:\n got %v\n expected %v", got, want)
	}
}

func TestConfigDirsFallBackToRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv(HomeEnv)

	reg := filepath.Join(home, "registry.conf")
	body := "work | " + home + "/Labs/ | ~/.claude-contours/work | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RegistryEnv, reg)

	got := ConfigDirs()
	want := []string{filepath.Join(home, ".claude"), filepath.Join(home, ".claude-contours", "work")}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the directories of the contours:\n got %v\n expected %v", got, want)
	}
}

func TestConfigDirsAreOneWhenNothingIsDescribed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv(HomeEnv)
	t.Setenv(RegistryEnv, filepath.Join(home, "no-such-file"))

	got := ConfigDirs()
	if len(got) != 1 || got[0] != filepath.Join(home, ".claude") {
		t.Errorf("on an install with one account got %v", got)
	}
}

func TestConfigDirsSayEachDirOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reg := filepath.Join(home, "registry.conf")
	if err := os.WriteFile(reg, []byte("personal | * | ~/.claude | -\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RegistryEnv, reg)
	t.Setenv(HomeEnv, filepath.Join(home, ".claude"))

	if got := ConfigDirs(); len(got) != 1 {
		t.Errorf("the directory is named twice: %v", got)
	}
}

func TestCodexHomesDefaultToTheCodexDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, value := range []string{"", "  "} {
		t.Setenv(CodexHomesEnv, value)
		got := CodexHomes()
		if len(got) != 1 || got[0].Dir != filepath.Join(home, ".codex") || got[0].Contour != Personal {
			t.Errorf("with %s=%q the homes are %+v", CodexHomesEnv, value, got)
		}
	}
	os.Unsetenv(CodexHomesEnv)
	if got := CodexHomes(); len(got) != 1 || got[0].Dir != filepath.Join(home, ".codex") {
		t.Errorf("with %s unset the homes are %+v", CodexHomesEnv, got)
	}
}

func TestCodexHomesAreNamedLikeClaudeContours(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CodexHomesEnv, strings.Join([]string{
		"~/.codex", "~/.codex-profiles/acme", "", filepath.Join(home, ".codex-work") + "/", "~/.codex/",
	}, string(os.PathListSeparator)))

	got := CodexHomes()
	want := []CodexHome{
		{Dir: filepath.Join(home, ".codex"), Contour: Personal},
		{Dir: filepath.Join(home, ".codex-profiles", "acme"), Contour: "acme"},
		{Dir: filepath.Join(home, ".codex-work"), Contour: "codex-work"},
	}
	if len(got) != len(want) {
		t.Fatalf("the homes are %+v, expected %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("home %d is %+v, expected %+v", i, got[i], want[i])
		}
	}
}

// The codex home of a project is found by its contour: the registry's name for
// the claude config directory of the contour, or the name of the directory
// itself where the registry holds none — the same name a codex home is given.
// A contour that names no codex home has none, and says which it is.
func TestTheCodexHomeOfAContourIsTheOneNamedAfterIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reg := filepath.Join(home, "registry.conf")
	body := "algo   | /srv/algo/ | ~/.claude-profiles/algo | -\n" +
		"acme   | /srv/acme/ | ~/.claude-profiles/client | -\n" +
		"personal | *        | ~/.claude | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RegistryEnv, reg)
	t.Setenv(CodexHomesEnv, strings.Join([]string{"~/.codex", "~/.codex-profiles/acme", "~/.codex-profiles/lab"},
		string(os.PathListSeparator)))

	for configDir, want := range map[string]string{
		"":                                  filepath.Join(home, ".codex"),
		filepath.Join(home, ".claude"):      filepath.Join(home, ".codex"),
		"~/.claude-profiles/client":         filepath.Join(home, ".codex-profiles", "acme"),
		filepath.Join(home, "x", ".lab"):    filepath.Join(home, ".codex-profiles", "lab"),
		filepath.Join(home, ".claude-algo"): "",
		"~/.claude-profiles/algo":           "",
	} {
		got, ok := CodexHomeOf(configDir)
		if ok != (want != "") || got.Dir != want {
			t.Errorf("the codex home of %q is %+v (%v), expected %q", configDir, got, ok, want)
		}
	}
	if got, _ := CodexHomeOf("~/.claude-profiles/algo"); got.Contour != "algo" {
		t.Errorf("a contour with no codex home is named %q", got.Contour)
	}
}
