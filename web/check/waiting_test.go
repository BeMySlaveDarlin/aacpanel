package check

import "testing"

type waitingResult struct {
	Wide               bool
	CountBefore        int
	Opened             bool
	CountAfterOpening  int
	CallGone           bool
	LoadedOlderWindow  bool
	RequestHighlighted bool
	CollapsedOnPick    bool
	CommandCopies      bool
	RequestVisible     bool
	BriefHighlighted   bool
	SecretHighlighted  bool
	FailureKeepsTask   bool
	CountAfterAnswer   int
	CountBackInList    int
	AnswerVisible      bool
	HorizontalOverflow bool
	Sent               struct {
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
			if got.CountBefore != 4 || got.CountAfterOpening != 3 || got.CountAfterAnswer != 2 || got.CountBackInList != 2 {
				t.Errorf("waiting counts: %+v", got)
			}
			for name, ok := range map[string]bool{"opens conversation": got.Opened, "call leaves": got.CallGone, "loads old card": got.LoadedOlderWindow,
				"request highlighted": got.RequestHighlighted, "tray collapses": got.CollapsedOnPick, "command copies": got.CommandCopies,
				"request visible": got.RequestVisible, "brief highlighted": got.BriefHighlighted, "secret highlighted": got.SecretHighlighted,
				"failed send stays": got.FailureKeepsTask, "answer visible": got.AnswerVisible} {
				if !ok {
					t.Errorf("%s: %+v", name, got)
				}
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
