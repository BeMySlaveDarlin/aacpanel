package install

import (
	"fmt"

	"aacpanel/internal/release"
)

// Release is a release of the panel; the service compares its own with the
// latest one by the same rule, so the rule lives in one place.
type Release = release.Release

// ParseRelease reads a tag as a release; what is no release an update passes
// by.
func ParseRelease(tag string) (Release, bool) {
	return release.Parse(tag)
}

// NewestRelease is the newest release among tags, whatever else they hold;
// the tags at the commit of a clone give the release the clone is on.
func NewestRelease(tags []string) (Release, bool) {
	return newest(tags, func(Release) bool { return true })
}

func newest(tags []string, keep func(Release) bool) (Release, bool) {
	var best Release
	found := false
	for _, tag := range tags {
		r, ok := ParseRelease(tag)
		if !ok || !keep(r) {
			continue
		}
		if !found || r.Compare(best) > 0 {
			best, found = r, true
		}
	}
	return best, found
}

// UpdateTarget is where an update takes a clone that is on a release.
type UpdateTarget struct {
	// To is the release to move to. It is the one the clone is on when
	// nothing newer came out in the paradigm.
	To Release
	// Ahead is the newest release of a later paradigm than To's, which an
	// update does not take without --to; nil when there is none.
	Ahead *Release
}

// PickUpdate chooses the release an update of a clone on current moves to,
// out of the tags of the repository: the newest release of paradigm, which
// is current's own for a plain update and the one of --to otherwise. An
// update goes forward only: a paradigm behind current is refused, and so is
// one nothing came out in.
func PickUpdate(current Release, tags []string, paradigm int) (UpdateTarget, error) {
	if paradigm < current.Paradigm {
		return UpdateTarget{}, fmt.Errorf("the clone is on %s: an update goes forward, not back to paradigm %d", current, paradigm)
	}
	to, ok := newest(tags, func(r Release) bool { return r.Paradigm == paradigm })
	switch {
	case !ok && paradigm == current.Paradigm:
		to = current
	case !ok:
		latest, _ := NewestRelease(append([]string{current.String()}, tags...))
		return UpdateTarget{}, fmt.Errorf("no release of paradigm %d is out: the newest is %s", paradigm, latest)
	case to.Compare(current) < 0:
		to = current
	}
	target := UpdateTarget{To: to}
	if ahead, ok := newest(tags, func(r Release) bool { return r.Paradigm > to.Paradigm }); ok {
		target.Ahead = &ahead
	}
	return target, nil
}
