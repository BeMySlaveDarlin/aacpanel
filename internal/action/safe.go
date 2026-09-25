package action

import (
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

func safePath(s string) error {
	if strings.Contains(s, "..") {
		return badRequest("the project path contains .. — a way out of the path")
	}
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
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

func safeTarget(s string) error {
	if strings.Contains(s, "..") {
		return badRequest("the target contains .. — a way out of the path")
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '/':
		default:
			return badRequest("the target contains a forbidden character %q", r)
		}
	}
	return nil
}
