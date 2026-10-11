package codex

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"aacpanel/internal/codex/contract"
	"aacpanel/internal/stream"
)

// Release is the release of codex the daemon of a home runs, beside the
// release its protocol was last checked against — the reference of the
// contract this executor was built with. The daemon updates itself, and a
// release past the reference may have dropped a field the panel decodes or
// renamed a status it compares; the link writes what each handshake names,
// and a daemon that updated itself comes back on a new connection, so the
// file follows it. The collector puts it in the snapshot, and the service
// says once a release that the two differ.
type Release struct {
	// At is when the daemon named it, in seconds since the epoch.
	At        int64  `json:"at"`
	Contour   string `json:"contour"`
	CodexHome string `json:"codexHome"`
	// Release is the number of the release the daemon runs, empty when its
	// handshake named none the link could read.
	Release string `json:"release"`
	// Reference is the number of the release the protocol was last checked
	// against.
	Reference string `json:"reference"`
}

// referenceRelease is the release the protocol was last checked against,
// read once off the reference the executor carries.
var referenceRelease = sync.OnceValue(func() string {
	ref, err := contract.Reference()
	if err != nil {
		return ""
	}
	return ref.Release()
})

// releaseOf is the release a user agent of codex names: the number after the
// name of the client in its first word, "aacpanel/0.162.1 (Debian 13.0.0;
// x86_64) xterm-256color (aacpanel; 1)". The name is whichever client shook
// hands with the daemon first, so only the number is read.
func releaseOf(agent string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(agent), " ")
	i := strings.LastIndex(first, "/")
	if i < 0 {
		return ""
	}
	return first[i+1:]
}

// released writes the release the daemon named at the handshake.
func (l *Link) released(release string) {
	body, err := json.Marshal(Release{At: time.Now().Unix(), Contour: l.contour, CodexHome: l.home,
		Release: release, Reference: referenceRelease()})
	if err == nil {
		err = write(stream.CodexReleasePath(l.contour), body)
	}
	if err != nil {
		log.Printf("codex %s: the release its daemon runs was not written: %v", l.home, err)
	}
}
