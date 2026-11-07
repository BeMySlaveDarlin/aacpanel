package session

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func letterListHost(t *testing.T, snapshot string) Host {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/host" {
			t.Errorf("listing asked %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(snapshot))
	}))
	t.Cleanup(srv.Close)
	return Host{Panel: srv.URL}
}

func TestTheLetterListIncludesSleepingRecipientsFromThePanel(t *testing.T) {
	h := letterListHost(t, `{"sessions":[
        {"session":"self","sessionId":"`+mine+`","sleeping":true},
        {"session":"timed","sessionId":"timed-id","profile":"work","cwd":"/srv/work","sleeping":true,"pauseAlarm":{"next":"2026-11-08T10:00:00+07:00"}},
        {"session":"indefinite","sessionId":"indefinite-id","profile":"personal","cwd":"/srv/proj","sleeping":true,"pauseAlarm":{"name":"pause-no-end","next":""}}
    ]}`)
	said, failed := call(t, Letter(h), bound(mine), map[string]any{})
	if failed {
		t.Fatal(said)
	}
	for _, want := range []string{"timed — work — /srv/work (sleeping until 2026-11-08T10:00:00+07:00)", "indefinite — personal — /srv/proj (sleeping)", "A letter wakes a sleeping recipient and lifts its pause"} {
		if !strings.Contains(said, want) {
			t.Errorf("the list lacks %q: %s", want, said)
		}
	}
	if strings.Contains(said, "- self") {
		t.Fatalf("the caller appears in its own address list: %s", said)
	}
}
