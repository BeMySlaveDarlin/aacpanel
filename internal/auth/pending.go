package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	ceremonyCookie = "monitor_ceremony"
	ceremonyTTL    = 5 * time.Minute
	pendingLimit   = 128
)

type ceremony uint8

const (
	ceremonyRegister ceremony = iota + 1
	ceremonyLogin
)

type pendingItem struct {
	kind    ceremony
	data    webauthn.SessionData
	code    int64
	expires time.Time
}

type pendingStore struct {
	mu    sync.Mutex
	items map[string]pendingItem
}

func newPendingStore() *pendingStore {
	return &pendingStore{items: map[string]pendingItem{}}
}

func (p *pendingStore) put(kind ceremony, data webauthn.SessionData, code int64) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(raw)

	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	for len(p.items) >= pendingLimit {
		p.dropOldestLocked()
	}
	p.items[key] = pendingItem{kind: kind, data: data, code: code, expires: time.Now().Add(ceremonyTTL)}
	return key, nil
}

func (p *pendingStore) dropOldestLocked() {
	var (
		oldestKey string
		oldest    time.Time
	)
	for k, v := range p.items {
		if oldest.IsZero() || v.expires.Before(oldest) {
			oldestKey, oldest = k, v.expires
		}
	}
	delete(p.items, oldestKey)
}

func (p *pendingStore) take(key string, kind ceremony) (pendingItem, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()

	item, ok := p.items[key]
	if !ok || item.kind != kind {
		return pendingItem{}, false
	}
	delete(p.items, key)
	return item, true
}

func (p *pendingStore) sweepLocked() {
	now := time.Now()
	for k, v := range p.items {
		if now.After(v.expires) {
			delete(p.items, k)
		}
	}
}

func setCeremonyCookie(w http.ResponseWriter, key string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     ceremonyCookie,
		Value:    key,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(ceremonyTTL / time.Second),
	})
}

func clearCeremonyCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     ceremonyCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
