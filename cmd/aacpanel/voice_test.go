package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

const voiceTestToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type voiceStand struct {
	dir    string
	client *http.Client
	server *Server
	got    chan action.Request
}

func newVoiceStand(t *testing.T) *voiceStand {
	t.Helper()
	v := &voiceStand{dir: filepath.Dir(socketPath(t, "sock")), got: make(chan action.Request, 64)}
	v.token(t, voiceTestToken)
	exec := action.NewServer(socketPath(t, "exec.sock"), action.ExecutorFunc(func(_ context.Context, r action.Request) (string, error) {
		v.got <- r
		switch r.Target {
		case "missing":
			return "", fmt.Errorf("%w missing", action.ErrSessionNotFound)
		case "duplicate":
			return "", fmt.Errorf("%w: duplicate", action.ErrSessionAmbiguous)
		case "broken":
			return "", fmt.Errorf("the session did not accept the input")
		}
		return "accepted", nil
	}), time.Second)
	ln, err := exec.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		exec.Run(ctx, ln)
	}()
	t.Cleanup(func() { cancel(); <-done })
	v.server = &Server{auth: &auth.Service{}, exec: action.NewClient(ln.Addr().String(), time.Second)}
	srv, voice, err := v.server.voiceServer(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(voice.Addr().String()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the voice socket must be 0600: %v, %v", info, err)
	}
	go srv.Serve(voice)
	t.Cleanup(func() { srv.Close() })
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", voice.Addr().String())
	}}
	t.Cleanup(transport.CloseIdleConnections)
	v.client = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	return v
}

func (v *voiceStand) token(t *testing.T, value string) {
	t.Helper()
	next := filepath.Join(v.dir, "token.next")
	if err := os.WriteFile(next, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, filepath.Join(v.dir, "token")); err != nil {
		t.Fatal(err)
	}
}

