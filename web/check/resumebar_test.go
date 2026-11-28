package check

import (
	"reflect"
	"strings"
	"testing"
)

func TestClosedConversationCanResumeInPlace(t *testing.T) {
	parallel(t)
	for _, screen := range []struct{ name, size, pointer string }{
		{"phone", phoneScreen, phonePointer},
		{"desktop", deskScreen, deskPointer},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Closed, Cancelled, Once, AwaitingLive, Revived, Retry, CodexLive bool
				Offline, Other, External, Follow, NoID                           bool
				Unavailable                                                      struct {
					Disabled    bool
					Reason, Tip string
				}
				Sent []struct {
					Kind, Target string
					Params       map[string]string
				}
				Error string
			}
			runFixtureOn(t, "resumebar.html", screen.size, screen.pointer, &got)
			if got.Error != "" {
				t.Fatal(got.Error)
			}
			for name, ok := range map[string]bool{
				"closed": got.Closed, "cancelled": got.Cancelled, "one request": got.Once,
				"waiting for live": got.AwaitingLive, "revived": got.Revived, "retry after failure": got.Retry,
				"codex composer": got.CodexLive, "offline": got.Offline, "other conversation": got.Other,
				"resumed elsewhere": got.External, "following restarts": got.Follow, "no id": got.NoID,
			} {
				if !ok {
					t.Errorf("%s: the conversation did not show the expected state", name)
				}
			}
			if !got.Unavailable.Disabled || !strings.Contains(got.Unavailable.Reason, "session.resume") ||
				!strings.Contains(got.Unavailable.Tip, "session.resume") {
				t.Errorf("the unknown action has no disabled button and explanation: %+v", got.Unavailable)
			}
			if len(got.Sent) != 3 {
				t.Fatalf("sent %d actions, expected one resume and two codex attempts: %+v", len(got.Sent), got.Sent)
			}
			for i, sent := range got.Sent {
				want := map[string]string{"session": "closed-claude"}
				target := "build"
				if i > 0 {
					want = map[string]string{"session": "closed-codex", "agent": "codex", "contour": "work"}
					target = "review"
				}
				if sent.Kind != "session.resume" || sent.Target != target || !reflect.DeepEqual(sent.Params, want) {
					t.Errorf("resume %d: %+v, expected session.resume for %s with %+v", i, sent, target, want)
				}
			}
		})
	}
}
