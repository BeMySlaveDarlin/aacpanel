package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestEveryMessageReachesEverySubscription(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	first, _ := testSubscription(t)
	first.Device, first.Endpoint = 1, srv.URL+"/push/1"
	second, _ := testSubscription(t)
	second.Device, second.Endpoint = 2, srv.URL+"/push/2"

	store := &memStore{subs: []Subscription{first, second}}
	s := New(store, "mailto:owner@example.net")
	s.client = srv.Client()
	s.client.Timeout = 5 * time.Second
	if err := s.loadKeys(context.Background()); err != nil {
		t.Fatalf("the keys: %v", err)
	}

	for _, sev := range []Severity{Info, Warning, Critical} {
		s.deliver(context.Background(), Message{
			Title: "a reason", Body: "the gist", Tag: "test", Severity: sev,
		})
	}

	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/push/1", "/push/2"} {
		if hits[path] != 3 {
			t.Errorf("device %s got %d notifications out of three — the delivery filtered something out",
				path, hits[path])
		}
	}
}
