package action

import (
	"bytes"
	"path"
	"strings"
	"unicode"
)

func safeUUID(s string) bool {
	groups := []int{8, 4, 4, 4, 12}
	parts := strings.Split(s, "-")
	if len(parts) != len(groups) {
		return false
	}
	for i, part := range parts {
		if len(part) != groups[i] {
			return false
		}
		for _, r := range part {
			switch {
			case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
			default:
				return false
			}
		}
	}
	return true
}

// nameRune says whether a character may stand in the name of a directory or of
// the session named after one. A directory is named in any script and may hold
// a space, and a session with no name of its own on the map answers to the name
// of its directory — a resume from the archive takes it from the directory the
// conversation ran in. A letter comes with the marks that compose it: a name
// written in decomposed form spells the short i of Cyrillic as i and a breve.
//
// None of it is a way into a command: a name and a path travel as one word of
// argv and are never put into a command line as a string. What stays out is
// what something on the way reads as more than a character — a control
// character, and the marks tmux reads in a target as the address of another
// pane, window or session.
func nameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || r == ' '
}

func safePath(s string) error {
	if strings.Contains(s, "..") {
		return badRequest("the project path contains .. — a way out of the path")
	}
	for _, r := range s {
		switch {
		case nameRune(r):
		case r == '-', r == '_', r == '.', r == '/':
		default:
			return badRequest("the project path contains a forbidden character %q", r)
		}
	}
	return nil
}

func safeText(s string) error {
	for _, r := range s {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return badRequest("the message contains a control character %q", r)
		}
	}
	return nil
}

func safeFileName(name string) error {
	if name == "" {
		return badRequest("file without a name")
	}
	if len([]rune(name)) > fileNameMax {
		return badRequest("the file name is longer than %d characters", fileNameMax)
	}
	if strings.Contains(name, "..") {
		return badRequest("the file name contains .. — a way out of the path")
	}
	if name[0] == '.' || name[0] == '-' {
		return badRequest("the file name starts with %q", name[:1])
	}
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == '-', r == '_', r == '.':
		default:
			return badRequest("the file name contains a forbidden character %q", r)
		}
	}
	return nil
}

// jpegMark opens every JPEG file: the start-of-image marker and the next one.
var jpegMark = []byte{0xff, 0xd8, 0xff}

// checkPreview holds the copy of a picture for the feed to what the collector
// serves it as: a JPEG, and small.
func (f File) checkPreview() error {
	if len(f.Preview) == 0 {
		return nil
	}
	if len(f.Preview) > PreviewMax {
		return badRequest("the copy of file %q for the feed is larger than %d MB", f.Name, PreviewMax>>20)
	}
	if !bytes.HasPrefix(f.Preview, jpegMark) {
		return badRequest("the copy of file %q for the feed is not a JPEG", f.Name)
	}
	return nil
}

func safeID(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// sessionNameMax is how long a name given to a session may be: it becomes the
// key the panel and the host find the session by, and a line in the list.
const sessionNameMax = 64

// safeSessionName checks a name given to a session. It is written into a URL,
// a file name and the list of sessions, so it keeps to letters, digits and
// three marks, and it starts with neither a dash nor a dot: one reads as a
// flag, the other as a hidden file.
func safeSessionName(s string) error {
	if s == "" {
		return badRequest("the new name of the session is empty")
	}
	if len(s) > sessionNameMax {
		return badRequest("the new name is longer than %d characters", sessionNameMax)
	}
	if s[0] == '-' || s[0] == '.' {
		return badRequest("a name starting with %q reads as a flag or a hidden file", s[0])
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return badRequest("the new name contains %q: a name keeps to latin letters, digits, - _ and .", r)
		}
	}
	return nil
}

// safeTarget checks what an action is aimed at: a container, a stack, a
// session. The name of a session is the one of its directory unless the map
// gives it another, so a target is made of what a directory name is made of.
func safeTarget(s string) error {
	if strings.Contains(s, "..") {
		return badRequest("the target contains .. — a way out of the path")
	}
	for _, r := range s {
		switch {
		case nameRune(r):
		case r == '-', r == '_', r == '.', r == '/':
		default:
			return badRequest("the target contains a forbidden character %q", r)
		}
	}
	return nil
}

// codexPathMax bounds a path a letter of codex names its sender's place by.
const codexPathMax = 4096

// codexPlace checks a path a letter of codex names where its sender runs: an
// absolute path in its clean form, with no control characters. Nothing is done
// in it; the recipient reads it.
func codexPlace(what, dir string) error {
	switch {
	case dir == "":
		return badRequest("a letter of codex without %s its sender runs in", what)
	case len(dir) > codexPathMax:
		return badRequest("%s of the sender is longer than %d bytes", what, codexPathMax)
	case !path.IsAbs(dir) || path.Clean(dir) != dir:
		return badRequest("%s of the sender, %q, is no absolute path in its clean form", what, dir)
	}
	for _, r := range dir {
		if r < 0x20 || r == 0x7f {
			return badRequest("%s of the sender contains a control character %q", what, r)
		}
	}
	return nil
}
