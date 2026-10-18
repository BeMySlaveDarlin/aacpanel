// Package sessionfile names the files the panel keeps of a session on the
// host, beside the rest of its state: the checklist of its work, its role in
// its project. A session is told by the account it runs in — the config
// directory of claude — by where it works and by its name, so the sessions of
// one place keep files of their own, and a session started again under its
// name finds what it left. What "where" is belongs to the file: a checklist
// is kept by the directory, a role by the repository. The collector computes
// the same names in agent/sessionfile.py.
package sessionfile

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Name is the name of the file kept of a session: a hash of the config
// directory, of where it works and of its name, or of the two paths alone for
// a session without a name. A path does not make a file name, so the name is
// a hash; the file holds what it was made of, and a reader takes it only for
// the session it names.
func Name(configDir, where, name string) string {
	key := configDir + "\x00" + where
	if name != "" {
		key += "\x00" + name
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) + ".json"
}

// Dir is the directory the files of one kind lie in: the owner's state,
// beside the rest of what the panel keeps on the host.
func Dir(kind string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "aacpanel", kind)
}
