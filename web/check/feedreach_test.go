package check

import (
	"os"
	"strings"
	"testing"
)

type reachShot struct {
	Wide  bool `json:"wide"`
	First struct {
		Bands   int    `json:"bands"`
		Full    int    `json:"full"`
		Waiting bool   `json:"waiting"`
		Now     string `json:"now"`
		Stubs   int    `json:"stubs"`
	} `json:"first"`
	Unfolded struct {
		Feed    string `json:"feed"`
		Stubs   int    `json:"stubs"`
		Shots   int    `json:"shots"`
		Command int    `json:"command"`
		Turns   int    `json:"turns"`
		Link    string `json:"link"`
	} `json:"unfolded"`
	Work []struct {
		Text     string       `json:"text"`
		Sections int          `json:"sections"`
		Badges   []badgeShot2 `json:"badges"`
	} `json:"work"`
	Summed  []badgeShot2 `json:"summed"`
	Missing []string     `json:"missing"`
	Marks   int          `json:"marks"`
}

type badgeShot2 struct {
	Badge  string `json:"badge"`
	Title  string `json:"title"`
	Failed bool   `json:"failed"`
	Calls  int    `json:"calls"`
}

// The work of an exchange — what was thought and said on the way, the lines
// that came beside it — is not in the feed: on a phone it is behind the line
// the work sits in, on a desk it stands beside the feed.
var workOnly = []string{"MARK-mind-13", "MARK-ai-16", "MARK-taskdone-17", "MARK-notice-15", "MARK-mailout-23", "MARK-mind-after-report"}

func checkReach(t *testing.T, got reachShot) {
	t.Helper()
	if got.Marks < 40 {
		t.Fatalf("the fixture found %d marks in the feed of every role: it is looking at the wrong thing", got.Marks)
	}
	for _, m := range got.Missing {
		if m == "MARK-link-20" && strings.Contains(got.Unfolded.Link, m) {
			continue
		}
		t.Errorf("%s is neither on the screen nor one tap away", m)
	}
	if got.First.Bands != 10 || got.First.Full != 1 {
		t.Errorf("the feed opened with %d bands and %d exchanges in full, expected every exchange but the one going on as a band: "+
			"a message waiting to be read counts as the exchange after it", got.First.Bands, got.First.Full)
	}
	if !got.First.Waiting {
		t.Error("the message in the queue is not drawn after the exchange going on")
	}
	if !strings.Contains(got.First.Now, "MARK-call-102") || !strings.Contains(got.First.Now, "running") {
		t.Errorf("the card of now says %q, expected the call the host says is out, running", got.First.Now)
	}
	for _, m := range workOnly {
		if strings.Contains(got.Unfolded.Feed, m) {
			t.Errorf("%s, said in the course of the work, stands in the feed itself", m)
		}
	}
	if got.Unfolded.Shots != 1 || got.Unfolded.Command != 1 || got.Unfolded.Turns != 3 {
		t.Errorf("unfolded, the feed draws %d pictures, %d command cards and %d ends of a turn, expected 1, 1 and 3",
			got.Unfolded.Shots, got.Unfolded.Command, got.Unfolded.Turns)
	}
	failed := false
	for _, w := range got.Work {
		for _, b := range w.Badges {
			if b.Calls == 0 && !strings.HasPrefix(b.Badge, "thinking") {
				t.Errorf("the badge %q opened an empty list of calls", b.Badge)
			}
			if b.Failed {
				failed = true
				if !strings.Contains(b.Title, "1 failed") {
					t.Errorf("a badge with a failed call says %q, expected the number of failed calls", b.Title)
				}
			}
		}
	}
	if !failed {
		t.Error("the call the host reported as failed marks no badge")
	}
}

func TestEveryRoleIsOnThePhoneOrATapAway(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got reachShot
	runFixture(t, "feedreach.html", &got)
	checkReach(t, got)
	if got.First.Stubs != 0 {
		t.Errorf("before anything is unfolded the feed draws %d lines of work: the one exchange in full has only the step going on now", got.First.Stubs)
	}
	if got.Unfolded.Stubs != 4 {
		t.Errorf("unfolded, %d exchanges show the line their work sits in, expected the four settled ones with work", got.Unfolded.Stubs)
	}
	// Every stub and the card of now opened a layer; one of them lists the
	// failed call, another the thought said after the report.
	all := ""
	for _, w := range got.Work {
		all += w.Text
	}
	for _, m := range workOnly {
		if !strings.Contains(all, m) {
			t.Errorf("%s is in no layer of work", m)
		}
	}
	if len(got.Summed) == 0 {
		t.Error("the lines of work in the feed carry no badges summing their calls")
	}
}

func TestEveryRoleIsOnTheDeskOrATapAway(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got reachShot
	runWideFixture(t, "feedreach.html", &got)
	if !got.Wide {
		t.Fatal("the desk fixture ran on a phone screen")
	}
	checkReach(t, got)
	if got.First.Stubs != 0 || got.Unfolded.Stubs != 0 {
		t.Errorf("the feed on a desk draws lines of work (%d, %d unfolded): the work stands beside it", got.First.Stubs, got.Unfolded.Stubs)
	}
	if len(got.Work) != 1 || got.Work[0].Sections != 5 {
		t.Fatalf("the process beside the feed has %+v — expected a section for every exchange with work once all are unfolded", got.Work)
	}
	for _, m := range workOnly {
		if !strings.Contains(got.Work[0].Text, m) {
			t.Errorf("%s is not in the process beside the feed", m)
		}
	}
}
