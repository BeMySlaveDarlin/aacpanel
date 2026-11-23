package check

import "testing"

func TestRequestChainOnPhoneAndDesktop(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desktop", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got map[string]bool
			screen.run(t, "chain.html", &got)
			for _, key := range []string{"noFlash", "pending", "yourMove", "inPlace", "openCopy", "stepsStayOpen", "future", "capabilities", "unavailable", "actError", "noMarkdownRun", "refused", "singleRun", "run", "running", "timer", "terminal", "polls", "stop", "commandCant", "agent", "agentsMove", "agentsCard", "pastOutput", "openReply", "failed", "manual", "agentNote", "agentClosed", "done", "closedCard", "closedStaysOpen", "oneResult", "closedCopy", "linkFollows", "reasonFirst", "cant", "legacy", "mixed", "pillReveals", "pillStep", "pillCycles", "detachedCard", "farRow", "replyReveals", "pushReveals", "pushOnce", "summaryRange", "summaryFailed", "summaryCant", "summaryDone", "summaryScattered", "summaryFallback", "detachedOpen", "attachedInPlace", "missingRecord", "withdrawn"} {
				if !got[key] {
					t.Errorf("%s: %+v", key, got)
				}
			}
		})
	}
}
