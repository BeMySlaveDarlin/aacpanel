// Package release is a release of the panel: the tag vP.M.m make release
// makes, the order releases come out in, and the check that asks GitHub
// which of them is the latest.
package release

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
)

// Repository is where the panel is published: an install is cloned from it,
// and its releases are the ones an install is told of.
const Repository = "https://github.com/BeMySlaveDarlin/aacpanel"

// Release is a release of the panel, the tag vP.M.m: Paradigm, Major, Minor.
// A paradigm is a change of approach, so an update never crosses one unless
// asked to; a major brings features and migrations; a minor brings fixes.
type Release struct {
	Paradigm, Major, Minor int
}

// tagForm is the form make release and install.sh accept too.
var tagForm = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Parse reads a tag as a release. Only vP.M.m made of plain numbers is one:
// v1.2.3-rc1, 1.2.3 and v01.2.3 are tags of something else, and an update
// passes them by.
func Parse(tag string) (Release, bool) {
	m := tagForm.FindStringSubmatch(tag)
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

// Page is the page of the release on GitHub, its notes and how to update.
func (r Release) Page() string {
	return Repository + "/releases/tag/" + r.String()
}

// UpdateTo is the command that moves an install on r to o. A plain update
// stays in the paradigm the clone is on, so a release of a later one is
// reached only by naming its paradigm.
func (r Release) UpdateTo(o Release) string {
	if o.Paradigm > r.Paradigm {
		return fmt.Sprintf("./install.sh update --to %d", o.Paradigm)
	}
	return "./install.sh update"
}
