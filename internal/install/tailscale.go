package install

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ---- S11: the tailnet node ----

const (
	tsContainer = "aacpanel-tailscale"
	// TSVolume keeps the node's keys and its name in the tailnet.
	TSVolume = Project + "_aacpanel-ts-state"
	// tsWait is how long a node gets to sign in with its key.
	tsWait = 90 * time.Second
)

// tsStatus is what tailscale status says of the node.
type tsStatus struct {
	BackendState string
	Self         struct {
		DNSName string
	}
	CertDomains []string
}

func (s tsStatus) dns() string { return strings.TrimSuffix(s.Self.DNSName, ".") }

// tsStatus asks the node in its container; a container that is not up
// answers nothing.
func (in *Install) tsStatus(r *Run) (tsStatus, bool) {
	out, err := r.Exec(Cmd{Argv: in.docker("exec", tsContainer, "tailscale", "status", "--json"), Quiet: true, Limit: 30 * time.Second})
	var st tsStatus
	if err != nil || json.Unmarshal([]byte(out), &st) != nil {
		return tsStatus{}, false
	}
	return st, true
}

// tsLeg tells whether the node is a further way in beside a domain, with a
// listener of its own, rather than the way in and the home of the passkeys.
func (in *Install) tsLeg() bool { return in.S.Has("domain") }

// tsWant are the keys of .env the node's name gives: the passkey address
// when the tailnet is the way in, the address of its listener when it is a
// leg beside a domain.
func (in *Install) tsWant(dns string) (set []Var, drop []string) {
	set = []Var{{Key: "AACP_TAILSCALE", Value: "1"}, {Key: "AACP_TS_HOSTNAME", Value: in.S.valueOr("tsname", "aacpanel")}}
	if in.tsLeg() {
		set = append(set, Var{Key: "AACP_TS_SERVE", Value: "./deploy/tailscale/serve-leg.json"}, Var{Key: "AACP_TS_ADDR", Value: ":8778"})
		if dns != "" {
			set = append(set, Var{Key: "AACP_TS_URL", Value: "https://" + dns})
		}
		return set, nil
	}
	if dns != "" {
		set = append(set, Var{Key: "AACP_RP_ID", Value: dns}, Var{Key: "AACP_RP_ORIGINS", Value: "https://" + dns})
	}
	return set, []string{"AACP_TS_SERVE", "AACP_TS_ADDR", "AACP_TS_URL"}
}

// tsTarget is where the node's serve proxies: the main listener, or the
// listener of the leg.
func (in *Install) tsTarget() string {
	if in.tsLeg() {
		return "http://aacpanel:8778"
	}
	return fmt.Sprintf("http://aacpanel:%d", PortPanel)
}

func (in *Install) template() []byte {
	raw, _ := in.M.ReadFile(filepath.Join(in.clone(), ".env.example"))
	return raw
}

func (in *Install) tailscaleStep() *Step {
	return &Step{ID: "tailscale", Title: "Tailscale",
		Done: func(r *Run) (bool, error) {
			st, ok := in.tsStatus(r)
			if !ok || st.BackendState != "Running" || st.dns() == "" {
				return false, nil
			}
			raw, env, err := in.readEnv(r)
			if err != nil || strings.TrimSpace(env["TS_AUTHKEY"]) != "" {
				return false, nil
			}
			set, drop := in.tsWant(st.dns())
			return len(changed(env, set)) == 0 && len(live(raw, drop)) == 0, nil
		},
		Apply: func(r *Run) error {
			raw, _, err := in.readEnv(r)
			if err != nil {
				return err
			}
			set, drop := in.tsWant("")
			if g, ok := in.S.Value("tskey"); ok && g.Value != "" {
				r.Hide(g.Value)
				set = append(set, Var{Key: "TS_AUTHKEY", Value: g.Value})
			}
			if err := writeFile(in.dotEnvPath(), Edit(raw, set, drop, in.template()), 0o600); err != nil {
				return err
			}
			if _, err := r.Exec(Cmd{Argv: in.docker("volume", "inspect", TSVolume), Quiet: true, Limit: time.Minute}); err != nil {
				if err := r.Once(Volume, TSVolume, "data"); err != nil {
					return err
				}
			}
			if err := r.Once(TSNode, in.S.valueOr("tsname", "aacpanel"), ""); err != nil {
				return err
			}
			if _, err := r.Exec(Cmd{Argv: in.docker("compose", "up", "-d", "tailscale"), Dir: in.clone()}); err != nil {
				why, fix := stackDiagnosis(err)
				return fail(why, err, fix...)
			}
			var st tsStatus
			up, err := r.Until(tsWait, 3*time.Second, func() (bool, error) {
				s, ok := in.tsStatus(r)
				st = s
				return ok && s.BackendState == "Running", nil
			})
			if err != nil {
				return err
			}
			if !up {
				logs, _ := r.Exec(Cmd{Argv: in.docker("logs", "--tail", "20", tsContainer), Limit: time.Minute})
				state := st.BackendState
				if state == "" {
					state = "no answer"
				}
				return &Failed{Diagnosis: fmt.Sprintf("the tailnet node did not sign in in %d s (%s): the auth key expired, or a one-time key was used already", int(tsWait.Seconds()), state),
					Fix:  []string{"Make a new key in the admin console (Settings → Keys) and run ./install.sh again: it asks for the key while the node is not in."},
					Tail: lastLines(logs, tailLines)}
			}
			if st.dns() == "" || len(st.CertDomains) == 0 {
				return &Failed{Diagnosis: "the tailnet gives the node no name with a certificate",
					Fix: []string{"Enable MagicDNS and HTTPS Certificates in the tailnet admin console → DNS, then run ./install.sh again."}}
			}
			// The node is in: the key has done its work and leaves .env,
			// and the panel learns the name it is reached by.
			raw, _, err = in.readEnv(r)
			if err != nil {
				return err
			}
			set, drop = in.tsWant(st.dns())
			if err := writeFile(in.dotEnvPath(), Edit(raw, set, append(drop, "TS_AUTHKEY"), in.template()), 0o600); err != nil {
				return err
			}
			if _, err := r.Exec(Cmd{Argv: in.docker("compose", "up", "-d"), Dir: in.clone()}); err != nil {
				why, fix := stackDiagnosis(err)
				return fail(why, err, fix...)
			}
			return nil
		},
		Verify: func(r *Run) error {
			_, env, err := in.readEnv(r)
			if err != nil {
				return err
			}
			if err := in.panelUp(r, env); err != nil {
				return err
			}
			st, ok := in.tsStatus(r)
			if !ok || st.BackendState != "Running" {
				return &Failed{Diagnosis: "the tailnet node is not running after the panel came up: docker logs " + tsContainer}
			}
			serve, err := r.Exec(Cmd{Argv: in.docker("exec", tsContainer, "tailscale", "serve", "status"), Limit: 30 * time.Second})
			if err != nil || !strings.Contains(serve, in.tsTarget()) {
				return &Failed{Diagnosis: "tailscale serve does not proxy to " + in.tsTarget() + ": docker exec " + tsContainer + " tailscale serve status",
					Tail: lastLines(serve, tailLines)}
			}
			r.Say(Pass, fmt.Sprintf("node %s running · https://%s proxies to %s", st.dns(), st.dns(), in.tsTarget()))
			return nil
		},
		Undo: UndoKind,
	}
}
