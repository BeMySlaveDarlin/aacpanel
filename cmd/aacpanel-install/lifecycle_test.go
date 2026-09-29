package main

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/install"
)

// here is a machine for the lifecycle commands: an installer's directory in
// a temporary tree, and the user the test runs as.
func here(t *testing.T, mode install.Mode, traces ...string) (env, *bytes.Buffer, install.Facts) {
	t.Helper()
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(me.Uid)
	root := t.TempDir()
	repo, _ := filepath.Abs(filepath.Join("..", ".."))
	t.Setenv(cloneEnv, repo)
	t.Setenv(cacheEnv, filepath.Join(root, "cache"))
	f := install.Facts{Account: install.Account{Name: me.Username, UID: uid, Home: root}, Clone: repo,
		InstallDir: filepath.Join(root, "aacpanel-install"), StateDir: filepath.Join(root, "state"), Mode: mode, Traces: traces}
	var out bytes.Buffer
	return env{stdout: &out, stderr: &out, terminal: func() bool { return false },
		inspect: func(string) install.Inspection { return install.Inspection{Facts: f} },
		machine: &install.Table{Acct: f.Account, Disk: root}}, &out, f
}

// TestUninstallWithoutAManifestRefuses: the manifest is the one account of
// the install; without it nothing is taken, and what the machine holds of
// the panel is named.
func TestUninstallWithoutAManifestRefuses(t *testing.T) {
	e, out, f := here(t, install.Adopt, "/var/lib/aacpanel/host.env", "compose project aacpanel: aacpanel, aacpanel-db")
	for _, args := range [][]string{{"uninstall", "--yes", "--purge-data"}, {"uninstall", "--dry-run"}} {
		out.Reset()
		status := runWith(args, e)
		said := out.String()
		if status != 1 || !strings.Contains(said, "stop: ~/aacpanel-install/manifest.tsv is not here") ||
			!strings.Contains(said, "compose project aacpanel: aacpanel, aacpanel-db") {
			t.Errorf("%q: status %d:\n%s", args, status, said)
		}
		if _, err := os.Stat(f.InstallDir); !os.IsNotExist(err) {
			t.Errorf("%q made the installer's directory", args)
		}
	}
}

// TestADryUninstallChangesNothing shows the plan and what root.sh would
// run, and leaves the manifest where it is.
func TestADryUninstallChangesNothing(t *testing.T) {
	e, out, f := here(t, install.Upgrade)
	m, err := install.OpenManifest(f.InstallDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []install.Entry{
		{Step: "root", Kind: "linger", Target: f.Account.Name, Meta: "by-installer"},
		{Step: "root", Kind: "sysunit", Target: "/etc/systemd/system/aacpanel-agent@.service", Meta: "created sha=1"},
		{Step: "compose", Kind: "compose", Target: "aacpanel", Meta: "project"},
	} {
		if err := m.Append(l); err != nil {
			t.Fatal(err)
		}
	}
	status := runWith([]string{"uninstall", "--dry-run", "--purge-state"}, e)
	said := out.String()
	for _, w := range []string{"Uninstall plan", "linger for " + f.Account.Name + ", which the installer turned on — last",
		"Root part — what root.sh would run", "Nothing on this machine was changed: --dry-run only looks."} {
		if !strings.Contains(said, w) {
			t.Errorf("a dry uninstall does not say %q:\n%s", w, said)
		}
	}
	if status != 0 {
		t.Errorf("status %d", status)
	}
	if _, err := os.Stat(m.Path); err != nil {
		t.Errorf("the manifest went: %v", err)
	}
}

// TestUninstallWithoutATerminalTakesYes: the question has no one to answer
// it, and --yes is named.
func TestUninstallWithoutATerminalTakesYes(t *testing.T) {
	e, out, f := here(t, install.Upgrade)
	if _, err := install.OpenManifest(f.InstallDir); err != nil {
		t.Fatal(err)
	}
	if status := runWith([]string{"uninstall"}, e); status != 1 || !strings.Contains(out.String(), `no terminal to ask "Remove the panel?"; pass --yes`) {
		t.Errorf("status %d:\n%s", status, out.String())
	}
}

// TestAnUpdateThatMovedHandsOver: the installer of the new tree carries on,
// told where the clone was; one that did not move goes on as install.
func TestAnUpdateThatMovedHandsOver(t *testing.T) {
	e, _, f := here(t, install.Upgrade)
	var gotTo int
	var argv, envs []string
	e.move = func(clone string, to int) (install.Moved, error) {
		gotTo = to
		return install.Moved{From: "aaaaaaa1", To: "bbbbbbb2", Says: "v1.0.0 → v1.1.0"}, nil
	}
	e.handover = func(a, en []string) error { argv, envs = a, en; return nil }
	t.Setenv(install.UpdatedEnv, "")
	os.Unsetenv(install.UpdatedEnv)
	if status := runWith([]string{"update", "--to", "1", "--yes", "--plain"}, e); status != 0 {
		t.Fatalf("status %d", status)
	}
	if gotTo != 1 || !slices.Equal(argv, []string{filepath.Join(f.Clone, "install.sh"), "update", "--to", "1", "--yes", "--plain"}) ||
		!slices.Contains(envs, install.UpdatedEnv+"=aaaaaaa1") {
		t.Errorf("to %d, handed over %q with %v", gotTo, argv, slices.ContainsFunc(envs, func(s string) bool { return strings.HasPrefix(s, install.UpdatedEnv) }))
	}
}

// TestCheckWantsAnInstall: a machine with no trace of the panel has no
// chain to check.
func TestCheckWantsAnInstall(t *testing.T) {
	e, out, _ := here(t, install.Fresh)
	if status := runWith([]string{"check"}, e); status != 1 || !strings.Contains(out.String(), "there is no install of the panel here") {
		t.Errorf("status %d:\n%s", status, out.String())
	}
}
