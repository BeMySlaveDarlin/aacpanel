package check

import (
	"reflect"
	"slices"
	"testing"
)

type pinLists struct {
	Phone, Desk, Keys, Ordered []string
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
