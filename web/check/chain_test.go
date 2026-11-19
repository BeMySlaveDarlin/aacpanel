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
			for _, key := range []string{"pending", "markerPending", "currentStaysOpen", "future", "capabilities", "noMarkdownRun", "refused", "singleRun", "run", "running", "markerRunning", "timer", "terminal", "polls", "stop", "commandCant", "agent", "markerAgent", "pastOutput", "openReply", "failed", "markerFailed", "manual", "agentNote", "agentClosed", "done", "closedCard", "headerToggle", "oneResult", "closedCopy", "linkStaysClosed", "linkStaysOpen", "reasonFirst", "cant", "legacy", "detachedClosed", "summaryRange", "summaryFailed", "summaryCant", "summaryDone", "summaryScattered", "summaryFallback", "detachedOpen", "attachedOpen", "missingRecord", "withdrawn"} {
				if !got[key] {
					t.Errorf("%s: %+v", key, got)
				}
			}
		})
	}
}
