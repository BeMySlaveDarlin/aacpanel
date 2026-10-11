package install

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func tlsPair(t *testing.T, until time.Time) (string, string) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "panel.example.org"}, DNSNames: []string{"panel.example.org"}, NotBefore: until.Add(-90 * 24 * time.Hour), NotAfter: until}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}))
}

func tlsRig(t *testing.T) *rig {
	g := newRig(t)
	c := lanTLS{Domain: "panel.example.org", Email: "owner@example.org", State: filepath.Join(g.home, ".config/aacpanel/lego"), Provider: "exec", Hook: filepath.Join(g.home, "dns-hook")}
	for k, v := range map[string]string{"kit": "relay,limits,tools,lan", "lancert": "lego", "lanaddr": "192.168.1.2", "tlsdomain": c.Domain, "tlsemail": c.Email, "tlsstate": c.State, "tlsprovider": c.Provider, "tlshook": c.Hook} {
		g.in.S.given[k] = Given{Value: v}
	}
	g.write(c.Hook, "#!/bin/sh\nexit 0\n", 0o700)
	for _, name := range []string{tlsService, tlsTimer} {
		raw, err := os.ReadFile(filepath.Join("../../deploy/systemd", name))
		if err != nil {
			t.Fatal(err)
		}
		g.write(filepath.Join(g.clone, "deploy/systemd", name), string(raw), 0o644)
	}
	lego := filepath.Join(g.home, "lego")
	crt, priv := tlsPair(t, now.Add(60*24*time.Hour))
	g.write(filepath.Join(g.home, "issued.crt"), crt, 0o644)
	g.write(filepath.Join(g.home, "issued.key"), priv, 0o600)
	g.write(lego, `#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
root = pathlib.Path(__file__).parent
args = sys.argv[1:]
(root / "lego-call.json").write_text(json.dumps({"args": args, "hook": os.environ["EXEC_PATH"]}))
if "run" in args or "renew" in args:
    certs = pathlib.Path(args[args.index("--path")+1]) / "certificates"
    certs.mkdir(parents=True, exist_ok=True)
    name = args[args.index("--domains")+1]
    shutil.copyfile(root / "issued.crt", certs / (name+".crt"))
    shutil.copyfile(root / "issued.key", certs / (name+".key"))
`, 0o700)
	g.m.Path = map[string]string{"lego": lego}
	for _, renew := range []bool{false, true} {
		cmd, err := g.in.tlsCommand(c, renew)
		if err != nil {
			t.Fatal(err)
		}
		cmd.Argv = append([]string{"sh", "-c", `exec "$@" >/dev/null 2>&1`, "aacpanel-lego"}, cmd.Argv...)
		g.says(cmd.Argv, "")
		g.m.Effects[Command(cmd.Argv[0], cmd.Argv[1:]...)] = func(c Cmd) error { _, err := (Local{}).Exec(context.Background(), c, func(string) {}); return err }
	}
	g.says([]string{"systemctl", "--user", "daemon-reload"}, "")
	g.says([]string{"systemctl", "--user", "is-enabled", tlsTimer}, "disabled")
	enable := []string{"systemctl", "--user", "enable", "--now", tlsTimer}
	g.says(enable, "")
	g.m.Effects[Command(enable[0], enable[1:]...)] = func(Cmd) error {
		g.says([]string{"systemctl", "--user", "is-enabled", tlsTimer}, "enabled")
		g.says([]string{"systemctl", "--user", "is-active", tlsTimer}, "active")
		return nil
	}
	return g
}

