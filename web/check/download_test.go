package check

import (
	"os"
	"strings"
	"testing"
)

const viewerFile = "src/screens/chat/look.js"

func TestFileViewerSavesTheFileToThePhone(t *testing.T) {
	src := srcFiles(t)[viewerFile]
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", viewerFile)
	}
	clean := withoutComments(src)

	if !strings.Contains(clean, "/api/chat/file/download?") {
		t.Fatalf("%s: the viewer asks for the file nowhere but the viewing route — the only way "+
			"to get a file off the screen is the clipboard, and a picture does not fit in it", viewerFile)
	}
	if !strings.Contains(clean, "download=$") {
		t.Errorf("%s: the link has no download attribute — the browser opens the file in a tab "+
			"instead of putting it on the phone", viewerFile)
	}
	if !strings.Contains(clean, "download=${state.name || look.path}") {
		t.Errorf("%s: the saved file is not named by what the collector called it — it lands "+
			"under the name of the route", viewerFile)
	}

	head := strings.Index(clean, "/api/chat/file/download")
	if body := strings.Index(clean, `class="callbody"`); head > body {
		t.Errorf("%s: the save link sits in the body of the viewer instead of the header — "+
			"the body scrolls, and the link leaves together with the content", viewerFile)
	}
}

func TestSaveIsOfferedForEveryKindOfFile(t *testing.T) {
	src := srcFiles(t)[viewerFile]
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", viewerFile)
	}
	clean := withoutComments(src)

	if !strings.Contains(clean, `const save = state.kind === "ready" && file;`) {
		t.Errorf("%s: the save link is held by something other than an opened file — a file "+
			"whose bytes the screen cannot show is the one there is most reason to save",
			viewerFile)
	}
	block := jsUntil(t, viewerFile, clean, "${save && html`", "</a>")
	for _, mark := range []string{"form", "binary", "tooBig", "data"} {
		if strings.Contains(block, mark) {
			t.Errorf("%s: the save link looks at %q — an executable, a binary and a large "+
				"clip lose it, and those are exactly the files that fit in no other way out "+
				"of the panel", viewerFile, mark)
		}
	}
}

func TestAnOpenFileHasOneSaveControl(t *testing.T) {
	const bodyFile = "src/screens/chat/filebody.js"
	files := srcFiles(t)
	body := files[bodyFile]
	if body == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", bodyFile)
	}
	if strings.Contains(withoutComments(body), "download=") {
		t.Errorf("%s: the body of the viewer saves the file as well — two controls about "+
			"one thing stand next to each other, and the one in the body is there only for "+
			"the kinds the screen can draw", bodyFile)
	}
	if got := strings.Count(withoutComments(files[viewerFile]), "download=$"); got != 1 {
		t.Errorf("%s: %d save controls in the header of the viewer, expected one",
			viewerFile, got)
	}
}

func TestSaveLinkAndTheRouteAreTheSamePath(t *testing.T) {
	const route = "GET /api/chat/file/download"
	raw, err := os.ReadFile(repoPath("cmd/aacpanel/routes.go"))
	if err != nil {
		t.Fatalf("the routes of the service: %v", err)
	}
	if !strings.Contains(withoutComments(string(raw)), route) {
		t.Fatalf("cmd/aacpanel/routes.go has no %q — the link in the viewer points into "+
			"nowhere, and the tap ends with the page of the panel instead of the file", route)
	}

	raw, err = os.ReadFile(repoPath("cmd/aacpanel/chat.go"))
	if err != nil {
		t.Fatalf("the chat handlers: %v", err)
	}
	body := withoutComments(string(raw))
	if !strings.Contains(body, "func (s *Server) apiChatDownload(") {
		t.Fatal("cmd/aacpanel/chat.go has no apiChatDownload — the route leads to another handler")
	}
	fn := body[strings.Index(body, "func (s *Server) apiChatDownload("):]
	if end := strings.Index(fn, "\nfunc "); end > 0 {
		fn = fn[:end]
	}
	if !strings.Contains(fn, "s.chat.RawFile(") {
		t.Error("apiChatDownload takes the file from somewhere other than the collector — " +
			"the check that the path lies inside the directory of the conversation lives there, " +
			"and a second way to the disk is a way around it")
	}
	if !strings.Contains(fn, `Query().Get("path")`) {
		t.Error("apiChatDownload does not read the path the viewer sends")
	}
}
