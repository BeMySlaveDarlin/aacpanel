package notify

import (
	"context"
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
	s, got := twoDevices(t)
	s.UseViews(&memViews{by: map[string][]int64{"work": {1}}})
	s.deliver(context.Background(), m)
	if got(1) != 0 || got(2) != 1 {
		t.Fatalf("request delivery: watching=%d other=%d", got(1), got(2))
	}
}
