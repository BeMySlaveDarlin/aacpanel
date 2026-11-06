package release

import (
	"testing"
)

func TestOnlyATagOfThreePlainNumbersIsARelease(t *testing.T) {
	for tag, want := range map[string]Release{
		"v0.1.0":   {0, 1, 0},
		"v1.2.3":   {1, 2, 3},
		"v10.20.3": {10, 20, 3},
	} {
		if got, ok := Parse(tag); !ok || got != want || got.String() != tag {
			t.Errorf("%s reads as %v (%v), written back %s", tag, got, ok, got.String())
		}
	}
	for _, tag := range []string{
		"", "v", "1.2.3", "V1.2.3", "v1.2", "v1.2.3.4", "v1.2.3-rc1", "v1.2.3+build",
		"v01.2.3", "v1.02.3", "v1.2.03", "v1..3", "v1.2.", "v-1.2.3", "v+1.2.3", "v1.2.3\n",
		" v1.2.3", "v1.2.x", "v99999999999999999999.0.0", "latest",
	} {
		if r, ok := Parse(tag); ok {
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

// A plain update stays in its paradigm, so the command a screen or a push
// names for a release of a later one is the one that crosses into it.
func TestTheUpdateToAReleaseNamesTheParadigmItCrosses(t *testing.T) {
	for _, c := range []struct {
		from, to Release
		want     string
	}{
		{Release{1, 2, 0}, Release{1, 2, 1}, "./install.sh update"},
		{Release{1, 2, 0}, Release{1, 3, 0}, "./install.sh update"},
		{Release{1, 2, 0}, Release{2, 0, 0}, "./install.sh update --to 2"},
		{Release{1, 9, 9}, Release{3, 0, 1}, "./install.sh update --to 3"},
	} {
		if got := c.from.UpdateTo(c.to); got != c.want {
			t.Errorf("%s to %s: %q, want %q", c.from, c.to, got, c.want)
		}
	}
	if got, want := (Release{1, 3, 0}).Page(), Repository+"/releases/tag/v1.3.0"; got != want {
		t.Errorf("the page of v1.3.0 is %q, want %q", got, want)
	}
}
