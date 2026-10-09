package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
)

// marker stands for the value of a credential: it may land in the file and
// nowhere else.
const marker = "ghp_m4rker_not_for_any_log"

// secretsHere moves the state of the owner into the test and returns the
// directory of secrets under it.
func secretsHere(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return SecretsDir()
}

// letterSession is a live session named aacpanel that takes messages as
// letters on its socket, and the channel of what it got.
func letterSession(t *testing.T) chan string {
	t.Helper()
	socket, got := listenFake(t)
	procFS(t, fakeProc{pid: 300, comm: "claude", args: []string{"claude"}, start: "77"})
	sessionFiles(t, fakeSession{pid: 300, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	return got
}

func letterText(t *testing.T, got chan string) string {
	t.Helper()
	select {
	case line := <-got:
		var msg struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("the protocol line was not parsed: %v (%q)", err, line)
		}
		return msg.Message.Content
	case <-time.After(2 * time.Second):
		t.Fatal("nothing reached the session")
		return ""
	}
}

func putSecret(t *testing.T, name, text string) (string, error) {
	t.Helper()
	return (&Executor{}).Execute(t.Context(), action.Request{
		ID: "1", Kind: action.SecretPut, Target: "aacpanel", Secret: &action.Secret{Name: name, Text: text},
	})
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// The notepad lands whole in a file only the owner reads, in a directory only
// the owner opens — closed again if it was left open — and the session is
// told the path with the names of the keys, never a value. Neither the
// answer nor anything the session is told carries the notepad.
func TestASecretIsSavedForTheOwnerAndTheSessionToldItsPath(t *testing.T) {
	dir := secretsHere(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := letterSession(t)

	text := "# Settings, Developer settings, Tokens\nGH_TOKEN=" + marker + "\nexport GH_USER=\"\"\n"
	detail, err := putSecret(t, "github-token", text)
	if err != nil {
		t.Fatalf("the secret was not saved: %v", err)
	}
	path := filepath.Join(dir, "github-token")
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != text {
		t.Fatalf("the file holds %q (%v), the notepad was %q", raw, err, text)
	}
	if mode := modeOf(t, path); mode != 0o600 {
		t.Errorf("the secret is %v, not 0600", mode)
	}
	if mode := modeOf(t, dir); mode != 0o700 {
		t.Errorf("the directory of secrets is %v, not 0700", mode)
	}
	if names := namesIn(t, dir); !slices.Equal(names, []string{"github-token"}) {
		t.Errorf("the directory holds %v: a temporary file stayed behind", names)
	}

	if detail != "saved github-token ("+strconv.Itoa(len(text))+" bytes) and told aacpanel" {
		t.Errorf("the answer is %q", detail)
	}
	told := letterText(t, got)
	want := `The secret "github-token" is saved: ` + path + ` (0600, 3 lines). Keys: GH_TOKEN, GH_USER; ` +
		`left empty: GH_USER. Use it by its path and never read or print it: whatever you read goes into the transcript.`
	if told != want {
		t.Errorf("the session was told\n%q\nwant\n%q", told, want)
	}
	if strings.Contains(detail+told, marker) {
		t.Errorf("the notepad left the file: %q, %q", detail, told)
	}
}

// A save under a name already there replaces the secret whole, and the file
// is the owner's alone whatever the old one was: that is how a token is
// rotated.
func TestASecondSaveReplacesTheSecret(t *testing.T) {
	dir := secretsHere(t)
	got := letterSession(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "evirma-db.env")
	if err := os.WriteFile(path, []byte("DB_PASSWORD=old and longer than the new one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := putSecret(t, "evirma-db.env", "DB_PASSWORD="+marker+"\n"); err != nil {
		t.Fatalf("the secret was not replaced: %v", err)
	}
	letterText(t, got)
	if raw, _ := os.ReadFile(path); string(raw) != "DB_PASSWORD="+marker+"\n" {
		t.Errorf("the file holds %q after the second save", raw)
	}
	if mode := modeOf(t, path); mode != 0o600 {
		t.Errorf("the replaced secret is %v, not 0600", mode)
	}
	if names := namesIn(t, dir); !slices.Equal(names, []string{"evirma-db.env"}) {
		t.Errorf("the directory holds %v after a replace", names)
	}
}

// A write that fails leaves nothing of the notepad behind and says nothing of
// it: here the name is taken by a directory, so the rename cannot land.
func TestAFailedWriteLeavesNoPieceOfTheNotepad(t *testing.T) {
	dir := secretsHere(t)
	letterSession(t)
	if err := os.MkdirAll(filepath.Join(dir, "github-token", "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	detail, err := putSecret(t, "github-token", "GH_TOKEN="+marker+"\n")
	if err == nil {
		t.Fatalf("a secret was saved over a directory: %q", detail)
	}
	if strings.Contains(err.Error(), marker) {
		t.Errorf("the refusal quotes the notepad: %v", err)
	}
	if names := namesIn(t, dir); !slices.Equal(names, []string{"github-token"}) {
		t.Errorf("the directory holds %v: a temporary file stayed behind", names)
	}
}

// A secret saved for a session that is not there stays saved, and the
// refusal names where it lies, so the person can say it in the conversation.
func TestASecretSavedForASessionThatIsGoneStays(t *testing.T) {
	dir := secretsHere(t)
	procFS(t)
	sessionFiles(t)
	_, err := putSecret(t, "github-token", "GH_TOKEN="+marker+"\n")
	path := filepath.Join(dir, "github-token")
	if err == nil || !strings.HasPrefix(err.Error(), "saved to "+path+", but the session was not told: ") {
		t.Fatalf("the refusal is %v", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Errorf("the refusal quotes the notepad: %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "GH_TOKEN="+marker+"\n" {
		t.Errorf("the file holds %q", raw)
	}
}

// The session hears which variables the notepad sets and which the person
// left empty, by the names alone; a notepad that sets none is said in lines.
func TestTheSessionHearsTheKeysAndNotTheValues(t *testing.T) {
	for _, c := range []struct{ text, said string }{
		{"A=1\nB=2", "(0600, 2 lines). Keys: A, B."},
		{"export A=1\n  B = \nC=\"\"\nD=''\nE=\" \"\n", "(0600, 5 lines). Keys: A, B, C, D, E; left empty: B, C, D."},
		{"# a comment\n#A=\nexport\tTOKEN=" + marker + "\r\n", "(0600, 3 lines). Keys: TOKEN."},
		{"A=1\nA=\n", "(0600, 2 lines). Keys: A; left empty: A."},
		{"just a password, no keys\n", "(0600, 1 line)."},
		{"1A=x\nA-B=y\n=z\n", "(0600, 3 lines)."},
	} {
		got := secretMessage("x", "/s/x", c.text)
		want := `The secret "x" is saved: /s/x ` + c.said +
			" Use it by its path and never read or print it: whatever you read goes into the transcript."
		if got != want {
			t.Errorf("%q is told as\n%q\nwant\n%q", c.text, got, want)
		}
		if strings.Contains(got, marker) || strings.Contains(got, "=") {
			t.Errorf("a value reached the session: %q", got)
		}
	}
}

// The feed of the panel recognises the message of a saved secret by its whole
// text and draws it as a card; the samples it is checked against are the ones
// read here. A message worded otherwise than its sample is one the feed no
// longer recognises: the sample, and the feed's parser with it, change in the
// same commit as the words.
func TestTheSecretMessageIsTheOneTheFeedReads(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "check", "testdata", "panel-said.json"))
	if err != nil {
		t.Fatal(err)
	}
	var samples struct {
		Secret []struct {
			Input struct {
				Name string `json:"name"`
				Path string `json:"path"`
				Text string `json:"text"`
			} `json:"input"`
			Text string `json:"text"`
		} `json:"secret"`
	}
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatalf("the samples do not parse: %v", err)
	}
	if len(samples.Secret) == 0 {
		t.Fatal("no samples of a secret message: the check compares nothing")
	}
	for _, s := range samples.Secret {
		if got := secretMessage(s.Input.Name, s.Input.Path, s.Input.Text); got != s.Text {
			t.Errorf("the secret %s is told as\n%q\nand the feed reads\n%q", s.Input.Name, got, s.Text)
		}
	}
}

// The list shows what the notepad saved and nothing else: no temporary file
// of a write under way, no file of another name, no directory and no link.
// Nobody has saved anything yet — the list is empty, not missing.
func TestTheListShowsTheSecretsAndNothingElse(t *testing.T) {
	dir := secretsHere(t)
	list, err := (&Executor{}).Secrets(t.Context())
	if err != nil || list.Dir != dir || list.Secrets == nil || len(list.Secrets) != 0 {
		t.Fatalf("a host with no secrets lists %+v (%v)", list, err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "a-dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"b-token": "GH_TOKEN=x\n", "a.env": "A=1\nB=22\n", ".tmp-0123abcd": "half", "UPPER": "x", "x y": "x",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "a.env"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 9, 12, 34, 56, 789_000_000, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "a.env"), when, when); err != nil {
		t.Fatal(err)
	}

	list, err = (&Executor{}).Secrets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range list.Secrets {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"a.env", "b-token"}) {
		t.Fatalf("the list shows %v", names)
	}
	if a := list.Secrets[0]; a.Bytes != 9 || a.At != "2026-10-09T12:34:56.789Z" {
		t.Errorf("a.env is listed as %+v", a)
	}
	if _, err := time.Parse(time.RFC3339, list.Secrets[1].At); err != nil {
		t.Errorf("the time of b-token is not RFC 3339: %v", err)
	}
}

// A secret goes by its name; one that is not there, or a name taken by
// something the list does not show, is said to be missing.
func TestASecretIsRemovedByItsName(t *testing.T) {
	dir := secretsHere(t)
	if err := os.MkdirAll(filepath.Join(dir, "a-dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "github-token"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	drop := func(name string) (string, error) {
		return (&Executor{}).Execute(t.Context(), action.Request{ID: "1", Kind: action.SecretDrop, Target: name})
	}
	if detail, err := drop("github-token"); err != nil || detail != "removed the secret github-token" {
		t.Fatalf("the secret was not removed: %q, %v", detail, err)
	}
	if names := namesIn(t, dir); !slices.Equal(names, []string{"a-dir"}) {
		t.Errorf("the directory holds %v", names)
	}
	for _, name := range []string{"github-token", "a-dir"} {
		if _, err := drop(name); err == nil || err.Error() != "there is no secret "+name {
			t.Errorf("removing %s answered %v", name, err)
		}
	}
}

// The notepad needs a session to tell, so a host without sessions does not
// offer it; removing a secret needs none.
func TestSecretsWithoutTmux(t *testing.T) {
	t.Setenv(tmuxEnv, filepath.Join(t.TempDir(), "no-such-binary"))
	got := (&Executor{}).Kinds()
	if hasKind(got, action.SecretPut) {
		t.Error("secret.put is offered with no sessions to tell")
	}
	if !hasKind(got, action.SecretDrop) {
		t.Error("secret.drop went away with the sessions")
	}
}
