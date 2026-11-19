package question

import (
	"regexp"
	"strings"
)

var (
	words     = regexp.MustCompile(`(?i)(^|[^\pL\pN_])(тво[йеёия][\pL\pN_]*|теб[еяю](?:$|[^\pL\pN_])|вле[йй]|влить|влива|влити|смерж|merge(?:$|[^\pL\pN_])|пришли|передай|передать|дёрни|дерни|не забудь|вопрос|switch(?:$|[^\pL\pN_])|запусти|выполни|прогони|перезапусти|перезагрузи|залогинь|войди|зайди|нажми|открой|подтверди|реши(?:$|[^\pL\pN_])|выбери|скажи|дай знать|напиши|жду(?:$|[^\pL\pN_])|жд[её]т(?:$|[^\pL\pN_])|руками|please(?:$|[^\pL\pN_])|let me know|your(?:$|[^\pL\pN_])|you(?:$|[^\pL\pN_])|confirm|decide)`)
	fence     = regexp.MustCompile("(?m)^ {0,3}(`{3,}|~{3,})")
	link      = regexp.MustCompile(`\[([^\]]*)\]\([^\s)]*(?:\s+"[^"]*")?\)`)
	reference = regexp.MustCompile(`(?m)^ {0,3}\[[^\]]+\]:\s*\S+.*$`)
	url       = regexp.MustCompile(`\b(?:[a-zA-Z][a-zA-Z0-9+.-]*://|www\.)[^\s<>]+`)
)

// Candidate keeps the cheap gate shared by Stop and the fallback scan. The
// tail limits count characters, so a non-ASCII report gets the same window.
func Candidate(text string) bool {
	r := []rune(text)
	return HasQuestion(text) || words.MatchString(string(r[max(0, len(r)-800):])) || fence.MatchString(string(r[max(0, len(r)-1200):]))
}

func HasQuestion(text string) bool {
	var prose []string
	marker := ""
	for _, line := range strings.Split(text, "\n") {
		m := fence.FindStringSubmatch(line)
		if marker != "" {
			if len(m) > 1 && m[1][0] == marker[0] && len(m[1]) >= len(marker) {
				marker = ""
			}
		} else if len(m) > 1 {
			marker = m[1]
		} else if !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "\t") {
			prose = append(prose, line)
		}
	}
	text = strings.Join(prose, "\n")
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '`' {
			out.WriteByte(text[i])
			i++
			continue
		}
		j := i
		for j < len(text) && text[j] == '`' {
			j++
		}
		delim := text[i:j]
		end := j
		for {
			n := strings.Index(text[end:], delim)
			if n < 0 {
				out.WriteString(delim)
				i = j
				break
			}
			end += n
			if (end == 0 || text[end-1] != '`') && (end+len(delim) == len(text) || text[end+len(delim)] != '`') {
				i = end + len(delim)
				break
			}
			end += len(delim)
		}
	}
	text = link.ReplaceAllString(out.String(), "$1")
	text = reference.ReplaceAllString(text, "")
	text = url.ReplaceAllString(text, "")
	return strings.Contains(text, "?")
}
