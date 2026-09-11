package hostcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unit(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "systemd", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func directives(body string) map[string][]string {
	out := map[string][]string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[key] = append(out[key], value)
	}
	return out
}

func TestAgentUnitKeepsHookSocketDirectory(t *testing.T) {
	d := directives(unit(t, "aacpanel-agent@.service"))
	if got := d["RuntimeDirectory"]; len(got) != 1 || got[0] != "aacpanel-agent" {
		t.Errorf("RuntimeDirectory=%v, expected aacpanel-agent: the hook socket (/run/aacpanel-agent/ask.sock) has nowhere to live", got)
	}
	if got := d["RuntimeDirectoryMode"]; len(got) != 1 || got[0] != "0700" {
		t.Errorf("RuntimeDirectoryMode=%v, expected 0700: the hook socket would become reachable for the service", got)
	}
	if got := d["User"]; len(got) != 1 || got[0] != "%i" {
		t.Errorf("User=%v, expected %%i: the unit is a template, the user arrives as the instance", got)
	}
}

func TestEveryUnitReadsTheSameHostEnv(t *testing.T) {
	const want = "-/var/lib/aacpanel/host.env"
	for _, name := range []string{"aacpanel-agent@.service", "aacpanel-exec.service"} {
		d := directives(unit(t, name))
		if got := d["EnvironmentFile"]; len(got) != 1 || got[0] != want {
			t.Errorf("%s: EnvironmentFile=%v, expected %s", name, got, want)
		}
	}
	if !strings.Contains(unit(t, "aacpanel-agent@.service"), defaultPath) {
		t.Errorf("aacpanel-agent@.service does not mention %s — the path of the description parted from internal/hostcfg", defaultPath)
	}
}
