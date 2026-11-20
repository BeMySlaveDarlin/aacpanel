package check

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type chatFollow struct {
	PastAsked       []string `json:"pastAsked"`
	PastComposer    bool     `json:"pastComposer"`
	LiveComposer    bool     `json:"liveComposer"`
	RestartAsked    []string `json:"restartAsked"`
	RestartComposer bool     `json:"restartComposer"`
	Navigation      []struct {
		Name       string `json:"name"`
		Scrollable bool   `json:"scrollable"`
		AtEnd      bool   `json:"atEnd"`
	} `json:"navigation"`
	JumpReturns []struct {
		Mode  string `json:"mode"`
		AtEnd bool   `json:"atEnd"`
	} `json:"jumpReturns"`
	ClickedAtEnd     *bool  `json:"clickedAtEnd"`
	ArrivedWhileAway int    `json:"arrivedWhileAway"`
	Error            string `json:"error"`
}

// A closed conversation opened beside a live session of the same name keeps
// its own feed and gets no composer: typed there, a message goes to the live
// session and its echo never reaches the feed on screen. A live conversation
// whose session restarts under the same name goes on to the new conversation
// instead of standing on the old feed under the live session's head.
func TestAnOpenConversationFollowsItsSessionAndNotItsName(t *testing.T) {
	parallel(t)
	for _, c := range []struct {
		screen  string
		fixture string
		run     func(*testing.T, string, any)
	}{
		{"phone", "chatfollow.html", runFixture},
		{"desktop", "chatfollowdesk.html", runDesktopChatFollow},
	} {
		t.Run(c.screen, func(t *testing.T) {
			var got chatFollow
			c.run(t, c.fixture, &got)
			if got.Error != "" {
				t.Fatalf("the fixture broke: %s", got.Error)
			}
			if strings.Join(got.PastAsked, ",") != "old0" {
				t.Errorf("the closed conversation asked its feed as %v, expected only its own id", got.PastAsked)
			}
			if got.PastComposer {
				t.Error("the closed conversation got the composer of the live session of its name")
			}
			if !got.LiveComposer {
				t.Fatal("the live conversation has no composer — the fixture does not open it as live")
			}
			if n := len(got.RestartAsked); n == 0 || got.RestartAsked[n-1] != "a2" {
				t.Errorf("after the restart the feed was asked as %v — it stayed on the old conversation", got.RestartAsked)
			}
			if !got.RestartComposer {
				t.Error("after the restart the conversation lost its composer")
			}
			if len(got.Navigation) != 3 {
				t.Fatalf("the fixture opened %d conversations, expected alpha, beta, alpha", len(got.Navigation))
			}
			for i, visit := range got.Navigation {
				if !visit.Scrollable {
					t.Errorf("visit %d to %s has no overflow — the fixture cannot check opening at the end", i+1, visit.Name)
				}
				if !visit.AtEnd {
					t.Errorf("visit %d to %s opened above the end of the conversation", i+1, visit.Name)
				}
			}
			if c.screen == "desktop" {
				if got.ClickedAtEnd == nil || !*got.ClickedAtEnd {
					t.Error("clicking an ordinary message moved the feed away from its end")
				}
				if got.ArrivedWhileAway != 8 {
					t.Errorf("%d messages arrived while the other conversation was open, expected 8", got.ArrivedWhileAway)
				}
				if len(got.JumpReturns) != 3 {
					t.Fatalf("the fixture checked %d returns after scrolling to the end, expected 3", len(got.JumpReturns))
				}
				for _, visit := range got.JumpReturns {
					if !visit.AtEnd {
						t.Errorf("the feed returned above its end after %s", visit.Mode)
					}
				}
			}
		})
	}
}

// A native click moves the browser's caret and focus; HTMLElement.click does
// neither, and misses the scroll anchoring that follows a session switch.
func runDesktopChatFollow(t *testing.T, fixture string, into any) {
	t.Helper()
	click := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var point struct{ X, Y, Wheel float64 }
		if err := json.NewDecoder(r.Body).Decode(&point); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		b, err := sharedBrowser(chromeBinary(), deskPointer)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var session string
		b.mu.Lock()
		for id, page := range b.pages {
			if page.url == "http://"+r.Host+"/fixture.html" {
				session = id
				break
			}
		}
		b.mu.Unlock()
		if session == "" {
			http.Error(w, "The fixture tab is gone", http.StatusGone)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if point.Wheel != 0 {
			if _, err := b.call(ctx, session, "Input.dispatchMouseEvent", map[string]any{
				"type": "mouseWheel", "x": point.X, "y": point.Y, "deltaX": 0, "deltaY": point.Wheel,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			if _, err := b.call(ctx, session, "Input.dispatchMouseEvent", map[string]any{
				"type": kind, "x": point.X, "y": point.Y, "button": "left", "clickCount": 1,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	runFixtureServing(t, fixture, deskScreen, deskPointer, map[string]http.Handler{"/check/pointer": click}, into)
}
