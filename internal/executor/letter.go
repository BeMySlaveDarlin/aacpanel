package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A letter from one session to another goes the way claude sends one: a line
// on the message socket of the recipient's claude, the text in the envelope
// claude puts a letter in, naming the sender's address and name. The
// recipient's claude shows the model the envelope whole, after a line saying
// another session sent it and before a word that it was not typed by the
// person, so a request of an agent is weighed as one. A letter out of an
// envelope reaches the model with no sender at all, and words typed into a
// terminal or put on the stream reach it as the person's own — so a letter is
// never delivered any other way.

// letterTag is the envelope of a letter between sessions, as claude names it.
const letterTag = "cross-session-message"

// sessionLetter sends a letter to target from the live session that runs the
// conversation from, in whichever account either of them lives. The sender is
// found by its conversation, which a session knows of itself, and not by a
// name it could be given: the name and the address the recipient reads are
// the sender's own.
func (e *Executor) sessionLetter(ctx context.Context, target, from, text string) (string, error) {
	sender, err := liveSessionOf(from)
	if err != nil {
		return "", err
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if s.PID == sender.PID {
		return "", fmt.Errorf("session %s is the one writing: a session writes no letter to itself", s.Name)
	}
	if s.Socket == "" {
		return "", fmt.Errorf("session %s takes no letters: its claude publishes no message socket, "+
			"and a letter is not typed into a session instead — it would read as the words of its person", s.Name)
	}
	address := letterAddress(sender.Socket)
	line := map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": letterEnvelope(address, sender.Name, text)},
	}
	if address != "" {
		line["from"] = address
	}
	if err := writeLine(ctx, s.Socket, line); err != nil {
		return "", fmt.Errorf("session %s did not take the letter: %w", s.Name, err)
	}
	return fmt.Sprintf("a letter from %s to %s (%s), %d characters",
		sender.Name, s.Name, sessionWhere(s), utf8.RuneCountInString(text)), nil
}

// liveSessionOf finds the live session that runs a conversation, in any
// account of the machine.
func liveSessionOf(conversation string) (liveSession, error) {
	all, err := allLiveSessions()
	if err != nil {
		return liveSession{}, err
	}
	for _, s := range all {
		if s.SessionID == conversation {
			return s, nil
		}
	}
	return liveSession{}, fmt.Errorf("no live session runs conversation %s: the letter has no sender", conversation)
}

// letterEnvelope puts a letter in the envelope claude reads one from: the
// sender's address, its name and the body, where a closing tag is escaped the
// way claude escapes it, so the body cannot end the envelope early. The
// receiving claude checks that the envelope is one it would have made itself
// before it takes the name out of it.
func letterEnvelope(address, name, body string) string {
	var attrs []string
	if address != "" {
		attrs = append(attrs, `from="`+address+`"`)
	}
	if name = letterName(name); name != "" {
		attrs = append(attrs, `from-name="`+name+`"`)
	}
	head := "<" + letterTag
	if len(attrs) > 0 {
		head += " " + strings.Join(attrs, " ")
	}
	return head + ">\n" + escapeLetter(body) + "\n</" + letterTag + ">"
}

// letterAddress is the address of a session on its message socket, where an
// answer to the letter reaches it: uds: and the path, with every byte outside
// the few claude leaves as they are written as %XX.
func letterAddress(socket string) string {
	if socket == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("uds:")
	for i := 0; i < len(socket); i++ {
		c := socket[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == ':', c == '_', c == '/', c == '.', c == '\\', c == '-':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// letterNameMax is how many characters of the sender's name claude keeps.
const letterNameMax = 64

// letterName is the sender's name as claude writes it into an envelope:
// without quotes and angle brackets, without control and format characters,
// trimmed, and cut at 64 characters.
func letterName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`"<>`, r) || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Cs, unicode.Zl, unicode.Zp) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > letterNameMax {
		name = string(runes[:letterNameMax]) + "…"
	}
	return name
}

