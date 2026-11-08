package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/host"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/store"
	"aacpanel/internal/taskreg"
)

func TestTaskSummaryUsesPeriodEventsAndInvalidatesItsCachePG(t *testing.T) {
	srv, root := profilesServer(t)
	id := fillMap(t, srv, root)
	profiles, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	contour := profiles[0].ID
	if _, err := srv.db.SwitchTaskWriter(t.Context(), id, filepath.Join(root, "aacpanel"), taskreg.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	from, until := time.Now().Add(-time.Hour).UTC(), time.Now().Add(time.Hour).UTC()
	before, after := from.Add(-2*time.Hour), from.Add(-time.Hour)
	author := taskreg.Actor{Conversation: "writer", Name: "inventory-worker"}
	title, note := "SER-1063: Finish inventory", "Verified stock against the ledger."
	row, err := srv.db.WriteTask(t.Context(), id, 0, author, taskreg.Edit{Title: &title, Markdown: &note})
	if err != nil {
		t.Fatal(err)
	}
	status := "done"
	if _, err := srv.db.WriteTask(t.Context(), id, row.Number, author, taskreg.Edit{Status: &status}); err != nil {
		t.Fatal(err)
	}
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE task_events SET actor_name='' WHERE project_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	live, _ := json.Marshal(map[string]any{"at": time.Now().Unix(), "sessions": []map[string]any{{"session": author.Name, "sessionId": author.Conversation, "cwd": filepath.Join(root, "aacpanel"), "contour": contour}}})
	srv.host = host.NewReader(snapshotWith(t, string(live)))
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: `{"text":"Stock is verified.","model":"sonnet"}`})
	srv.exec = client
	hostFile := filepath.Join(root, "host.env")
	if err := os.WriteFile(hostFile, []byte("AACP_LANG=en_US.UTF-8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(hostcfg.PathEnv, hostFile)
	t.Setenv(hostcfg.LangEnv, "")
	call := func(method string, start, end time.Time, want int) *store.TaskSummary {
		t.Helper()
		query := url.Values{"contour": {strconv.Itoa(contour)}, "from": {start.Format(time.RFC3339Nano)}, "until": {end.Format(time.RFC3339Nano)}}
		path := "/api/tasks/summary?" + query.Encode()
		raw, _ := json.Marshal(map[string]any{"contour": contour, "from": start, "until": end})
		w := httptest.NewRecorder()
		srv.apiTaskSummary(w, httptest.NewRequest(method, path, strings.NewReader(string(raw))))
		if w.Code != want {
			t.Fatalf("summary %s: %d %s", method, w.Code, w.Body.String())
		}
		var out struct {
			Summary *store.TaskSummary `json:"summary"`
		}
		if want == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return out.Summary
	}
	if got := call("GET", from, until, 200); got != nil {
		t.Fatal("a summary exists before the first run")
	}
	if got := call("POST", before, after, 200); got != nil || len(fake.got) != 0 {
		t.Fatal("an empty period started a model run")
	}
	got := call("POST", from, until, 200)
	if got == nil || got.Text != "Stock is verified." || got.Model != "sonnet" || got.CreatedAt.IsZero() {
		t.Fatalf("summary metadata: %+v", got)
	}
	req := <-fake.got
	for _, want := range []string{title, note, "created", "closed", "inventory-worker", "en_US.UTF-8", "SER-1063"} {
		if !strings.Contains(req.Text, want) {
			t.Errorf("the prompt omits %q", want)
		}
	}
	if req.Kind != action.TaskSummary || req.Project == nil || req.Project.Path != filepath.Join(root, "aacpanel") || req.Contour != strconv.Itoa(contour) {
		t.Fatalf("wrong contour launch: %+v", req)
	}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	if cached := call("GET", from, until, 200); cached == nil || *cached != *got || len(fake.got) != 0 {
		t.Fatalf("the unchanged period did not use the cache: %+v", cached)
	}
	note = "A new verification note."
	if _, err := srv.db.WriteTask(t.Context(), id, row.Number, author, taskreg.Edit{Markdown: &note}); err != nil {
		t.Fatal(err)
	}
	if cached := call("GET", from, until, 200); cached != nil {
		t.Fatal("a new event reused the old fingerprint's summary")
	}
	entries, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 10})
	if err != nil || len(entries) == 0 || entries[0].Kind != string(action.TaskSummary) || entries[0].Target != strconv.Itoa(contour) || entries[0].Result != store.ActionOK {
		t.Fatalf("summary journal: %+v %v", entries, err)
	}
}

