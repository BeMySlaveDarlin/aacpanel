package install

import (
	"testing"
)

func TestOnlyATagOfThreePlainNumbersIsARelease(t *testing.T) {
	for tag, want := range map[string]Release{
		"v0.1.0":   {0, 1, 0},
		"v1.2.3":   {1, 2, 3},
		"v10.20.3": {10, 20, 3},
	} {
		if got, ok := ParseRelease(tag); !ok || got != want || got.String() != tag {
			t.Errorf("%s reads as %v (%v), written back %s", tag, got, ok, got.String())
		}
	}
	for _, tag := range []string{
		"", "v", "1.2.3", "V1.2.3", "v1.2", "v1.2.3.4", "v1.2.3-rc1", "v1.2.3+build",
		"v01.2.3", "v1.02.3", "v1.2.03", "v1..3", "v1.2.", "v-1.2.3", "v+1.2.3", "v1.2.3\n",
		" v1.2.3", "v1.2.x", "v99999999999999999999.0.0", "latest",
	} {
		if r, ok := ParseRelease(tag); ok {
			t.Errorf("%q is taken for release %s", tag, r)
		}
	}
}

func TestReleasesAreOrderedByParadigmThenMajorThenMinor(t *testing.T) {
	order := []Release{{0, 9, 9}, {1, 0, 0}, {1, 0, 1}, {1, 0, 10}, {1, 1, 0}, {1, 10, 0}, {2, 0, 0}}
	for i, a := range order {
		for j, b := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.Compare(b); got != want {
				t.Errorf("%s against %s: %d, want %d", a, b, got, want)
			}
		}
	}
}

func TestTheNewestReleaseIsPickedAmongOtherTags(t *testing.T) {
	r, ok := NewestRelease([]string{"v1.2.0", "nightly", "v1.10.0", "v1.9.9", "v2.0.0-rc1", "v01.99.0"})
	if !ok || r != (Release{1, 10, 0}) {
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
		{"the newest of its own paradigm", Release{1, 0, 1}, 1, Release{1, 4, 2}, "v2.1.0"},
		{"already the newest", Release{1, 4, 2}, 1, Release{1, 4, 2}, "v2.1.0"},
		{"the newest paradigm has nothing ahead", Release{2, 0, 0}, 2, Release{2, 1, 0}, ""},
		{"--to a later paradigm", Release{1, 1, 0}, 2, Release{2, 1, 0}, ""},
		{"--to its own paradigm is a plain update", Release{1, 1, 0}, 1, Release{1, 4, 2}, "v2.1.0"},
		{"a clone ahead of the tags stays", Release{2, 5, 0}, 2, Release{2, 5, 0}, ""},
		{"its release is gone from the tags", Release{0, 3, 0}, 0, Release{0, 3, 0}, "v2.1.0"},
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
		{"back a paradigm", Release{2, 1, 0}, 1, "the clone is on v2.1.0: an update goes forward, not back to paradigm 1"},
		{"a paradigm with no release", Release{2, 1, 0}, 3, "no release of paradigm 3 is out: the newest is v2.1.0"},
		{"a clone newer than the tags", Release{2, 5, 0}, 4, "no release of paradigm 4 is out: the newest is v2.5.0"},
	} {
		got, err := PickUpdate(c.current, tags, c.paradigm)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: %+v, %v; want the refusal %q", c.name, got, err, c.want)
		}
	}
}
