package check

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type handedCard struct {
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Size   string `json:"size"`
	Button bool   `json:"button"`
}

type feedHandedShot struct {
	Feed struct {
		CaptionWords      string       `json:"captionWords"`
		CaptionCards      []handedCard `json:"captionCards"`
		CaptionUnder      bool         `json:"captionUnder"`
		CaptionRowWords   string       `json:"captionRowWords"`
		AloneBubbles      int          `json:"aloneBubbles"`
		AloneCards        []handedCard `json:"aloneCards"`
		AloneStamp        string       `json:"aloneStamp"`
		AloneRowWords     string       `json:"aloneRowWords"`
		PicturePictures   int          `json:"picturePictures"`
		PictureOverBubble bool         `json:"pictureOverBubble"`
		PictureWords      string       `json:"pictureWords"`
		PictureCards      []handedCard `json:"pictureCards"`
		PictureUnder      bool         `json:"pictureUnder"`
		TypedCards        int          `json:"typedCards"`
		TypedWords        string       `json:"typedWords"`
	} `json:"feed"`
	Look struct {
		Opened []string `json:"opened"`
		Title  string   `json:"title"`
		Body   string   `json:"body"`
	} `json:"look"`
	Asked []string `json:"asked"`
}

const handedDir = "/home/dev/.local/share/aacpanel-exec/files"

// A file the panel sent with a message reaches the session as the path the
// host saved it under. The message shows its words alone, and the file stands
// under them as a file: the name it went under, its size and its type, the
// way the files a session delivers stand. The path is nowhere in the row.
func TestAFileThePanelSentStandsUnderItsMessageAsAFile(t *testing.T) {
	var got feedHandedShot
	runFixture(t, "feedhanded.html", &got)
	f := got.Feed

	if f.CaptionWords != "the measurements from the boss" {
		t.Errorf("the message reads %q: the words stay, the path of the file goes", f.CaptionWords)
	}
	want := []handedCard{{Name: "diag.txt", Tag: "TXT", Size: "98 KB", Button: true}}
	if !reflect.DeepEqual(f.CaptionCards, want) {
		t.Errorf("the file of the message is %+v, want %+v", f.CaptionCards, want)
	}
	if !f.CaptionUnder {
		t.Error("the file does not stand under its message, on its side of the feed")
	}
	if strings.Contains(f.CaptionRowWords, handedDir) || strings.Contains(f.CaptionRowWords, "ec828d") {
		t.Errorf("the row reads %q: the path of the file is shown as words", f.CaptionRowWords)
	}

	want = []handedCard{{Name: "report.pdf", Tag: "PDF", Size: "2.0 KB", Button: true},
		{Name: "logs.tar.gz", Tag: "GZ", Size: "5.0 MB", Button: true}}
	if f.AloneBubbles != 0 || !reflect.DeepEqual(f.AloneCards, want) || f.AloneStamp == "" {
		t.Errorf("a message of files alone draws %d bubbles, the files %+v and the time %q: an empty bubble "+
			"over the files says nothing", f.AloneBubbles, f.AloneCards, f.AloneStamp)
	}
	if strings.Contains(f.AloneRowWords, handedDir) {
		t.Errorf("the row of files alone reads %q: a path is shown as words", f.AloneRowWords)
	}

	if f.TypedCards != 0 || !strings.Contains(f.TypedWords, "/srv/proj/shop/docs/diag.txt") {
		t.Errorf("a path the person typed became %d files and reads %q: it is words", f.TypedCards, f.TypedWords)
	}
}

// A picture sent with a file is drawn over the message as before, and the
// file stands under it.
func TestAPictureSentWithAFileIsStillDrawnOverItsMessage(t *testing.T) {
	var got feedHandedShot
	runFixture(t, "feedhanded.html", &got)
	f := got.Feed

	if f.PicturePictures != 1 || !f.PictureOverBubble {
		t.Errorf("the message shows %d pictures, over it %v: the picture stands over its words",
			f.PicturePictures, f.PictureOverBubble)
	}
	if f.PictureWords != "and the board" {
		t.Errorf("the message reads %q: the paths of the picture and the file both go", f.PictureWords)
	}
	want := []handedCard{{Name: "notes.zip", Tag: "ZIP", Size: "900 B", Button: true}}
	if !reflect.DeepEqual(f.PictureCards, want) || !f.PictureUnder {
		t.Errorf("the file beside the picture is %+v, under the message %v", f.PictureCards, f.PictureUnder)
	}
}

// A tap on a sent file opens it the way any file of the feed opens: in the
// sheet of the conversation, asked for by the path it came with, under the
// name it went under.
func TestATapOpensAFileThePanelSent(t *testing.T) {
	var got feedHandedShot
	runFixture(t, "feedhanded.html", &got)
	l := got.Look

	path := handedDir + "/20261003-175757-ec828d-diag.txt"
	if !reflect.DeepEqual(l.Opened, []string{path}) {
		t.Fatalf("the tap opened %v, want the file by its path %s", l.Opened, path)
	}
	if l.Title != "diag.txt" || !strings.Contains(l.Body, "cpu 12%") {
		t.Errorf("the sheet is titled %q and reads %q: the file is not shown", l.Title, l.Body)
	}
	asked := false
	for _, u := range got.Asked {
		if strings.HasPrefix(u, "/api/chat/file?") && strings.Contains(u, "&path="+url.QueryEscape(path)) {
			asked = true
		}
	}
	if !asked {
		t.Errorf("the file was not asked for by its path: %v", got.Asked)
	}
}
