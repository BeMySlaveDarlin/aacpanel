package check

import (
	"reflect"
	"testing"
)

// The walk uses the visible feed, so a manual scroll changes the next target
// and the panel's cards never become stops in the person's conversation.
func TestFeedWalksPreviousHumanMessages(t *testing.T) {
	parallel(t)
	for _, screen := range []struct {
		name, size, pointer string
	}{
		{"phone", phoneScreen, phonePointer},
		{"desktop", deskScreen, deskPointer},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				End struct {
					Up, Down   bool
					Tip, Label string
				}
				Middle                                   struct{ Up, Down bool }
				Walk                                     []int
				Held, StartHidden, EmptyHidden, Returned bool
				Manual, Paged                            int
				Reads, Exhausted, Stalled                []int
				Error                                    string
			}
			runFixtureOn(t, "feedprevious.html", screen.size, screen.pointer, &got)
			if got.Error != "" {
				t.Fatal(got.Error)
			}
			if !got.End.Up || got.End.Down || got.End.Tip != "" || got.End.Label != "to your previous message" {
				t.Errorf("at the end: %+v, expected only the labelled previous-message button, without a tip", got.End)
			}
			if !got.Middle.Up || !got.Middle.Down {
				t.Errorf("above the end: %+v, expected both ways through the feed", got.Middle)
			}
			if !reflect.DeepEqual(got.Walk, []int{50, 30, 10}) {
				t.Errorf("the walk stopped at %v, expected the person's messages only", got.Walk)
			}
			if !got.Held || !got.Returned {
				t.Errorf("new content held the position: %v; the end button resumed following: %v", got.Held, got.Returned)
			}
			if !got.StartHidden || !got.EmptyHidden {
				t.Errorf("no earlier human message: hidden at the start %v, in a feed of cards %v", got.StartHidden, got.EmptyHidden)
			}
			if got.Manual != 30 {
				t.Errorf("after a manual scroll the walk went to %d, expected 30", got.Manual)
			}
			if got.Paged != 10 || !reflect.DeepEqual(got.Reads, []int{100, 80}) {
				t.Errorf("earlier pages: target %d, reads %v, expected 10 after reads [100 80]", got.Paged, got.Reads)
			}
			if !reflect.DeepEqual(got.Exhausted, []int{100, 80}) || !reflect.DeepEqual(got.Stalled, []int{100}) {
				t.Errorf("paging did not stop at the beginning or without progress: exhausted %v, stalled %v", got.Exhausted, got.Stalled)
			}
		})
	}
}
