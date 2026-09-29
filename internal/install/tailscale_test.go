package install

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/hostcfg"
)

const tsKey = "tskey-auth-k1CNTRL-s3cr3t"

func tailnet(opts ...rigOption) []rigOption {
	return append([]rigOption{answer("--kit", "+tailscale"), answer("--ts-authkey-file", "~/ts.key"), homeFile("ts.key", tsKey+"\n")}, opts...)
}

// nodeUp makes compose bring the node up: its status says state and the
// name the tailnet gave it, with a certificate when certs holds.
func (g *rig) nodeUp(state string, certs bool) {
	status := []string{"docker", "exec", tsContainer, "tailscale", "status", "--json"}
	g.fails(status, "Error response from daemon: No such container: "+tsContainer)
	up := []string{"docker", "compose", "up", "-d", "tailscale"}
	g.says(up, " Container aacpanel-tailscale  Started\n")
	body := `{"BackendState":"` + state + `","Self":{"DNSName":"aacpanel.tail1234.ts.net."},"CertDomains":null}`
	if certs {
		body = strings.Replace(body, "null", `["aacpanel.tail1234.ts.net"]`, 1)
	}
	g.m.Effects[Command(up[0], up[1:]...)] = func(Cmd) error {
		g.says(status, body)
		return nil
	}
	g.fails([]string{"docker", "volume", "inspect", TSVolume}, "Error: No such volume: "+TSVolume)
	g.says([]string{"docker", "logs", "--tail", "20", tsContainer}, "boot: 2026/09/29 authkey expired\n")
	g.says([]string{"docker", "exec", tsContainer, "tailscale", "serve", "status"},
		"https://aacpanel.tail1234.ts.net (tailnet only)\n|-- / proxy http://aacpanel:8776\n")
}

