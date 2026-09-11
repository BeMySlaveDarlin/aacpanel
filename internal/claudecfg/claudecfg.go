// Package claudecfg manages directory trust in .claude.json.
package claudecfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TrustKey is the trust key in .claude.json.
const TrustKey = "hasTrustDialogAccepted"

// FileName is the name of the account config inside the account directory.
const FileName = ".claude.json"

// Path returns the path to the config of an account.
func Path(configDir string) string {
	return filepath.Join(configDir, FileName)
}

// Dir normalises a directory to the form claude writes it in.
func Dir(dir string) string {
	dir = strings.TrimRight(dir, "/")
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = strings.TrimRight(real, "/")
	}
	return dir
}

// AsksForTrust reports whether claude will meet a session with the trust prompt in this directory.
func AsksForTrust(dir string) bool {
	_, err := os.Lstat(filepath.Join(Dir(dir), ".git"))
	return err == nil
}

// Trusted reports whether the trust prompt was passed for a directory.
func Trusted(configFile, dir string) (bool, error) {
	raw, err := os.ReadFile(configFile)
	if err != nil {
		return false, err
	}
	var cfg struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return false, err
	}
	p, ok := cfg.Projects[Dir(dir)]
	if !ok {
		return false, nil
	}
	return p.Trusted, nil
}

// Grant marks directories as trusted and reports how many were granted just now.
func Grant(configFile string, dirs ...string) (int, error) {
	if len(dirs) == 0 {
		return 0, nil
	}

	cfg := map[string]any{}
	raw, err := os.ReadFile(configFile)
	switch {
	case err == nil:
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&cfg); err != nil {
			return 0, fmt.Errorf("parsing %s: %w", configFile, err)
		}
	case os.IsNotExist(err):
	default:
		return 0, fmt.Errorf("reading %s: %w", configFile, err)
	}

	projects, _ := cfg["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
	}
	added := 0
	for _, dir := range dirs {
		dir = Dir(dir)
		if dir == "" {
			continue
		}
		entry, _ := projects[dir].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
		}
		if trusted, _ := entry[TrustKey].(bool); trusted {
			continue
		}
		entry[TrustKey] = true
		projects[dir] = entry
		added++
	}
	if added == 0 {
		return 0, nil
	}
	cfg["projects"] = projects

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return 0, fmt.Errorf("encoding %s: %w", configFile, err)
	}
	if err := writeAtomic(configFile, buf.Bytes()); err != nil {
		return 0, err
	}
	return added, nil
}

func writeAtomic(path string, body []byte) error {
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".aacpanel-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
