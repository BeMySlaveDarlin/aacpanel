package check

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

type slashRow struct {
	Label  string `json:"label"`
	Hint   string `json:"hint"`
	Off    bool   `json:"off"`
	Screen bool   `json:"screen"`
}

type sideAsk struct {
	Name     string `json:"name"`
	Question string `json:"question"`
	History  []struct {
		Question string `json:"question"`
		Response string `json:"response"`
	} `json:"history"`
}

type slashBtwShot struct {
	Theirs             bool       `json:"theirs"`
	Rows               []slashRow `json:"rows"`
	HintUnder          bool       `json:"hintUnder"`
	HintCut            bool       `json:"hintCut"`
	HintInside         bool       `json:"hintInside"`
	Filtered           []string   `json:"filtered"`
	Refused            string     `json:"refused"`
	RefusedHint        string     `json:"refusedHint"`
	AfterRefused       string     `json:"afterRefused"`
	Lists              int        `json:"lists"`
	McpTag             bool       `json:"mcpTag"`
	Picked             string     `json:"picked"`
	Screen             string     `json:"screen"`
	ActionsAfterScreen int        `json:"actionsAfterScreen"`
	Waiting            []string   `json:"waiting"`
	SideOpen           string     `json:"sideOpen"`
	Questions          []string   `json:"questions"`
	Answers            []string   `json:"answers"`
	AnswerBold         bool       `json:"answerBold"`
	ComposerAfter      string     `json:"composerAfter"`
	Answers2           []string   `json:"answers2"`
	Errors             []string   `json:"errors"`
	Side               []sideAsk  `json:"side"`
	ActionsAfterSide   []string   `json:"actionsAfterSide"`
	AfterBin           int        `json:"afterBin"`
	BinEmpty           int        `json:"binEmpty"`
	TermTheirs         bool       `json:"termTheirs"`
	TermRows           []string   `json:"termRows"`
	TermLists          int        `json:"termLists"`
	TermSent           struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"termSent"`
	TermSide int `json:"termSide"`
}

func slashBtwFixture(t *testing.T) slashBtwShot {
	t.Helper()
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: run make front first")
	}
	var got slashBtwShot
	runFixture(t, "slashbtw.html", &got)
	return got
}

func labels(rows []slashRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Label)
	}
	return out
}

// On the stream the list under a slash is the session's own: every command and
// skill it takes, with what each does, asked for once and filtered as the name
// is typed.
func TestTheSlashListOfAStreamSessionIsItsOwn(t *testing.T) {
	got := slashBtwFixture(t)
	if !got.Theirs {
		t.Fatal("the stream composer kept the panel's own list")
	}
	names := labels(got.Rows)
	for _, want := range []string{"/brief", "/usage", "/restart-session", "/config", "/btw"} {
		if !listed(names, want) {
			t.Errorf("the list lacks %s: %v", want, names)
		}
	}
	for _, r := range got.Rows {
		if r.Label == "/usage" && r.Hint != "Show plan usage and rate limits" {
			t.Errorf("/usage carries %q — not what the session says it does", r.Hint)
		}
	}
	if !got.HintUnder || !got.HintCut {
		t.Errorf("on a phone the description is not a cut line under the name: under %v, cut %v", got.HintUnder, got.HintCut)
	}
	if !got.HintInside {
		t.Error("a long description runs past the edge of the composer instead of being cut at it")
	}
	if want := []string{"/usage", "/usage-credits", "/status"}; !reflect.DeepEqual(got.Filtered, want) {
		t.Errorf("/us lists %v, expected %v — the names that start with it, then the ones that hold it", got.Filtered, want)
	}
	if got.Lists != 1 {
		t.Errorf("the session was asked for its list %d times while one line was typed", got.Lists)
	}
}

// A command the panel does not send stays in the list, and cannot be picked.
func TestARefusedCommandIsListedAndOff(t *testing.T) {
	got := slashBtwFixture(t)
	if got.Refused != "/permissions" || !strings.Contains(got.RefusedHint, "not sent from the panel") {
		t.Errorf("the refused command reads %q with %q", got.Refused, got.RefusedHint)
	}
	if got.AfterRefused != "/per" {
		t.Errorf("a tap on the refused command put %q into the composer", got.AfterRefused)
	}
	for _, r := range got.Rows {
		if r.Label == "/clear" && !r.Off {
			t.Error("/clear can be picked on the stream, where it drops the session off the panel")
		}
	}
}

// A command the panel answers with a screen stays a screen when the list is
// the session's: picked and sent, it opens, and nothing goes out.
func TestAScreenCommandFromTheSessionsListOpensTheScreen(t *testing.T) {
	got := slashBtwFixture(t)
	if !got.McpTag {
		t.Error("/mcp in the session's list is not marked as a screen")
	}
	if got.Picked != "/mcp " {
		t.Errorf("picking /mcp put %q into the composer", got.Picked)
	}
	if got.Screen != "MCP servers" {
		t.Errorf("sending /mcp opened %q", got.Screen)
	}
	if got.ActionsAfterScreen != 0 {
		t.Errorf("sending /mcp went out as %d actions", got.ActionsAfterScreen)
	}
}

// /btw on the stream is asked aside: the question goes to the side chat with
// the side chat so far, the answer is shown there as the feed shows answers,
// and nothing goes into the conversation.
func TestBtwOnTheStreamAsksAside(t *testing.T) {
	got := slashBtwFixture(t)
	if len(got.Waiting) != 1 {
		t.Errorf("while the answer was thought over the side chat showed %v", got.Waiting)
	}
	if got.SideOpen != "side chat" {
		t.Fatalf("/btw opened %q", got.SideOpen)
	}
	if len(got.ActionsAfterSide) != 0 {
		t.Errorf("a question aside went into the conversation: %v", got.ActionsAfterSide)
	}
	if got.ComposerAfter != "" {
		t.Errorf("the composer kept %q after the question went aside", got.ComposerAfter)
	}
	if len(got.Side) != 3 {
		t.Fatalf("the side chat asked %d times, expected three", len(got.Side))
	}
	first := got.Side[0]
	if first.Name != "proj" || first.Question != "which word did I ask you to remember?" || len(first.History) != 0 {
		t.Errorf("the first question went as %+v", first)
	}
	second := got.Side[1]
	if second.Question != "and its first letter?" || len(second.History) != 1 ||
		second.History[0].Question != first.Question || second.History[0].Response != "The word is **tangerine**." {
		t.Errorf("the follow-up did not carry the side chat so far: %+v", second)
	}
	if len(got.Side[2].History) != 2 {
		t.Errorf("the third question carried %d turns of history", len(got.Side[2].History))
	}
	if len(got.Answers) != 1 || got.Answers[0] != "The word is tangerine." || !got.AnswerBold {
		t.Errorf("the answer is shown as %v (bold %v) — it is markdown, as in the feed", got.Answers, got.AnswerBold)
	}
	if len(got.Answers2) != 2 {
		t.Errorf("after the follow-up the side chat shows %v", got.Answers2)
	}
	if len(got.Errors) != 1 || !strings.Contains(got.Errors[0], "not answering") {
		t.Errorf("a refusal is shown as %v", got.Errors)
	}
	if got.AfterBin != 0 || got.BinEmpty != 1 {
		t.Errorf("the bin left %d turns", got.AfterBin)
	}
}

// A terminal session keeps the panel's list, and /btw goes to its screen as a
// command, as it always did.
func TestATerminalKeepsThePanelsListAndItsBtw(t *testing.T) {
	got := slashBtwFixture(t)
	if got.TermTheirs || got.TermLists != 0 {
		t.Errorf("a terminal was given a session's list (asked %d times)", got.TermLists)
	}
	if !listed(got.TermRows, "/clear") || listed(got.TermRows, "/usage") {
		t.Errorf("the terminal lists %v", got.TermRows)
	}
	if got.TermSent.Kind != "session.send" || got.TermSent.Text != "/btw hello" || got.TermSide != 0 {
		t.Errorf("/btw in a terminal went as %+v, %d asked aside", got.TermSent, got.TermSide)
	}
}

func listed(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

type slashTip struct {
	Text string `json:"text"`
	Top  int    `json:"top"`
	Left int    `json:"left"`
}

type slashBtwDeskShot struct {
	ListWidth   int       `json:"listWidth"`
	HintsInRows int       `json:"hintsInRows"`
	Tip0        *slashTip `json:"tip0"`
	Row0Top     int       `json:"row0Top"`
	ListRight   int       `json:"listRight"`
	Hot0        bool      `json:"hot0"`
	HotBg       bool      `json:"hotBg"`
	Tip2        *slashTip `json:"tip2"`
	Row2Top     int       `json:"row2Top"`
	Tip3        *slashTip `json:"tip3"`
	Row3Top     int       `json:"row3Top"`
	Tabbed      string    `json:"tabbed"`
	Scrolls     bool      `json:"scrolls"`
	TipLast     *slashTip `json:"tipLast"`
	RowLastTop  int       `json:"rowLastTop"`
	Card        *struct {
		Top   int    `json:"top"`
		Right int    `json:"right"`
		Width int    `json:"width"`
		Title string `json:"title"`
	} `json:"card"`
	CardAnswer    string   `json:"cardAnswer"`
	CardList      int      `json:"cardList"`
	Asked         []string `json:"asked"`
	Actions       []string `json:"actions"`
	Closed        bool     `json:"closed"`
	ReopenedTurns int      `json:"reopenedTurns"`
	ReopenedAsked int      `json:"reopenedAsked"`
}

// On a wide screen the session's list is a column of names, and what the row
// under the pointer does stands beside it, whole, the way the native client
// shows it; the arrows walk the list and Tab takes the row.
func TestTheSlashListOnAWideScreenShowsTheDescriptionBeside(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: run make front first")
	}
	var got slashBtwDeskShot
	runWideFixture(t, "slashbtwdesk.html", &got)

	if got.HintsInRows != 0 {
		t.Errorf("the rows carry %d descriptions of their own — on a wide screen the row is the name", got.HintsInRows)
	}
	if got.ListWidth > 320 {
		t.Errorf("the list is %d px wide — a column of names, not the width of the composer", got.ListWidth)
	}
	if got.Tip0 == nil || got.Tip0.Text != "List the workflows of the session" {
		t.Fatalf("nothing stands beside the first row: %+v", got.Tip0)
	}
	if got.Tip0.Left < got.ListRight || abs(got.Tip0.Top-got.Row0Top) > 2 {
		t.Errorf("the description stands at %+v, the row at top %d, the list ends at %d", got.Tip0, got.Row0Top, got.ListRight)
	}
	if !got.Hot0 || !got.HotBg {
		t.Error("the row the description belongs to is not lit")
	}
	if got.Tip2 == nil || got.Tip2.Text != "Show plan usage and rate limits" || abs(got.Tip2.Top-got.Row2Top) > 2 {
		t.Errorf("under the pointer the third row shows %+v at its top %d", got.Tip2, got.Row2Top)
	}
	if got.Tip3 == nil || got.Tip3.Text != "Show the credits left on the plan" || abs(got.Tip3.Top-got.Row3Top) > 2 {
		t.Errorf("an arrow down shows %+v at the row's top %d", got.Tip3, got.Row3Top)
	}
	if got.Tabbed != "/usage-credits " {
		t.Errorf("Tab took %q", got.Tabbed)
	}
	if !got.Scrolls {
		t.Fatal("the list does not scroll: the case of a long list is not tried")
	}
	if got.TipLast == nil || abs(got.TipLast.Top-got.RowLastTop) > 2 {
		t.Errorf("in a scrolled list the description stands at %+v, its row at %d", got.TipLast, got.RowLastTop)
	}
}

// On a wide screen the side chat is a card at the top right of the feed; it
// closes and opens again with what it held.
func TestTheSideChatOnAWideScreenIsACardOverTheFeed(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: run make front first")
	}
	var got slashBtwDeskShot
	runWideFixture(t, "slashbtwdesk.html", &got)
	if got.Card == nil {
		t.Fatal("/btw brought up no card")
	}
	if got.Card.Title != "Side chat" || got.Card.Top < 0 || got.Card.Top > 16 || got.Card.Right < 0 || got.Card.Right > 24 {
		t.Errorf("the card stands %+v from the top right of the feed", *got.Card)
	}
	if got.Card.Width > 420 {
		t.Errorf("the card is %d px wide — it covers the conversation it asks about", got.Card.Width)
	}
	if !strings.HasPrefix(got.CardAnswer, "All is well") || got.CardList != 2 {
		t.Errorf("the answer reads %q with %d list items", got.CardAnswer, got.CardList)
	}
	if !reflect.DeepEqual(got.Asked, []string{"is everything all right?"}) || len(got.Actions) != 0 {
		t.Errorf("asked %v aside and %v into the conversation", got.Asked, got.Actions)
	}
	if !got.Closed {
		t.Error("the cross left the card up")
	}
	if got.ReopenedTurns != 1 || got.ReopenedAsked != 1 {
		t.Errorf("/btw alone opened the card with %d turns and asked %d times", got.ReopenedTurns, got.ReopenedAsked)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
