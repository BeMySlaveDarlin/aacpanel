package action

import (
	"context"
	"regexp"
	"strings"
)

// AskSecrets asks for the secrets the host keeps: their names, sizes and
// times, never what they hold. It changes nothing and goes to no journal.
const AskSecrets = "secrets"

// SecretTextMax is the most a notepad of a secret holds, in bytes.
const SecretTextMax = 64 << 10

// secretName is the name of a secret: a file of its directory that is neither
// hidden nor a way out of it, and that the temporary file of a write, which
// starts with a dot, never looks like.
var secretName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// SecretName reports whether s names a secret.
func SecretName(s string) bool { return secretName.MatchString(s) }

// Secret is what secret.put writes: the name of the file and the notepad the
// person filled in. The text crosses the socket and nothing else: no journal,
// no log, no answer carries it.
type Secret struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// SecretFile is one secret on the host, as the list shows it: what the file
// is, not what it holds. At is when it was last written, in RFC 3339.
type SecretFile struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	At    string `json:"at"`
}

// Secrets is the directory the host keeps the secrets in and what lies there,
// sorted by name.
type Secrets struct {
	Dir     string       `json:"dir"`
	Secrets []SecretFile `json:"secrets"`
}

// SecretsAsker is an executor that keeps the secrets of the host.
type SecretsAsker interface {
	Secrets(ctx context.Context) (*Secrets, error)
}

// errSecretName names the rule and not the name: a name that breaks it may be
// anything at all, a pasted token included.
var errSecretName = ErrBadRequest{msg: "the name of a secret is 1 to 64 characters of a-z, 0-9, '.', '_' and '-', " +
	"starting with a letter or a digit"}

// validateSecret checks the two actions of the notepad. A refusal names the
// rule the notepad broke and never quotes it.
func validateSecret(r Request) error {
	if r.Kind == SecretDrop {
		if r.Secret != nil {
			return badRequest("action %s names the secret by its target and carries nothing else", r.Kind)
		}
		if !SecretName(r.Target) {
			return errSecretName
		}
		return nil
	}
	if r.Secret == nil {
		return badRequest("action %s without the secret to save", r.Kind)
	}
	if !SecretName(r.Secret.Name) {
		return errSecretName
	}
	text := r.Secret.Text
	switch {
	case strings.TrimSpace(text) == "":
		return badRequest("the notepad is empty: there is nothing to save")
	case len(text) > SecretTextMax:
		return badRequest("the notepad is %d bytes, more than the %d a secret holds", len(text), SecretTextMax)
	case strings.IndexByte(text, 0) >= 0:
		return badRequest("the notepad holds a NUL byte: a secret is a text file")
	}
	return nil
}
