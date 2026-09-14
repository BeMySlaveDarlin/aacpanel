package notify

import (
	"context"
	"crypto/ecdh"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu      sync.Mutex
	subs    []Subscription
	private []byte
	public  []byte

	dropped []int64
	sent    []int64
	failed  []string
	saves   int
}

func (m *memStore) Subscriptions(context.Context) ([]Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Subscription(nil), m.subs...), nil
}

func (m *memStore) DropSubscription(_ context.Context, device int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropped = append(m.dropped, device)
	for i := range m.subs {
		if m.subs[i].Device == device {
			m.subs = append(m.subs[:i], m.subs[i+1:]...)
			break
		}
	}
	return nil
}

func (m *memStore) SubscriptionSent(_ context.Context, device int64, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, device)
	return nil
}

func (m *memStore) SubscriptionFailed(_ context.Context, device int64, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failed = append(m.failed, strconv.FormatInt(device, 10)+": "+reason)
	return nil
}

func (m *memStore) PushKeys(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.private, nil
}

func (m *memStore) SavePushKeys(_ context.Context, private, public []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	if m.private == nil {
		m.private, m.public = private, public
	}
	return nil
}

func (m *memStore) counts() (dropped, sent, failed int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.dropped), len(m.sent), len(m.failed)
}

func stand(t *testing.T, handler http.HandlerFunc) (*Sender, *memStore, *ecdh.PrivateKey) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	sub, uaPrivate := testSubscription(t)
	sub.Device = 1
	sub.Endpoint = srv.URL + "/push/1"

	store := &memStore{subs: []Subscription{sub}}
	s := New(store, "mailto:owner@example.net")
	s.client = srv.Client()
	s.client.Timeout = 5 * time.Second

	if err := s.loadKeys(context.Background()); err != nil {
		t.Fatalf("the keys: %v", err)
	}
	return s, store, uaPrivate
}

func testMessage() Message {
	return Message{
		Title:    "shop stopped",
		Body:     "5 containers affected",
		Tag:      "container:shop",
		Severity: Critical,
	}
}

func TestDeliverSendsReadablePush(t *testing.T) {
	var (
		gotAuth, gotEncoding, gotTTL, gotUrgency, gotTopic string
		gotBody                                            []byte
	)
	s, store, uaPrivate := stand(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotEncoding = r.Header.Get("Content-Encoding")
		gotTTL = r.Header.Get("TTL")
		gotUrgency = r.Header.Get("Urgency")
		gotTopic = r.Header.Get("Topic")
		gotBody, _ = readAll(r)
		w.WriteHeader(http.StatusCreated)
	})

	s.deliver(context.Background(), testMessage())

	if gotEncoding != "aes128gcm" {
		t.Fatalf("Content-Encoding: %q", gotEncoding)
	}
	if len(gotAuth) < 8 || gotAuth[:8] != "vapid t=" {
		t.Fatalf("Authorization: %q", gotAuth)
	}
	if ttl, err := strconv.Atoi(gotTTL); err != nil || ttl != int(ttlCritical.Seconds()) {
		t.Fatalf("TTL: %q", gotTTL)
	}
	if gotUrgency != "high" {
		t.Fatalf("Urgency: %q — a critical one must travel urgent", gotUrgency)
	}
	if gotTopic != "containershop" {
		t.Fatalf("Topic: %q", gotTopic)
	}

	plain, err := decryptAsDevice(t, gotBody, uaPrivate, store.subs[0].Auth)
	if err != nil {
		t.Fatalf("the device did not decrypt the push: %v", err)
	}
	var payload struct {
		Title, Body, Tag, URL, Severity string
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		t.Fatalf("the body is not JSON: %v (%s)", err, plain)
	}
	if payload.Title != "shop stopped" || payload.Body != "5 containers affected" {
		t.Fatalf("the push does not carry the gist: %+v", payload)
	}
	if payload.Severity != "critical" {
		t.Fatalf("the severity is lost: %+v", payload)
	}
	if payload.URL != "" {
		t.Fatalf("the push carried a link to follow: %+v", payload)
	}

	if _, sent, _ := store.counts(); sent != 1 {
		t.Fatalf("a successful delivery is not marked: %d", sent)
	}
}

func TestDeliverDropsDeadSubscription(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusGone} {
		calls := 0
		s, store, _ := stand(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(code)
		})
		journal := newJournal()
		s.UseJournal(journal)

		s.deliver(context.Background(), testMessage())

		dropped, sent, _ := store.counts()
		if dropped != 1 {
			t.Fatalf("code %d: the subscription is not removed", code)
		}
		if sent != 0 {
			t.Fatalf("code %d: the delivery is marked successful", code)
		}
		if calls != 1 {
			t.Fatalf("code %d: %d attempts, there is no point retrying a dead subscription", code, calls)
		}

		note, ok := journal.rows["push:1"]
		if !ok {
			t.Fatalf("code %d: the death is not written down — the row is gone and the screen of the device still says \"on\", nothing else says when the device stopped getting pushes: %v", code, journal.rows)
		}
		if !strings.Contains(note.Body, strconv.Itoa(code)) {
			t.Errorf("code %d: the note does not say what the push service answered: %q", code, note.Body)
		}
		if note.Domain != domainPush {
			t.Errorf("code %d: the note is filed under %q — the tracker clears any domain somebody reports on", code, note.Domain)
		}
	}
}

