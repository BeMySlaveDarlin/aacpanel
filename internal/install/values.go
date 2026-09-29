package install

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"aacpanel/internal/contours"
	"aacpanel/internal/hostcfg"
)

// StateDir is the state directory the answers chose.
func (s *Survey) StateDir() string { return s.valueOr("state", DefaultStateDir) }

// ExecDir is where the executor keeps its socket: the directory compose
// mounts into the service.
func (f Facts) ExecDir() string {
	return fmt.Sprintf("/run/user/%d/aacpanel-exec", f.Account.UID)
}

// HostEnv is what the answers write into host.env: the keys every process
// of the panel reads, and the two the machine gives without asking — the
// clone and the user.
func (s *Survey) HostEnv() []Var {
	f := s.Facts
	home := f.Account.Home
	out := []Var{
		{Key: hostcfg.StateDirEnv, Value: s.StateDir()},
		{Key: hostcfg.HostEnv, Value: s.valueOr("host", "")},
		{Key: hostcfg.RepoEnv, Value: f.Clone},
		{Key: hostcfg.UnixUserEnv, Value: f.Account.Name},
		{Key: hostcfg.HomeSessionEnv, Value: s.valueOr("home", "")},
		{Key: hostcfg.LangEnv, Value: s.valueOr("locale", "")},
	}
	// No windows is an answer of its own: the empty template, and no
	// display to open anything on.
	terminal, display := s.valueOr("terminal", ""), ""
	if terminal != "" {
		display = s.valueOr("display", "")
	}
	out = append(out, Var{Key: "AACP_TERMINAL", Value: terminal}, Var{Key: hostcfg.DisplayEnv, Value: display})
	if g, ok := s.Value("auto"); ok && terminal != "" {
		v := "1"
		if g.Value == no {
			v = "0"
		}
		out = append(out, Var{Key: "AACP_TERMINAL_AUTO", Value: v})
	}
	if c := s.valueOr("claude", ""); c != "" {
		out = append(out, Var{Key: "AACP_CLAUDE", Value: c})
	}
	roots := split(s.valueOr("roots", home))
	var scan []string
	for _, r := range roots {
		if r != home {
			scan = append(scan, r)
		}
	}
	if len(scan) == 0 {
		scan = []string{home}
	}
	out = append(out,
		Var{Key: hostcfg.ProjectRootsEnv, Value: strings.Join(roots, ":")},
		Var{Key: "AACP_PROJECT_SCAN", Value: strings.Join(scan, ":")},
	)
	if accounts := split(s.valueOr("accounts", "")); len(accounts) > 0 {
		out = append(out, Var{Key: contours.HomeEnv, Value: strings.Join(accounts, ":")})
	}
	if g, ok := s.tuned("probes"); ok {
		out = append(out, Var{Key: "AACP_PROBE_PORTS", Value: g.Value})
	}
	return out
}

// tuned is the answer to a rare setting, when the person chose to tune
// them: untuned, they keep whatever the files hold, the defaults included.
func (s *Survey) tuned(id string) (Given, bool) {
	if s.valueOr("tune", no) != yes {
		return Given{}, false
	}
	return s.Value(id)
}

// The keys of each way in: what turning it off takes back to the template.
// The tailnet's are written by its own step; turning it off still takes
// them back here.
var legKeys = map[string][]string{
	"tailscale": {"AACP_TAILSCALE", "AACP_TS_HOSTNAME", "AACP_TS_SERVE", "AACP_TS_ADDR", "AACP_TS_URL", "TS_AUTHKEY"},
	"lan":       {"AACP_LAN_BIND", "AACP_LAN_ADDR", "AACP_LAN_URL", "AACP_LAN_PORT", "AACP_TLS_DIR", "AACP_TLS_CERT", "AACP_TLS_KEY"},
	"domain":    {"AACP_PUBLIC_URL", "AACP_BIND"},
}

// tlsKeys are the keys of the home-network listener with its certificate:
// plain http has no such listener.
var tlsKeys = []string{"AACP_LAN_ADDR", "AACP_LAN_URL", "AACP_TLS_DIR", "AACP_TLS_CERT", "AACP_TLS_KEY"}

