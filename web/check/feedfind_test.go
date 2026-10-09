package check

import (
	"strings"
	"testing"
)

type findMark struct {
	Text   string `json:"text"`
	Pos    int    `json:"pos"`
	Role   string `json:"role"`
	Nth    int    `json:"nth"`
	Offset int    `json:"offset"`
}

type findMatchRow struct {
	Pos  int    `json:"pos"`
	Role string `json:"role"`
	Nth  int    `json:"nth"`
}

type findMarks struct {
	All int        `json:"all"`
	Now []findMark `json:"now"`
}

type findStep struct {
	Count    string         `json:"count"`
	Now      []findMark     `json:"now"`
	All      int            `json:"all"`
	Shown    bool           `json:"shown"`
	Outlined []findMatchRow `json:"outlined"`
}

type findClose struct {
	Bar   bool      `json:"bar"`
	Marks findMarks `json:"marks"`
	Away  bool      `json:"away"`
	Chat  int       `json:"chat"`
}

type feedFind struct {
	Wide    bool `json:"wide"`
	TailEnd bool `json:"tailEnd"`
	Opened  struct {
		Item      bool `json:"item"`
		Prevented bool `json:"prevented"`
		Bar       bool `json:"bar"`
		SheetGone bool `json:"sheetGone"`
		Focused   bool `json:"focused"`
	} `json:"opened"`
	EarlyAsks int `json:"earlyAsks"`
	Asks      []struct {
		Q       string `json:"q"`
		Session string `json:"session"`
		ID      string `json:"id"`
	} `json:"asks"`
	AskDelay int      `json:"askDelay"`
	First    findStep `json:"first"`
	Hidden   findStep `json:"hidden"`
	Second   findStep `json:"second"`
	Third    findStep `json:"third"`
	Fourth   findStep `json:"fourth"`
	Back     findStep `json:"back"`
	Far      struct {
		Count    string     `json:"count"`
		Now      []findMark `json:"now"`
		Pieces   []string   `json:"pieces"`
		Row      bool       `json:"row"`
		Shown    bool       `json:"shown"`
		TailGone bool       `json:"tailGone"`
		Way      string     `json:"way"`
	} `json:"far"`
	Held struct {
		Drawn bool `json:"drawn"`
	} `json:"held"`
	FirstClose findClose `json:"firstClose"`
	Home       struct {
		Last bool `json:"last"`
		Tail bool `json:"tail"`
		Far  bool `json:"far"`
		End  bool `json:"end"`
		Away bool `json:"away"`
	} `json:"home"`
	Keys struct {
		Prevented bool `json:"prevented"`
		Bar       bool `json:"bar"`
		Focused   bool `json:"focused"`
		Selected  bool `json:"selected"`
	} `json:"keys"`
	Stale struct {
		Count string    `json:"count"`
		Asks  []string  `json:"asks"`
		Marks findMarks `json:"marks"`
	} `json:"stale"`
	Again       findStep  `json:"again"`
	SecondClose findClose `json:"secondClose"`
	ChatClosed  []string  `json:"chatClosed"`
	Error       string    `json:"error"`
}

const findID = "0b6c1a2e-3d4f-4a5b-8c6d-7e8f9a0b1c2d"

// TestFeedFind runs the search of a conversation in the real screen of a
// phone and of a desk: the conversation is longer than the feed has loaded,
// the host answers by the contract, and the fixture walks the matches newest
// first, steps off the loaded rows and comes back to the end.
func TestFeedFind(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{
		{"phone", runFixture},
		{"desk", runWideFixture},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got feedFind
			screen.run(t, "feedfind.html", &got)
			if got.Error != "" {
				t.Fatalf("the fixture broke: %s", got.Error)
			}
			if !got.TailEnd {
				t.Fatal("the feed did not open at its end — the fixture does not open the conversation as the screen does")
			}
			for _, part := range []struct {
				name  string
				check func(*testing.T, feedFind)
			}{
				{"theBarOpens", findBarOpens},
				{"oneQuestionAfterTheLastKey", findAsksOnce},
				{"theNewestMatchComesFirst", findNewestFirst},
				{"enterGoesOlderShiftEnterNewer", findWalks},
				{"aMatchOffTheLoadedRowsBringsItsWindow", findLoadsTheWindow},
				{"theStreamWaitsWhileTheFeedIsAway", findHoldsTheStream},
				{"closingTakesTheMarksOff", findClosingDropsMarks},
				{"backToTheEndPutsTheTailBack", findBackToTheEnd},
				{"ctrlFIsTakenFromTheBrowser", findTakesCtrlF},
				{"aSlowAnswerToAnOldQuestionLoses", findDropsStaleAnswers},
				{"theGestureStillClosesTheConversation", findLeavesTheConversationClosable},
			} {
				t.Run(part.name, func(t *testing.T) { part.check(t, got) })
			}
		})
	}
}

