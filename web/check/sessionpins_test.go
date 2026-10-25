package check

import (
	"reflect"
	"slices"
	"testing"
)

type pinLists struct {
	Phone, Desk, Keys, Ordered []string
}

func TestPhoneSectionsKeepPinsAndMapOrder(t *testing.T) {
	type section struct {
		Title string
		Rows  []string
	}
	var got struct {
		Sections struct {
			Before, Changed, Answered   []section
			StatesBefore, StatesChanged map[string]string
			DeskBefore, DeskChanged     []string
		}
	}
	runFixture(t, "sessionpins.html", &got)
	p := got.Sections
	want := []section{
		{"pinned", []string{"review", "audit"}},
		{"needs you", []string{"asking"}},
		{"sessions", []string{"a-helper", "quiet", "z-helper", "beta", "alpha", "outside-a", "outside-z"}},
	}
	if !reflect.DeepEqual(p.Before, want) || !reflect.DeepEqual(p.Changed, want) {
		t.Errorf("the phone sections read %+v, then %+v after activity changed; expected %+v", p.Before, p.Changed, want)
	}
	for name, state := range map[string][2]string{
		"quiet": {"idle", "working"}, "alpha": {"working", "idle"}, "audit": {"working", "idle"},
	} {
		if p.StatesBefore[name] != state[0] || p.StatesChanged[name] != state[1] {
			t.Errorf("%s says %q, then %q; expected %q, then %q without moving", name,
				p.StatesBefore[name], p.StatesChanged[name], state[0], state[1])
		}
	}
	answered := []section{
		{"pinned", []string{"review", "audit"}},
		{"sessions", []string{"a-helper", "quiet", "z-helper", "beta", "asking", "alpha", "outside-a", "outside-z"}},
	}
	if !reflect.DeepEqual(p.Answered, answered) {
		t.Errorf("after the question is answered the sections read %+v; expected the project back in the map, %+v", p.Answered, answered)
	}
	if !slices.Equal(p.DeskBefore, p.DeskChanged) || !slices.Equal(p.DeskBefore, append([]string{"review", "audit"}, answered[1].Rows...)) {
		t.Errorf("the desktop order reads %v, then %v; it keeps the map's order after the pins", p.DeskBefore, p.DeskChanged)
	}
}

func TestPinnedGroupsKeepTheirChildrenAndTheirContour(t *testing.T) {
	var got struct {
		Pins struct {
			Before, Pinned, Multiple, Reopened, Unpinned, Restored pinLists
			Pressed, Sent                                          []string
			Confirmation                                           bool
		}
	}
	runWideFixture(t, "sessiongroups.html", &got)
	p := got.Pins
	if len(p.Before.Phone) != 6 || len(p.Before.Desk) != 7 {
		t.Fatalf("the lists did not start with all sessions: %+v", p.Before)
	}
	first := []string{"review", "child", "thread", "alpha"}
	if len(p.Pinned.Phone) < len(first) || !slices.Equal(p.Pinned.Phone[:len(first)], first) ||
		len(p.Pinned.Desk) < len(first) || !slices.Equal(p.Pinned.Desk[:len(first)], first) {
		t.Errorf("the pinned group did not stand first, alone with its children: %+v", p.Pinned)
	}
	multiple := []string{"review", "child", "thread", "audit", "alpha", "beta", "gamma"}
	if !slices.Equal(p.Multiple.Desk, multiple) || !slices.Equal(p.Multiple.Phone, multiple[:6]) ||
		!slices.Equal(p.Multiple.Ordered, multiple) || !slices.Equal(p.Multiple.Keys, multiple) {
		t.Errorf("pins lost their creation order, children or contour: %+v", p.Multiple)
	}
	if !reflect.DeepEqual(p.Multiple, p.Reopened) {
		t.Errorf("a new conversation or state moved the pinned group: %+v", p.Reopened)
	}
	if len(p.Unpinned.Desk) != 7 || p.Unpinned.Desk[0] != "audit" || p.Unpinned.Phone[0] != "audit" {
		t.Errorf("unpinning the first group moved the second: %+v", p.Unpinned)
	}
	if !reflect.DeepEqual(p.Restored, p.Before) {
		t.Errorf("removing the pins did not restore the original order: %+v, expected %+v", p.Restored, p.Before)
	}
	if len(p.Pressed) != 3 {
		t.Errorf("the pinned root and its children do not show the shared pin: %v", p.Pressed)
	}
	sent := []string{
		`session.pin:review:{"on":true}`,
		`session.pin:audit:{"on":true}`,
		`session.pin:review:{"on":false}`,
		`session.pin:audit:{"on":false}`,
	}
	if !slices.Equal(p.Sent, sent) || p.Confirmation {
		t.Errorf("the phone and menus sent %v, asked again %v; expected %v", p.Sent, p.Confirmation, sent)
	}
}

