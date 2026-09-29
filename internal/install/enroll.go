package install

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
)

// ---- S15: the first device ----

// codeLife is how long an enrolment code lives: auth.EnrollTTL of the panel.
const codeLife = 5 * time.Minute

// Enrollment is a code a device enrols with, where to open the panel to
// enter it, and how to get another: the screen draws it in a frame with its
// countdown, the plain view prints it once.
type Enrollment struct {
	Code    string
	Expires time.Time
	// Open are the addresses a device enters the code at, the first the
	// passkeys' own; Hints are what to do where there is no browser here.
	Open, Hints []string
	// Again gets a new code; the old one stays good until it expires.
	Again func() (string, time.Time, error)
}

// newCode asks the panel for a code: its own binary in its container prints
// one and writes it into the database. The code goes to the screen only.
func (in *Install) newCode(r *Run) (string, time.Time, error) {
	out, err := r.Exec(Cmd{Argv: in.docker("compose", "exec", "-T", "aacpanel", "/aacpanel", "-enroll"),
		Dir: in.clone(), Quiet: true, Limit: time.Minute})
	code := strings.TrimSpace(firstLine(out))
	if err != nil || code == "" {
		return "", time.Time{}, fail("the panel gave no enrolment code: docker compose exec aacpanel /aacpanel -enroll", err,
			"docker compose logs aacpanel says why; the code needs the database up.")
	}
	return code, r.clock().Now().Add(codeLife), nil
}

// devices counts the devices enrolled in the panel, asked through its
// local listener; ok is false when the listener does not say.
func (in *Install) devices() (int, bool) {
	var list struct {
		Devices []json.RawMessage `json:"devices"`
	}
	status, err := in.page("/api/devices", &list)
	if err != nil || status != 200 {
		return 0, false
	}
	return len(list.Devices), true
}

// addresses are where a device opens the panel to enrol: the passkeys'
// address — a domain or the tailnet's name, else this machine — and what to
// do from a machine without a browser of its own.
func (in *Install) addresses(env map[string]string) (open, hints []string) {
	rp := strings.TrimSpace(env["AACP_RP_ID"])
	switch {
	case env["AACP_PUBLIC_URL"] != "":
		open = append(open, env["AACP_PUBLIC_URL"])
	case rp != "" && rp != "localhost":
		open = append(open, "https://"+rp)
	}
	open = append(open, fmt.Sprintf("http://localhost:%d", PortPanel))
	if in.LANToken(env) {
		hints = append(hints, "a phone at home: "+env["AACP_LAN_URL"]+" signs in with the token, AACP_TOKEN in "+in.short(in.dotEnvPath()))
	}
	raw, _ := in.M.ReadFile(in.hostEnvPath())
	if hostcfg.Parse(raw)[hostcfg.DisplayEnv] == "" {
		host := in.S.valueOr("host", "")
		if host == "" {
			host = "this-machine"
		}
		hints = append(hints, fmt.Sprintf("no browser here: ssh -L %d:127.0.0.1:%d %s@%s on your computer, then http://localhost:%d there",
			PortPanel, PortPanel, in.user(), strings.ToLower(host), PortPanel))
	}
	return open, hints
}

// LANToken tells whether a phone at home signs in with the token: the home
// network is the one way in besides this machine, and the .env holds one.
func (in *Install) LANToken(env map[string]string) bool {
	return env["AACP_LAN_BIND"] != "" && strings.TrimSpace(env["AACP_TOKEN"]) != "" &&
		env["AACP_PUBLIC_URL"] == "" && env["AACP_TAILSCALE"] != "1"
}

// Enroll gets a code and hands it to whoever shows it: the screen's frame,
// which waits for the person, or the lines of the plain view.
func (in *Install) Enroll(r *Run) error {
	_, env, err := in.readEnv(r)
	if err != nil {
		return err
	}
	code, expires, err := in.newCode(r)
	if err != nil {
		return err
	}
	e := Enrollment{Code: code, Expires: expires, Again: func() (string, time.Time, error) { return in.newCode(r) }}
	e.Open, e.Hints = in.addresses(env)
	if r.Enroll != nil {
		if err := r.Enroll(e); err != nil {
			return err
		}
		r.Say(Pass, "a code was on the screen; another device: ./install.sh enroll")
		return nil
	}
	r.Say(Pass, "open "+e.Open[0]+" and enter the code "+code)
	for _, o := range e.Open[1:] {
		r.Say(Note, "or "+o)
	}
	for _, h := range e.Hints {
		r.Say(Note, h)
	}
	r.Say(Note, fmt.Sprintf("valid %s, until %s, once · another code: ./install.sh enroll", codeLife, expires.Local().Format("15:04:05")))
	return nil
}

// EnrollStep is S15: a code for the first device, when there is none yet.
func (in *Install) EnrollStep() *Step {
	return &Step{ID: "enroll", Title: "First device",
		Done: func(r *Run) (bool, error) {
			n, ok := in.devices()
			return ok && n > 0, nil
		},
		Apply: in.Enroll,
	}
}
