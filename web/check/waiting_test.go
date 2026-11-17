package check

import "testing"

type waitingResult struct {
	NeedsYou              bool
	ContourMark           bool
	ContourPick           string
	AiHighlighted         bool
	QuestionSummary       bool
	QuestionFullText      bool
	QuestionSummaryGone   bool
	QuestionHighlightKept bool
	QuestionHighlightGone bool
	HostHighlightBefore   bool
	HostHighlightGone     bool
	ProseRepliesAbsent    bool
	RepliesMissing        bool
	RepliesEmpty          bool
	RepliesUnavailable    bool
	RepliesOutside        bool
	RepliesCodex          bool
	ReplyBusy             bool
	ReplyPlain            bool
	ReplyFailureKeeps     bool
	ReplyFailedRow        bool
	ReplyText             bool
	ReplyMessageId        bool
	ReplyLocalSending     bool
	ReplyLocalQueued      bool
	ReplyGone             bool
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
	Sent                  struct {
		Kind   string
		Target string
		Params struct{ Act, Answer, Note string }
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
			if wide && got.ContourPick != "personal:1:true|work:0:false" {
				t.Errorf("the picker of contours reads %q, want the open sessions of each and a dot where one waits", got.ContourPick)
			}
			if got.CountBefore != 5 || got.CountAfterOpening != 4 || got.CountAfterAnswer != 3 || got.CountAfterReply != 2 || got.CountBackInList != 2 {
				t.Errorf("waiting counts: %+v", got)
			}
			for name, ok := range map[string]bool{"needs you": got.NeedsYou, "contour mark": got.ContourMark, "question anchor": got.AiHighlighted, "question summary": got.QuestionSummary, "full question message": got.QuestionFullText, "answered summary leaves": got.QuestionSummaryGone, "opens conversation": got.Opened, "call leaves": got.CallGone, "loads old card": got.LoadedOlderWindow,
				"reading state": got.ReadingState, "one history entry": got.OneHistoryEntry, "back closes": got.BackCloses,
				"feed unmoved": got.FeedUnmoved, "no window load": got.NoWindowLoad, "sheet shape": got.SheetShape,
				"run confirms": got.RunConfirms, "run sent": got.RunSent, "answer closes": got.AnswerCloses, "request leaves": got.RequestGone, "tray collapses": got.CollapsedOnPick, "command copies": got.CommandCopies,
				"request visible": got.RequestVisible, "brief highlighted": got.BriefHighlighted, "secret highlighted": got.SecretHighlighted,
				"prose reply controls absent": got.ProseRepliesAbsent, "missing replies": got.RepliesMissing, "empty replies": got.RepliesEmpty,
				"unavailable send": got.RepliesUnavailable, "outside session": got.RepliesOutside, "codex session": got.RepliesCodex,
				"composer reply busy": got.ReplyBusy, "plain reply": got.ReplyPlain, "failed reply keeps question": got.ReplyFailureKeeps,
				"failed reply row": got.ReplyFailedRow, "reply answers both questions": got.ReplyText, "named composer reply": got.ReplyMessageId,
				"local reply sending": got.ReplyLocalSending, "local reply queued": got.ReplyLocalQueued, "answered question leaves": got.ReplyGone,
				"failed send stays": got.FailureKeepsTask, "answer visible": got.AnswerVisible} {
				if !ok {
					t.Errorf("%s: %+v", name, got)
				}
			}
			if !got.QuestionHighlightKept || !got.QuestionHighlightGone {
				t.Errorf("question highlight before reply: %v, gone after reply: %v — it follows its own waiting item", got.QuestionHighlightKept, got.QuestionHighlightGone)
			}
			if !got.HostHighlightBefore || !got.HostHighlightGone {
				t.Errorf("host highlight before update: %v, gone after update: %v — a card dropped by the host leaves no contour", got.HostHighlightBefore, got.HostHighlightGone)
			}
			if got.Sent.Kind != "session.send" || got.Sent.Target != "work" || got.Sent.Params.Act != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || got.Sent.Params.Answer != "done" || got.Sent.Params.Note != "Wi-Fi is back." {
				t.Errorf("answer bypassed the action gate: %+v", got.Sent)
			}
		})
	}
}
