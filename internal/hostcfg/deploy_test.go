package hostcfg

import (
	"os"
	"path/filepath"
	"regexp"
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

// offeredKeys are the keys a template names, set or commented out.
func offeredKeys(t *testing.T, rel string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*#?([A-Za-z_][A-Za-z0-9_]*)=`).FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = true
	}
	return out
}

// Compose fills the environment of the containers from .env and never reads
// the host description, so a setting compose passes on is offered in the .env
// template. The host template may name one beside it, never instead of it:
// set only where that template says, it changes nothing.
func TestContainerSettingsAreOfferedWhereComposeReadsThem(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	interpolated := regexp.MustCompile(`(?:^|[^$])\$\{([A-Za-z_][A-Za-z0-9_]*)`)
	compose := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, m := range interpolated.FindAllStringSubmatch(line, -1) {
			compose[m[1]] = true
		}
	}
	if len(compose) < 10 {
		t.Fatalf("docker-compose.yml gave %d variables: the check is walking over nothing", len(compose))
	}

	dotEnv := offeredKeys(t, ".env.example")
	shared := 0
	for key := range offeredKeys(t, filepath.Join("deploy", "host.env.example")) {
		if !compose[key] {
			continue
		}
		shared++
		if !dotEnv[key] {
			t.Errorf("%s: deploy/host.env.example offers it, compose reads it from .env, and .env.example does not name it", key)
		}
	}
	if shared == 0 {
		t.Fatal("no key of deploy/host.env.example is one compose passes on: the check proves nothing")
	}
}
