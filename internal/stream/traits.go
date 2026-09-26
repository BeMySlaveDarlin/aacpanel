package stream

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Trait is what a model takes: whether it thinks with an effort at all, which
// efforts, and whether it has the auto permission mode. It is a property of
// the model, the same in every account.
type Trait struct {
	Effort   bool     `json:"effort"`
	Efforts  []string `json:"efforts,omitempty"`
	AutoMode bool     `json:"autoMode"`
}

// Traits is what the holders learned of the models, by resolved id.
type Traits struct {
	At     int64            `json:"at"`
	Models map[string]Trait `json:"models"`
}

// TraitsPath keeps what claude said of its models in the last handshake of
// any session on the stream. The screens of the map strike out what a model
// does not take, and no other source says it without a session of its own.
func TraitsPath() string {
	return filepath.Join(keptDir(), "traits.json")
}

// keepTraits lays what a handshake said of the models over what earlier ones
// said: an account that offers fewer models does not take the others' away.
func keepTraits(init json.RawMessage, now time.Time) error {
	var body struct {
		Models []struct {
			Resolved string   `json:"resolvedModel"`
			Effort   bool     `json:"supportsEffort"`
			Efforts  []string `json:"supportedEffortLevels"`
			AutoMode bool     `json:"supportsAutoMode"`
		} `json:"models"`
	}
	if len(init) == 0 || json.Unmarshal(init, &body) != nil || len(body.Models) == 0 {
		return nil
	}
	kept := Traits{Models: map[string]Trait{}}
	if raw, err := os.ReadFile(TraitsPath()); err == nil {
		_ = json.Unmarshal(raw, &kept)
		if kept.Models == nil {
			kept.Models = map[string]Trait{}
		}
	}
	for _, m := range body.Models {
		if m.Resolved == "" {
			continue
		}
		kept.Models[m.Resolved] = Trait{Effort: m.Effort, Efforts: m.Efforts, AutoMode: m.AutoMode}
	}
	kept.At = now.Unix()
	out, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	path := TraitsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Two holders starting at once each write their own file and rename it:
	// one of the two merges is lost until the next handshake, never the file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".traits-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
