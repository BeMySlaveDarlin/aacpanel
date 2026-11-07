package taskreg

import (
	"regexp"
	"slices"
	"strings"
)

var issueNumber = regexp.MustCompile(`\b(?:[A-Z][A-Z0-9]*-[0-9]+|(?i:SER|LMS)[_-]?[0-9]+)\b`)
var compactIssue = regexp.MustCompile(`(?i)^(SER|LMS)[_-]?([0-9]+)$`)

// IssueNumbers recognizes explicit tracker keys and compact session names.
// Bodies are not passed here: mentioning a related issue does not bind a task.
func IssueNumbers(texts ...string) []string {
	out := []string{}
	for _, text := range texts {
		for _, key := range issueNumber.FindAllString(text, -1) {
			if parts := compactIssue.FindStringSubmatch(key); parts != nil {
				key = strings.ToUpper(parts[1]) + "-" + parts[2]
			}
			out = append(out, key)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
