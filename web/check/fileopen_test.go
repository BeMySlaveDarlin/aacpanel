package check

import (
	"os"
	"strings"
	"testing"
)

const bodyFile = "src/screens/chat/filebody.js"

func TestAPDFOnThePhoneOpensOutsideThePanel(t *testing.T) {
	src := srcFiles(t)[bodyFile]
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", bodyFile)
	}
	clean := withoutComments(src)

	if !strings.Contains(clean, `import { useWide } from "../../ui/wide.js";`) {
		t.Fatalf("%s tells the phone from the wide screen by something other than useWide — "+
			"the shells are told apart by width, and a second signal parts from the first silently",
			bodyFile)
	}
	paper := jsBlock(t, bodyFile, clean, "function Paper(")
	if !strings.Contains(paper, "useWide()") {
		t.Fatalf("%s: the PDF view does not ask the width — the phone gets the embedded document, "+
			"and its browser draws a plate whose \"open\" leads nowhere from inside the app", bodyFile)
	}

	phone := jsBlock(t, bodyFile, clean, "function PaperOpen(")
	for _, mark := range []string{"href=${open}", `target="_blank"`, `rel="noopener"`} {
		if !strings.Contains(phone, mark) {
			t.Errorf("%s: the phone's link to the PDF has no %s — a navigation of the browser's "+
				"own to the file is what hands the document to the viewer the phone has",
				bodyFile, mark)
		}
	}
	if strings.Contains(phone, "download") {
		t.Errorf("%s: the phone's link saves the file instead of showing it — the icon in the "+
			"header already does that", bodyFile)
	}
	if strings.Contains(phone, "useBytes(") || strings.Contains(phone, "createObjectURL") {
		t.Errorf("%s: the phone's link points into a blob — that is the address the browser's "+
			"\"open\" fails on from inside the installed app", bodyFile)
	}

	wide := jsBlock(t, bodyFile, clean, "function PaperFrame(")
	if !strings.Contains(wide, `class="filepdf"`) || !strings.Contains(wide, "useBytes(") {
		t.Errorf("%s: the wide screen lost the document embedded in the page", bodyFile)
	}
}

func TestTheOpenLinkAndTheSaveLinkAreOneRoute(t *testing.T) {
	src := srcFiles(t)[viewerFile]
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", viewerFile)
	}
	clean := withoutComments(src)

	open := jsBlock(t, viewerFile, clean, "function openURL(")
	if !strings.Contains(open, "saveURL(") {
		t.Errorf("%s: the address for showing the file is built apart from the one for saving "+
			"it — two routes are two checks of the path against the directory of the "+
			"conversation, and the service worker knows one of them", viewerFile)
	}
	if !strings.Contains(open, "inline=1") {
		t.Errorf("%s: the address for showing the file does not say so — the service answers "+
			"with a file to save, and the tap on \"open\" ends in the download shade", viewerFile)
	}
	if !strings.Contains(clean, "open=${openURL(base, look.path)}") {
		t.Errorf("%s: the body of the viewer is not given the address for showing the file", viewerFile)
	}

	raw, err := os.ReadFile(repoPath("cmd/aacpanel/chat.go"))
	if err != nil {
		t.Fatalf("the chat handlers: %v", err)
	}
	fn := funcBody(t, withoutComments(string(raw)), "func (s *Server) apiChatDownload(")
	if !strings.Contains(fn, `Query().Get("inline")`) {
		t.Error("apiChatDownload does not read the flag the viewer sends — the file the phone " +
			"opens comes back as one to save")
	}
	if !strings.Contains(fn, "shownInPlace(") {
		t.Error("apiChatDownload shows in place whatever is asked for — a page from a project " +
			"directory would run its script with the cookies of the panel")
	}
}

func TestWorkerLetsAFileShownInPlacePastIt(t *testing.T) {
	got := runWorker(t, []swWorld{{
		Name:     "a file being shown is not taken by the worker",
		Requests: []string{"/api/chat/file/download?session=warden&path=cv.pdf&inline=1"},
	}})
	if len(got) != 1 {
		t.Fatalf("%d runs came back, expected one", len(got))
	}
	if len(got[0].Asked) != 0 {
		t.Errorf("the request went to %v — the worker took a file on its way to the viewer, "+
			"and cuts it off after five seconds", got[0].Asked)
	}
	if strings.Join(got[0].Bodies, " ") != "past the worker" {
		t.Errorf("the page got %v, expected the answer past the worker", got[0].Bodies)
	}
}
