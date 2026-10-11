package install

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	tlsService = "aacpanel-lego.service"
	tlsTimer   = "aacpanel-lego.timer"
)

// Only paths to credentials travel through the installer. The hook reads the
// token itself, so neither the plan nor the journal can carry its value.
type lanTLS struct {
	Domain    string `json:"domain"`
	Email     string `json:"email"`
	State     string `json:"state"`
	Provider  string `json:"provider"`
	Hook      string `json:"hook"`
	TokenFile string `json:"tokenFile,omitempty"`
	Zone      string `json:"zone,omitempty"`
}

func tlsConfigPath(home string) string { return filepath.Join(home, ".config/aacpanel/lan-tls.json") }

func (s *Survey) managedTLS() bool { return s.Has("lan") && s.valueOr("lancert", "") == "lego" }

func (s *Survey) tlsAnswers() lanTLS {
	return lanTLS{Domain: s.valueOr("tlsdomain", ""), Email: s.valueOr("tlsemail", ""),
		State: s.valueOr("tlsstate", ""), Provider: s.valueOr("tlsprovider", "exec"),
		Hook: s.valueOr("tlshook", ""), TokenFile: s.valueOr("tlstoken", ""), Zone: s.valueOr("tlszone", "")}
}

func (c lanTLS) cert() string { return filepath.Join(c.State, "certificates", c.Domain+".crt") }

// The hand-written unit is read only for defaults. Its shell is never run,
// and an unfamiliar spelling simply leaves the question unanswered.
func (s *Survey) tlsBefore() lanTLS {
	var c lanTLS
	if raw, err := s.m.ReadFile(tlsConfigPath(s.Facts.Account.Home)); err == nil && json.Unmarshal(raw, &c) == nil {
		return c
	}
	home := s.Facts.Account.Home
	c.State = filepath.Join(home, ".config/aacpanel/lego")
	c.Email = s.Found.Email
	c.Provider = "exec"
	if u, err := url.Parse(s.was.env["AACP_LAN_URL"]); err == nil {
		c.Domain = u.Hostname()
	}
	if dir := s.was.env["AACP_TLS_DIR"]; filepath.Base(dir) == "certificates" {
		c.State = filepath.Dir(dir)
	}
	raw, _ := s.m.ReadFile(filepath.Join(home, ".config/systemd/user", tlsService))
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "Environment=EXEC_PATH="); ok {
			c.Hook = strings.ReplaceAll(v, "%h", home)
		}
		if v, ok := strings.CutPrefix(line, "ExecStart="); ok {
			words := strings.Fields(v)
			for i := 1; i+1 < len(words); i++ {
				v := strings.ReplaceAll(words[i+1], "%h", home)
				switch words[i] {
				case "--email":
					c.Email = v
				case "--path":
					c.State = v
				case "--domains":
					c.Domain = v
				}
			}
		}
	}
	return c
}

var dnsName = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

func checkTLSDomain(v string) error {
	if !dnsName.MatchString(v) || len(v) > 253 {
		return errors.New("a DNS name such as panel.example.org is required")
	}
	return nil
}

func checkTLSPath(v string) error {
	if strings.Contains(v, ",") {
		return errors.New("give one path")
	}
	return checkPaths(v)
}

func checkTLSEmail(v string) error {
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || strings.ContainsAny(v, " %\"'\\\n\r\t") {
		return errors.New("an email address such as owner@example.org is required")
	}
	return nil
}

