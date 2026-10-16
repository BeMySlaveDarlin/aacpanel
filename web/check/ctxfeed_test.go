package check

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

type ctxOpened struct {
	Label string   `json:"label"`
	List  []string `json:"list"`
}

type ctxSaved struct {
	Href string `json:"href"`
	Name string `json:"name"`
}

type ctxOff struct {
	Aria     string  `json:"aria"`
	Disabled bool    `json:"disabled"`
	Focused  bool    `json:"focused"`
	Tip      string  `json:"tip"`
	Opacity  float64 `json:"opacity"`
	Sent     int     `json:"sent"`
	Sheet    bool    `json:"sheet"`
}

type ctxFeed struct {
	Answer           ctxOpened `json:"answer"`
	AnswerCopied     string    `json:"answerCopied"`
	Quoted           string    `json:"quoted"`
	Mine             ctxOpened `json:"mine"`
	MineCopied       string    `json:"mineCopied"`
	Edited           string    `json:"edited"`
	Queued           ctxOpened `json:"queued"`
	TakenByMenu      []ctxSent `json:"takenByMenu"`
	AfterTakeBack    string    `json:"afterTakeBack"`
	TakenByButton    []ctxSent `json:"takenByButton"`
	EditedQueued     []ctxSent `json:"editedQueued"`
	EditedQueuedText string    `json:"editedQueuedText"`
	Failed           ctxOpened `json:"failed"`
	FailedList       []string  `json:"failedList"`
	SentAgain        []ctxSent `json:"sentAgain"`
	ExecAsked        int       `json:"execAsked"`
	Letter           ctxOpened `json:"letter"`
	LetterCopied     string    `json:"letterCopied"`
	LetterHead       string    `json:"letterHead"`
	Path             ctxOpened `json:"path"`
	PathCopied       string    `json:"pathCopied"`
	PathOpened       string    `json:"pathOpened"`
	Selection        ctxNative `json:"selection"`
	File             ctxOpened `json:"file"`
	FileSaved        *ctxSaved `json:"fileSaved"`
	ViewerSave       *ctxSaved `json:"viewerSave"`
	Shot             ctxOpened `json:"shot"`
	ShotSaved        *ctxSaved `json:"shotSaved"`
	PhotoSave        *ctxSaved `json:"photoSave"`
	Phone            ctxNative `json:"phone"`
	Narrow           struct {
		Width     int  `json:"width"`
		Prevented bool `json:"prevented"`
		Menu      bool `json:"menu"`
	} `json:"narrow"`
	TermIdle       ctxOpened `json:"termIdle"`
	Window         []ctxSent `json:"window"`
	WindowByButton []ctxSent `json:"windowByButton"`
	RenameValue    string    `json:"renameValue"`
	Renamed        []ctxSent `json:"renamed"`
	TermForeign    ctxOpened `json:"termForeign"`
	TermSession    ctxOpened `json:"termSession"`
	SessionOpened  *struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"sessionOpened"`
	SessionClosed []ctxSent `json:"sessionClosed"`
	TermRun       ctxOpened `json:"termRun"`
	RunAsks       struct {
		Asked      bool `json:"asked"`
		SentBefore int  `json:"sentBefore"`
	} `json:"runAsks"`
	RunClosed []ctxSent `json:"runClosed"`
	NewTab    []ctxSent `json:"newTab"`
	Tabs      []string  `json:"tabs"`
	TabMenu   ctxOpened `json:"tabMenu"`
	WindowOff ctxOff    `json:"windowOff"`
	PlusOff   ctxOff    `json:"plusOff"`
}

func ctxFeedPage(t *testing.T) ctxFeed {
	t.Helper()
	var got ctxFeed
	runWideFixture(t, "ctxfeed.html", &got)
	return got
}

func menuOf(o ctxOpened) string {
	return strings.Join(o.List, " · ")
}

// one says whether a press sent exactly one action, of kind to target.
func one(sent []ctxSent, kind, target string) bool {
	return len(sent) == 1 && sent[0].Kind == kind && sent[0].Target == target
}

// A message has the menu of its words: the model's answer is copied whole as
// markdown and quoted into the composer in the form a quote of a selection
// takes; the person's message is copied, quoted and written again in the
// composer, taking nothing back.
func TestAMessageHasTheMenuOfItsWords(t *testing.T) {
	got := ctxFeedPage(t)
	if got.Answer.Label != "the answer" || menuOf(got.Answer) != "Copy · Quote in the composer" {
		t.Errorf("the menu of an answer is %q: %q", got.Answer.Label, menuOf(got.Answer))
	}
	const answer = "The fix is in `web/src/x.js`.\nSecond line of the answer.\nThird line."
	if got.AnswerCopied != answer {
		t.Errorf("Copy put %q into the clipboard, expected the answer as markdown %q", got.AnswerCopied, answer)
	}
	if want := "> The fix is in `web/src/x.js`.\n> Second line of the answer.…\n\n"; got.Quoted != want {
		t.Errorf("Quote put %q into the composer, expected the quote a selection makes, %q", got.Quoted, want)
	}
	if got.Mine.Label != "your message" || menuOf(got.Mine) != "Copy · Quote in the composer · Edit" {
		t.Errorf("the menu of the person's message is %q: %q", got.Mine.Label, menuOf(got.Mine))
	}
	if got.MineCopied != "please **check** it" || got.Edited != "please **check** it" {
		t.Errorf("Copy took %q and Edit put %q into the composer — expected the words of the message", got.MineCopied, got.Edited)
	}
}

// A message in the queue is edited or taken back as its buttons do, through
// the same calls; one that did not go out offers what its row offers, and its
// second attempt is the row's: Esc, then the same words once more. Whether the
// host can make it is asked of the executor the conversation was handed, not
// of the host again by the row and by every menu opened on it.
func TestAQueuedAndAFailedMessageDoWhatTheirRowsDo(t *testing.T) {
	got := ctxFeedPage(t)
	if menuOf(got.Queued) != "Edit · Take back" {
		t.Errorf("the menu of a queued message is %q, expected Edit · Take back", menuOf(got.Queued))
	}
	if !one(got.TakenByMenu, "session.unqueue", "acme") || got.TakenByMenu[0].Params["messageId"] != "m5" {
		t.Errorf("Take back sent %+v, expected session.unqueue of m5", got.TakenByMenu)
	}
	if got.AfterTakeBack != "" {
		t.Errorf("Take back put %q into the composer: it only withdraws the message, Edit is what writes it again", got.AfterTakeBack)
	}
	if !one(got.TakenByButton, "session.unqueue", "acme") || got.TakenByButton[0].Params["messageId"] != "m6" {
		t.Fatalf("the take back button sent %+v — the comparison has no button", got.TakenByButton)
	}
	if len(got.TakenByMenu) == 1 && !reflect.DeepEqual(keysOf(got.TakenByMenu[0].Params), keysOf(got.TakenByButton[0].Params)) {
		t.Errorf("the menu sent %+v and the button %+v: not the same call", got.TakenByMenu[0], got.TakenByButton[0])
	}
	if !one(got.EditedQueued, "session.unqueue", "acme") || got.EditedQueuedText != "queued m7" {
		t.Errorf("Edit of a queued message sent %+v and put %q into the composer — expected it taken back and its words there",
			got.EditedQueued, got.EditedQueuedText)
	}
	if want := "Copy · Esc and send again"; strings.Join(got.FailedList, " · ") != want {
		t.Errorf("the menu of a message that did not go out is %q, expected %q", strings.Join(got.FailedList, " · "), want)
	}
	if len(got.SentAgain) != 2 || got.SentAgain[0].Kind != "session.escape" || got.SentAgain[1].Kind != "session.send" ||
		got.SentAgain[1].Params["text"] != "again please" {
		t.Errorf("Esc and send again sent %+v, expected session.escape and then the same words", got.SentAgain)
	}
	if got.ExecAsked != 0 {
		t.Errorf("the row and the menu of a message that did not go out asked the host for its executor %d times — "+
			"the conversation has it already", got.ExecAsked)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A letter is copied by its menu where its head copies it, the same text.
func TestALetterCopiesAsItsHeadDoes(t *testing.T) {
	got := ctxFeedPage(t)
	if menuOf(got.Letter) != "Copy" {
		t.Errorf("the menu of a letter is %q, expected Copy", menuOf(got.Letter))
	}
	if got.LetterCopied != got.LetterHead || got.LetterCopied == "" {
		t.Errorf("the menu copied %q and the head copies %q", got.LetterCopied, got.LetterHead)
	}
}

// A path inside the words of an answer takes the click before the answer: its
// menu opens, not the answer's. It opens the path as a press does and copies
// it. A selection inside a message leaves the browser's menu.
func TestAPathInsideAMessageTakesTheClickFirst(t *testing.T) {
	got := ctxFeedPage(t)
	if got.Path.Label != "path web/src/x.js" {
		t.Fatalf("a right click on a path in an answer opened %q — the answer took the click its path should have", got.Path.Label)
	}
	if menuOf(got.Path) != "Open · Copy the path" {
		t.Errorf("the menu of a path is %q", menuOf(got.Path))
	}
	if got.PathCopied != "web/src/x.js" || got.PathOpened != "web/src/x.js" {
		t.Errorf("the path was copied as %q and opened as %q", got.PathCopied, got.PathOpened)
	}
	if got.Selection.Prevented || got.Selection.Menu {
		t.Errorf("a right click over a selection inside a message was taken (%+v): copy is what it is for", got.Selection)
	}
}

// A file and a picture of the feed open as a press opens them and are saved
// as their viewers save them: the same address under the same name.
func TestAFileAndAPictureSaveAsTheirViewers(t *testing.T) {
	got := ctxFeedPage(t)
	if menuOf(got.File) != "Open · Copy the path · | · Download" {
		t.Errorf("the menu of a file is %q", menuOf(got.File))
	}
	if got.FileSaved == nil || got.ViewerSave == nil || *got.FileSaved != *got.ViewerSave ||
		!strings.Contains(got.ViewerSave.Href, "/api/chat/file/download?") {
		t.Errorf("the menu saved %+v and the viewer saves %+v", got.FileSaved, got.ViewerSave)
	}
	if menuOf(got.Shot) != "Open · Download" {
		t.Errorf("the menu of a picture is %q", menuOf(got.Shot))
	}
	if got.ShotSaved == nil || got.PhotoSave == nil || *got.ShotSaved != *got.PhotoSave || got.PhotoSave.Href == "" {
		t.Errorf("the menu saved the picture as %+v and its viewer as %+v", got.ShotSaved, got.PhotoSave)
	}
}

// The rows are the phone's as well: where no host stands, and on a screen
// narrower than the shell, a right click on a message is the browser's.
func TestAMessageOnThePhoneKeepsTheBrowsersMenu(t *testing.T) {
	got := ctxFeedPage(t)
	if got.Phone.Prevented || got.Phone.Menu {
		t.Errorf("a message with no host over it took the right click: %+v", got.Phone)
	}
	if got.Narrow.Width >= 1100 {
		t.Fatalf("the narrow frame is %d px wide — not narrower than the shell", got.Narrow.Width)
	}
	if got.Narrow.Prevented || got.Narrow.Menu {
		t.Errorf("a message on a %d px screen took the right click: %+v", got.Narrow.Width, got.Narrow)
	}
}

// A terminal has the menu of its card and its tab: opened, opened in a window
// with the call of the window button, renamed on its sheet, a new tab in its
// place, and closed the way its × closes it — a terminal where something runs
// asks first. A tmux the panel did not start is opened and closed; the
// terminal of a session opens the session and closes it through the gate.
func TestATerminalHasTheMenuOfItsCardAndTab(t *testing.T) {
	got := ctxFeedPage(t)
	full := "Open · | · Open in a window · Rename… · New tab here · | · Close"
	for name, o := range map[string]ctxOpened{"an idle card": got.TermIdle, "a running card": got.TermRun, "a tab": got.TabMenu} {
		if menuOf(o) != full {
			t.Errorf("the menu of %s is %q, expected %q", name, menuOf(o), full)
		}
	}
	if menuOf(got.TermForeign) != "Open · | · Close" || menuOf(got.TermSession) != "Open · | · Close" {
		t.Errorf("a foreign tmux has %q and a session's terminal %q — expected Open and Close alone",
			menuOf(got.TermForeign), menuOf(got.TermSession))
	}
	if !one(got.Window, "term.console", "t-idle") || got.Window[0].Params["id"] != "t-idle" {
		t.Errorf("Open in a window sent %+v, expected term.console of t-idle", got.Window)
	}
	if len(got.WindowByButton) != 1 || got.WindowByButton[0].Kind != "term.console" ||
		got.WindowByButton[0].Params["id"] != got.WindowByButton[0].Target {
		t.Errorf("the window button sent %+v — the call the menu sends is the button's: term.console by the id", got.WindowByButton)
	}
	if got.RenameValue != "zsh" || !one(got.Renamed, "term.rename", "t-idle") || got.Renamed[0].Params["name"] != "build" {
		t.Errorf("Rename… opened a sheet with %q and saved %+v", got.RenameValue, got.Renamed)
	}
	if !one(got.NewTab, "term.start", "/srv/proj") || len(got.Tabs) != 3 {
		t.Errorf("New tab here sent %+v and the terminal opened with the tabs %q", got.NewTab, got.Tabs)
	}
	if got.SessionOpened == nil || got.SessionOpened.Name != "acme" || !one(got.SessionClosed, "session.close", "acme") {
		t.Errorf("the terminal of a session opened %+v and closed with %+v", got.SessionOpened, got.SessionClosed)
	}
	if !got.RunAsks.Asked || got.RunAsks.SentBefore != 0 {
		t.Errorf("Close of a terminal where something runs: asked %v, sent %d before the answer — it must ask first",
			got.RunAsks.Asked, got.RunAsks.SentBefore)
	}
	if !one(got.RunClosed, "term.close", "t-run") {
		t.Errorf("the answer to the question closed with %+v", got.RunClosed)
	}
}

// The plus of a project and the window of a terminal that cannot be pressed
// say why on their tip: they keep the focus and the pointer the tip is read
// by, are dimmed as before, and a press sends nothing.
func TestButtonsThatCannotBePressedSayWhy(t *testing.T) {
	got := ctxFeedPage(t)
	for name, c := range map[string]struct {
		off       ctxOff
		why       string
		dimmedFor float64
	}{
		"the plus of a project":    {got.PlusOff, "session.open", 0.45},
		"the window of a terminal": {got.WindowOff, "term.console", 0.55},
	} {
		o := c.off
		if o.Disabled || o.Aria != "true" {
			t.Errorf("%s is disabled %v, aria-disabled %q — a disabled button takes neither the pointer nor the focus its tip is read by",
				name, o.Disabled, o.Aria)
		}
		if !o.Focused {
			t.Errorf("%s takes no focus: the keyboard never reads why it is off", name)
		}
		if !strings.Contains(o.Tip, c.why) {
			t.Errorf("%s shows the tip %q, expected why the host cannot (%s)", name, o.Tip, c.why)
		}
		if o.Opacity != c.dimmedFor {
			t.Errorf("%s is drawn at opacity %v, the dimmed look is %v", name, o.Opacity, c.dimmedFor)
		}
		if o.Sent != 0 || o.Sheet {
			t.Errorf("a press on %s sent %d actions (a sheet opened: %v)", name, o.Sent, o.Sheet)
		}
	}
}
