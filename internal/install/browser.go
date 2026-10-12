package install

import (
	"errors"
	"os"
	"path/filepath"
)

func browserProgram(m Machine) string {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := m.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func (s *Survey) browserQuestion() Question {
	program := s.Found.Browser
	if p, ok := s.before(HostEnvFile, "AACP_CHROME"); ok && p != "" {
		program = p
	}
	q := Question{
		ID: "browser", Tab: "Browser", Form: One,
		Prompt: "Browser on this host, controlled from the panel?",
		Flag:   "--browser", Default: "off",
		Own:    "Type the absolute Chrome or Chromium executable path.",
		Writes: []Target{{HostEnvFile, "AACP_BROWSER"}, {HostEnvFile, "AACP_CHROME"}},
		Note: "Panel access grants control of these signed-in sites. " +
			"Only public websites; no uploads or downloads. " +
			"Cookies stay in separate host profiles; your desktop browser profile is never read.",
		Options: []Option{
			{
				Value:  "off",
				Label:  "Off (Recommended)",
				Source: "default",
				Detail: "No browser process or control socket.",
			},
		},
		Check: func(v string) error {
			if v == "off" {
				return nil
			}
			if !filepath.IsAbs(v) {
				return errors.New("name an absolute browser executable path or off")
			}
			if _, err := s.m.LookPath(v); err != nil {
				return errors.New("the browser executable is unavailable")
			}
			return nil
		},
	}
	if program != "" {
		q.Options = append(q.Options, Option{Value: program, Label: "Enable", Detail: program, Source: "browser executable"})
	}
	if enabled, ok := s.before(HostEnvFile, "AACP_BROWSER"); ok {
		value := "off"
		if enabled == "1" && program != "" {
			value = program
		}
		s.preferValue(&q, value, "")
	}
	return q
}

// The root is recorded as data so uninstall can leave sign-ins intact. Its
// children are created only by the host executor, never by the service.
func (in *Install) browserStateStep() *Step {
	base := in.M.Env("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(in.home(), ".local", "state")
	}
	path := filepath.Join(base, "aacpanel", "browser")
	return &Step{ID: "browser-state", Title: "Browser profiles", Undo: UndoKind, Apply: func(r *Run) error {
		if err := in.makeDirs(r, filepath.Dir(path)); err != nil {
			return err
		}
		if _, err := os.Stat(path); err == nil {
			return r.Adopt(Dir, path, "data browser")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := r.Record(Dir, path, "created data browser"); err != nil {
			return err
		}
		return os.Mkdir(path, 0700)
	}}
}