func TestTheTailnetNodeSignsInAndNamesThePasskeys(t *testing.T) {
	g := newRig(t, tailnet()...)
	g.stackUp()
	g.nodeUp("Running", true)
	if !slices.ContainsFunc(g.in.Steps(), func(s *Step) bool { return s.ID == "tailscale" }) {
		t.Fatal("a kit with Tailscale has no step for it")
	}
	var keyed []string
	g.m.Effects[Command("docker", "compose", "up", "-d", "tailscale")] = func(Cmd) error {
		keyed = append(keyed, hostcfg.Parse([]byte(g.read(filepath.Join(g.clone, ".env"))))["TS_AUTHKEY"])
		g.says([]string{"docker", "exec", tsContainer, "tailscale", "status", "--json"},
			`{"BackendState":"Running","Self":{"DNSName":"aacpanel.tail1234.ts.net."},"CertDomains":["aacpanel.tail1234.ts.net"]}`)
		return nil
	}
	if err := g.do(g.step("tailscale")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(keyed, []string{tsKey}) {
		t.Errorf("the node came up with the key %q", keyed)
	}
	env := hostcfg.Parse([]byte(g.read(filepath.Join(g.clone, ".env"))))
	for k, v := range map[string]string{"AACP_TAILSCALE": "1", "AACP_TS_HOSTNAME": "aacpanel",
		"AACP_RP_ID": "aacpanel.tail1234.ts.net", "AACP_RP_ORIGINS": "https://aacpanel.tail1234.ts.net"} {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	if _, left := env["TS_AUTHKEY"]; left {
		t.Error("the auth key stays in .env after the node signed in")
	}
	if want := []string{"volume " + TSVolume + " data", "tsnode aacpanel"}; !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	if got := g.ran(true); !slices.Contains(got, "docker compose up -d") || slices.Index(got, "docker compose up -d") < slices.Index(got, "docker compose up -d tailscale") {
		t.Errorf("the panel was not brought up again after the name: %q", got)
	}
	g.journal.Close()
	if strings.Contains(g.read(g.journal.Path), tsKey) {
		t.Error("the journal holds the auth key")
	}
	if !g.already(g.step("tailscale")) {
		t.Error("a node in the tailnet was brought up again")
	}
}

func TestANodeThatDoesNotSignInIsDiagnosed(t *testing.T) {
	g := newRig(t, tailnet()...)
	g.stackUp()
	g.nodeUp("NeedsLogin", false)
	f := failedWith(t, g.do(g.step("tailscale")), "the tailnet node did not sign in in 90 s (NeedsLogin)")
	if len(f.Tail) == 0 || !strings.Contains(f.Tail[0], "authkey expired") {
		t.Errorf("the tail is %q", f.Tail)
	}
	if g.clock.Now().Sub(now) < 90*time.Second {
		t.Errorf("the wait gave up after %s", g.clock.Now().Sub(now))
	}
	// The key stays for the next run to ask again.
	if env := hostcfg.Parse([]byte(g.read(filepath.Join(g.clone, ".env")))); env["TS_AUTHKEY"] != tsKey {
		t.Error("the key that did not take is gone from .env")
	}

	g = newRig(t, tailnet()...)
	g.stackUp()
	g.nodeUp("Running", false)
	failedWith(t, g.do(g.step("tailscale")), "the tailnet gives the node no name with a certificate")
}

// TestANodeBesideADomainIsALeg: with a domain the passkeys stay with the
// domain, and the node proxies to a listener of its own.
func TestANodeBesideADomainIsALeg(t *testing.T) {
	g := newRig(t, tailnet(answer("--kit", "+tailscale,+domain"), answer("--domain", "panel.example.org"), answer("--bind", "127.0.0.1"))...)
	g.stackUp()
	g.nodeUp("Running", true)
	g.says([]string{"docker", "exec", tsContainer, "tailscale", "serve", "status"},
		"https://aacpanel.tail1234.ts.net (tailnet only)\n|-- / proxy http://aacpanel:8778\n")
	if err := g.do(g.step("tailscale")); err != nil {
		t.Fatal(err)
	}
	env := hostcfg.Parse([]byte(g.read(filepath.Join(g.clone, ".env"))))
	for k, v := range map[string]string{"AACP_TS_SERVE": "./deploy/tailscale/serve-leg.json", "AACP_TS_ADDR": ":8778",
		"AACP_TS_URL": "https://aacpanel.tail1234.ts.net"} {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	if env["AACP_RP_ID"] == "aacpanel.tail1234.ts.net" {
		t.Error("the node took the passkeys from the domain")
	}
}

// TestTheKeyIsAskedUntilTheNodeIsIn: a key the installer left in .env is
// one that did not take, and the next run asks for another; an install by
// hand keeps its key after the node is in, and nothing is asked of it.
func TestTheKeyIsAskedUntilTheNodeIsIn(t *testing.T) {
	for _, c := range []struct {
		env      string
		manifest bool
		asks     bool
	}{
		{"AACP_TAILSCALE=1\n", true, false},
		{"AACP_TAILSCALE=1\nTS_AUTHKEY=" + tsKey + "\n", true, true},
		{"AACP_TAILSCALE=1\nTS_AUTHKEY=" + tsKey + "\n", false, false},
		{"", false, true},
	} {
		m := desktop()
		m.Files[clone+"/.env"] = "AACP_DB_PASSWORD=x\n" + c.env
		if c.manifest {
			m.Files[home+"/.local/state/aacpanel-install/"+ManifestName] = ""
		}
		s := survey(m, &Run{Yes: true, Answers: map[string]string{"--kit": "+tailscale", "--keep": "review"}})
		s.Keep(Given{Value: "review"})
		if _, err := s.Settle(BlockK); err != nil {
			t.Fatal(err)
		}
		if got := slices.Contains(ids(s.Questions(BlockD)), "tskey"); got != c.asks {
			t.Errorf("%q: the key is asked %v, want %v", c.env, got, c.asks)
		}
	}
}
