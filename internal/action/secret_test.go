package action

import (
	"context"
	"strings"
	"testing"
)

// marker stands for the value of a credential: a refusal names the rule the
// notepad broke and never quotes it.
const marker = "ghp_m4rker_not_for_any_log"

func put(name, text string) Request {
	return Request{ID: "1", Kind: SecretPut, Target: "aacpanel", Secret: &Secret{Name: name, Text: text}}
}

// A name of a secret is a plain file of its directory: no way out of it, no
// hidden file — the temporary one of a write is hidden — and nothing a
// shell or a URL reads twice.
func TestASecretIsNamedAsAPlainFile(t *testing.T) {
	for _, name := range []string{"github-token", "evirma-db.env", "a", "0", "a_b-c.d", strings.Repeat("a", 64), "a..b"} {
		if err := put(name, "A=1").Validate(); err != nil {
			t.Errorf("%q is refused: %v", name, err)
		}
		if err := (Request{ID: "1", Kind: SecretDrop, Target: name}).Validate(); err != nil {
			t.Errorf("removing %q is refused: %v", name, err)
		}
	}
	for _, name := range []string{"../x", ".x", "..", ".", "A", "Token", "", strings.Repeat("a", 65),
		"a/b", "-x", "_x", "a b", "a\nb", "ä", ".tmp-0123"} {
		if err := put(name, "A=1").Validate(); err == nil {
			t.Errorf("%q is taken for the name of a secret", name)
		}
		if err := (Request{ID: "1", Kind: SecretDrop, Target: name}).Validate(); err == nil {
			t.Errorf("removing %q is taken", name)
		}
	}
}

// A notepad is text the person typed: an empty one saves nothing, a huge one
// is not a credential, and a NUL byte is not text. The refusal says which,
// and never what the notepad holds.
func TestANotepadIsTextOfASecretsSize(t *testing.T) {
	for _, text := range []string{"A=1", "  \n" + marker + "\n", strings.Repeat("x", SecretTextMax),
		"A=1\r\nB=2\x1b[0m\n"} {
		if err := put("a", text).Validate(); err != nil {
			t.Errorf("a notepad of %d bytes is refused: %v", len(text), err)
		}
	}
	for _, c := range []struct{ text, says string }{
		{"", "empty"},
		{" \n\t \r\n", "empty"},
		{marker + strings.Repeat("x", SecretTextMax+1-len(marker)), "65537 bytes"},
		{"A=" + marker + "\x00", "NUL"},
	} {
		err := put("a", c.text).Validate()
		if err == nil {
			t.Errorf("a notepad of %d bytes is taken", len(c.text))
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("the refusal does not say %q: %v", c.says, err)
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("the refusal quotes the notepad: %v", err)
		}
	}
	if err := put("../"+marker, "A=1").Validate(); err == nil || strings.Contains(err.Error(), marker) {
		t.Errorf("a bad name is refused as %v", err)
	}
}

// The secret travels only with secret.put, and secret.drop names it by its
// target alone; the list is a question of the host with no target and
// nothing to carry.
func TestTheSecretGoesWithItsActionAlone(t *testing.T) {
	cases := []struct {
		name string
		req  Request
	}{
		{"a save without the secret", Request{ID: "1", Kind: SecretPut, Target: "aacpanel"}},
		{"a save with the text as a message", Request{ID: "1", Kind: SecretPut, Target: "aacpanel", Text: "A=1",
			Secret: &Secret{Name: "a", Text: "A=1"}}},
		{"a save for a session with a forbidden name", Request{ID: "1", Kind: SecretPut, Target: "a:b",
			Secret: &Secret{Name: "a", Text: "A=1"}}},
		{"a removal that carries a notepad", Request{ID: "1", Kind: SecretDrop, Target: "a",
			Secret: &Secret{Name: "a", Text: "A=1"}}},
		{"a message carrying a secret", Request{ID: "1", Kind: SessionSend, Target: "aacpanel", Text: "hi",
			Secret: &Secret{Name: "a", Text: "A=1"}}},
		{"the list of a session", Request{Ask: AskSecrets, Target: "aacpanel"}},
		{"the list carrying a secret", Request{Ask: AskSecrets, Secret: &Secret{Name: "a", Text: "A=1"}}},
	}
	for _, c := range cases {
		if err := c.req.Validate(); err == nil {
			t.Errorf("%s is taken", c.name)
		}
	}
	if err := (Request{Ask: AskSecrets}).Validate(); err != nil {
		t.Errorf("the list is refused: %v", err)
	}
}

type secretsExec struct {
	Executor
	list *Secrets
}

func (e secretsExec) Secrets(context.Context) (*Secrets, error) { return e.list, nil }

// The list crosses the socket as the executor gave it; an executor that keeps
// no secrets says so rather than answering with nothing.
func TestServerListsTheSecretsOfTheHost(t *testing.T) {
	want := &Secrets{Dir: "/home/u/.local/state/aacpanel/secrets",
		Secrets: []SecretFile{{Name: "github-token", Bytes: 41, At: "2026-10-09T12:34:56.789Z"}}}
	got, err := serve(t, secretsExec{Executor: okExecutor("done"), list: want}).Secrets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Dir != want.Dir || len(got.Secrets) != 1 || got.Secrets[0] != want.Secrets[0] {
		t.Errorf("the secrets arrived as %+v", got)
	}
	if _, err := serve(t, okExecutor("done")).Secrets(context.Background()); err == nil {
		t.Error("an executor that keeps no secrets answered with a list")
	}
}
