package session

import (
	"net/http"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

func TestHelperReportsUseTheLetterActionAndRequireAConfirmedJournal(t *testing.T) {
	report := action.HelperReport{Thread: mine, Parent: other}
	for _, c := range []struct {
		name, body string
		status     int
		fail       bool
	}{
		{"delivered", `{"ok":true,"logged":true}`, http.StatusOK, false},
		{"not journaled", `{"ok":true}`, http.StatusOK, true},
		{"closed", `{"error":"the recipient is closed"}`, http.StatusNotFound, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, url := startPanel(t, c.status, c.body)
			err := (Host{Panel: url}).Report(t.Context(), report, "The complete report")
			if (err != nil) != c.fail {
				t.Fatalf("report result: %v", err)
			}
			calls := p.calls()
			if len(calls) != 1 || calls[0]["kind"] != "session.letter" || calls[0]["target"] != "" {
				t.Fatalf("report action: %+v", calls)
			}
			params := calls[0]["params"].(map[string]any)
			bound := params["report"].(map[string]any)
			if bound["thread"] != mine || bound["parent"] != other || params["text"] != "The complete report" {
				t.Fatalf("report parameters: %+v", params)
			}
		})
	}
	if err := (Host{}).Report(t.Context(), action.HelperReport{Thread: "not-a-thread", Parent: other}, "body"); err == nil || !strings.Contains(err.Error(), "helper report") {
		t.Fatalf("an invalid report reached the network: %v", err)
	}
}
