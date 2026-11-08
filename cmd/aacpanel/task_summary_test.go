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
	from, until := time.Now(), time.Now().Add(time.Hour)
	if got := summaryPrompt(store.TaskFilter{From: &from, Until: &until}, store.SummarySource{Records: json.RawMessage(`{}`)}); !strings.Contains(got, "language ru ") {
		t.Fatal("the summary has no Russian language fallback")
	}
}