func findBarOpens(t *testing.T, got feedFind) {
	o := got.Opened
	if !got.Wide {
		if !o.Item {
			t.Fatal("the tools of the session have no \"Find in the conversation\" — on a phone there is no Ctrl+F to open the bar with")
		}
		if !o.SheetGone {
			t.Error("the tools stay open over the bar they opened")
		}
	} else if !o.Prevented {
		t.Error("Ctrl+F at a desk is left to the browser: its find sees only the rows loaded")
	}
	if !o.Bar {
		t.Fatal("the bar did not open")
	}
	if !o.Focused {
		t.Error("the bar opened without its field in hand: the words go nowhere")
	}
}

func findAsksOnce(t *testing.T, got feedFind) {
	if got.EarlyAsks != 0 {
		t.Errorf("%d searches went out before the wait after the last key was over — a question typed letter by letter asks once a letter", got.EarlyAsks)
	}
	if len(got.Asks) != 1 {
		t.Fatalf("typing one question asked the host %d times: %+v", len(got.Asks), got.Asks)
	}
	a := got.Asks[0]
	if a.Q != "kestrel" || a.Session != "helios" || a.ID != findID {
		t.Errorf("the search asked %+v, not the words of the field in this conversation", a)
	}
	if got.AskDelay < 230 || got.AskDelay > 1500 {
		t.Errorf("the search went out %d ms after the last key, not after the wait of 250", got.AskDelay)
	}
}

// onlyMark checks the current mark: one range, over the words of the match,
// in the row of this position and role (and number nth), at this offset of
// its text node unless the offset is -1.
func onlyMark(t *testing.T, step string, now []findMark, pos int, role string, offset int) {
	t.Helper()
	onlyMarkNth(t, step, now, pos, role, 0, offset)
}

func onlyMarkNth(t *testing.T, step string, now []findMark, pos int, role string, nth, offset int) {
	t.Helper()
	if len(now) != 1 {
		t.Errorf("%s: %d current marks, not one: %+v", step, len(now), now)
		return
	}
	m := now[0]
	if strings.ToLower(m.Text) != "kestrel" || m.Pos != pos || m.Role != role || m.Nth != nth ||
		(offset >= 0 && m.Offset != offset) {
		t.Errorf("%s: the current mark is %+v, expected the words of the match in the %s row %d of %d", step, m, role, nth, pos)
	}
}

// The newest record holds two letters and the answer beside them, the letters
// drawn first and found last: a row is told by its role and its number in the
// record as well as its position, and the hits of one item go from the last.
func findNewestFirst(t *testing.T, got feedFind) {
	if got.First.Count != "1 of 7" {
		t.Errorf("the bar says %q over seven matches, not \"1 of 7\"", got.First.Count)
	}
	onlyMarkNth(t, "the first match", got.First.Now, 1036, "mail", 1, -1)
	if got.First.All < 3 {
		t.Errorf("%d other matches marked in the tail, which shows the two hits of the answer and one more message", got.First.All)
	}
	if !got.First.Shown {
		t.Error("the row of the first match is not in view")
	}
}

func findWalks(t *testing.T, got feedFind) {
	h := got.Hidden
	if h.Count != "2 of 7" {
		t.Errorf("Enter went to %q, not \"2 of 7\"", h.Count)
	}
	if len(h.Now) != 0 {
		t.Errorf("the match in a paragraph the closed letter does not draw was marked in %+v — the words of another row "+
			"were taken for it", h.Now)
	}
	if len(h.Outlined) != 1 || h.Outlined[0] != (findMatchRow{Pos: 1036, Role: "mail", Nth: 0}) {
		t.Errorf("the rows outlined for a match whose words are not drawn: %+v, expected the first letter of the record", h.Outlined)
	}
	if got.Second.Count != "3 of 7" {
		t.Errorf("a second Enter went to %q, not \"3 of 7\"", got.Second.Count)
	}
	onlyMark(t, "the second Enter", got.Second.Now, 1036, "ai", -1)
	if len(got.Second.Now) == 1 && got.Second.Now[0].Offset == 0 {
		t.Error("the walk came to the first hit of the answer before its last — the hits of one item are not walked " +
			"from the end, or the letters beside it were counted among them")
	}
	if len(got.Second.Outlined) != 0 {
		t.Errorf("the outline of the previous match stays: %+v", got.Second.Outlined)
	}
	if got.Third.Count != "4 of 7" {
		t.Errorf("a third Enter went to %q, not \"4 of 7\"", got.Third.Count)
	}
	onlyMark(t, "the third Enter", got.Third.Now, 1036, "ai", 0)
	if got.Fourth.Count != "5 of 7" {
		t.Errorf("a fourth Enter went to %q, not \"5 of 7\"", got.Fourth.Count)
	}
	onlyMark(t, "the fourth Enter", got.Fourth.Now, 1030, "me", -1)
	if got.Back.Count != "4 of 7" {
		t.Errorf("Shift+Enter went to %q, not back to \"4 of 7\"", got.Back.Count)
	}
	onlyMark(t, "Shift+Enter", got.Back.Now, 1036, "ai", 0)
}