func (s *Survey) tlsQuestions() []Question {
	c := s.tlsBefore()
	question := func(id, tab, prompt, flag, value string, check func(string) error) Question {
		q := Question{ID: id, Tab: tab, Prompt: prompt, Form: One, Own: typeOwn, Flag: flag, Required: value == "",
			Check: check, Writes: []Target{{File: "LAN TLS settings", Key: id}}}
		if value != "" {
			q.Options = []Option{{Value: value, Source: "this machine"}}
		}
		return q
	}
	qs := []Question{
		question("tlsdomain", "TLS name", "DNS name for the home-network certificate?", "--tls-domain", c.Domain, checkTLSDomain),
		question("tlsemail", "ACME email", "Email of the ACME account?", "--tls-email", c.Email, checkTLSEmail),
		question("tlsstate", "ACME files", "Where does lego keep its account and certificates?", "--tls-state", c.State, checkTLSPath),
		{ID: "tlsprovider", Tab: "DNS provider", Prompt: "Who answers the DNS-01 challenge?", Form: One,
			Options: []Option{{Value: "exec", Label: "Your exec hook", Source: "default"}, {Value: "hetzner", Label: "Hetzner Cloud DNS — shipped hook"}},
			Default: c.Provider, Flag: "--tls-provider", Values: []string{"exec", "hetzner"}, Writes: []Target{{File: "LAN TLS settings", Key: "provider"}}},
	}
	qs = append(qs,
		question("tlszone", "DNS zone", "Hetzner Cloud DNS zone name?", "--tls-zone", c.Zone, checkTLSDomain),
		question("tlstoken", "Token file", "Path of the Hetzner API token file (0600)?", "--tls-token-file", c.TokenFile, checkTLSPath),
		question("tlshook", "DNS hook", "Path of your executable lego DNS hook?", "--tls-hook", c.Hook, checkTLSPath))
	for i := range qs {
		qs[i].Moot = !s.managedTLS()
		if qs[i].ID == "tlszone" || qs[i].ID == "tlstoken" {
			qs[i].Moot = qs[i].Moot || s.valueOr("tlsprovider", c.Provider) != "hetzner"
		}
		if qs[i].ID == "tlshook" {
			qs[i].Moot = qs[i].Moot || s.valueOr("tlsprovider", c.Provider) == "hetzner"
		}
	}
	qs[0].Note = "The installer issues a missing certificate with Let's Encrypt and accepts its terms. A daily timer renews with 30 days left. DNS must point this name to the LAN address."
	return qs
}

func (in *Install) tlsCommand(c lanTLS, renew bool) (Cmd, error) {
	lego, err := in.M.LookPath("lego")
	if err != nil {
		return Cmd{}, errors.New("lego is missing: install the lego package and run the installer again")
	}
	if err := checkTLSPath(lego); err != nil {
		return Cmd{}, fmt.Errorf("lego executable: %w", err)
	}
	args := []string{lego, "--accept-tos", "--email", c.Email, "--path", c.State, "--dns", "exec", "--domains", c.Domain}
	if renew {
		args = append(args, "renew", "--days", "30")
	} else {
		args = append(args, "run")
	}
	hook := c.Hook
	if c.Provider == "hetzner" {
		hook = filepath.Join(in.clone(), "deploy/tls/hetzner-dns-hook")
	}
	return Cmd{Argv: args, Env: []string{"EXEC_PATH=" + hook, "EXEC_PROPAGATION_TIMEOUT=300", "EXEC_POLLING_INTERVAL=10",
		"HETZNER_DNS_ZONE=" + c.Zone, "HETZNER_DNS_TOKEN_FILE=" + c.TokenFile}, Quiet: true, Limit: 15 * time.Minute}, nil
}

func (in *Install) tlsValidate(c lanTLS) error {
	for _, field := range []struct {
		value string
		check func(string) error
	}{{c.Domain, checkTLSDomain}, {c.Email, checkTLSEmail}, {c.State, checkTLSPath}} {
		if err := field.check(field.value); err != nil {
			return err
		}
	}
	if !filepath.IsAbs(c.State) {
		return errors.New("the lego state directory must be absolute")
	}
	switch c.Provider {
	case "exec":
		if err := checkTLSPath(c.Hook); err != nil {
			return err
		}
		st, err := in.M.Stat(c.Hook)
		if err != nil || !st.Mode.IsRegular() || st.Mode.Perm()&0o111 == 0 {
			return errors.New("the DNS hook must be an executable file")
		}
	case "hetzner":
		if err := checkTLSDomain(c.Zone); err != nil {
			return err
		}
		if c.Domain != c.Zone && !strings.HasSuffix(c.Domain, "."+c.Zone) {
			return errors.New("the certificate name is outside the DNS zone")
		}
		if err := checkTLSPath(c.TokenFile); err != nil {
			return err
		}
		st, err := in.M.Stat(c.TokenFile)
		if err != nil || !st.Mode.IsRegular() || st.Mode.Perm() != 0o600 || st.UID != in.uid() {
			return errors.New("the DNS token file must belong to you and have mode 0600")
		}
	default:
		return errors.New("the DNS provider must be exec or hetzner")
	}
	return nil
}

