package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"aacpanel/internal/action"
)

const (
	secretMode     = 0o600
	secretsDirMode = 0o700
)

// SecretsDir is where the host keeps the secrets a person filled in for a
// session: a file a secret, readable by the owner alone. It lies beside the
// guards in the owner's state — not under the state directory of the panel,
// which is mounted into the container, and not among the files sent from the
// phone, which the feed opens.
func SecretsDir() string {
	return filepath.Join(stateDir(), "secrets")
}

// secretsHome makes the directory of secrets, closed to everyone but the
// owner, whatever it was before.
func secretsHome() (string, error) {
	dir := SecretsDir()
	if err := os.MkdirAll(dir, secretsDirMode); err != nil {
		return "", fmt.Errorf("the directory of secrets was not created: %w", err)
	}
	if err := os.Chmod(dir, secretsDirMode); err != nil {
		return "", fmt.Errorf("permissions of the directory of secrets: %w", err)
	}
	return dir, nil
}

// secretPut saves the notepad and tells the session where it lies. The file
// is written first: a session told before it exists would open nothing. A
// file the session was not told of stays, and the refusal names it, so the
// person can say it in the conversation instead. Neither the answer nor an
// error carries a byte of the notepad.
func (e *Executor) secretPut(ctx context.Context, target string, secret *action.Secret) (string, error) {
	if secret == nil || !action.SecretName(secret.Name) {
		return "", errors.New("secret.put without a valid name of the secret")
	}
	path, err := writeSecret(secret.Name, secret.Text)
	if err != nil {
		return "", err
	}
	s, err := findOneLiveSession(target)
	if err == nil {
		_, err = deliverText(ctx, s, senderName, secretMessage(secret.Name, path, secret.Text))
	}
	if err != nil {
		return "", fmt.Errorf("saved to %s, but the session was not told: %w", path, err)
	}
	return fmt.Sprintf("saved %s (%d bytes) and told %s", secret.Name, len(secret.Text), s.Name), nil
}

// writeSecret replaces the file of the secret whole: the notepad goes into a
// temporary file of the same directory, made by this call alone and closed to
// everyone but the owner from its first byte, and is renamed over the file. A
// reader sees the old secret or the new one, never half of either, and a
// write that failed leaves no piece of the notepad behind. The temporary name
// starts with a dot, which no name of a secret does, so the list never shows it.
func writeSecret(name, text string) (string, error) {
	dir, err := secretsHome()
	if err != nil {
		return "", err
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("the name of the temporary file was not built: %w", err)
	}
	tmp := filepath.Join(dir, ".tmp-"+hex.EncodeToString(buf[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, secretMode)
	if err != nil {
		return "", fmt.Errorf("the secret was not written: %w", err)
	}
	done := false
	defer func() {
		if !done {
			os.Remove(tmp)
		}
	}()
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return "", fmt.Errorf("the secret was not written: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", fmt.Errorf("the secret was not written: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("the secret was not written: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.Rename(tmp, path); err != nil {
		return "", fmt.Errorf("the secret was not put in place: %w", err)
	}
	done = true
	return path, nil
}

// secretKey is a line of a notepad that sets a variable, as an env file or a
// shell script sets one.
var secretKey = regexp.MustCompile(`^\s*(export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$`)

// secretMessage is what the session is told of a saved secret: where it lies,
// how long it is, which variables it sets and which of them the person left
// empty — the names, never a value — and how a secret is used.
func secretMessage(name, path, text string) string {
	lines := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		lines++
	}
	unit := "lines"
	if lines == 1 {
		unit = "line"
	}
	msg := fmt.Sprintf(`The secret "%s" is saved: %s (0600, %d %s).`, name, path, lines, unit)
	keys, empty := secretKeys(text)
	if len(keys) > 0 {
		msg += " Keys: " + strings.Join(keys, ", ")
		if len(empty) > 0 {
			msg += "; left empty: " + strings.Join(empty, ", ")
		}
		msg += "."
	}
	return msg + " Use it by its path and never read or print it: whatever you read goes into the transcript."
}

// secretKeys returns the variables a notepad sets, in the order they first
// appear, and the ones among them whose value is empty — by the last line
// that sets each, as a shell reading the file would leave them.
func secretKeys(text string) (keys, empty []string) {
	blank := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		m := secretKey.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[2]
		if _, seen := blank[key]; !seen {
			keys = append(keys, key)
		}
		value := strings.TrimSpace(m[3])
		blank[key] = value == "" || value == `""` || value == `''`
	}
	for _, key := range keys {
		if blank[key] {
			empty = append(empty, key)
		}
	}
	return keys, empty
}

// secretDrop removes a secret. Only what the list shows can go: a regular
// file under a name of a secret.
func secretDrop(name string) (string, error) {
	if !action.SecretName(name) {
		return "", errors.New("secret.drop without a valid name of the secret")
	}
	path := filepath.Join(SecretsDir(), name)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		return "", fmt.Errorf("there is no secret %s", name)
	}
	if err != nil {
		return "", fmt.Errorf("the secret %s was not looked at: %w", name, err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("the secret %s was not removed: %w", name, err)
	}
	return "removed the secret " + name, nil
}

// Secrets lists the secrets of the host: the directory, and every regular
// file in it under a name of a secret, sorted by name. A directory nobody has
// written to yet holds none.
func (e *Executor) Secrets(context.Context) (*action.Secrets, error) {
	dir := SecretsDir()
	out := &action.Secrets{Dir: dir, Secrets: []action.SecretFile{}}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the directory of secrets was not read: %w", err)
	}
	for _, entry := range entries {
		if !action.SecretName(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out.Secrets = append(out.Secrets, action.SecretFile{
			Name:  entry.Name(),
			Bytes: info.Size(),
			At:    info.ModTime().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		})
	}
	return out, nil
}
