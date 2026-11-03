package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
	"aacpanel/internal/stream"
)

const CrewName = "crew_status"

const CrewInstructions = "Use crew_status before writing to children and before reporting their work to the person."

const CrewDescription = "Call without arguments for this conversation's session_open descendants, indented by depth: " +
	"name, agent, contour, working, human wait and its question, permission or request, own background wait, idle or closed; " +
	"state time or last activity, checklist progress and context fill. Reads the panel's snapshot and opening records. " +
	"Closed branches without live descendants are counted at the end instead of listed. " +
	"Shows no parent, peers, other sessions' children or agents inside this session; screen grouping grants no ownership. " +
	"This is a snapshot, not a wait: continue work or end the turn for reports, without polling."

// Crew reads the same enriched snapshot the panel groups into needs you;
// guessing questions from transcripts here would disagree with its verdicts.
func Crew(h Host) mcp.Tool {
	return mcp.Tool{
		Name: CrewName, Title: "Read the work of child sessions", Allowed: true,
		Description: CrewDescription, Instructions: CrewInstructions,
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		Call: func(ctx context.Context, bind mcp.Bind, raw json.RawMessage) (string, bool) {
			if len(raw) > 0 {
				var args struct{}
				read := json.NewDecoder(bytes.NewReader(raw))
				read.DisallowUnknownFields()
				if err := read.Decode(&args); err != nil {
					return "The crew is not listed: crew_status takes no arguments (" + err.Error() + ").", true
				}
			}
			b, err := bind()
			if err != nil {
				return "The crew is not listed: " + err.Error(), true
			}
			if b.SessionID == "" {
				return "The crew is not listed: the calling conversation is not known yet.", true
			}
			records, err := stream.OpenedSessions()
			if err != nil {
				return "The crew is not listed: the opening records are not read: " + err.Error(), true
			}
			var snapshot struct {
				Sessions []crewRow `json:"sessions"`
			}
			if err := h.crewRead(ctx, "/api/host", &snapshot); err != nil {
				return "The crew is not listed: " + err.Error(), true
			}
			hidden := 0
			slices.SortStableFunc(records, func(a, b stream.OpenedSession) int {
				return strings.Compare(a.Session, b.Session)
			})
			seen := map[string]bool{}
			var descend func(string, int) []string
			descend = func(parent string, depth int) []string {
				var lines []string
				for _, record := range records {
					if record.Conversation != parent || seen[record.Key] || record.SessionID == b.SessionID {
						continue
					}
					seen[record.Key] = true
					row, live := crewLive(record, snapshot.Sessions)
					child := record.SessionID
					if live {
						child = row.SessionID
					} else if child == "" && !strings.HasPrefix(record.Key, "process-") {
						// A legacy Codex opening is keyed by its thread, even
						// when the record keeps no identity of the child yet.
						child = record.Key
					}
					var children []string
					if child != "" && child != b.SessionID {
						children = descend(child, depth+1)
					}
					if !live && len(children) == 0 {
						hidden++
						continue
					}
					name, agent, profile := record.Session, record.Agent, record.Profile
					state, at := "closed", ""
					if live {
						name, agent, profile = row.Name, row.Agent, row.Profile
						state, at = h.crewState(ctx, row)
					}
					if name == "" {
						name = record.Key
					}
					if agent == "" {
						agent = "claude"
						if !live && !strings.HasPrefix(record.Key, "process-") {
							agent = agentCodex
						}
					}
					line := fmt.Sprintf("%s%s | %s | %s | %s", strings.Repeat("  ", depth),
						crewText(name), agent, orUnknown(profile), state)
					if at != "" {
						line += " | " + at
					} else if row.Last != "" {
						line += " | last " + row.Last
					} else {
						line += " | time unknown"
					}
					if live {
						line += crewProgress(row)
					}
					lines = append(lines, line)
					lines = append(lines, children...)
				}
				return lines
			}
			lines := descend(b.SessionID, 0)
			if len(lines) == 0 {
				if hidden > 0 {
					return "This conversation has no live children; " + plural(hidden, "closed child", "closed children") + " not shown.", false
				}
				return "This conversation has no children opened through session_open.", false
			}
			if hidden > 0 {
				lines = append(lines, plural(hidden, "closed child", "closed children")+" not shown.")
			}
			return strings.Join(lines, "\n"), false
		},
	}
}