func TestLANTLSIssuesOnceAndRenewsWithItsAccount(t *testing.T) {
	g := tlsRig(t)
	var steps []string
	for _, step := range g.in.Steps() {
		steps = append(steps, step.ID)
	}
	if at := slices.Index(steps, "lan-tls"); at < 0 || at > slices.Index(steps, "compose") {
		t.Fatalf("the certificate must precede the stack: %v", steps)
	}
	if err := g.do(g.in.tlsStep()); err != nil {
		t.Fatal(err)
	}
	c := g.in.S.tlsAnswers()
	vars, _ := g.in.S.DotEnv()
	if !slices.ContainsFunc(vars, func(v Var) bool { return v.Key == "AACP_TLS_DIR" && v.Value == filepath.Dir(c.cert()) }) ||
		!slices.ContainsFunc(vars, func(v Var) bool { return v.Key == "AACP_TLS_CERT" && v.Value == "/tls/panel.example.org.crt" }) {
		t.Fatalf("compose cannot reach the issued certificate: %v", vars)
	}
	service := g.read(filepath.Join(g.in.userUnitDir(), tlsService))
	timer := g.read(filepath.Join(g.in.userUnitDir(), tlsTimer))
	if !strings.Contains(timer, "OnCalendar=*-*-* 04:17:00") || !strings.Contains(timer, "Persistent=true") {
		t.Fatalf("timer: %s", timer)
	}
	var renew Cmd
	for _, line := range strings.Split(service, "\n") {
		if v, ok := strings.CutPrefix(line, "Environment="); ok {
			renew.Env = append(renew.Env, v)
		}
		if v, ok := strings.CutPrefix(line, "ExecStart="); ok {
			renew.Argv = strings.Fields(v)
		}
	}
	if _, err := (Local{}).Exec(context.Background(), renew, func(string) {}); err != nil {
		t.Fatal(err)
	}
	var call struct {
		Args []string
		Hook string
	}
	if err := json.Unmarshal([]byte(g.read(filepath.Join(g.home, "lego-call.json"))), &call); err != nil {
		t.Fatal(err)
	}
	want := []string{"--accept-tos", "--email", c.Email, "--path", c.State, "--dns", "exec", "--domains", c.Domain, "renew", "--days", "30"}
	if !slices.Equal(call.Args, want) || call.Hook != c.Hook {
		t.Fatalf("renewal called %+v; want %v and %s", call, want, c.Hook)
	}
	before := len(g.m.Ran)
	if err := g.do(g.in.tlsStep()); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range g.m.Ran[before:] {
		if !strings.Contains(cmd, " is-") {
			t.Errorf("second run changes the host: %s", cmd)
		}
	}
	for _, want := range []string{"file " + c.State + " data", "file " + tlsConfigPath(g.home) + " created", "userunit " + filepath.Join(g.in.userUnitDir(), tlsService) + " created", "userunit " + filepath.Join(g.in.userUnitDir(), tlsTimer) + " created", "enabled user " + tlsTimer + " by-installer"} {
		if !slices.Contains(g.lines(), want) {
			t.Errorf("manifest lacks %s", want)
		}
	}
	env := map[string]string{"AACP_LAN_ADDR": ":8778", "AACP_TLS_DIR": filepath.Dir(c.cert()), "AACP_TLS_CERT": "/tls/" + filepath.Base(c.cert()), "AACP_TLS_KEY": "/tls/" + c.Domain + ".key"}
	if l := g.in.tlsLink(g.r, env); l.mark != Pass || !strings.Contains(l.text, tlsTimer+" active and enabled") {
		t.Fatalf("Verify: %+v", l)
	}
	g.says([]string{"systemctl", "--user", "is-failed", tlsService}, "failed")
	if l := g.in.tlsLink(g.r, env); l.mark != Stop || !strings.Contains(l.text, "last renewal failed") {
		t.Fatalf("failed renewal passed Verify: %+v", l)
	}
	g.says([]string{"systemctl", "--user", "is-failed", tlsService}, "inactive")
	other := filepath.Join(g.home, "other-certificate")
	g.write(filepath.Join(other, c.Domain+".crt"), g.read(c.cert()), 0o644)
	g.write(filepath.Join(other, c.Domain+".key"), g.read(strings.TrimSuffix(c.cert(), ".crt")+".key"), 0o600)
	env["AACP_TLS_DIR"] = other
	if l := g.in.tlsLink(g.r, env); l.mark != Stop || !strings.Contains(l.text, "different certificate") {
		t.Fatalf("unmanaged mounted certificate passed Verify: %+v", l)
	}
	env["AACP_TLS_DIR"] = filepath.Dir(c.cert())
	g.says([]string{"systemctl", "--user", "is-active", tlsTimer}, "inactive")
	if l := g.in.tlsLink(g.r, env); l.mark != Stop {
		t.Fatalf("inactive renewal passed Verify: %+v", l)
	}
	for _, name := range []string{tlsService, tlsTimer} {
		g.says([]string{"systemctl", "--user", "disable", "--now", name}, "")
	}
	es, err := ReadManifest(g.r.Manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(es) - 1; i >= 1; i-- {
		if es[i].Step == "lan-tls" {
			if err := UndoKind(g.r, es[i]); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, name := range []string{tlsService, tlsTimer} {
		if _, err := os.Stat(filepath.Join(g.in.userUnitDir(), name)); !os.IsNotExist(err) {
			t.Errorf("uninstall left %s: %v", name, err)
		}
	}
	if _, err := os.Stat(c.cert()); err != nil {
		t.Errorf("uninstall removed the certificate: %v", err)
	}
}

func TestLANTLSKeepsItsAnswersAndCanBeTurnedOff(t *testing.T) {
	g := tlsRig(t)
	if err := g.do(g.in.tlsStep()); err != nil {
		t.Fatal(err)
	}
	g.write(filepath.Join(g.clone, ".env"), "AACP_LAN_ADDR=:8778\nAACP_LAN_BIND=192.168.1.2\nAACP_TLS_DIR="+filepath.Join(g.in.S.tlsAnswers().State, "certificates")+"\nAACP_TLS_CERT=/tls/panel.example.org.crt\n", 0o600)
	s := NewSurvey(g.m, Inspection{Facts: g.in.S.Facts}, &Run{Yes: true, Answers: map[string]string{"--kit": "+lan"}}, now)
	if _, err := s.Settle(BlockK); err != nil {
		t.Fatal(err)
	}
	qs, err := s.Settle(BlockD)
	if err != nil || len(qs) != 0 || s.tlsAnswers() != g.in.S.tlsAnswers() {
		t.Fatalf("the saved TLS answers need another question: %+v %v; %+v", qs, err, s.tlsAnswers())
	}
	for _, name := range []string{tlsService, tlsTimer} {
		g.says([]string{"systemctl", "--user", "disable", "--now", name}, "")
	}
	if err := g.do(g.in.tlsOffStep()); err != nil {
		t.Fatal(err)
	}
	if err := g.do(g.in.tlsOffStep()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tlsConfigPath(g.home)); !os.IsNotExist(err) {
		t.Fatalf("managed settings left behind: %v", err)
	}
	if _, err := os.Stat(g.in.S.tlsAnswers().cert()); err != nil {
		t.Fatalf("turning renewal off removed the certificate: %v", err)
	}
}

func TestLANTLSAdoptsWithoutIssuingAndKeepsTheOriginalUnits(t *testing.T) {
	g := tlsRig(t)
	c := g.in.S.tlsAnswers()
	crt, key := tlsPair(t, now.Add(20*24*time.Hour))
	g.write(c.cert(), crt, 0o644)
	g.write(strings.TrimSuffix(c.cert(), ".crt")+".key", key, 0o600)
	old := "[Service]\nExecStart=/usr/bin/true\n"
	g.write(filepath.Join(g.in.userUnitDir(), tlsService), old, 0o644)
	if err := g.do(g.in.tlsStep()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(g.home, "lego-call.json")); !os.IsNotExist(err) {
		t.Fatalf("adoption ran ACME: %v", err)
	}
	g.says([]string{"systemctl", "--user", "disable", "--now", tlsService}, "")
	es, _ := ReadManifest(g.r.Manifest.Path)
	for _, e := range es {
		if e.Kind == string(UserUnit) && filepath.Base(e.Target) == tlsService {
			if err := UndoKind(g.r, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := g.read(filepath.Join(g.in.userUnitDir(), tlsService)); got != old {
		t.Fatalf("the original unit was lost: %s", got)
	}
}

func TestLANTLSResumesAnIncompletePairAndRenewsAnExpiredOne(t *testing.T) {
	for _, expired := range []bool{false, true} {
		g := tlsRig(t)
		c := g.in.S.tlsAnswers()
		until := now.Add(20 * 24 * time.Hour)
		if expired {
			until = now.Add(-time.Hour)
		}
		crt, key := tlsPair(t, until)
		g.write(c.cert(), crt, 0o644)
		if expired {
			g.write(strings.TrimSuffix(c.cert(), ".crt")+".key", key, 0o600)
		}
		if err := g.do(g.in.tlsStep()); err != nil {
			t.Fatal(err)
		}
		var call struct{ Args []string }
		if err := json.Unmarshal([]byte(g.read(filepath.Join(g.home, "lego-call.json"))), &call); err != nil {
			t.Fatal(err)
		}
		verb := "run"
		if expired {
			verb = "renew"
		}
		if !slices.Contains(call.Args, verb) {
			t.Fatalf("expired %v: %v, want %s", expired, call.Args, verb)
		}
	}
}

func TestHetznerHookDryRunKeepsOtherChallengesAndTheToken(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("private-test-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"present", "cleanup"} {
		cmd := exec.Command("python3", "../../deploy/tls/hetzner-dns-hook", "--dry-run", action, "_acme-challenge.panel.example.org.", "challenge-value")
		cmd.Env = append(os.Environ(), "HETZNER_DNS_ZONE=example.org", "HETZNER_DNS_TOKEN_FILE="+token)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v: %s", action, err, out)
		}
		var body struct {
			Method, Path string
			Body         struct{ Records []struct{ Value string } }
		}
		if err := json.Unmarshal(out, &body); err != nil {
			t.Fatal(err)
		}
		if body.Method != "POST" || len(body.Body.Records) != 1 || body.Body.Records[0].Value != `"challenge-value"` {
			t.Fatalf("%s affects more than its TXT value: %s", action, out)
		}
		if action == "cleanup" && !strings.HasSuffix(body.Path, "/actions/remove_records") {
			t.Fatalf("cleanup removes the whole RRset: %s", out)
		}
		if strings.Contains(string(out), "private-test-token") {
			t.Fatal("the token escaped")
		}
	}
	if err := os.Chmod(token, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "../../deploy/tls/hetzner-dns-hook", "--dry-run", "present", "_acme-challenge.example.org.", "challenge")
	cmd.Env = append(os.Environ(), "HETZNER_DNS_ZONE=example.org", "HETZNER_DNS_TOKEN_FILE="+token)
	if out, err := cmd.CombinedOutput(); err == nil || strings.Contains(string(out), "private-test-token") {
		t.Fatalf("public token file: %v: %s", err, out)
	}
}
