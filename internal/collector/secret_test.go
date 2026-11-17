package collector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// The answer opens with the name the feed draws the card by, on a line of its
// own, and goes on with how the file is used; the person is called to the
// conversation the server serves, whatever session the arguments name.
func TestASecretIsAskedUnderItsNameAndThePersonCalled(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true}))
	said, failed := callTool(t, SecretAsk(f.path, secretPanel(t)), bound(sid),
		`{"name":"github-token","title":"  GitHub token\n to push  ","template":"# Settings → Tokens\nGH_TOKEN=\n",`+
			`"sessionId":"forged"}`)
	if failed {
		t.Fatalf("the ask failed: %q", said)
	}
	first, rest, _ := strings.Cut(said, "\n")
	if first != "Asked as github-token (act aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa)." {
		t.Errorf("the answer opens with %q", first)
	}
	for _, rule := range []string{"Do not wait for it", "--env-file", "never read, cat, grep or print it"} {
		if !strings.Contains(rest, rule) {
			t.Errorf("the answer does not say %q: %q", rule, said)
		}
	}
	if strings.Contains(said, "not called") {
		t.Errorf("a call that went out is said to have failed: %q", said)
	}
	want := map[string]any{"sessionId": sid, "text": "Asks for a secret: GitHub token to push"}
	if got := f.request(t); !reflect.DeepEqual(got, want) {
		t.Errorf("the collector was handed %v", got)
	}
}

// An argument the panel cannot take asks nothing and calls nobody, and says
// which argument is wrong. The answer has no "Asked as": the feed would draw
// a card for a notepad nobody asked for.
func TestABadArgumentAsksNothing(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true}))
	long := strings.Repeat("a", 65)
	for _, c := range []struct{ args, names string }{
		{`{"name":"../x","title":"T"}`, "name"},
		{`{"name":".x","title":"T"}`, "name"},
		{`{"name":"A","title":"T"}`, "name"},
		{`{"name":"","title":"T"}`, "name"},
		{`{"title":"T"}`, "name"},
		{`{"name":"` + long + `","title":"T"}`, "name"},
		{`{"name":"a/b","title":"T"}`, "name"},
		{`{"name":"a","title":"  \n "}`, "title"},
		{`{"name":"a"}`, "title"},
		{`{"name":"a","title":"` + strings.Repeat("ü", secretTitleMax+1) + `"}`, "title"},
		{`{"name":"a","title":"T","template":"` + strings.Repeat("x", secretTemplateMax+1) + `"}`, "template"},
		{`{"name":"a","title":"T","template":7}`, "arguments"},
		{`[]`, "arguments"},
	} {
		said, failed := callTool(t, SecretAsk(f.path, secretPanel(t)), bound(sid), c.args)
		if !failed || strings.Contains(said, "Asked as") || !strings.HasPrefix(said, "Nothing was asked") {
			t.Errorf("%.60s was answered %q (error %v)", c.args, said, failed)
		}
		if !strings.Contains(said, c.names) {
			t.Errorf("%.60s: the refusal does not name %s: %q", c.args, c.names, said)
		}
	}
	f.quiet(t)

	at := strings.Repeat("a", 64)
	ok := []string{
		`{"name":"` + at + `","title":"T"}`,
		`{"name":"evirma-db.env","title":"` + strings.Repeat("ü", secretTitleMax) + `"}`,
		`{"name":"a_b-c.d","title":"T","template":"` + strings.Repeat("x", secretTemplateMax) + `"}`,
	}
	for _, args := range ok {
		if said, failed := callTool(t, SecretAsk(f.path, secretPanel(t)), bound(sid), args); failed {
			t.Errorf("%.60s was refused: %q", args, said)
		}
		f.request(t)
	}
}

// The phone is a courtesy: the card is in the feed whether or not the push
// went, so a collector that is down or refuses the call — one call a minute,
// the phone already on this session — does not take the ask back. The
// answer says the person was not called, and why.
func TestAPhoneThatWasNotCalledDoesNotFailTheAsk(t *testing.T) {
	why := "this session called 12 s ago: one call a minute, 48 s left"
	refusing := listen(t, answers(map[string]any{"ok": false, "error": why}))
	for _, c := range []struct {
		socket, says string
	}{
		{nowhere(t), "not listening on"},
		{refusing.path, why},
	} {
		said, failed := callTool(t, SecretAsk(c.socket, secretPanel(t)), bound(sid), `{"name":"github-token","title":"GitHub token"}`)
		if failed || !strings.HasPrefix(said, "Asked as github-token (act aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa).\n") {
			t.Errorf("an ask with no call to the phone answered %q (error %v)", said, failed)
		}
		if !strings.Contains(said, "not called") || !strings.Contains(said, c.says) {
			t.Errorf("the answer does not say why the phone was not called: %q", said)
		}
	}
}

func secretPanel(t *testing.T) string {
	t.Helper()
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/acts" {
			t.Errorf("secret destination: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Conversation, Kind, Title string
			Body                      struct{ Name, Template string }
			Blocking                  bool
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Conversation != sid || body.Kind != "secret" || body.Body.Name == "" || body.Title == "" || body.Blocking {
			t.Errorf("secret act: %+v", body)
		}
		w.Write([]byte(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	}))
	t.Cleanup(panel.Close)
	return panel.URL
}

func TestAnUnconfirmedSecretActCallsNobody(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true}))
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the caller is not live", http.StatusConflict)
	}))
	defer panel.Close()
	said, failed := callTool(t, SecretAsk(f.path, panel.URL), bound(sid), `{"name":"github-token","title":"Token"}`)
	if !failed || strings.Contains(said, "Asked as") || !strings.Contains(said, "not confirm") {
		t.Fatalf("unconfirmed act: %s (%v)", said, failed)
	}
	f.quiet(t)
}
