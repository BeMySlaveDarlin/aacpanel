package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

// marker stands for the value of a credential: it reaches the executor and
// nothing else of the service.
const secretMarker = "ghp_m4rker_not_for_any_log"

func journalDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.New(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

// The notepad reaches the executor whole; the journal of actions keeps the
// name of the secret and the length of the notepad, and neither the journal,
// the log of the service nor the answer to the phone carries a byte of it.
func TestSecretPutKeepsTheNotepadOutOfTheJournalPG(t *testing.T) {
	db := journalDB(t)
	logged := tailLog(t)
	client, exec := startFakeExec(t, action.Response{OK: true, Detail: "saved github-token (36 bytes) and told aacpanel"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client, db: db}

	text := "# Tokens\\nGH_TOKEN=" + secretMarker + "\\n"
	w := post(t, srv, `{"kind":"secret.put","target":"aacpanel","params":{"name":"github-token","text":"`+text+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	req := <-exec.got
	if req.Kind != action.SecretPut || req.Target != "aacpanel" || req.Secret == nil ||
		req.Secret.Name != "github-token" || req.Secret.Text != "# Tokens\nGH_TOKEN="+secretMarker+"\n" {
		t.Fatalf("the executor got %+v", req)
	}
	if req.Text != "" {
		t.Errorf("the notepad went as a message too: %q", req.Text)
	}

	list, err := db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("the action did not reach the journal")
	}
	chars := len([]rune(req.Secret.Text))
	if want := map[string]any{"name": "github-token", "chars": chars}; !sameParams(list[0].Params, want) {
		t.Errorf("the journal keeps %v, want %v", list[0].Params, want)
	}
	row, _ := json.Marshal(list[0])
	for _, out := range []string{string(row), w.Body.String(), logged.String()} {
		if strings.Contains(out, secretMarker) {
			t.Errorf("the notepad left for the service:\n%s", out)
		}
	}
}

// A removal is journaled by the secret it removes, its target, and nothing
// else: whatever the phone sends along stays out.
func TestSecretDropJournalsTheNameAlonePG(t *testing.T) {
	db := journalDB(t)
	client, exec := startFakeExec(t, action.Response{OK: true, Detail: "removed the secret github-token"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client, db: db}

	w := post(t, srv, `{"kind":"secret.drop","target":"github-token","params":{"text":"`+secretMarker+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if req := <-exec.got; req.Kind != action.SecretDrop || req.Target != "github-token" || req.Secret != nil {
		t.Errorf("the executor got %+v", req)
	}
	list, err := db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil || len(list) == 0 {
		t.Fatalf("the journal holds %v (%v)", list, err)
	}
	if list[0].Target != "github-token" || len(list[0].Params) != 0 {
		t.Errorf("the journal keeps %q with %v", list[0].Target, list[0].Params)
	}
}

// A notepad the service refuses is refused by the rule it broke, before
// anything is journaled or sent, and the refusal does not quote it.
func TestARefusedNotepadIsNotQuoted(t *testing.T) {
	client, exec := startFakeExec(t, action.Response{OK: true})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	for _, c := range []struct{ params, says string }{
		{`{"name":"github-token","text":"GH_TOKEN=` + secretMarker + `\u0000"}`, "NUL"},
		{`{"name":"github-token","text":" \n "}`, "empty"},
		{`{"name":"../` + secretMarker + `","text":"A=1"}`, "name of a secret"},
		{`{"name":"github-token"}`, "empty"},
	} {
		w := post(t, srv, `{"kind":"secret.put","target":"aacpanel","params":`+c.params+`}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.says) {
			t.Errorf("%s answered %d %q", c.params, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), secretMarker) {
			t.Errorf("the refusal quotes the notepad: %q", w.Body.String())
		}
	}
	select {
	case req := <-exec.got:
		t.Errorf("a refused notepad reached the executor: %+v", req.Kind)
	default:
	}
}

// The list comes from the executor as it is, and an empty one is a list, not
// nothing: the screen reads it without asking whether it is there.
func TestTheSecretsAreListed(t *testing.T) {
	want := action.Secrets{Dir: "/home/u/.local/state/aacpanel/secrets",
		Secrets: []action.SecretFile{{Name: "github-token", Bytes: 36, At: "2026-10-09T12:34:56.789Z"}}}
	for _, c := range []struct {
		from action.Secrets
		body string
	}{
		{want, `{"dir":"/home/u/.local/state/aacpanel/secrets","secrets":[{"name":"github-token","bytes":36,` +
			`"at":"2026-10-09T12:34:56.789Z"}]}`},
		{action.Secrets{Dir: "/home/u/.local/state/aacpanel/secrets"},
			`{"dir":"/home/u/.local/state/aacpanel/secrets","secrets":[]}`},
	} {
		client, exec := startFakeExec(t, action.Response{OK: true, Secrets: &c.from})
		srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
		w := httptest.NewRecorder()
		srv.routes(srv.localGate()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/secrets", nil))
		if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != c.body {
			t.Errorf("the list answered %d %s", w.Code, w.Body.String())
		}
		if req := <-exec.got; req.Ask != action.AskSecrets {
			t.Errorf("the executor was asked %+v", req)
		}
	}

	down := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: action.NewClient(socketPath(t, "gone.sock"), 0)}
	w := httptest.NewRecorder()
	down.apiSecrets(w, httptest.NewRequest(http.MethodGet, "/api/secrets", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("an executor that is down answered %d", w.Code)
	}
}

// sameParams compares the params of a journal row, read back from JSON, with
// what was meant.
func sameParams(got, want map[string]any) bool {
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	return string(a) == string(b)
}
