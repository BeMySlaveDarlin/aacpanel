package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"aacpanel/internal/mcp"
)

// The answers live in the panel's database, not on the collector's shelf.
// A handshake reads them once, within the same wait as the brief hook: a
// panel that is down must not hold up the session's first turn.
func briefStanding(b mcp.Binding) string {
	base, ok := os.LookupEnv("AACP_PANEL_URL")
	if !ok {
		base = "http://127.0.0.1:8777"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(strings.TrimRight(base, "/") + "/api/briefs")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var shelf struct {
		Briefs []json.RawMessage `json:"briefs"`
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil || json.Unmarshal(raw, &shelf) != nil {
		return ""
	}
	return waitingBriefs(shelf.Briefs, b.Place.Dir)
}

type waitingBrief struct {
	ID, Title, CWD, At  string
	Questions, Answered int
	Sent                bool
}

func waitingBriefs(cards []json.RawMessage, cwd string) string {
	var waiting []waitingBrief
	for _, raw := range cards {
		var c waitingBrief
		if json.Unmarshal(raw, &c) == nil && c.CWD == cwd && !c.Sent && c.Answered != 0 {
			waiting = append(waiting, c)
		}
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].At > waiting[j].At })
	if len(waiting) == 0 {
		return ""
	}
	var parts []string
	for _, c := range waiting[:min(3, len(waiting))] {
		title := c.Title
		if title == "" {
			title = c.ID
		}
		if title == "" {
			title = "a brief"
		}
		how := fmt.Sprint(c.Answered)
		if c.Questions != 0 {
			how += fmt.Sprintf(" of %d", c.Questions)
		}
		parts = append(parts, fmt.Sprintf("«%s» (%s answered)", title, how))
	}
	if rest := len(waiting) - len(parts); rest > 0 {
		parts = append(parts, fmt.Sprintf("and %d more", rest))
	}
	return "A brief of this project is answered and the answers have not been sent: " +
		strings.Join(parts, ", ") + ". The person filled it in the panel, in Briefs — they may not know " +
		"the session that asked is gone. Say so if it bears on what you are about to do."
}
