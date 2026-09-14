package check

import (
	"regexp"
	"strings"
	"testing"
)

// A session name belongs to the directory it was opened in, and two contours may
// hold a project of the same name. Resuming by name alone takes whichever of
// them spoke last, so the person presses on one conversation in the archive and
// a different one comes up, in somebody else's tree. Every screen with a resume
// button has the identifier of the row it drew, and has to hand it over.
func TestResumingNamesTheConversationAndNotOnlyTheSession(t *testing.T) {
	files := srcFiles(t)
	call := regexp.MustCompile(`run\("session\.resume",[^)]*\)`)

	seen := 0
	for _, path := range sortedKeys(files) {
		for _, found := range call.FindAllString(withoutComments(files[path]), -1) {
			seen++
			if !strings.Contains(found, "session:") {
				t.Errorf("%s resumes without saying which conversation: %s — the name alone resumes whichever "+
					"project of that name spoke last, which need not be the one on the screen", path, found)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no screen resumes a conversation at all — the test guards an empty place")
	}
}
