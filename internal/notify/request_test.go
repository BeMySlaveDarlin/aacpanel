package notify

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRequestPushCannotBeQuietedAndSkipsTheWatchingDevice(t *testing.T) {
	m := Requested(Session{ID: "conversation", Name: "work"}, "task", "Restart the router")
	if m.Kind != KindRequest || kindOf(m.Tag) != KindRequest || m.Title != "Restart the router" || m.Session != "work" {
		t.Fatalf("request push: %+v", m)
	}
	prefs := Prefs{Off: []string{KindRequest}, Sessions: []string{"/srv"}}
	if !prefs.Allows(m) || quietOf(m.Kind, m.Source) != nil {
		t.Fatal("a request push can be quieted")
	}
	soon := CommandWindow(Session{ID: "conversation", Name: "work"}, "task", m.Title, 2, "2099-01-01T21:00:00+07:00", false)
	open := CommandWindow(Session{ID: "conversation", Name: "work"}, "task", m.Title, 2, "2099-01-01T21:00:00+07:00", true)
	if soon.Tag == open.Tag || soon.Tag == m.Tag {
		t.Fatal("window pushes share an identity")
	}
	for _, window := range []Message{soon, open} {
		if window.Kind != KindRequest || kindOf(window.Tag) != KindRequest || !prefs.Allows(window) || quietOf(window.Kind, window.Source) != nil {
			t.Fatalf("a command window changed its request preferences: %+v", window)
		}
	}
	s, got := twoDevices(t)
	s.UseViews(&memViews{by: map[string][]int64{"work": {1}}})
	s.deliver(context.Background(), m)
	if got(1) != 0 || got(2) != 1 {
		t.Fatalf("request delivery: watching=%d other=%d", got(1), got(2))
	}
}

// A chain stays open for hours where it was posted, so a tap on the push of
// its step has to name the act: the end of the feed is not where it stands.
// Every other push about a session opens the session alone.
func TestRequestPushOpensTheConversationOnItsAct(t *testing.T) {
	s := Session{ID: "conversation", Name: "harness rework", Ask: &Ask{Text: "Which way?", At: "1"}}
	for _, c := range []struct {
		name string
		push Message
		want string
	}{
		{"request", Requested(s, "a1b2c3", "Restart the router"), "/app?session=harness+rework&act=a1b2c3"},
		{"command soon", CommandWindow(s, "a1b2c3", "Restart the router", 2, "2099-01-01T21:00:00+07:00", false), "/app?session=harness+rework&act=a1b2c3"},
		{"command open", CommandWindow(s, "a1b2c3", "Restart the router", 2, "2099-01-01T21:00:00+07:00", true), "/app?session=harness+rework&act=a1b2c3"},
		{"question", asked(s).raised(), "/app?session=harness+rework"},
		{"question cleared", asked(s).gone(), "/app?session=harness+rework"},
	} {
		body, err := payloadOf(c.push)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("%s: the body is not JSON: %v (%s)", c.name, err, body)
		}
		if payload.URL != c.want {
			t.Errorf("%s: the push points at %q, expected %q", c.name, payload.URL, c.want)
		}
	}
}