func (v *voiceStand) request(t *testing.T, token, method, path, body string, want int) string {
	t.Helper()
	r, err := http.NewRequest(method, "http://voice"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := v.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	return string(raw)
}

func (v *voiceStand) noAction(t *testing.T) {
	t.Helper()
	select {
	case req := <-v.got:
		t.Fatalf("a refused request reached the executor: %+v", req)
	default:
	}
}

func TestVoiceSocketAcceptsOnlyAuthenticatedInput(t *testing.T) {
	v := newVoiceStand(t)
	const body = `{"to":"demo","text":"run the tests"}`
	for _, token := range []string{"", "wrong"} {
		v.request(t, token, "POST", "/session.send", body, http.StatusUnauthorized)
	}
	v.noAction(t)
	answer := v.request(t, voiceTestToken, "POST", "/session.send", body, http.StatusOK)
	if !strings.Contains(answer, `"ok":true`) {
		t.Fatalf("accepted input: %s", answer)
	}
	voice := <-v.got
	if voice.Kind != action.SessionSend || voice.Target != "demo" || voice.Text != "run the tests" ||
		voice.Device != "voice" || voice.Origin != action.OriginVoice {
		t.Fatalf("the executor got %+v", voice)
	}
	if w := post(t, v.server, `{"kind":"session.send","target":"demo","params":{"text":"run the tests","origin":"voice"}}`); w.Code != http.StatusOK {
		t.Fatalf("the composer: %d %s", w.Code, w.Body.String())
	}
	composer := <-v.got
	if composer.Device == "voice" || composer.Origin != "" {
		t.Fatal("the voice source escaped its listener")
	}
	voice.ID, voice.Device, composer.ID, composer.Device = "", "", "", ""
	voice.Origin = ""
	if !reflect.DeepEqual(voice, composer) {
		t.Fatalf("voice and composer use different sending paths: %+v, %+v", voice, composer)
	}

	rotated := strings.Repeat("a", 64)
	v.token(t, rotated)
	v.request(t, voiceTestToken, "POST", "/session.send", body, http.StatusUnauthorized)
	v.noAction(t)
	v.request(t, rotated, "POST", "/session.send", body, http.StatusOK)
	<-v.got
	if err := os.Chmod(filepath.Join(v.dir, "token"), 0o644); err != nil {
		t.Fatal(err)
	}
	v.request(t, rotated, "POST", "/session.send", body, http.StatusServiceUnavailable)
	v.token(t, "")
	v.request(t, rotated, "POST", "/session.send", body, http.StatusServiceUnavailable)
	if err := os.Remove(filepath.Join(v.dir, "token")); err != nil {
		t.Fatal(err)
	}
	v.request(t, rotated, "POST", "/session.send", body, http.StatusServiceUnavailable)
	v.noAction(t)
}

func TestVoiceSocketHasNoOtherActionsOrPanelRoutes(t *testing.T) {
	v := newVoiceStand(t)
	for _, kind := range action.Kinds {
		body := fmt.Sprintf(`{"to":"demo","text":"hello","kind":%q}`, kind)
		v.request(t, voiceTestToken, "POST", "/session.send", body, http.StatusBadRequest)
	}
	for _, path := range []string{"/api/actions", "/api/term/input", "/api/tree", "/", "/session.stop"} {
		v.request(t, voiceTestToken, "POST", path, `{"kind":"session.stop","target":"demo"}`, http.StatusNotFound)
	}
	v.request(t, voiceTestToken, "GET", "/session.send", "", http.StatusMethodNotAllowed)
	for _, body := range []string{
		`{"to":"demo","text":"hello","params":{"asked":"bypass-the-queue"}}`,
		`{"to":"demo","text":"hello"} {"to":"demo","text":"again"}`,
		`{"to":"demo"}`, `{"text":"hello"}`, `{"to":"bad;name","text":"hello"}`,
		`{"to":"demo","text":"` + strings.Repeat("x", actionBodyMax) + `"}`,
	} {
		v.request(t, voiceTestToken, "POST", "/session.send", body, http.StatusBadRequest)
	}
	v.noAction(t)
}

func TestVoiceSocketReportsSessionLookupFailures(t *testing.T) {
	v := newVoiceStand(t)
	for _, c := range []struct {
		to, code string
		status   int
	}{
		{"missing", action.SessionNotFound, http.StatusNotFound},
		{"duplicate", action.SessionAmbiguous, http.StatusConflict},
		{"broken", "", http.StatusBadGateway},
	} {
		raw := v.request(t, voiceTestToken, "POST", "/session.send", fmt.Sprintf(`{"to":%q,"text":"hello"}`, c.to), c.status)
		var got struct{ Error, Code string }
		if err := json.Unmarshal([]byte(raw), &got); err != nil || got.Code != c.code || got.Error == "" {
			t.Fatalf("%s: %s (%v)", c.to, raw, err)
		}
		<-v.got
	}
}

func TestVoiceInputJournalPG(t *testing.T) {
	db, err := store.New(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	v := newVoiceStand(t)
	v.server.db = db
	const words = "check the stack logs"
	raw := v.request(t, voiceTestToken, "POST", "/session.send", `{"to":"demo","text":"`+words+`"}`, http.StatusOK)
	if !strings.Contains(raw, `"logged":true`) {
		t.Fatalf("the input was not journalled: %s", raw)
	}
	got := <-v.got
	list, err := db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil || len(list) != 1 {
		t.Fatalf("the journal: %+v, %v", list, err)
	}
	a := list[0]
	if a.DeviceName != "voice" || a.DeviceID != nil || a.Kind != "session.send" || a.Target != "demo" || a.Result != store.ActionOK || got.ID != requestID(a.ID) {
		t.Fatalf("voice input lost its source or outcome: %+v, executor: %+v", a, got)
	}
	if len(a.Params) != 1 || a.Params["chars"] != float64(len(words)) || got.Text != words {
		t.Fatalf("only the session receives the words: journal %+v, executor %+v", a.Params, got)
	}
}