type pinLook struct {
	Pressed, Pending, Off, Dim, Mark, MarkOn, MarkPending, MarkDim bool
}

type pinWaits struct {
	Before, Pinning, Answered, Pinned, SecondPinning, Unpinning, Unpinned, Restored, Expired pinLists
	PinLook, ConfirmedLook, UnpinLook, UnpinnedLook, ExpiredLook                             pinLook
	AnswerPending, Lagging, Polled                                                           bool
	PinMenu, UnpinMenu                                                                       struct {
		Label, Note string
		Off         bool
	}
	Refused []struct {
		During, After    pinLists
		Pending, Visible bool
		Reason           string
	}
	RefusedUnpin struct{ Before, During, After pinLists }
}

func TestPinPressMovesBothListsBeforeTheSnapshot(t *testing.T) {
	for _, screen := range []string{"phone", "desk"} {
		t.Run(screen, func(t *testing.T) {
			var p pinWaits
			if screen == "phone" {
				runFixture(t, "sessionpins.html", &p)
			} else {
				var got struct{ Pins pinWaits }
				runWideFixture(t, "sessiongroups.html", &got)
				p = got.Pins
			}
			if !reflect.DeepEqual(p.Pinning, p.Pinned) || !reflect.DeepEqual(p.Answered, p.Pinned) ||
				reflect.DeepEqual(p.Pinning, p.Before) {
				t.Errorf("the press did not move the group before the answer and snapshot: %+v", p)
			}
			want := []string{"review", "child", "thread", "audit", "alpha", "beta", "gamma"}
			if !slices.Equal(p.SecondPinning.Desk, want) || !slices.Equal(p.SecondPinning.Keys, want) ||
				!slices.Equal(p.SecondPinning.Ordered, want) || !slices.Equal(p.SecondPinning.Phone, want[:6]) {
				t.Errorf("the next press did not follow the existing pin: %+v", p.SecondPinning)
			}
			if !reflect.DeepEqual(p.Unpinning, p.Unpinned) {
				t.Errorf("unpinning waited for a snapshot: %+v, confirmed %+v", p.Unpinning, p.Unpinned)
			}
			if !p.PinLook.Pressed || !p.PinLook.Pending || !p.PinLook.Off || !p.PinLook.Dim ||
				!p.PinLook.MarkOn || !p.PinLook.MarkPending || (screen == "desk" && !p.PinLook.MarkDim) {
				t.Errorf("a pin in flight does not show its expected state quietly: %+v", p.PinLook)
			}
			if !p.AnswerPending || !p.Lagging {
				t.Error("the executor's answer or a snapshot without the pin cleared the wait")
			}
			if !p.ConfirmedLook.Pressed || p.ConfirmedLook.Pending || p.ConfirmedLook.Off || p.ConfirmedLook.Dim ||
				!p.ConfirmedLook.MarkOn || p.ConfirmedLook.MarkPending {
				t.Errorf("the confirmed pin still looks pending: %+v", p.ConfirmedLook)
			}
			if p.UnpinLook.Pressed || !p.UnpinLook.Pending || !p.UnpinLook.Off || !p.UnpinLook.Dim ||
				p.UnpinLook.MarkOn || !p.UnpinLook.MarkPending {
				t.Errorf("unpinning did not show the expected state: %+v", p.UnpinLook)
			}
			if p.UnpinnedLook.Pressed || p.UnpinnedLook.Pending || p.UnpinnedLook.Off || !p.UnpinnedLook.Mark || p.UnpinnedLook.MarkOn || p.UnpinnedLook.MarkPending {
				t.Errorf("the confirmed unpin left the button pressed or pending: %+v", p.UnpinnedLook)
			}
			if screen == "desk" && (p.PinMenu.Label != "Unpin the group of review" || !p.PinMenu.Off || p.PinMenu.Note != "Pinning the group" ||
				p.UnpinMenu.Label != "Pin the group of review" || !p.UnpinMenu.Off || p.UnpinMenu.Note != "Unpinning the group") {
				t.Errorf("the menus do not follow the expected pin: %+v, %+v", p.PinMenu, p.UnpinMenu)
			}
			if len(p.Refused) != 2 {
				t.Fatalf("expected a refusal from each screen, got %+v", p.Refused)
			}
			for _, refusal := range p.Refused {
				if reflect.DeepEqual(refusal.During, p.Restored) || !reflect.DeepEqual(refusal.After, p.Restored) ||
					refusal.Pending || !refusal.Visible || refusal.Reason != "the pin could not be saved" {
					t.Errorf("a refusal did not restore the list and show its reason: %+v", refusal)
				}
			}
			if reflect.DeepEqual(p.RefusedUnpin.During, p.RefusedUnpin.Before) ||
				!reflect.DeepEqual(p.RefusedUnpin.After, p.RefusedUnpin.Before) {
				t.Errorf("a refused unpin did not put the group back: %+v", p.RefusedUnpin)
			}
			if !reflect.DeepEqual(p.Expired, p.Restored) || !p.Polled || p.ExpiredLook.Pending || p.ExpiredLook.Pressed {
				t.Errorf("a snapshot that never arrives left the pin waiting: %+v", p.Expired)
			}
		})
	}
}