// What claude takes for the angle brackets and the slash of a tag: a closing
// tag written with a lookalike ends the envelope for a reader all the same.
const (
	openLike  = "<＜﹤〈⟨〈‹˂ᐸ❬❮❰⧼≮≺⋖"
	closeLike = ">＞﹥〉⟩〉›˃ᐳ❭❯❱⧽≯≻⋗"
	slashLike = "/／∕⁄"
	// unseen are the characters claude lets stand between the letters of a
	// tag: the invisible ones, the combining marks and the controls.
	unseen = `\x{00ad}\x{034f}\x{0600}-\x{0605}\x{061c}\x{06dd}\x{070f}\x{0890}\x{0891}\x{08e2}\x{115f}\x{1160}` +
		`\x{17b4}\x{17b5}\x{180b}-\x{180f}\x{200b}-\x{200f}\x{202a}-\x{202e}\x{2060}-\x{206f}\x{3164}` +
		`\x{fe00}-\x{fe0f}\x{feff}\x{ffa0}\x{fff0}-\x{fffb}\x{110bd}\x{110cd}\x{13430}-\x{1343f}` +
		`\x{1bca0}-\x{1bca3}\x{1d173}-\x{1d17a}\x{16fe4}\x{e0000}-\x{e0fff}` +
		`\x{0300}-\x{0344}\x{0346}-\x{036f}\x{0483}-\x{0489}\x{0591}-\x{05bd}\x{05bf}\x{05c1}\x{05c2}` +
		`\x{05c4}\x{05c5}\x{05c7}\x{0610}-\x{061a}\x{064b}-\x{065f}\x{0670}\x{06d6}-\x{06dc}\x{06df}-\x{06e4}` +
		`\x{06e7}\x{06e8}\x{06ea}-\x{06ed}\x{1ab0}-\x{1aff}\x{1dc0}-\x{1dff}\x{20d0}-\x{20ff}\x{3099}\x{309a}` +
		`\x{fe20}-\x{fe2f}\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x{9f}\x{2028}\x{2029}`
	// dashLike is what claude takes for the dashes of a tag's name.
	dashLike = `_\p{Pc}\x{2017}\x{02cd}\x{07fa}\x{0640}\p{Pd}\x{2212}\x{207b}\x{208b}\x{02d7}\x{2796}\x{2043}\x{30fc}\x{ff70}`
)

// closingLetter matches what follows the opening bracket of a closing tag of
// the envelope, the way claude finds one to escape: anything but a letter, a
// digit or a bracket up to the slash, the same after it, the name in any case
// with unseen characters between its letters, and no letter right after it.
var closingLetter = regexp.MustCompile(func() string {
	word := `A-Za-z0-9_\-`
	var name strings.Builder
	for i, c := range letterTag {
		if i > 0 {
			name.WriteString("[" + unseen + "]*")
		}
		if c == '-' {
			name.WriteString("[" + dashLike + "]")
			continue
		}
		name.WriteRune(c)
	}
	return `^(?i)[^` + word + openLike + closeLike + slashLike + `]*[` + slashLike + `]` +
		`[^` + word + openLike + closeLike + `]*` + name.String() + `(?:[^` + word + `]|$)`
}())

// escapeLetter escapes every closing tag of the envelope in a letter's body
// the way claude does: its opening bracket, or a lookalike of it, becomes <\.
// A bracket already followed by a backslash is escaped as it stands.
func escapeLetter(body string) string {
	var b strings.Builder
	for i := 0; i < len(body); {
		r, size := utf8.DecodeRuneInString(body[i:])
		rest := body[i+size:]
		if strings.ContainsRune(openLike, r) && !strings.HasPrefix(rest, `\`) && closingLetter.MatchString(rest) {
			b.WriteString(`<\`)
		} else {
			b.WriteString(body[i : i+size])
		}
		i += size
	}
	return b.String()
}
