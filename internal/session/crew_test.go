package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/stream"
)

func crewPanel(t *testing.T, rows string) Host {
	t.Helper()
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/host":
			_, _ = w.Write([]byte(`{"sessionsAt":2000,"sessions":` + rows + `}`))
		case "/api/session/permission":
			if r.URL.Query().Get("name") != "permit" {
				t.Errorf("looked for the permission of %q", r.URL.Query().Get("name"))
			}
			_, _ = w.Write([]byte(`{"state":"ok","permission":{"tool":"Bash"}}`))
		case "/api/chat":
			_, _ = w.Write([]byte(`{"state":{"tasks":[{"kind":"wake","due":"2026-01-03T10:00:00Z"},{"kind":"cron","schedule":"every hour"},{"kind":"wake","done":true,"due":"old"}]}}`))
		default:
			t.Errorf("unexpected panel request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(panel.Close)
	return Host{Panel: panel.URL}
}

func keepCrew(t *testing.T, key, name, id, owner, agent string) {
	t.Helper()
	if err := stream.KeepOpened(key, stream.OpenedSession{
		SessionOwner: stream.SessionOwner{Name: "lead", Conversation: owner},
		Session:      name, SessionID: id, Agent: agent, Profile: "personal",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCrewStatusListsOnlyTheCallingConversationsDescendants(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	keepCrew(t, "process-11-100", "child", "child-id", mine, "claude")
	keepCrew(t, "grand-id", "grandchild", "grand-id", "child-id", "codex")
	keepCrew(t, "gone-id", "finished", "gone-id", mine, "codex")
	keepCrew(t, "parent-id", "parent", mine, "ancestor-id", "claude")
	keepCrew(t, "peer-id", "peer", "peer-id", "ancestor-id", "claude")
	keepCrew(t, "stray-id", "stranger", "stray-id", other, "codex")
	keepCrew(t, "stray-child", "foreign-child", "stray-child", "stray-id", "claude")
	h := crewPanel(t, `[
		{"session":"lead","sessionId":"`+mine+`","profile":"personal","status":"busy"},
		{"session":"child","sessionId":"child-id","openedKey":"process-11-100","profile":"work","status":"busy","statusUpdatedAt":1000000,"pct":23.6,
		 "checklist":{"items":[{"text":"Read","status":"done"},{"text":"Verify\ncode","status":"active"},{"text":"Write report","status":"pending"}]}},
		{"session":"grandchild","sessionId":"grand-id","agent":"codex","profile":"personal","status":"idle","lastRequestAt":"2026-01-01T00:00:00Z","pct":0},
		{"session":"peer","sessionId":"peer-id"},
		{"session":"stranger","sessionId":"stray-id","agent":"codex"},
		{"session":"foreign-child","sessionId":"stray-child"},
		{"session":"grouped-only","sessionId":"grouped-id","parent":{"session":"lead"}}
	]`)
	said, failed := call(t, Crew(h), bound(mine), map[string]any{})
	if failed {
		t.Fatal(said)
	}
	lines := strings.Split(said, "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "child | claude | work | working | since 1970-01-01T00:16:40Z") ||
		!strings.HasPrefix(lines[1], "  grandchild | codex | personal | idle | last 2026-01-01T00:00:00Z") ||
		!strings.HasPrefix(lines[2], "finished | codex | personal | closed") {
		t.Fatalf("unexpected subtree:\n%s", said)
	}
	for _, hidden := range []string{"peer |", "stranger |", "foreign-child |", "parent |", "grouped-only |", "lead |"} {
		if strings.Contains(said, hidden) {
			t.Errorf("the crew exposed %s:\n%s", hidden, said)
		}
	}
	if !strings.Contains(lines[0], "checklist 1/3: Verify code | context 24%") ||
		!strings.Contains(lines[1], "checklist none | context 0%") {
		t.Fatalf("progress was lost:\n%s", said)
	}
}

func TestCrewStatusNamesEveryHumanAndOwnWait(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	rows := `[
		{"session":"ask","sessionId":"ask","ask":{"header":"Next\nstep","text":"Which path?","count":2,"at":"2026-01-01T00:00:00Z"}},
		{"session":"permit","sessionId":"permit","status":"waiting","waitingFor":"dialog open"},
		{"session":"text","sessionId":"text","status":"idle","waiting":[{"kind":"question","title":"Use the saved plan?","at":"2026-01-02T00:00:00Z"}]},
		{"session":"request","sessionId":"request","waiting":[{"kind":"request","title":"Sign in"}]},
		{"session":"tasks","sessionId":"tasks","status":"idle","work":{"tasks":2,"wakes":1}},
		{"session":"agents","sessionId":"agents","status":"busy","turnOver":true,"work":{"agents":2}},
		{"session":"observer","sessionId":"observer","work":{"observers":1}},
		{"session":"alarm","sessionId":"alarm","nextAlarm":{"name":"return","next":"2026-01-03T12:00:00Z"},"work":{"alarms":1}},
		{"session":"wake","sessionId":"wake","work":{"tasks":1,"wakes":1}},
		{"session":"flow","sessionId":"flow","work":{"workflows":1}},
		{"session":"terminal","sessionId":"terminal","agent":"codex","processes":2},
		{"session":"busy","sessionId":"busy","status":"busy","work":{"agents":1}},
		{"session":"idle","sessionId":"idle","status":"idle","waiting":[{"kind":"call","title":"Called you"}]}
	]`
	var sessions []crewRow
	if err := json.Unmarshal([]byte(rows), &sessions); err != nil {
		t.Fatal(err)
	}
	for _, row := range sessions {
		keepCrew(t, row.SessionID, row.Name, row.SessionID, mine, "claude")
	}
	said, failed := call(t, Crew(crewPanel(t, rows)), bound(mine), map[string]any{})
	if failed {
		t.Fatal(said)
	}
	for _, want := range []string{
		"ask | claude | not known | human: question Next step — Which path? (2 questions) | since 2026-01-01T00:00:00Z",
		"permit | claude | not known | human: permission for Bash",
		"text | claude | not known | human: question — Use the saved plan? | since 2026-01-02T00:00:00Z",
		"request | claude | not known | human: request — Sign in",
		"tasks | claude | not known | waiting: 1 background task; scheduled wake at 2026-01-03T10:00:00Z; scheduled wake every hour",
		"agents | claude | not known | waiting: 2 agents",
		"observer | claude | not known | waiting: 1 observer",
		"alarm | claude | not known | waiting: alarm return at 2026-01-03T12:00:00Z",
		"wake | claude | not known | waiting: scheduled wake at 2026-01-03T10:00:00Z",
		"flow | claude | not known | waiting: 1 workflow",
		"terminal | codex | not known | waiting: 2 background tasks",
		"busy | claude | not known | working",
		"idle | claude | not known | idle",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("missing %q:\n%s", want, said)
		}
	}
}

func TestCrewStatusHandlesNoChildrenAndRefusesAnUnknownCaller(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	h := crewPanel(t, `[]`)
	tool := Crew(h)
	if !tool.Allowed || tool.Instructions != CrewInstructions || tool.Name != CrewName ||
		!strings.Contains(tool.Description, "no parent, peers") {
		t.Fatalf("the tool's contract is incomplete: %+v", tool)
	}
	said, failed := call(t, tool, bound(mine), map[string]any{})
	if failed || said != "This conversation has no children opened through session_open." {
		t.Fatalf("empty crew: %q, error %v", said, failed)
	}
	for _, bind := range []func() (string, bool){
		func() (string, bool) { return call(t, tool, lost, map[string]any{}) },
		func() (string, bool) { return call(t, tool, bound(""), map[string]any{}) },
		func() (string, bool) { return call(t, tool, bound(mine), map[string]any{"session": other}) },
	} {
		if said, failed := bind(); !failed {
			t.Fatalf("an invalid call listed the crew: %s", said)
		}
	}
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "snapshot missing", http.StatusServiceUnavailable)
	}))
	t.Cleanup(broken.Close)
	if said, failed := call(t, Crew(Host{Panel: broken.URL}), bound(mine), map[string]any{}); !failed || !strings.Contains(said, "503") {
		t.Fatalf("a missing snapshot became an empty crew: %s", said)
	}
}

func TestCrewStatusKeepsDescendantsOfAClosedChildAndStopsCycles(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	keepCrew(t, "closed", "closed-parent", "closed", mine, "codex")
	keepCrew(t, "living", "living-child", "living", "closed", "codex")
	keepCrew(t, "cycle", "cycle", mine, "living", "codex")
	said, failed := call(t, Crew(crewPanel(t, `[{"session":"living-child","sessionId":"living","agent":"codex","status":"busy"}]`)), bound(mine), map[string]any{})
	if failed || len(strings.Split(said, "\n")) != 2 || !strings.Contains(said, "\n  living-child | codex") {
		t.Fatalf("the closed branch or a cycle lost its boundary: %s", said)
	}
}