type crewRow struct {
	Name      string          `json:"session"`
	SessionID string          `json:"sessionId"`
	OpenedKey string          `json:"openedKey"`
	Agent     string          `json:"agent"`
	Profile   string          `json:"profile"`
	Status    string          `json:"status"`
	StatusAt  int64           `json:"statusUpdatedAt"`
	Waiting   string          `json:"waitingFor"`
	TurnOver  bool            `json:"turnOver"`
	Processes int             `json:"processes"`
	Last      string          `json:"lastRequestAt"`
	Pct       *float64        `json:"pct"`
	Work      json.RawMessage `json:"work"`
	Ask       *struct {
		Header string `json:"header"`
		Text   string `json:"text"`
		Count  int    `json:"count"`
		At     string `json:"at"`
	} `json:"ask"`
	Items []struct {
		Kind  string `json:"kind"`
		Title string `json:"title"`
		At    string `json:"at"`
	} `json:"waiting"`
	Checklist *checklist.Checklist `json:"checklist"`
	NextAlarm *struct {
		Name string `json:"name"`
		Next string `json:"next"`
	} `json:"nextAlarm"`
}

func crewLive(record stream.OpenedSession, rows []crewRow) (crewRow, bool) {
	for _, row := range rows {
		if row.OpenedKey == record.Key || (row.OpenedKey == "" && record.SessionID != "" && row.SessionID == record.SessionID) ||
			(row.Agent == agentCodex && row.SessionID == record.Key) {
			return row, true
		}
	}
	return crewRow{}, false
}

func (h Host) crewRead(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, letterWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(h.Panel, "/")+path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("the panel does not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the panel answered %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("the panel's snapshot is not read: %w", err)
	}
	return nil
}

func (h Host) crewState(ctx context.Context, row crewRow) (string, string) {
	at := ""
	if row.StatusAt > 0 && !row.TurnOver {
		at = "since " + time.UnixMilli(row.StatusAt).UTC().Format(time.RFC3339)
	}
	if row.Ask != nil {
		if row.Ask.At != "" {
			at = "since " + row.Ask.At
		}
		return fmt.Sprintf("human: question %s — %s (%s)", crewText(row.Ask.Header), crewText(row.Ask.Text),
			plural(row.Ask.Count, "question", "questions")), at
	}
	if row.Waiting != "" || row.Status == "waiting" {
		var reply struct {
			Permission *action.Permission `json:"permission"`
		}
		if row.Waiting != "input needed" && h.crewRead(ctx,
			"/api/session/permission?name="+url.QueryEscape(row.Name), &reply) == nil && reply.Permission != nil && reply.Permission.Tool != "" {
			return "human: permission for " + crewText(reply.Permission.Tool), at
		}
		return "human: " + orUnknown(row.Waiting), at
	}
	for _, item := range row.Items {
		if item.Kind == "question" || item.Kind == "request" {
			if item.At != "" {
				at = "since " + item.At
			}
			return "human: " + item.Kind + " — " + crewText(item.Title), at
		}
	}
	if row.Status == "busy" && !row.TurnOver {
		return "working", at
	}
	w := backgroundWork(row.Work)
	if row.Agent == agentCodex {
		w.Tasks = max(w.Tasks, row.Processes)
	}
	var counts map[string]json.RawMessage
	_ = json.Unmarshal(row.Work, &counts)
	var own []string
	if w.any() {
		own = append(own, w.words())
	}
	if row.NextAlarm != nil {
		own = append(own, "alarm "+crewText(row.NextAlarm.Name)+" at "+row.NextAlarm.Next)
	} else if n := count(counts["alarms"]); n > 0 {
		own = append(own, plural(n, "alarm", "alarms")+" (time unknown)")
	}
	if n := count(counts["wakes"]); n > 0 {
		var reply struct {
			State *chat.Work `json:"state"`
		}
		var wakes []string
		if h.crewRead(ctx, "/api/chat?session="+url.QueryEscape(row.Name)+"&limit=1", &reply) == nil && reply.State != nil {
			for _, task := range reply.State.Tasks {
				if task.Done || (task.Kind != "wake" && task.Kind != "cron") {
					continue
				}
				when := "time unknown"
				if task.Due != "" {
					when = "at " + task.Due
				} else if task.Schedule != "" {
					when = crewText(task.Schedule)
				}
				wakes = append(wakes, "scheduled wake "+when)
			}
		}
		if len(wakes) == 0 {
			wakes = append(wakes, plural(n, "scheduled wake", "scheduled wakes")+" (time unknown)")
		}
		own = append(own, wakes...)
	}
	if len(own) > 0 {
		return "waiting: " + strings.Join(own, "; "), at
	}
	return "idle", at
}

func crewProgress(row crewRow) string {
	progress := " | checklist none"
	if list := row.Checklist; list != nil && len(list.Items) > 0 {
		done, active := 0, ""
		for _, item := range list.Items {
			if item.Status == checklist.Done {
				done++
			}
			if item.Status == checklist.Active {
				active = item.Text
			}
		}
		progress = fmt.Sprintf(" | checklist %d/%d", done, len(list.Items))
		if active != "" {
			progress += ": " + crewText(active)
		}
	}
	if row.Pct == nil {
		return progress + " | context unknown"
	}
	return progress + fmt.Sprintf(" | context %.0f%%", *row.Pct)
}

func crewText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
