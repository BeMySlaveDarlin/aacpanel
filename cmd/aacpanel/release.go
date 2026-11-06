package main

import (
	"log"

	"aacpanel/internal/notify"
	"aacpanel/internal/release"
)

// version is the release the service is built from. The image build writes
// the RELEASE= line of install.sh into it with -ldflags -X main.version: the
// build context has no .git to ask, and the line names the release of the
// commit a release tags and, between releases, the last one. A plain go
// build leaves it empty, and such a service knows no release of its own and
// is told of none.
var version string

// releaseCheck is the check for newer releases a service built from build
// runs. AACP_UPDATE_CHECK=0 turns it off, and then nothing goes out at all.
func releaseCheck(build string) *release.Check {
	check := release.NewCheck(build, env("AACP_UPDATE_CHECK", "1") != "1")
	switch {
	case !check.Asks() && check.Off:
		log.Print("the check for new releases is off: AACP_UPDATE_CHECK=0")
	case !check.Asks():
		log.Print("this build names no release: it is told of none, and GitHub is not asked")
	default:
		log.Printf("release %s: GitHub is asked for a newer one once a day", build)
	}
	return check
}

// panelRelease is what the screens say of the release of the panel.
type panelRelease struct {
	// Version is the release the service was built from; empty for a build
	// that names none.
	Version string `json:"version"`
	// Check says whether the service asks GitHub for newer releases.
	Check bool `json:"check"`
	// Newer is the latest release when it is newer than Version.
	Newer *newerRelease `json:"newer,omitempty"`
}

type newerRelease struct {
	Tag string `json:"tag"`
	// Page is the page of the release on GitHub: its notes and how to update.
	Page string `json:"page"`
	// Update is the command that moves this install to it.
	Update string `json:"update"`
}

// panelRelease reads the check for the screens. A server with no check knows
// the release it was built from, if any, and asks nothing.
func (s *Server) panelRelease() panelRelease {
	check := s.releases
	if check == nil {
		check = &release.Check{Build: version, Off: true}
	}
	out := panelRelease{Check: check.Asks()}
	build, known := release.Parse(check.Build)
	if known {
		out.Version = build.String()
	}
	if newer, ok := check.Newer(); ok {
		out.Newer = &newerRelease{Tag: newer.String(), Page: newer.Page(), Update: build.UpdateTo(newer)}
	}
	return out
}

// readRelease puts the newer release of the panel into the look of the host,
// once GitHub has answered: before that the absence of news is not news, and
// a reason told before a restart of the service would be forgotten and told
// again.
func (w *watcher) readRelease(cur *notify.World) {
	check := w.srv.releases
	if check == nil || !check.Answered() {
		return
	}
	cur.ReleaseSeen = true
	newer, ok := check.Newer()
	if !ok {
		return
	}
	build, _ := release.Parse(check.Build)
	cur.Release = &notify.PanelRelease{Running: build.String(), Latest: newer.String(), Update: build.UpdateTo(newer)}
}