func (in *Install) tlsLeaf(c lanTLS) (*x509.Certificate, error) {
	cert, err := in.M.ReadFile(c.cert())
	if err != nil {
		return nil, err
	}
	key, err := in.M.ReadFile(strings.TrimSuffix(c.cert(), ".crt") + ".key")
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err == nil {
		err = leaf.VerifyHostname(c.Domain)
	}
	return leaf, err
}

func (in *Install) tlsUnits(c lanTLS) (map[string][]byte, error) {
	cmd, err := in.tlsCommand(c, true)
	if err != nil {
		return nil, err
	}
	units := map[string][]byte{}
	for _, name := range []string{tlsService, tlsTimer} {
		raw, err := in.M.ReadFile(filepath.Join(in.clone(), "deploy/systemd", name))
		if err != nil {
			return nil, err
		}
		var env strings.Builder
		for _, v := range cmd.Env {
			fmt.Fprintf(&env, "Environment=%s\n", v)
		}
		raw = bytes.ReplaceAll(raw, []byte("@ENVIRONMENT@\n"), []byte(env.String()))
		raw = bytes.ReplaceAll(raw, []byte("@COMMAND@"), []byte(strings.Join(cmd.Argv, " ")))
		units[name] = raw
	}
	return units, nil
}

func (in *Install) tlsDone(r *Run) (bool, error) {
	c := in.S.tlsAnswers()
	if err := in.tlsValidate(c); err != nil {
		return false, nil
	}
	want, err := in.tlsUnits(c)
	if err != nil {
		return false, nil
	}
	raw, err := in.M.ReadFile(tlsConfigPath(in.home()))
	var have lanTLS
	if err != nil || json.Unmarshal(raw, &have) != nil || have != c {
		return false, nil
	}
	for name, body := range want {
		raw, err := in.M.ReadFile(filepath.Join(in.userUnitDir(), name))
		if err != nil || !bytes.Equal(raw, body) {
			return false, nil
		}
	}
	leaf, err := in.tlsLeaf(c)
	if err != nil || !leaf.NotAfter.After(r.clock().Now()) || leaf.NotBefore.After(r.clock().Now()) {
		return false, nil
	}
	active, enabled := in.unitState(r, true, tlsTimer)
	return active == "active" && enabled == "enabled", nil
}

