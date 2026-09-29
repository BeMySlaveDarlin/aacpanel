package check

import (
	"strings"
	"testing"
)

type feedPicturesShot struct {
	Feed struct {
		SentPictures    int    `json:"sentPictures"`
		SentLoaded      bool   `json:"sentLoaded"`
		SentWords       string `json:"sentWords"`
		StripOverBubble bool   `json:"stripOverBubble"`
		StripRight      *int   `json:"stripRight"`
		TypedPictures   int    `json:"typedPictures"`
		TypedWords      string `json:"typedWords"`
		AloneBubbles    int    `json:"aloneBubbles"`
		AlonePictures   int    `json:"alonePictures"`
		AloneStamp      string `json:"aloneStamp"`
	} `json:"feed"`
	Calls struct {
		Nodes []struct {
			Pic    bool `json:"pic"`
			Thumbs int  `json:"thumbs"`
		} `json:"nodes"`
		ThumbLoaded bool `json:"thumbLoaded"`
		ThumbInside bool `json:"thumbInside"`
	} `json:"calls"`
	View struct {
		Pictures int    `json:"pictures"`
		Loaded   bool   `json:"loaded"`
		After    string `json:"after"`
	} `json:"view"`
	Asked []string `json:"asked"`
}

// A picture the panel sent reaches the session as the path the host saved it
// under. The message shows the picture over its words, the way a pasted one
// is shown, and the line of the path leaves the words; a path the person
// typed to a picture anywhere else stays words. A message that was nothing
// but the picture is the picture and its time.
func TestAPictureThePanelSentIsDrawnOverItsMessage(t *testing.T) {
	var got feedPicturesShot
	runFixture(t, "feedpictures.html", &got)
	f := got.Feed

	if f.SentPictures != 1 || !f.SentLoaded {
		t.Errorf("the message with a sent picture shows %d pictures, loaded %v: the preview is missing",
			f.SentPictures, f.SentLoaded)
	}
	if f.SentWords != "the task is on the board" {
		t.Errorf("the message reads %q: the words stay, the path of the drawn picture goes", f.SentWords)
	}
	if !f.StripOverBubble || f.StripRight == nil || *f.StripRight != 0 {
		t.Errorf("the picture stands over its message: over %v, right edge off by %v", f.StripOverBubble, f.StripRight)
	}
	if f.TypedPictures != 0 || !strings.Contains(f.TypedWords, "/srv/proj/shop/docs/board.png") {
		t.Errorf("a typed path became %d pictures and reads %q: it is words", f.TypedPictures, f.TypedWords)
	}
	if f.AloneBubbles != 0 || f.AlonePictures != 1 || f.AloneStamp == "" {
		t.Errorf("a message of a picture alone draws %d bubbles, %d pictures and the time %q: an empty bubble "+
			"under the picture says nothing", f.AloneBubbles, f.AlonePictures, f.AloneStamp)
	}
	sent := 0
	for _, u := range got.Asked {
		if strings.Contains(u, "&upload=20260920-100000-ab12cd-board.png") {
			sent++
		}
	}
	if sent == 0 {
		t.Errorf("the sent picture was never asked for by its name: %v", got.Asked)
	}
}

// A picture a call returned is on the row of the call in its sheet, and in
// full where the call opens, under what came out.
func TestAPictureACallReturnedIsOnItsCall(t *testing.T) {
	var got feedPicturesShot
	runFixture(t, "feedpictures.html", &got)
	c := got.Calls

	if len(c.Nodes) != 2 || !c.Nodes[0].Pic || c.Nodes[0].Thumbs != 1 || c.Nodes[1].Pic || c.Nodes[1].Thumbs != 0 {
		t.Fatalf("the rows of the sheet are %+v: the read of the picture shows it, the read of the code does not", c.Nodes)
	}
	if !c.ThumbLoaded || !c.ThumbInside {
		t.Errorf("the picture on the row: loaded %v, inside the row %v", c.ThumbLoaded, c.ThumbInside)
	}
	if got.View.Pictures != 1 || !got.View.Loaded {
		t.Errorf("the opened call shows %d pictures, loaded %v", got.View.Pictures, got.View.Loaded)
	}
	if got.View.After != "what came out" {
		t.Errorf("the picture stands after %q: it is what the call returned", got.View.After)
	}
	placed := false
	for _, u := range got.Asked {
		if strings.Contains(u, "&pos=44&i=0&part=0") {
			placed = true
		}
	}
	if !placed {
		t.Errorf("the picture was not asked for by the place of the result: %v", got.Asked)
	}
}
