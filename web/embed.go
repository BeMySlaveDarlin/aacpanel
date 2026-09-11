// Package web serves the markup built into the binary.
//
// The file lives next to the markup itself rather than in cmd/: the go:embed
// directive takes paths relative to itself, and from cmd/aacpanel there is no
// reaching web/.
//
// dist is the result of the front-end build (see cmd/webbuild). It is not kept
// in git, so a .gitkeep lies in the directory: without a single file go:embed
// does not compile, and a broken `go build` on a fresh clone is a bad trade for
// an empty directory. Hence all: as well — otherwise the dotted files do not
// make it into the set.
//
//go:generate go run aacpanel/cmd/webbuild
package web

import "embed"

//go:embed *.html manifest.webmanifest static all:dist
var FS embed.FS