func (in *Install) tlsStep() *Step {
	return &Step{ID: "lan-tls", Title: "Home-network certificate", Done: in.tlsDone, Undo: UndoKind,
		Apply: func(r *Run) error {
			c := in.S.tlsAnswers()
			if err := in.tlsValidate(c); err != nil {
				return err
			}
			units, err := in.tlsUnits(c)
			if err != nil {
				return err
			}
			// Account keys and certificates outlive uninstall, like the database.
			// Recording the directory before lego starts covers its generated files.
			if err := r.Once(File, c.State, "data"); err != nil {
				return err
			}
			if err := in.makeDirs(r, filepath.Dir(c.State)); err != nil {
				return err
			}
			if err := os.MkdirAll(c.State, 0o700); err != nil {
				return err
			}
			leaf, err := in.tlsLeaf(c)
			missing := errors.Is(err, os.ErrNotExist)
			if err != nil && !missing {
				return fmt.Errorf("the home-network certificate and key: %w", err)
			}
			if missing || !leaf.NotAfter.After(r.clock().Now()) {
				cmd, err := in.tlsCommand(c, !missing)
				if err != nil {
					return err
				}
				// An arbitrary user hook may echo its credentials on stderr. Quiet
				// keeps stdout, but a command error also carries stderr.
				cmd.Argv = append([]string{"sh", "-c", `exec "$@" >/dev/null 2>&1`, "aacpanel-lego"}, cmd.Argv...)
				if _, err := r.Exec(cmd); err != nil {
					return fail("lego could not obtain the home-network certificate", err)
				}
			}
			leaf, err = in.tlsLeaf(c)
			if err != nil {
				return fmt.Errorf("the home-network certificate and key: %w", err)
			}
			if !leaf.NotAfter.After(r.clock().Now()) || leaf.NotBefore.After(r.clock().Now()) {
				return errors.New("the home-network certificate is outside its validity period")
			}
			config := tlsConfigPath(in.home())
			if err := in.makeDirs(r, filepath.Dir(config)); err != nil {
				return err
			}
			if err := in.touch(r, File, config, ""); err != nil {
				return err
			}
			raw, err := json.MarshalIndent(c, "", "  ")
			if err != nil {
				return err
			}
			if err := writeFile(config, append(raw, '\n'), 0o600); err != nil {
				return err
			}
			if err := in.makeDirs(r, in.userUnitDir()); err != nil {
				return err
			}
			for _, name := range []string{tlsService, tlsTimer} {
				path := filepath.Join(in.userUnitDir(), name)
				if err := in.touch(r, UserUnit, path, ""); err != nil {
					return err
				}
				if err := writeFile(path, units[name], 0o644); err != nil {
					return err
				}
			}
			if _, err := r.Exec(Cmd{Argv: []string{"systemctl", "--user", "daemon-reload"}}); err != nil {
				return err
			}
			if in.ask(r, "systemctl", "--user", "is-enabled", tlsTimer) != "enabled" {
				if err := in.recordEnable(r, tlsTimer, "timers.target"); err != nil {
					return err
				}
				_, err = r.Exec(Cmd{Argv: []string{"systemctl", "--user", "enable", "--now", tlsTimer}})
			} else {
				_, err = r.Exec(Cmd{Argv: []string{"systemctl", "--user", "restart", tlsTimer}})
			}
			return err
		},
		Verify: func(r *Run) error {
			ok, err := in.tlsDone(r)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("the home-network certificate or its renewal timer is not ready")
			}
			leaf, err := in.tlsLeaf(in.S.tlsAnswers())
			if err != nil {
				return err
			}
			r.Say(Pass, tlsTimer+" active and enabled · certificate expires "+leaf.NotAfter.UTC().Format(time.RFC3339))
			return nil
		},
	}
}