func TestTaskSummaryLanguageDefaultsToRussian(t *testing.T) {
	t.Setenv(hostcfg.PathEnv, filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv(hostcfg.LangEnv, "")
	got, err := summaryPrompt(store.SummarySource{}, taskContext{})
	if err != nil || !strings.Contains(got, "language ru ") {
		t.Fatal("the summary has no Russian language fallback")
	}
}

func TestTaskSummaryResolvesArchivedAuthorsLikeThePeriodPG(t *testing.T) {
	srv, root := profilesServer(t)
	project := fillMap(t, srv, root)
	profiles, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	contour := profiles[0].ID
	if _, err := srv.db.SwitchTaskWriter(t.Context(), project, filepath.Join(root, "aacpanel"), taskreg.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	title := "SER-1063: Verify inventory"
	actor := taskreg.Actor{Conversation: "11111111", Name: "inventory-worker"}
	if _, err := srv.db.WriteTask(t.Context(), project, 0, actor, taskreg.Edit{Title: &title}); err != nil {
		t.Fatal(err)
	}
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE task_events SET actor_name='' WHERE project_id=$1`, project); err != nil {
		t.Fatal(err)
	}
	live, _ := json.Marshal(map[string]any{"at": time.Now().Unix(), "sessions": []any{}})
	srv.host = hostWith(t, string(live))
	agent := startAgent(t, map[string]any{"ok": true, "archive": chat.ArchivePage{
		Rows: []chat.ArchiveRow{{SessionID: "11111111-2222-4333-8444-555555555555", Name: actor.Name}}, Total: 1, Limit: 100,
	}})
	srv.chat = chat.New(agent.path)
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: `{"text":"Inventory verified.","model":"sonnet"}`})
	srv.exec = client
	from, until := time.Now().Add(-time.Hour).UTC(), time.Now().Add(time.Hour).UTC()
	query := url.Values{"contour": {strconv.Itoa(contour)}, "from": {from.Format(time.RFC3339Nano)}, "until": {until.Format(time.RFC3339Nano)}}
	w := httptest.NewRecorder()
	srv.apiTaskPeriod(w, httptest.NewRequest("GET", "/api/tasks/period?"+query.Encode(), nil))
	var period store.TaskPeriod
	if err := json.Unmarshal(w.Body.Bytes(), &period); err != nil || w.Code != http.StatusOK || len(period.Groups) != 1 || len(period.Groups[0].Tasks) != 1 || len(period.Groups[0].Tasks[0].Events) != 1 {
		t.Fatalf("period: %d %s, %v", w.Code, w.Body.String(), err)
	}
	periodName := period.Groups[0].Tasks[0].Events[0].ActorName
	if periodName != actor.Name {
		t.Fatalf("the period did not resolve the archived author: %q", periodName)
	}
	body, _ := json.Marshal(map[string]any{"contour": contour, "from": from, "until": until})
	w = httptest.NewRecorder()
	srv.apiTaskSummary(w, httptest.NewRequest("POST", "/api/tasks/summary", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", w.Code, w.Body.String())
	}
	request := <-fake.got
	_, raw, found := strings.Cut(request.Text, "Activity records:\n")
	var records map[string][]store.SummaryEvent
	if err := json.Unmarshal([]byte(raw), &records); err != nil || !found || len(records["SER-1063"]) != 1 {
		t.Fatalf("the summary prompt has no inventory event: %q, %v", raw, err)
	}
	if got := records["SER-1063"][0].Event.ActorName; got != periodName {
		t.Fatalf("the summary event names %q instead of the period's archived author %q", got, periodName)
	}
	for range 2 {
		select {
		case request := <-agent.got:
			if request.Archive == nil || request.Archive.Profile != root || len(request.Archive.Conversations) != 1 || request.Archive.Conversations[0] != actor.Conversation {
				t.Fatalf("the period and summary must use the same filtered archive lookup: %+v", request)
			}
		default:
			t.Fatal("the period or summary did not ask the archive for its author")
		}
	}
}

func TestTaskSummaryPromptStaysAboutWorkAndOmitsDates(t *testing.T) {
	prompt, err := summaryPrompt(store.SummarySource{}, taskContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		"Write only about the work: what was completed, started, and remains.",
		"Keep it to at most five short bullets or paragraphs.",
		"Combine completed tasks on the same topic into one line; do not enumerate every task.",
		"never print conversation ids",
		"Do not print dates, times, timestamps, or period boundaries.",
		"without a heading or introduction",
		"Do not describe the input data, its fields, format, missing values, or missing chronology.",
		"Do not invent results or explanations for missing information.",
	} {
		if !strings.Contains(prompt, rule) {
			t.Errorf("the summary prompt omits the rule %q", rule)
		}
	}
}
