package install

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
)

// Release is a release of the panel, the tag vP.M.m: Paradigm, Major, Minor.
// A paradigm is a change of approach, so an update never crosses one unless
// asked to; a major brings features and migrations; a minor brings fixes.
type Release struct {
	Paradigm, Major, Minor int
}

// releaseTag is the form make release and install.sh accept too.
var releaseTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ParseRelease reads a tag as a release. Only vP.M.m made of plain numbers
// is one: v1.2.3-rc1, 1.2.3 and v01.2.3 are tags of something else, and an
// update passes them by.
func ParseRelease(tag string) (Release, bool) {
	m := releaseTag.FindStringSubmatch(tag)
	if m == nil {
		return Release{}, false
	}
	var n [3]int
	for i, p := range m[1:] {
		v, err := strconv.Atoi(p)
		if err != nil {
			return Release{}, false // too long a number to be a release
		}
		n[i] = v
	}
	return Release{n[0], n[1], n[2]}, true
}

// String is the tag of the release.
func (r Release) String() string {
	return fmt.Sprintf("v%d.%d.%d", r.Paradigm, r.Major, r.Minor)
}

// Compare orders releases the way they come out: -1 when r is older than o,
// 1 when newer, 0 when it is the same release.
func (r Release) Compare(o Release) int {
	if c := cmp.Compare(r.Paradigm, o.Paradigm); c != 0 {
		return c
	}
	if c := cmp.Compare(r.Major, o.Major); c != 0 {
		return c
	}
	return cmp.Compare(r.Minor, o.Minor)
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
