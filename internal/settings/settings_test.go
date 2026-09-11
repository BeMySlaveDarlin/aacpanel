package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clean(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

func find(t *testing.T, items []Item, key string) Item {
	t.Helper()
	for _, it := range items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("the setting %q is not in the list", key)
	return Item{}
}

func TestSecretValueNeverLeaves(t *testing.T) {
	t.Setenv("AACP_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("AACP_TOKEN", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "host.env")
	if err := os.WriteFile(path, []byte("AACP_SECRET=a-completely-different-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	items := Collect(path)
	secret := find(t, items, "AACP_SECRET")

	if secret.Value != "" || secret.FileValue != "" {
		t.Errorf("the value of the secret travelled into the list: value=%q fileValue=%q", secret.Value, secret.FileValue)
	}
	if !secret.Set {
		t.Error("the secret is set while the list says it is not: a person has nothing to tell a token that exists from an empty one")
	}

	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"0123456789abcdef0123456789abcdef", "a-completely-different-secret"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the secret is visible in what goes to the page: %q", leak)
		}
	}
}

func TestEmptySecretIsNotSet(t *testing.T) {
	t.Setenv("AACP_TOKEN", "")
	items := Collect(filepath.Join(t.TempDir(), "no-such-file"))

	token := find(t, items, "AACP_TOKEN")
	if token.Set {
		t.Error("an empty token is shown as set: a person will look for a door that is not there")
	}
}

func TestEnvironmentBeatsFileAndTheDisagreementIsShown(t *testing.T) {
	clean(t, "AACP_HOME_SESSION")
	t.Setenv("AACP_HOST", "from-the-environment")

	dir := t.TempDir()
	path := filepath.Join(dir, "host.env")
	if err := os.WriteFile(path, []byte("AACP_HOST=from-the-file\nAACP_HOME_SESSION=only-in-the-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	items := Collect(path)

	host := find(t, items, "AACP_HOST")
	if host.Value != "from-the-environment" || host.Source != SourceEnv {
		t.Errorf("%q from %q is in force while the environment outranks the file", host.Value, host.Source)
	}
	if host.FileValue != "from-the-file" {
		t.Errorf("the divergence from the file is not shown (%q): a person will not see why the edit does not work", host.FileValue)
	}

	home := find(t, items, "AACP_HOME_SESSION")
	if home.Value != "only-in-the-file" || home.Source != SourceFile {
		t.Errorf("the value from the file is lost: %q from %q", home.Value, home.Source)
	}
	if home.FileValue != "" {
		t.Error("a matching value is shown twice: that is noise, not an explanation")
	}
}

func TestTestingHooksAreNotSettings(t *testing.T) {
	t.Setenv("AACP_TMUX", "/substituted/tmux")
	t.Setenv("AACP_TEST_DSN", "postgres://somebody@somewhere/something")

	items := Collect(filepath.Join(t.TempDir(), "no-such-file"))
	for _, it := range items {
		if it.Key == "AACP_TMUX" || it.Key == "AACP_TEST_DSN" {
			t.Errorf("the substituted entry point of the runs is shown as a setting: %s", it.Key)
		}
	}
}

func TestEverySettingNamesItsCost(t *testing.T) {
	items := Collect(filepath.Join(t.TempDir(), "no-such-file"))
	if len(items) == 0 {
		t.Fatal("the list of settings is empty")
	}
	known := map[Cost]bool{
		CostLive: true, CostSession: true, CostExec: true,
		CostAgent: true, CostService: true, CostRecreate: true, CostNever: true,
	}
	for _, it := range items {
		if !known[it.Cost] {
			t.Errorf("%s: the price of the edit is not named (%q)", it.Key, it.Cost)
		}
		if it.Group == "" {
			t.Errorf("%s: a setting with no group — there is nowhere on the page for it to land", it.Key)
		}
		if it.Note == "" {
			t.Errorf("%s: a setting with no explanation — a person has nothing to understand it by", it.Key)
		}
	}
}

func TestNotesAreWrittenInThePanelLanguage(t *testing.T) {
	for _, it := range Collect(filepath.Join(t.TempDir(), "no-such-file")) {
		for _, r := range it.Note {
			if r >= 'а' && r <= 'я' || r >= 'А' && r <= 'Я' || r == 'ё' || r == 'Ё' {
				t.Errorf("%s: the explanation is written in Russian while it travels to an English screen: %q",
					it.Key, it.Note)
				break
			}
		}
	}
}
