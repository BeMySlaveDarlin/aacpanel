package hostcfg

import (
	"os"
	"path/filepath"
	"testing"
)

func clearHostEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		HostEnv, RepoEnv, UnixUserEnv, DisplayEnv, HomeSessionEnv, LangEnv,
		StateDirEnv, PathEnv,
	} {
		t.Setenv(key, "")
	}
}

func TestLoadFromMissingFileGivesDefaults(t *testing.T) {
	clearHostEnv(t)

	got := LoadFrom(filepath.Join(t.TempDir(), "no-such-file"))
	if got != defaults() {
		t.Fatalf("with no file the defaults of this machine were expected, got %+v", got)
	}
}

func TestLoadFromFileOverridesDefault(t *testing.T) {
	clearHostEnv(t)
	path := filepath.Join(t.TempDir(), "host.env")
	body := "# another machine\n" + HostEnv + "=STAGE\n" + RepoEnv + "=/srv/aacpanel\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := LoadFrom(path)
	if got.Name != "STAGE" {
		t.Errorf("host name %q, expected STAGE — the file was not substituted", got.Name)
	}
	if got.RepoRoot != "/srv/aacpanel" {
		t.Errorf("repository path %q, expected /srv/aacpanel", got.RepoRoot)
	}
	if got.UnixUser != defaults().UnixUser || got.Display != defaults().Display {
		t.Errorf("the fields that were not overridden got substituted: %+v", got)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	clearHostEnv(t)
	path := filepath.Join(t.TempDir(), "host.env")
	if err := os.WriteFile(path, []byte(HostEnv+"=FROM-FILE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HostEnv, "FROM-ENV")

	got := LoadFrom(path)
	if got.Name != "FROM-ENV" {
		t.Errorf("host name %q, expected FROM-ENV — the environment variable does not outrank the file", got.Name)
	}
}

func TestLoadUsesPathEnvWhenSet(t *testing.T) {
	clearHostEnv(t)
	path := filepath.Join(t.TempDir(), "custom.env")
	if err := os.WriteFile(path, []byte(HostEnv+"=VIA-PATHENV\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PathEnv, path)

	got := Load()
	if got.Name != "VIA-PATHENV" {
		t.Errorf("Load() did not pick up PathEnv: %+v", got)
	}
}

func TestParseEnvFileIgnoresCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.env")
	body := "# a comment\n\n" + HostEnv + "=WITH-COMMENTS\n  \n# one more\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := parseEnvFile(path)
	if len(got) != 1 || got[HostEnv] != "WITH-COMMENTS" {
		t.Errorf("parsing with comments gave %+v", got)
	}
}

func TestParseEnvFileTrimsQuotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.env")
	if err := os.WriteFile(path, []byte(HostEnv+`="QUOTED"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := parseEnvFile(path)
	if got[HostEnv] != "QUOTED" {
		t.Errorf("the quotes were not trimmed: %+v", got)
	}
}

func TestDefaultsDescribeTheMachineTheyRunOn(t *testing.T) {
	got := defaultsFor("wg-lab", "u")

	if got.Name != "wg-lab" {
		t.Errorf("host name %q — the default describes a machine other than this one", got.Name)
	}
	if got.UnixUser != "u" {
		t.Errorf("user %q: who the units are installed for and whose uid travels into the "+
			"container depend on it — somebody else's name gives an install for a user who does not exist", got.UnixUser)
	}
	if got.HomeSession != "wg-lab" {
		t.Errorf("main session %q, expected the name of the machine", got.HomeSession)
	}
	if other := defaultsFor("another", "somebody"); other.RepoRoot != got.RepoRoot ||
		other.Display != got.Display || other.Lang != got.Lang || other.StateDir != got.StateDir {
		t.Errorf("the install defaults moved along with the name of the machine: %+v against %+v", other, got)
	}
}

func TestDefaultsAskTheMachineItself(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("the machine did not name itself: %v", err)
	}
	if got := defaults(); got != defaultsFor(host, currentUser()) {
		t.Errorf("the defaults %+v are built from something other than what the machine said about itself (%q, %q)",
			got, host, currentUser())
	}
}

func TestCurrentUserSurvivesEmptyEnvironment(t *testing.T) {
	t.Setenv("USER", "")
	if got := currentUser(); got == "" {
		t.Error("with no $USER the user was not determined — os/user was not asked")
	}
}

func TestDefaultLangNeedsNoLocaleGen(t *testing.T) {
	if DefaultLang != "C.UTF-8" {
		t.Errorf("locale default %q — on a machine with no locale-gen only C.UTF-8 is there", DefaultLang)
	}
	if got := defaultsFor("wg-lab", "u").Lang; got != DefaultLang {
		t.Errorf("the description defaults give the locale %q while the constant gives %q — two defaults for one field", got, DefaultLang)
	}
}
