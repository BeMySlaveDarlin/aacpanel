package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/executor"
)

// marker stands for the value of a credential: it may land in the file of
// the secret and nowhere else.
const marker = "ghp_m4rker_not_for_any_log"

// lockedBuffer takes the log of the executor while the server writes it from
// its own goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The notepad crosses the socket of the executor and lands in its file; the
// journal of the host, the answer and the refusal say what was done to which
// secret and never what it holds — whether the request was refused at the
// door, saved and not told, or not saved at all.
func TestTheAuditKeepsTheNotepadOut(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("AACP_SESSIONS", t.TempDir())
	journal := &lockedBuffer{}
	was := log.Writer()
	log.SetOutput(journal)
	t.Cleanup(func() { log.SetOutput(was) })

	dir, err := os.MkdirTemp("", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	srv := action.NewServer(socket, audited{next: executor.New(nil, "")}, 5*time.Second)
	ln, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Run(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Saved, and the session is not there to be told.
	text := "GH_TOKEN=" + marker + "\n"
	resp, err := action.NewClient(socket, 5*time.Second).Do(t.Context(), action.Request{
		ID: "r1", Kind: action.SecretPut, Target: "nobody", Secret: &action.Secret{Name: "github-token", Text: text},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || !strings.Contains(resp.Error, "but the session was not told") {
		t.Fatalf("the executor answered %+v", resp)
	}
	if raw, _ := os.ReadFile(filepath.Join(executor.SecretsDir(), "github-token")); string(raw) != text {
		t.Fatalf("the file holds %q", raw)
	}

	// Not saved: the name is taken by a directory.
	if err := os.MkdirAll(filepath.Join(executor.SecretsDir(), "taken", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	failed, err := action.NewClient(socket, 5*time.Second).Do(t.Context(), action.Request{
		ID: "r2", Kind: action.SecretPut, Target: "nobody", Secret: &action.Secret{Name: "taken", Text: text},
	})
	if err != nil || failed.OK {
		t.Fatalf("a save over a directory answered %+v, %v", failed, err)
	}

	// Refused at the door: a client that skipped its own check sends a
	// notepad with a NUL byte in it.
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(action.Request{ID: "r3", Kind: action.SecretPut, Target: "nobody",
		Secret: &action.Secret{Name: "github-token", Text: text + "\x00"}})
	conn.Write(append(body, '\n'))
	var refused action.Response
	if err := json.NewDecoder(conn).Decode(&refused); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if refused.OK || !strings.Contains(refused.Error, "NUL") {
		t.Fatalf("a notepad with a NUL byte answered %+v", refused)
	}

	said := journal.String()
	if !strings.Contains(said, "audit phase=done action=secret.put target=nobody") ||
		!strings.Contains(said, "rejected: the notepad holds a NUL byte") {
		t.Fatalf("the journal of the host was not caught:\n%s", said)
	}
	for _, out := range []string{said, resp.Error, resp.Detail, failed.Error, refused.Error} {
		if strings.Contains(out, marker) {
			t.Errorf("the notepad left the file:\n%s", out)
		}
	}
}

// The list of secrets passes the audit wrapper, as every question does.
func TestAuditedForwardsTheSecrets(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	asker, ok := any(audited{next: executor.New(nil, "")}).(action.SecretsAsker)
	if !ok {
		t.Fatal("the journal wrapper does not implement action.SecretsAsker — the server will answer " +
			"\"keeps no secrets\" on a host that keeps them")
	}
	got, err := asker.Secrets(t.Context())
	if err != nil || got.Dir != executor.SecretsDir() {
		t.Errorf("the wrapper answered %+v, %v", got, err)
	}
	if _, err := any(audited{next: muteExec{}}).(action.SecretsAsker).Secrets(t.Context()); err == nil {
		t.Error("an executor that keeps no secrets answered with a list")
	}
}
