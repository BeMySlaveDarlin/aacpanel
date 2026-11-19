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
			for _, key := range []string{"pending", "yourMove", "markerPending", "stepsStayOpen", "future", "capabilities", "noMarkdownRun", "refused", "singleRun", "run", "running", "markerRunning", "timer", "terminal", "polls", "stop", "commandCant", "agent", "markerAgent", "agentsMove", "agentsCard", "pastOutput", "openReply", "failed", "markerFailed", "manual", "agentNote", "agentClosed", "done", "closedCard", "closedStaysOpen", "oneResult", "closedCopy", "linkFollows", "reasonFirst", "cant", "legacy", "detachedCard", "summaryRange", "summaryFailed", "summaryCant", "summaryDone", "summaryScattered", "summaryFallback", "detachedOpen", "attachedOpen", "missingRecord", "withdrawn"} {
				if !got[key] {
					t.Errorf("%s: %+v", key, got)
				}
			}
		})
	}
}
