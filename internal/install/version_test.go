package install

import (
	"testing"
)

func TestTheNewestReleaseIsPickedAmongOtherTags(t *testing.T) {
	r, ok := NewestRelease([]string{"v1.2.0", "nightly", "v1.10.0", "v1.9.9", "v2.0.0-rc1", "v01.99.0"})
	if !ok || r != rel(1, 10, 0) {
		t.Errorf("the newest is %s (%v), want v1.10.0: numbers compare as numbers, and what is not a release is passed by", r, ok)
	}
	if r, ok := NewestRelease([]string{"nightly", "v2.0.0-rc1"}); ok {
		t.Errorf("tags with no release gave %s", r)
	}
}

func TestAnUpdateStaysInItsParadigm(t *testing.T) {
	tags := []string{"v1.0.0", "v1.0.1", "v1.1.0", "v1.4.2", "v1.10.0-rc1", "v2.0.0", "v2.1.0", "v3.0.0-rc1"}
	for _, c := range []struct {
		name     string
		current  Release
		paradigm int
		to       Release
		ahead    string
	}{
		{"the newest of its own paradigm", rel(1, 0, 1), 1, rel(1, 4, 2), "v2.1.0"},
		{"already the newest", rel(1, 4, 2), 1, rel(1, 4, 2), "v2.1.0"},
		{"the newest paradigm has nothing ahead", rel(2, 0, 0), 2, rel(2, 1, 0), ""},
		{"--to a later paradigm", rel(1, 1, 0), 2, rel(2, 1, 0), ""},
		{"--to its own paradigm is a plain update", rel(1, 1, 0), 1, rel(1, 4, 2), "v2.1.0"},
		{"a clone ahead of the tags stays", rel(2, 5, 0), 2, rel(2, 5, 0), ""},
		{"its release is gone from the tags", rel(0, 3, 0), 0, rel(0, 3, 0), "v2.1.0"},
	} {
		got, err := PickUpdate(c.current, tags, c.paradigm)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		ahead := ""
		if got.Ahead != nil {
			ahead = got.Ahead.String()
		}
		if got.To != c.to || ahead != c.ahead {
			t.Errorf("%s: from %s to paradigm %d goes to %s with %q ahead, want %s with %q ahead",
				c.name, c.current, c.paradigm, got.To, ahead, c.to, c.ahead)
		}
	}
}

func TestAnUpdateDoesNotGoBackOrNowhere(t *testing.T) {
	tags := []string{"v1.0.0", "v1.4.2", "v2.1.0", "v3.0.0-rc1"}
	for _, c := range []struct {
		name     string
		current  Release
		paradigm int
		want     string
	}{
		{"back a paradigm", rel(2, 1, 0), 1, "the clone is on v2.1.0: an update goes forward, not back to paradigm 1"},
		{"a paradigm with no release", rel(2, 1, 0), 3, "no release of paradigm 3 is out: the newest is v2.1.0"},
		{"a clone newer than the tags", rel(2, 5, 0), 4, "no release of paradigm 4 is out: the newest is v2.5.0"},
	} {
		got, err := PickUpdate(c.current, tags, c.paradigm)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: %+v, %v; want the refusal %q", c.name, got, err, c.want)
		}
	}
}

// rel is the release P.M.m.
func rel(paradigm, major, minor int) Release {
	return Release{Paradigm: paradigm, Major: major, Minor: minor}
}
