package check

import "testing"

// One conversation exercises the form lifecycle on both shells without a
// browser per case. The checks read controls and messages, never dimensions.
func TestMoveFormsOnPhoneAndDesktop(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desktop", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got map[string]bool
			screen.run(t, "waiting.html", &got)
			for _, key := range []string{"toolsHome", "terminal", "outsideDeck"} {
				if !got[key] {
					t.Errorf("%s: the conversation tools lost their home or action: %+v", key, got)
				}
			}
			for _, key := range []string{"head", "markerKeeps", "next", "inFeed", "sentStays", "runConfirms", "run", "auto", "waitTime", "markers", "noteQuiet", "focus", "clipTap", "replyKeeps", "keepsCurrent", "cantReason", "failureKeeps", "cant", "done", "nextForm", "secretPath", "decline", "quietReply", "fullReply", "oneStep", "steps", "back", "review", "round", "oneWay", "dismissHint", "dismissFirst", "discussion", "order", "closedWins", "detachedReply", "askMarker", "askRequired", "askEmpty", "askReview", "askProse", "askClosed", "askReply", "askSources", "askTool", "askDetached", "askWithoutWaiting", "askBody", "askCard", "askConversation", "askWithdrawn", "askDeclined", "askPutAway", "askImmediateProse", "askImmediateTool", "askOneTitle", "claimOut", "claimAnswered", "claimIncoming", "claimOverdue", "claimClosed", "claimReply", "claimNotMove", "claimCapability", "claimRefused", "claimWithdraw", "claimArchive"} {
				if !got[key] {
					t.Errorf("%s: %+v", key, got)
				}
			}
		})
	}
}