func TestPinWaitSettlesOnlyOnTheRequestedRootState(t *testing.T) {
	cases := []struct {
		name   string
		on     bool
		at     int
		names  []string
		pins   map[string]string
		settle bool
	}{
		{"pin is absent", true, 2, []string{"root"}, nil, false},
		{"pin is present", true, 2, []string{"root"}, map[string]string{"root": "stamp"}, true},
		{"unpin is absent", false, 2, []string{"root"}, nil, true},
		{"unpin is present", false, 2, []string{"root"}, map[string]string{"root": "stamp"}, false},
		{"only a child is pinned", true, 2, []string{"root", "child"}, map[string]string{"child": "stamp"}, false},
		{"root is gone", true, 2, []string{"child"}, nil, true},
		{"unpinned root is gone", false, 2, []string{"child"}, nil, true},
		{"pin snapshot is stale", true, 1, []string{"root"}, map[string]string{"root": "stamp"}, false},
		{"unpin snapshot is stale", false, 1, []string{"root"}, nil, false},
	}
	calls := make([][]any, 0, len(cases))
	for _, c := range cases {
		pins := c.pins
		if pins == nil {
			pins = map[string]string{}
		}
		calls = append(calls, []any{map[string]any{
			"kind": "pin", "target": "root", "seen": 1, "params": map[string]bool{"on": c.on},
		}, c.names, c.at, map[string]string{}, pins})
	}
	got := runModuleJS(t, "src/catchup.js", "settled", calls)
	for i, c := range cases {
		if done, _ := got[i].(bool); done != c.settle {
			t.Errorf("%s: settled=%v, expected %v", c.name, done, c.settle)
		}
	}
}
