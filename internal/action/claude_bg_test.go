package action

import "testing"

func TestBackgroundActionsValidateTheirLaunchBoundary(t *testing.T) {
	for _, tc := range []struct {
		req   Request
		valid bool
	}{
		{Request{ID: "bg", Kind: SessionBGStop, Target: "lead"}, true},
		{Request{ID: "bg", Kind: SessionBGAttach, Target: "lead", Terminal: "t-1234abcd"}, true},
		{Request{ID: "bg", Kind: SessionBGAttach, Target: "lead"}, false},
		{Request{ID: "bg", Kind: SessionBGAttach, Target: "lead", Terminal: "$(touch /tmp/no)"}, false},
		{Request{ID: "bg", Kind: SessionBGStop, Target: "lead", Terminal: "t-1234abcd"}, false},
		{Request{ID: "bg", Kind: SessionBGAdopt, Target: "lead"}, false},
		{Request{ID: "bg", Kind: SessionBGAdopt, Target: "lead", Project: &Project{Path: "/srv/proj", Session: "lead"}}, true},
	} {
		if err := tc.req.Validate(); (err == nil) != tc.valid {
			t.Errorf("%+v: %v, valid=%v", tc.req, err, tc.valid)
		}
	}
}
