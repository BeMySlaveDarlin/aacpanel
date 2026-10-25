package action

import (
	"testing"
	"time"
)

func TestPauseScopeAndTimeStayInTheService(t *testing.T) {
	if Valid(ProjectPause) || (Request{ID: "p", Kind: ProjectPause, Target: "lab"}).Validate() == nil {
		t.Fatal("the host accepted the service's pause action")
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		pause Pause
		ok    bool
	}{
		{Pause{Project: 1, Until: now.Add(time.Hour).Format(time.RFC3339)}, true},
		{Pause{Project: 1, Resume: true}, true},
		{Pause{Resume: true}, false},
		{Pause{Project: 1}, false},
		{Pause{Project: 1, Until: now.Format(time.RFC3339)}, false},
		{Pause{Project: 1, Until: now.Add(8 * 24 * time.Hour).Format(time.RFC3339)}, false},
		{Pause{Project: 1, Resume: true, Until: now.Add(time.Hour).Format(time.RFC3339)}, false},
	} {
		_, err := c.pause.Validate(now)
		if (err == nil) != c.ok {
			t.Errorf("%+v: %v, want valid %v", c.pause, err, c.ok)
		}
	}
}
