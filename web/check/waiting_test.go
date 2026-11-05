package check

import "testing"

type waitingResult struct {
	NeedsYou              bool
	ContourMark           bool
	AiHighlighted         bool
	QuestionSummary       bool
	QuestionFullText      bool
	QuestionSummaryGone   bool
	QuestionHighlightKept bool
	ContourInside         bool
	QuestionHighlightGone bool
	HostHighlightBefore   bool
	HostHighlightGone     bool
	QuickButtons          bool
	QuickMissing          bool
	QuickEmpty            bool
	QuickUnavailable      bool
	QuickOutside          bool
	QuickCodex            bool
	QuickBusy             bool
	QuickPlain            bool
	QuickFailureKeeps     bool
	QuickFailedRow        bool
	QuickNote             bool
	QuickMessageId        bool
	QuickLocalSending     bool
	QuickLocalQueued      bool
	QuickGone             bool
	Wide                  bool
	CountBefore           int
	Opened                bool
	CountAfterOpening     int
	CallGone              bool
	LoadedOlderWindow     bool
	ReadingState          bool
	OneHistoryEntry       bool
	BackCloses            bool
	FeedUnmoved           bool
	NoWindowLoad          bool
	SheetShape            bool
	RunConfirms           bool
	RunSent               bool
	AnswerCloses          bool
	RequestGone           bool
	CollapsedOnPick       bool
	CommandCopies         bool
	RequestVisible        bool
	BriefHighlighted      bool
	SecretHighlighted     bool
	FailureKeepsTask      bool
	CountAfterAnswer      int
	CountAfterReply       int
	CountBackInList       int
	AnswerVisible         bool
	HorizontalOverflow    bool
	Sent                  struct {
		Kind   string
		Target string
		Params struct{ Request, Answer, Note string }
	}
}

func TestWaitingQueueOnPhoneAndDesktop(t *testing.T) {
	for _, wide := range []bool{false, true} {
		name := "phone"
		if wide {
			name = "desktop"
		}
		t.Run(name, func(t *testing.T) {
			var got waitingResult
			if wide {
				runWideFixture(t, "waiting.html", &got)
			} else {
				runFixture(t, "waiting.html", &got)
			}
			if got.CountBefore != 5 || got.CountAfterOpening != 4 || got.CountAfterAnswer != 3 || got.CountAfterReply != 2 || got.CountBackInList != 2 {
				t.Errorf("waiting counts: %+v", got)
			}
			for name, ok := range map[string]bool{"needs you": got.NeedsYou, "contour mark": got.ContourMark, "question anchor": got.AiHighlighted, "question summary": got.QuestionSummary, "full question message": got.QuestionFullText, "answered summary leaves": got.QuestionSummaryGone, "opens conversation": got.Opened, "call leaves": got.CallGone, "loads old card": got.LoadedOlderWindow,
				"reading state": got.ReadingState, "one history entry": got.OneHistoryEntry, "back closes": got.BackCloses,
				"feed unmoved": got.FeedUnmoved, "no window load": got.NoWindowLoad, "sheet shape": got.SheetShape,
				"run confirms": got.RunConfirms, "run sent": got.RunSent, "answer closes": got.AnswerCloses, "request leaves": got.RequestGone, "tray collapses": got.CollapsedOnPick, "command copies": got.CommandCopies,
				"request visible": got.RequestVisible, "brief highlighted": got.BriefHighlighted, "secret highlighted": got.SecretHighlighted,
				"quick reply buttons": got.QuickButtons, "missing replies": got.QuickMissing, "empty replies": got.QuickEmpty,
				"unavailable send": got.QuickUnavailable, "outside session": got.QuickOutside, "codex session": got.QuickCodex,
				"quick reply busy": got.QuickBusy, "plain reply": got.QuickPlain, "failed reply keeps question": got.QuickFailureKeeps,
				"failed reply row": got.QuickFailedRow, "reply with note": got.QuickNote, "named quick reply": got.QuickMessageId,
				"local reply sending": got.QuickLocalSending, "local reply queued": got.QuickLocalQueued, "answered quick reply leaves": got.QuickGone,
				"failed send stays": got.FailureKeepsTask, "answer visible": got.AnswerVisible} {
				if !ok {
					t.Errorf("%s: %+v", name, got)
				}
			}
			if !got.ContourInside {
				t.Error("the contour of a revealed card reaches past the feed's left edge and is clipped there")
			}
			if !got.QuestionHighlightKept || !got.QuestionHighlightGone {
				t.Errorf("question highlight before reply: %v, gone after reply: %v — it follows its own waiting item", got.QuestionHighlightKept, got.QuestionHighlightGone)
			}
			if !got.HostHighlightBefore || !got.HostHighlightGone {
				t.Errorf("host highlight before update: %v, gone after update: %v — a card dropped by the host leaves no contour", got.HostHighlightBefore, got.HostHighlightGone)
			}
			if got.HorizontalOverflow {
				t.Error("waiting UI scrolls horizontally")
			}
			if got.Sent.Kind != "session.send" || got.Sent.Target != "work" || got.Sent.Params.Request != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || got.Sent.Params.Answer != "done" || got.Sent.Params.Note != "Wi-Fi is back." {
				t.Errorf("answer bypassed the action gate: %+v", got.Sent)
			}
		})
	}
}
