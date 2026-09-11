package auth

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"aacpanel/internal/store"
)

func pushBody(t *testing.T, endpoint string) []byte {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatalf("subscription secret: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"endpoint": endpoint,
		"keys": map[string]string{
			"p256dh": base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
			"auth":   base64.RawURLEncoding.EncodeToString(auth),
		},
	})
	return body
}

func TestSubscribeBindsToCurrentDevice(t *testing.T) {
	s := newStand(t)
	key := newSoftKey(t)
	c, id := s.registerFirst(t, key, "phone")

	if code, body := s.do(t, c, "POST", "/api/push/subscription", pushBody(t, "https://push.example.net/abc")); code != http.StatusNoContent {
		t.Fatalf("subscribe: %d %s", code, body)
	}

	subs, err := s.devices.Subscriptions(context.Background())
	if err != nil {
		t.Fatalf("list of subscriptions: %v", err)
	}
	if len(subs) != 1 || subs[0].Device != id {
		t.Fatalf("the subscription is bound to the wrong device: %+v", subs)
	}
	if len(subs[0].P256dh) != 65 || len(subs[0].Auth) != 16 {
		t.Fatalf("the subscription keys were written wrong: %d, %d", len(subs[0].P256dh), len(subs[0].Auth))
	}

	if code, _ := s.do(t, c, "POST", "/api/push/subscription", pushBody(t, "https://push.example.net/xyz")); code != http.StatusNoContent {
		t.Fatalf("resubscribe: %d", code)
	}
	subs, _ = s.devices.Subscriptions(context.Background())
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example.net/xyz" {
		t.Fatalf("resubscribing did not replace the previous subscription: %+v", subs)
	}

	if code, body := s.do(t, c, "DELETE", "/api/push/subscription", nil); code != http.StatusNoContent {
		t.Fatalf("unsubscribe: %d %s", code, body)
	}
	if subs, _ = s.devices.Subscriptions(context.Background()); len(subs) != 0 {
		t.Fatalf("after unsubscribing there is still: %+v", subs)
	}
}

func TestSubscribeRejectsBadPayload(t *testing.T) {
	s := newStand(t)
	c, _ := s.registerFirst(t, newSoftKey(t), "phone")

	cases := map[string][]byte{
		"not JSON":              []byte("{"),
		"no endpoint":           pushBodyWith(t, "", "AAAA", "BBBB"),
		"http instead of https": pushBodyWith(t, "http://push.example.net/x", "", ""),
		"short keys":            pushBodyWith(t, "https://push.example.net/x", "AAAA", "BBBB"),
	}
	for name, body := range cases {
		if code, _ := s.do(t, c, "POST", "/api/push/subscription", body); code != http.StatusBadRequest {
			t.Errorf("%s: answered %d, want 400", name, code)
		}
	}
	if subs, _ := s.devices.Subscriptions(context.Background()); len(subs) != 0 {
		t.Fatalf("a broken subscription reached the database: %+v", subs)
	}
}

func TestSubscribeNeedsSession(t *testing.T) {
	s := newStand(t)
	body := pushBody(t, "https://push.example.net/abc")
	if code, _ := s.do(t, s.client(t), "POST", "/api/push/subscription", body); code != http.StatusUnauthorized {
		t.Fatalf("subscribe without a session: %d", code)
	}
	if code, _ := s.do(t, s.client(t), "DELETE", "/api/push/subscription", nil); code != http.StatusUnauthorized {
		t.Fatalf("unsubscribe without a session: %d", code)
	}
}

func pushBodyWith(t *testing.T, endpoint, p256dh, auth string) []byte {
	t.Helper()
	if p256dh == "" && auth == "" {
		priv, err := ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		p256dh = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
		auth = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	}
	body, _ := json.Marshal(map[string]any{
		"endpoint": endpoint,
		"keys":     map[string]string{"p256dh": p256dh, "auth": auth},
	})
	return body
}

func TestDatabaseErrorsAreDistinguished(t *testing.T) {
	cases := map[string]struct {
		fail error
		want int
	}{
		"database is down":    {store.ErrUnavailable, http.StatusServiceUnavailable},
		"pool is closed":      {store.ErrClosed, http.StatusServiceUnavailable},
		"constraint violated": {errors.New("duplicate key value violates unique constraint"), http.StatusInternalServerError},
	}

	for name, c := range cases {
		s := newStand(t)
		c1, _ := s.registerFirst(t, newSoftKey(t), "phone")

		if code, _ := s.do(t, c1, "GET", "/api/devices", nil); code != http.StatusOK {
			t.Fatalf("%s: the setup failed, answered %d", name, code)
		}

		s.mem.mu.Lock()
		s.mem.fail = c.fail
		s.mem.mu.Unlock()

		if code, body := s.do(t, c1, "GET", "/api/devices", nil); code != c.want {
			t.Errorf("%s: the device list answered %d, want %d (%s)", name, code, c.want, body)
		}
	}
}
