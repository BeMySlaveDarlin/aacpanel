package sessionintent

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Quick names the phrases offered as messages in the session's command ring.
const Quick = "quick"

const (
	QuickMax       = 5
	QuickPhraseMax = 200
	DefaultQuick   = "AskUserQuestion\nGo on\nTidy up the checklist\nStatus\nThat's all for today"
)

// NormalizeQuick keeps one phrase per line. Blank lines cannot hide an
// inherited list, and limits count characters rather than UTF-8 bytes.
func NormalizeQuick(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	for _, r := range text {
		if r != '\n' && r != '\t' && (r < 0x20 || r == 0x7f) {
			return "", fmt.Errorf("the quick phrases hold a control character")
		}
	}
	var phrases []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > QuickPhraseMax {
			return "", fmt.Errorf("a quick phrase is longer than %d characters", QuickPhraseMax)
		}
		phrases = append(phrases, line)
	}
	if len(phrases) > QuickMax {
		return "", fmt.Errorf("there are more than %d quick phrases", QuickMax)
	}
	return strings.Join(phrases, "\n"), nil
}
