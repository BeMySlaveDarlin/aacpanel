package install

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/hostcfg"
)

func lanOnly(opts ...rigOption) []rigOption {
	return append([]rigOption{answer("--kit", "+lan"), answer("--lan-addr", "192.168.1.20"),
		func(s *rigSetup) { s.answers["--lan-cert"] = "plain" }}, opts...)
}

// TestAHomeNetworkAloneGetsAToken: the home network as the one way in hands
// a sign-in to localhost, so the phone gets a token, made once and shown
// nowhere; the end of the run says where it lies.
func TestAHomeNetworkAloneGetsAToken(t *testing.T) {
	g := newRig(t, lanOnly()...)
	g.composeConfig(os.Getuid(), os.Getgid(), "/run/user/"+strconv.Itoa(os.Getuid())+"/aacpanel-exec")
	if err := g.do(g.step("envfile")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(g.clone, ".env")
	token := hostcfg.Parse([]byte(g.read(path)))["AACP_TOKEN"]
	if len(token) != 48 || strings.Count(g.read(path), "AACP_TOKEN=") != 1 {
		t.Fatalf("the token is %q in:\n%s", token, g.read(path))
	}
	if !slices.Contains(g.lines(), "envkey AACP_TOKEN generated") {
		t.Errorf("the manifest holds %q", g.lines())
	}
	g.journal.Close()
	if strings.Contains(g.read(g.journal.Path), token) {
		t.Error("the journal holds the token")
	}
	g.says([]string{"docker", "compose", "config", "-q"}, "")
	if !g.already(g.step("envfile")) || hostcfg.Parse([]byte(g.read(path)))["AACP_TOKEN"] != token {
		t.Error("the token was made again")
	}
	closing := strings.Join(g.in.S.Closing(), "\n")
	if !strings.Contains(closing, "with the token: AACP_TOKEN in ~/aacpanel/.env") || strings.Contains(closing, token) {
		t.Errorf("the end of the run says %q", closing)
	}
}

// TestNoTokenIsAWarningInThePlanAndAtTheEnd: without a token the plan and
// the end say a phone cannot sign in, and a token there goes back to the
// template.
func TestNoTokenIsAWarningInThePlanAndAtTheEnd(t *testing.T) {
	g := newRig(t, lanOnly(answer("--lan-token", no),
		homeFile("aacpanel/.env", "AACP_SECRET=s\nAACP_DB_PASSWORD=p\nAACP_TOKEN=0123456789abcdef0123456789abcdef\n"))...)
	g.composeConfig(os.Getuid(), os.Getgid(), "/run/user/"+strconv.Itoa(os.Getuid())+"/aacpanel-exec")
	if err := g.do(g.step("envfile")); err != nil {
		t.Fatal(err)
	}
	body := g.read(filepath.Join(g.clone, ".env"))
	if _, ok := hostcfg.Parse([]byte(body))["AACP_TOKEN"]; ok || !strings.Contains(body, "\n#AACP_TOKEN=\n") {
		t.Errorf("the token did not go back to the template:\n%s", body)
	}
	var plan []string
	for _, r := range g.in.S.Plan() {
		plan = append(plan, r.Text)
	}
	text := strings.Join(plan, "\n")
	warn := "A phone cannot sign in over the home network"
	if !strings.Contains(text, "Mind\n  · "+warn) || !strings.Contains(strings.Join(g.in.S.Closing(), "\n"), warn) {
		t.Errorf("the plan and the end do not warn:\n%s\n%q", text, g.in.S.Closing())
	}
}

// TestAWayInBesidesTheHomeNetworkAsksNoToken: a domain or a tailnet keeps
// the sign-in where a phone reaches it.
func TestAWayInBesidesTheHomeNetworkAsksNoToken(t *testing.T) {
	for _, kit := range []string{"+lan,+tailscale", "+lan,+domain", "+tailscale", ""} {
		r := &Run{Yes: true, Answers: map[string]string{"--kit": kit}}
		s := survey(desktop(), r)
		if _, err := s.Settle(BlockK); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(ids(s.Questions(BlockD)), "token") {
			t.Errorf("kit %q asks for a token", kit)
		}
		if want, asked := s.Token(); want || asked {
			t.Errorf("kit %q: token %v, asked %v", kit, want, asked)
		}
	}
}
