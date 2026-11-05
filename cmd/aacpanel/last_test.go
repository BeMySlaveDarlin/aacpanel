package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aacpanel/internal/chat"
	"aacpanel/internal/host"
)

func TestLastOfReadsTheStepAndHowTheSessionRan(t *testing.T) {
	row := func(body string) lastRow {
		t.Helper()
		var r lastRow
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, c := range []struct{ row, want string }{
		{`{"sessionId":"a","model":"claude-opus-5-5","effort":"high","mode":"auto","transport":"stream","cap":80,"autoRestart":true,
			"remote":"https://claude.ai/code/x","checkout":"/srv/repo","checklist":{"items":[
			{"text":"read","status":"done"},{"text":"skip","status":"dropped"},{"text":"build","status":"active"},{"text":"ship","status":"pending"}]}}`,
			`{"step":"build","done":2,"total":4,"agent":"claude","model":"claude-opus-5-5","effort":"high","mode":"auto","transport":"stream","cap":80,"autoRestart":true,"remote":"https://claude.ai/code/x","checkout":"/srv/repo"}`},
		{`{"sessionId":"b","agent":"codex","checklist":{"items":[{"text":"read","status":"done"},{"text":"ship","status":"pending"},{"text":"tell","status":"pending"}]}}`,
			`{"step":"ship","done":1,"total":3,"agent":"codex"}`},
		{`{"sessionId":"c","checklist":{"items":[{"text":"read","status":"done"},{"text":"skip","status":"dropped"}]}}`,
			`{"done":2,"total":2,"agent":"claude"}`},
		{`{"sessionId":"d"}`, `{"done":0,"total":0,"agent":"claude"}`},
	} {
		got, err := json.Marshal(lastOf(row(c.row)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("lastOf(%s)\n got %s\nwant %s", c.row, got, c.want)
		}
	}
}

// The archive card of a conversation shows what its live card showed last,
// with the cap the map gave it; the look writes only what changed, and a
// conversation the panel never saw live has nothing.
func TestTheArchiveCardShowsWhatTheLiveCardLastShowedPG(t *testing.T) {
	srv, _, dir := switchServer(t, "stream", "stream")
	const unseen = "12121212-1212-4121-8121-121212121212"
	page := map[string]any{"ok": true, "archive": map[string]any{"total": 2, "limit": 5, "offset": 0,
		"rows": []map[string]any{{"sessionId": switchSID, "name": "aacpanel-2"}, {"sessionId": unseen, "name": "old"}}}}
	srv.chat = chat.New(startAgent(t, page).path)
	live := func(step string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"at": 1, "sessions": []map[string]any{
			{"session": "aacpanel-2", "sessionId": switchSID, "cwd": dir, "model": "claude-opus-5-5", "transport": "stream",
				"checklist": map[string]any{"items": []map[string]string{{"text": "read", "status": "done"}, {"text": step, "status": "active"}}}},
			{"session": "probe", "sessionId": "99999999-9999-4999-8999-999999999999", "cwd": dir, "outside": true},
		}})
		srv.host = host.NewReader(snapshotWith(t, string(raw)))
	}
	archived := func() []chat.ArchiveRow {
		t.Helper()
		w := httptest.NewRecorder()
		srv.apiSessionsArchive(w, httptest.NewRequest(http.MethodGet, "/api/sessions/archive?limit=5", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("the archive: %d %s", w.Code, w.Body.String())
		}
		var got chat.ArchivePage
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Rows) != 2 {
			t.Fatalf("the archive page: %s %v", w.Body.String(), err)
		}
		return got.Rows
	}
	last := func(rows []chat.ArchiveRow) sessionLast {
		t.Helper()
		var got sessionLast
		if len(rows[1].Last) != 0 {
			t.Fatalf("a conversation never seen live has a last card: %s", rows[1].Last)
		}
		if err := json.Unmarshal(rows[0].Last, &got); err != nil {
			t.Fatalf("the last card %q: %v", rows[0].Last, err)
		}
		return got
	}
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	var stored int
	count := func() int {
		t.Helper()
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM session_last`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		return stored
	}

	kept := map[string]string{}
	live("build")
	if err := srv.keepLast(t.Context(), kept); err != nil {
		t.Fatal(err)
	}
	got := last(archived())
	if got.Step != "build" || got.Done != 1 || got.Total != 2 || got.Agent != "claude" || got.Model != "claude-opus-5-5" ||
		got.Transport != "stream" || got.Cap == nil || *got.Cap != 80 || got.AutoRestart == nil {
		t.Fatalf("the last card: %+v", got)
	}
	if n := count(); n != 1 {
		t.Fatalf("the look kept %d cards, want the live session's alone", n)
	}

	// Nothing changed: nothing is written, even what the database lost.
	if _, err := pool.Exec(t.Context(), `DELETE FROM session_last`); err != nil {
		t.Fatal(err)
	}
	if err := srv.keepLast(t.Context(), kept); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 0 {
		t.Fatalf("an unchanged card was written again")
	}

	live("ship")
	if err := srv.keepLast(t.Context(), kept); err != nil {
		t.Fatal(err)
	}
	if got := last(archived()); got.Step != "ship" {
		t.Fatalf("the card after the next step: %+v", got)
	}

	// The session is gone and comes back: its card is written again.
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[]}`))
	if err := srv.keepLast(t.Context(), kept); err != nil {
		t.Fatal(err)
	}
	if len(kept) != 0 {
		t.Fatalf("the look remembers sessions that are gone: %v", kept)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM session_last`); err != nil {
		t.Fatal(err)
	}
	live("ship")
	if err := srv.keepLast(t.Context(), kept); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("a session back on the host has %d cards kept", n)
	}
}