func TestDeliverWithoutJournalStillDropsDeadSubscription(t *testing.T) {
	s, store, _ := stand(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	})

	s.deliver(context.Background(), testMessage())

	if dropped, _, _ := store.counts(); dropped != 1 {
		t.Fatal("without a database for the notes the subscription is not removed")
	}
}

func TestDeliverRetriesOnTooManyRequests(t *testing.T) {
	calls := 0
	s, store, _ := stand(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})

	start := time.Now()
	s.deliver(context.Background(), testMessage())

	if calls != 2 {
		t.Fatalf("%d attempts, two expected", calls)
	}
	if waited := time.Since(start); waited < time.Second {
		t.Fatalf("the retry came after %s, while the push service asked for a second", waited)
	}
	if dropped, sent, _ := store.counts(); sent != 1 || dropped != 0 {
		t.Fatalf("after the retry: %d deliveries, %d removals", sent, dropped)
	}
}

func TestDeliverDoesNotRetryClientErrors(t *testing.T) {
	calls := 0
	s, store, _ := stand(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	})

	s.deliver(context.Background(), testMessage())

	if calls != 1 {
		t.Fatalf("%d attempts, there is no point retrying a 400", calls)
	}
	if dropped, sent, failed := store.counts(); dropped != 0 || sent != 0 || failed == 0 {
		t.Fatalf("the subscription is removed or the error is not recorded: %d %d %d", dropped, sent, failed)
	}
}

func TestSendDoesNotBlockCaller(t *testing.T) {
	hold := make(chan struct{})
	s, _, _ := stand(t, func(w http.ResponseWriter, r *http.Request) {
		<-hold
		w.WriteHeader(http.StatusCreated)
	})
	t.Cleanup(func() { close(hold) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range workers * 2 {
			s.Send(testMessage())
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked on a silent push service")
	}
}

func TestSendDropsOnFullQueue(t *testing.T) {
	s := New(&memStore{}, "mailto:owner@example.net")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range queueSize + 10 {
			s.Send(testMessage())
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Send hung on a full queue — sending blocks whoever asks for it")
	}
	if len(s.queue) != queueSize {
		t.Fatalf("%d in the queue, the ceiling is %d", len(s.queue), queueSize)
	}
}

func TestSendRejectsEmptyMessage(t *testing.T) {
	s := New(&memStore{}, "mailto:owner@example.net")
	s.Send(Message{Title: "Attention"})
	s.Send(Message{Body: "something happened"})
	if len(s.queue) != 0 {
		t.Fatalf("empty notifications made it into the queue: %d", len(s.queue))
	}
}

func TestKeysGeneratedOnceAndReused(t *testing.T) {
	store := &memStore{}
	first := New(store, "mailto:owner@example.net")
	if err := first.loadKeys(context.Background()); err != nil {
		t.Fatalf("the first start: %v", err)
	}

	second := New(store, "mailto:owner@example.net")
	if err := second.loadKeys(context.Background()); err != nil {
		t.Fatalf("the second start: %v", err)
	}

	if first.PublicKey() != second.PublicKey() {
		t.Fatal("the key changed on restart — every subscription would turn to garbage")
	}
	if store.saves != 1 {
		t.Fatalf("the pair was written %d times", store.saves)
	}
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

func TestPushAboutASessionCarriesItsScreen(t *testing.T) {
	var gotBody []byte
	s, store, uaPrivate := stand(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = readAll(r)
		w.WriteHeader(http.StatusCreated)
	})

	s.deliver(context.Background(), Message{
		Title: "Question · harness rework", Body: "Which way?", Tag: "ask:u-1:1",
		Severity: Critical, Session: "harness rework",
	})

	plain, err := decryptAsDevice(t, gotBody, uaPrivate, store.subs[0].Auth)
	if err != nil {
		t.Fatalf("the device did not decrypt the push: %v", err)
	}
	var payload struct{ URL string }
	if err := json.Unmarshal(plain, &payload); err != nil {
		t.Fatalf("the body is not JSON: %v (%s)", err, plain)
	}
	if payload.URL != "/app?session=harness+rework" {
		t.Fatalf("the push points at %q, expected the screen of the session — a tap lands on the home screen otherwise", payload.URL)
	}
}