// DotEnv is what the answers write into the .env of the clone, besides the
// secrets, and what they take back: the keys of a way in the earlier
// install had and this kit does not. An install whose kit is unknown — by
// hand, with nothing wired — loses no key.
func (s *Survey) DotEnv() (set []Var, drop []string) {
	f := s.Facts
	set = []Var{
		{Key: "AACP_UID", Value: strconv.Itoa(f.Account.UID)},
		{Key: "AACP_GID", Value: strconv.Itoa(f.Account.GID)},
		{Key: "AACP_EXEC_DIR", Value: f.ExecDir()},
		{Key: hostcfg.StateDirEnv, Value: s.StateDir()},
	}
	if f.LANPort != 0 && f.LANPort != PortLAN {
		set = append(set, Var{Key: "AACP_LAN_PORT", Value: strconv.Itoa(f.LANPort)})
	}
	set = append(set, s.passkeyVars()...)

	if s.Has("domain") {
		d := s.valueOr("domain", "")
		set = append(set, Var{Key: "AACP_PUBLIC_URL", Value: "https://" + d})
		if b, ok := s.Value("bind"); ok {
			set = append(set, Var{Key: "AACP_BIND", Value: b.Value})
		}
	}
	plainBefore := s.was.env["AACP_SECURE"] == "0" && slices.Contains(s.was.kit, "lan")
	if s.Has("lan") {
		addr := s.valueOr("lanaddr", "")
		set = append(set, Var{Key: "AACP_LAN_BIND", Value: addr})
		if cert := s.valueOr("lancert", ""); cert == "plain" {
			// The main listener goes on the home address, and the cookie
			// travels without Secure.
			set = append(set, Var{Key: "AACP_BIND", Value: addr}, Var{Key: "AACP_SECURE", Value: "0"})
			drop = append(drop, tlsKeys...)
		} else if cert != "" {
			set = append(set, s.tlsVars(cert)...)
			if plainBefore {
				drop = append(drop, "AACP_SECURE", "AACP_BIND")
			}
		}
	}
	if len(s.legs()) > 0 {
		if g, ok := s.Value("term"); ok {
			v := "0"
			if g.Value == yes {
				v = "1"
			}
			set = append(set, Var{Key: "AACP_TERM_PUBLIC", Value: v})
		}
	}
	if g, ok := s.tuned("push"); ok {
		set = append(set, Var{Key: "AACP_PUSH_CONTACT", Value: g.Value})
	}
	if g, ok := s.tuned("life"); ok {
		idle, max, _ := strings.Cut(g.Value, "/")
		set = append(set, Var{Key: "AACP_SESSION_IDLE", Value: idle}, Var{Key: "AACP_SESSION_MAX", Value: max})
	}

	for _, leg := range Legs {
		if slices.Contains(s.was.kit, leg) && !s.Has(leg) {
			drop = append(drop, legKeys[leg]...)
			if leg == "lan" && plainBefore {
				drop = append(drop, "AACP_SECURE")
			}
		}
	}
	var kept []string
	for _, k := range drop {
		if !slices.ContainsFunc(set, func(v Var) bool { return v.Key == k }) && !slices.Contains(kept, k) {
			kept = append(kept, k)
		}
	}
	return set, kept
}

// passkeyVars are the passkey address and its origin, when this run knows
// them and may set them: a move voids every passkey and goes only with the
// person's word, and a tailnet's name is known only once its node signs in.
func (s *Survey) passkeyVars() []Var {
	to, _ := s.PasskeyHome()
	if strings.HasSuffix(to, ".<tailnet>.ts.net") {
		return nil
	}
	if _, target, moves := s.passkeyMoves(); moves {
		if g, ok := s.Value("passkey"); !ok || g.Value != target {
			return nil
		}
	}
	origin := "https://" + to
	if to == "localhost" {
		origin = fmt.Sprintf("http://localhost:%d", PortPanel)
	}
	return []Var{{Key: "AACP_RP_ID", Value: to}, {Key: "AACP_RP_ORIGINS", Value: origin}}
}

// tlsVars are the keys of the home-network listener for a certificate: the
// directory is mounted as /tls, the key lies beside the certificate, and
// the address the phone opens is the certificate's name at the port on the
// host.
func (s *Survey) tlsVars(cert string) []Var {
	key := strings.TrimSuffix(cert, filepath.Ext(cert)) + ".key"
	name := strings.TrimSuffix(filepath.Base(cert), filepath.Ext(cert))
	for _, c := range s.Found.Certs {
		if c.Cert == cert {
			key, name = c.Key, c.Name
		}
	}
	port := s.Facts.LANPort
	if port == 0 {
		port = PortLAN
	}
	url := "https://" + name
	if port != 443 {
		url += ":" + strconv.Itoa(port)
	}
	return []Var{
		{Key: "AACP_TLS_DIR", Value: filepath.Dir(cert)},
		{Key: "AACP_TLS_CERT", Value: "/tls/" + filepath.Base(cert)},
		{Key: "AACP_TLS_KEY", Value: "/tls/" + filepath.Base(key)},
		{Key: "AACP_LAN_ADDR", Value: fmt.Sprintf(":%d", PortLAN)},
		{Key: "AACP_LAN_PORT", Value: strconv.Itoa(port)},
		{Key: "AACP_LAN_URL", Value: url},
	}
}

// Packages are the apt packages the root step installs: what the person
// agreed to in block P, claude aside, which installs as the person.
func (s *Survey) Packages() []string { return s.packages() }
