package check

import (
	"os"
	"strings"
	"testing"
)

type storyShot struct {
	Wide       bool     `json:"wide"`
	Bands      int      `json:"bands"`
	Full       int      `json:"full"`
	Stubs      []string `json:"stubs"`
	Cut        bool     `json:"cut"`
	ReadAll    string   `json:"readAll"`
	Changed    string   `json:"changed"`
	Now        string   `json:"now"`
	NowStep    string   `json:"nowStep"`
	NowColumns string   `json:"nowColumns"`
	FeedWork   bool     `json:"feedWork"`

	SummedCalls    int `json:"summedCalls"`
	SummedThinking int `json:"summedThinking"`
	Layer          *struct {
		Head       string `json:"head"`
		Entries    int    `json:"entries"`
		Earlier    bool   `json:"earlier"`
		Later      bool   `json:"later"`
		StepCalls  int    `json:"stepCalls"`
		StepBadge  string `json:"stepBadge"`
		Next       string `json:"next"`
		Running    string `json:"running"`
		BackCloses bool   `json:"backCloses"`
	} `json:"layer"`
	Unfolded int  `json:"unfolded"`
	Whole    bool `json:"whole"`

	Side *struct {
		Sections    int       `json:"sections"`
		Live        int       `json:"live"`
		Running     string    `json:"running"`
		AtEnd       bool      `json:"atEnd"`
		Text        string    `json:"text"`
		AfterUnfold int       `json:"afterUnfold"`
		Scroll      []float64 `json:"scroll"`
	} `json:"side"`

	Idle struct {
		Out string `json:"out"`
		Now int    `json:"now"`
	} `json:"idle"`
}

func checkStory(t *testing.T, got storyShot) {
	t.Helper()
	if got.Bands != 2 || got.Full != 2 {
		t.Errorf("the feed draws %d bands and %d exchanges in full, expected the two far ones pressed and the two near ones whole",
			got.Bands, got.Full)
	}
	if !got.Cut || got.ReadAll != "read the whole report, 17 lines" {
		t.Errorf("the exchange before the last shows its report cut %v, under it %q — expected its head and a way to the whole of it",
			got.Cut, got.ReadAll)
	}
	if !strings.Contains(got.Changed, "desklimits.html") {
		t.Errorf("the exchange before the last lists the files %q, expected the one its replies named", got.Changed)
	}
	for _, want := range []string{"now", "Bash", "running", "0:11"} {
		if !strings.Contains(got.Now, want) {
			t.Errorf("the card of now says %q, without %q", got.Now, want)
		}
	}
	if got.FeedWork {
		t.Error("a reply said on the way stands in the feed: the work of an exchange is kept out of it")
	}
	if got.Idle.Now != 0 || !strings.Contains(got.Idle.Out, "Taking the next one from the queue") {
		t.Errorf("idle, the card of now is drawn %d times and the last exchange ends on %q — expected no card and the last thing said",
			got.Idle.Now, got.Idle.Out)
	}
}

func TestTheFeedOfALiveConversationOnAPhone(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got storyShot
	runFixture(t, "feedstory.html", &got)
	checkStory(t, got)
	if len(got.Stubs) != 2 || !strings.HasPrefix(got.Stubs[0], "6 min of work") || !strings.Contains(got.Stubs[0], "4 thoughts, 5 said") ||
		!strings.HasPrefix(got.Stubs[1], "thought before going on") {
		t.Errorf("the lines of work read %q", got.Stubs)
	}
	if got.SummedCalls != 21 || got.SummedThinking == 0 {
		t.Errorf("the summed badge of six minutes of work lists %d calls and %d thinking blocks, expected all 21 calls of its runs and their thinking",
			got.SummedCalls, got.SummedThinking)
	}
	if got.NowColumns == "side by side" {
		t.Error("the card of now lays its call and its words side by side on a phone")
	}
	l := got.Layer
	if l == nil {
		t.Fatal("the line of work opened no layer")
	}
	if !strings.HasPrefix(l.Head, "Work") || !strings.Contains(l.Head, "after your answer") || l.Entries != 10 {
		t.Errorf("the layer is headed %q with %d entries, expected the work after the answer, ten steps", l.Head, l.Entries)
	}
	if !l.Earlier || !l.Later {
		t.Errorf("the layer leads to earlier work %v and to later work %v, expected both", l.Earlier, l.Later)
	}
	if l.StepCalls < 1 || l.StepCalls >= got.SummedCalls {
		t.Errorf("a badge of one step (%s) lists %d calls — expected the calls of that step alone", l.StepBadge, l.StepCalls)
	}
	if !strings.Contains(l.Next, "after go") || !strings.Contains(l.Running, "Bash") || !strings.Contains(l.Running, "0:11") {
		t.Errorf("the later work is %q running %q, expected the exchange going on now and its call", l.Next, l.Running)
	}
	if !l.BackCloses {
		t.Error("the back gesture does not put the layer down")
	}
	if got.Unfolded != got.Bands-1 || !got.Whole {
		t.Errorf("a tap unfolded the band %v and read the report whole %v", got.Unfolded == got.Bands-1, got.Whole)
	}
}

func TestTheFeedOfALiveConversationOnADesk(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got storyShot
	runWideFixture(t, "feedstory.html", &got)
	if !got.Wide {
		t.Fatal("the desk fixture ran on a phone screen")
	}
	checkStory(t, got)
	if len(got.Stubs) != 0 {
		t.Errorf("the feed on a desk draws lines of work %q: the work stands beside it", got.Stubs)
	}
	if got.NowColumns != "side by side" || !strings.Contains(got.NowStep, "this step") {
		t.Errorf("the card of now on a desk lays out %q with %q, expected the call beside the words and the badges of the step",
			got.NowColumns, got.NowStep)
	}
	s := got.Side
	if s == nil {
		t.Fatal("no process beside the feed")
	}
	if s.Sections != 2 || s.Live != 1 || !strings.Contains(s.Running, "Bash") || !strings.Contains(s.Running, "0:11") {
		t.Errorf("the process has %d sections, %d live, running %q — expected the two exchanges in full, the last going on with its call",
			s.Sections, s.Live, s.Running)
	}
	if !s.AtEnd {
		t.Errorf("the process does not stand at its end, where the work going on is: %v", s.Scroll)
	}
	if !strings.Contains(s.Text, "Doing B. First closing") {
		t.Error("a reply said on the way is not in the process")
	}
	if s.AfterUnfold != 3 {
		t.Errorf("an unfolded band brought %d sections into the process, expected its own beside the two", s.AfterUnfold)
	}
}
