package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type memViews struct {
	mu    sync.Mutex
	by    map[string][]int64
	fail  bool
	asked []string
	fresh time.Duration
}

func (m *memViews) Watching(_ context.Context, session string, fresh time.Duration) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asked = append(m.asked, session)
	m.fresh = fresh
	if m.fail {
		return nil, errors.New("the database is unavailable")
	}
	return m.by[session], nil
}

func twoDevices(t *testing.T) (*Sender, func(device int) int) {
	t.Helper()

	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	phone, _ := testSubscription(t)
	phone.Device, phone.Endpoint = 1, srv.URL+"/push/1"
	monitor, _ := testSubscription(t)
	monitor.Device, monitor.Endpoint = 2, srv.URL+"/push/2"

	s := New(&memStore{subs: []Subscription{phone, monitor}}, "mailto:owner@example.net")
	s.client = srv.Client()
	s.client.Timeout = 5 * time.Second
	if err := s.loadKeys(context.Background()); err != nil {
		t.Fatalf("the keys: %v", err)
	}

	got := func(device int) int {
		mu.Lock()
		defer mu.Unlock()
		switch device {
		case 1:
			return hits["/push/1"]
		default:
			return hits["/push/2"]
		}
	}
	return s, got
}

func sessionMessage() Message {
	return Message{
		Title:    "Question · aacpanel",
		Body:     "Which way: a or b — the session is blocked until you answer",
		Tag:      "ask:aacpanel:1",
		Severity: Critical,
		Session:  "aacpanel",
	}
}

func TestPushSkipsTheDeviceWatchingTheSession(t *testing.T) {
	s, got := twoDevices(t)
	views := &memViews{by: map[string][]int64{"aacpanel": {1}}}
	s.UseViews(views)

	s.deliver(context.Background(), sessionMessage())

	if got(1) != 0 {
		t.Errorf("the device is watching the session, yet a push about it went out anyway (%d)", got(1))
	}
	if got(2) != 1 {
		t.Errorf("the second device got no notification (%d): the session is open on the monitor while the phone is in a pocket",
			got(2))
	}
	if len(views.asked) != 1 || views.asked[0] != "aacpanel" {
		t.Errorf("the question was about the wrong session: %v", views.asked)
	}
	if views.fresh <= 0 {
		t.Errorf("the mark has no freshness window (%s): without one it never goes stale", views.fresh)
	}
}

func TestPushOnlyStaysSilentAboutTheSessionOnScreen(t *testing.T) {
	s, got := twoDevices(t)
	s.UseViews(&memViews{by: map[string][]int64{"aacpanel": {1}}})

	other := sessionMessage()
	other.Session, other.Tag = "refactor", "ask:harness:1"
	s.deliver(context.Background(), other)
	s.deliver(context.Background(), testMessage())

	for _, device := range []int{1, 2} {
		if got(device) != 2 {
			t.Errorf("device %d got %d notifications out of two: watching one session silenced more than it should",
				device, got(device))
		}
	}
}

func TestPushGoesToEveryoneWhenViewsAreUnavailable(t *testing.T) {
	s, got := twoDevices(t)
	s.UseViews(&memViews{by: map[string][]int64{"aacpanel": {1}}, fail: true})

	s.deliver(context.Background(), sessionMessage())

	for _, device := range []int{1, 2} {
		if got(device) != 1 {
			t.Errorf("device %d got no notification (%d), though nothing is known about what it watches",
				device, got(device))
		}
	}
}

func TestSessionEventsCarryTheSessionName(t *testing.T) {
	cur := World{Now: time.Now(), Sessions: []Session{{
		ID: "u-1", Name: "aacpanel", Status: "waiting", StatusAt: 1,
		Ask: &Ask{Header: "Which way", Text: "a or b", At: "1"},
	}}}
	prev := World{Now: cur.Now, Sessions: []Session{{ID: "u-1", Name: "aacpanel", Status: "busy"}}}

	r := Look(prev, cur)
	if len(r.Raise) == 0 {
		t.Fatal("a session question did not become an event")
	}
	for _, e := range r.Raise {
		if e.Domain != DomainSession {
			continue
		}
		if e.Session != "aacpanel" {
			t.Errorf("event %q arrived without the session name (%q)", e.Key, e.Session)
		}
		if e.raised().Session != "aacpanel" {
			t.Errorf("the session name got lost on the way into the message: %+v", e.raised())
		}
	}
}

func TestGoneMessageKeepsTheSessionName(t *testing.T) {
	e := Event{
		Key: "wait:u-1:7", Domain: DomainSession, Session: "aacpanel",
		Title: "Waiting for permission · aacpanel", Body: "Input needed",
		GoneTitle: "Session is running · aacpanel", GoneBody: "The wait is over",
	}
	if got := e.gone().Session; got != "aacpanel" {
		t.Errorf("clearing the event arrived without the session name (%q): there would be nothing to clear it by", got)
	}
}