func (in *Install) tlsLink(r *Run, env map[string]string) chainLink {
	if env["AACP_LAN_ADDR"] == "" {
		return chainLink{mark: Note, text: "Home-network TLS: not configured"}
	}
	cert := filepath.Join(env["AACP_TLS_DIR"], filepath.Base(env["AACP_TLS_CERT"]))
	key := filepath.Join(env["AACP_TLS_DIR"], filepath.Base(env["AACP_TLS_KEY"]))
	crt, err := in.M.ReadFile(cert)
	if err != nil {
		return broken("Check AACP_TLS_DIR and AACP_TLS_CERT in .env.", "Home-network certificate: %s", err)
	}
	priv, err := in.M.ReadFile(key)
	if err != nil {
		return broken("Check AACP_TLS_KEY in .env.", "Home-network key: %s", err)
	}
	pair, err := tls.X509KeyPair(crt, priv)
	if err != nil {
		return broken("Replace the certificate and key with a matching pair.", "Home-network TLS: %s", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return broken("Replace the invalid certificate.", "Home-network TLS: %s", err)
	}
	if !leaf.NotAfter.After(r.clock().Now()) || leaf.NotBefore.After(r.clock().Now()) {
		return broken("Check the renewal service and renew the certificate.", "Home-network certificate is outside its validity period (expires %s)", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	state := "externally renewed"
	if raw, err := in.M.ReadFile(tlsConfigPath(in.home())); err == nil {
		var c lanTLS
		if json.Unmarshal(raw, &c) != nil {
			return broken("Run ./install.sh --keep access to repair the TLS settings.", "Home-network TLS settings cannot be read")
		}
		if c.cert() != cert {
			return broken("Match AACP_TLS_DIR and AACP_TLS_CERT to the managed certificate.", "The LAN listener uses a different certificate from the renewal timer")
		}
		if err := in.tlsValidate(c); err != nil {
			return broken("Repair the DNS hook settings and run ./install.sh check.", "Home-network renewal: %s", err)
		}
		units, err := in.tlsUnits(c)
		if err != nil {
			return broken("Run ./install.sh --keep access to repair renewal.", "Home-network renewal: %s", err)
		}
		for name, want := range units {
			have, err := in.M.ReadFile(filepath.Join(in.userUnitDir(), name))
			if err != nil || !bytes.Equal(have, want) {
				return broken("Run ./install.sh --keep access to install the delivery's renewal units.", "Home-network renewal: %s differs from the delivery", name)
			}
		}
		active, enabled := in.unitState(r, true, tlsTimer)
		if active != "active" || enabled != "enabled" {
			return broken("systemctl --user status "+tlsTimer, "%s is %s and %s", tlsTimer, orNone(active), orNone(enabled))
		}
		state = tlsTimer + " active and enabled"
		if in.ask(r, "systemctl", "--user", "is-failed", tlsService) == "failed" {
			return broken("systemctl --user status "+tlsService+"; check the DNS hook and its credential file.", "Home-network certificate expires %s · the last renewal failed", leaf.NotAfter.UTC().Format(time.RFC3339))
		}
	}
	l := pass("Home-network certificate expires %s · %s", leaf.NotAfter.UTC().Format(time.RFC3339), state)
	if leaf.NotAfter.Sub(r.clock().Now()) <= 14*24*time.Hour {
		l.mark = Warn
		l.text += " · renewal needs attention"
	}
	return l
}

// Turning off managed renewal takes back only files held in the manifest.
// The account and certificates remain usable by an external renewal job.
func (in *Install) tlsOffStep() *Step {
	owned := func(r *Run) []Entry {
		if r.Manifest == nil {
			return nil
		}
		es, _ := ReadManifest(r.Manifest.Path)
		var out []Entry
		for _, e := range es {
			if (e.Kind == string(Enabled) && e.Target == "user "+tlsTimer) ||
				(e.Kind == string(UserUnit) && (e.Target == filepath.Join(in.userUnitDir(), tlsService) || e.Target == filepath.Join(in.userUnitDir(), tlsTimer))) ||
				(e.Kind == string(File) && e.Target == tlsConfigPath(in.home())) {
				out = append(out, e)
			}
		}
		return out
	}
	return &Step{ID: "lan-tls-off", Title: "Remove managed certificate renewal", Undo: UndoKind,
		Done: func(r *Run) (bool, error) { return len(owned(r)) == 0, nil },
		Apply: func(r *Run) error {
			es := owned(r)
			for i := len(es) - 1; i >= 0; i-- {
				if err := UndoKind(r, es[i]); err != nil {
					return err
				}
			}
			if err := r.Manifest.Drop(func(e Entry) bool { return slices.Contains(es, e) }); err != nil {
				return err
			}
			r.TookAway(tlsTimer)
			return nil
		},
		Verify: func(r *Run) error {
			if len(owned(r)) != 0 {
				return errors.New("managed renewal is still on record")
			}
			r.Say(Pass, "managed certificate renewal removed; the certificate and ACME account stay")
			return nil
		},
	}
}