func findLoadsTheWindow(t *testing.T, got feedFind) {
	f := got.Far
	if f.Count != "6 of 7" {
		t.Errorf("the walk stands at %q, not \"6 of 7\"", f.Count)
	}
	asked := strings.Join(f.Pieces, " ")
	if !strings.Contains(asked, "before=201&limit=20") || !strings.Contains(asked, "after=200&limit=20") {
		t.Errorf("the feed asked for %v, not a window around the position of the match", f.Pieces)
	}
	if !f.Row {
		t.Fatal("the row of a match off the loaded rows never came — the feed did not read the window around it")
	}
	if !f.Shown {
		t.Error("the row of the match is drawn but not in view")
	}
	onlyMark(t, "the match off the loaded rows", f.Now, 200, "ai", -1)
	if !f.TailGone {
		t.Error("the window was sewn onto the tail instead of taking its place")
	}
	if f.Way != "Back to the end" {
		t.Errorf("the way back reads %q, not \"Back to the end\"", f.Way)
	}
}

func findHoldsTheStream(t *testing.T, got feedFind) {
	if got.Held.Drawn {
		t.Error("a message of the stream was drawn under a window away from the end — under rows it does not follow")
	}
}

func findClosingDropsMarks(t *testing.T, got feedFind) {
	first, second := "the gesture", "Esc"
	if got.Wide {
		first, second = second, first
	}
	for _, c := range []struct {
		how string
		got findClose
	}{{first, got.FirstClose}, {second, got.SecondClose}} {
		if c.got.Bar {
			t.Errorf("%s did not close the bar", c.how)
		}
		if c.got.Marks.All != 0 || len(c.got.Marks.Now) != 0 {
			t.Errorf("%s closed the bar and left the marks: %+v", c.how, c.got.Marks)
		}
		if c.got.Chat != 0 {
			t.Errorf("%s closed the conversation along with the bar", c.how)
		}
	}
	if !got.FirstClose.Away {
		t.Error("closing the bar took the feed off the window it stood on")
	}
	if got.Again.Count != "1 of 7" || len(got.Again.Now) != 1 {
		t.Errorf("before the second close the search reads %q with %+v — there was nothing to take off", got.Again.Count, got.Again.Now)
	}
}

func findBackToTheEnd(t *testing.T, got feedFind) {
	h := got.Home
	if !h.Tail || h.Far {
		t.Errorf("back to the end shows the tail %v and the window %v", h.Tail, h.Far)
	}
	if !h.Last {
		t.Error("the message the stream brought while the feed was away is not in the tail it came back to")
	}
	if !h.End {
		t.Error("back to the end left the feed short of its end")
	}
	if h.Away {
		t.Error("the way back is still offered at the end")
	}
}

func findTakesCtrlF(t *testing.T, got feedFind) {
	k := got.Keys
	if !k.Prevented {
		t.Error("Ctrl+F is left to the browser")
	}
	if !k.Bar || !k.Focused {
		t.Errorf("Ctrl+F: the bar %v, its field in hand %v", k.Bar, k.Focused)
	}
	if !k.Selected {
		t.Error("the bar opened again without the last words selected")
	}
}

func findDropsStaleAnswers(t *testing.T, got feedFind) {
	s := got.Stale
	if len(s.Asks) == 0 || s.Asks[0] != "slowword" || s.Asks[len(s.Asks)-1] != "nomatch" {
		t.Fatalf("the fixture asked %v — the slow question did not go out before the new one", s.Asks)
	}
	if s.Count != "No matches" {
		t.Errorf("after a slow answer to an old question the bar reads %q, not the answer to the words in the field", s.Count)
	}
	if s.Marks.All != 0 || len(s.Marks.Now) != 0 {
		t.Errorf("the marks of an old question stand over the feed: %+v", s.Marks)
	}
}

func findLeavesTheConversationClosable(t *testing.T, got feedFind) {
	if len(got.ChatClosed) != 1 || got.ChatClosed[0] != "chat" {
		t.Errorf("the gesture after the bar closed %v, not the conversation — the bar took its entry of the history", got.ChatClosed)
	}
}
