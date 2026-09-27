package check

import (
	"strings"
	"testing"
)

type chatFollow struct {
	PastAsked       []string `json:"pastAsked"`
	PastComposer    bool     `json:"pastComposer"`
	LiveComposer    bool     `json:"liveComposer"`
	RestartAsked    []string `json:"restartAsked"`
	RestartComposer bool     `json:"restartComposer"`
	Error           string   `json:"error"`
}

// A closed conversation opened beside a live session of the same name keeps
// its own feed and gets no composer: typed there, a message goes to the live
// session and its echo never reaches the feed on screen. A live conversation
// whose session restarts under the same name goes on to the new conversation
// instead of standing on the old feed under the live session's head.
func TestAnOpenConversationFollowsItsSessionAndNotItsName(t *testing.T) {
	for _, c := range []struct {
		screen  string
		fixture string
		run     func(*testing.T, string, any)
	}{
		{"phone", "chatfollow.html", runFixture},
		{"desktop", "chatfollowdesk.html", runWideFixture},
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
		})
	}
}
