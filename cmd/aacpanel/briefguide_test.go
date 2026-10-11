package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/collector"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/mcp"
	"aacpanel/internal/toolset"
)

// The HTTP editor and the real MCP registration meet at the stored guide.
// Only the collector's fixed-file read is stood in for at the socket edge.
func TestTheBriefGuideMovesFromTheHostToSettingsPG(t *testing.T) {
	db := journalDB(t)
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM brief_guide"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM brief_guide") })
	file := filepath.Join(t.TempDir(), "brief-guide.md")
	const imported = "Name the cost of each choice.\nGive a concrete example."
	if err := os.WriteFile(file, []byte(imported), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	agent := startAgent(t, map[string]any{"ok": true, "briefGuide": string(raw)})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	srv := &Server{db: db, chat: chat.New(agent.path)}
	srv.runBriefGuideImport(ctx)
	select {
	case req := <-agent.got:
		if !req.BriefGuide {
			t.Fatalf("the import asked %+v", req)
		}
	case <-ctx.Done():
		t.Fatal("the startup import never asked for the guide")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	// A second start, after the source is gone, must never need the collector.
	srv = &Server{db: db}
	srv.runBriefGuideImport(ctx)
	panel := httptest.NewServer(srv.routes(srv.localGate()))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	t.Setenv(hostcfg.PathEnv, filepath.Join(t.TempDir(), "host.env"))
	claude := toolset.Server(nil)
	codex := toolset.CodexServer(nil)
	readRules := func(server *mcp.Server, want string) {
		t.Helper()
		for _, tool := range server.Tools {
			if tool.Name != collector.PublishName {
				continue
			}
			got, failed := tool.Call(t.Context(), nil, json.RawMessage(`{}`))
			if failed || !strings.Contains(got, "questions:") ||
				(want != "" && !strings.HasSuffix(got, want+"\n")) ||
				(want == "" && strings.Contains(got, "## The rules of this machine")) {
				t.Fatalf("the guide %q did not reach the tool: failed=%v, tail=%q", want, failed, got[max(0, len(got)-250):])
			}
			return
		}
		t.Fatal("the brief tool is not registered")
	}
	readRules(claude, imported)
	for _, text := range []string{"Keep each choice short.\nExplain its tradeoff.", ""} {
		body, _ := json.Marshal(map[string]string{"text": text})
		w := httptest.NewRecorder()
		srv.routes(srv.localGate()).ServeHTTP(w, httptest.NewRequest(http.MethodPut,
			"/api/settings/brief-guide", strings.NewReader(string(body))))
		if w.Code != http.StatusOK {
			t.Fatalf("saving: %d %s", w.Code, w.Body.String())
		}
		// An import finishing late must lose to a save, even an empty one.
		if err := db.ImportBriefGuide(t.Context(), imported); err != nil {
			t.Fatal(err)
		}
		srv.runBriefGuideImport(ctx)
		readRules(claude, text)
		readRules(codex, text)
	}
	select {
	case req := <-agent.got:
		t.Fatalf("the host file was read again: %+v", req)
	default:
	}
	if err := db.SaveBriefGuide(t.Context(), imported); err != nil {
		t.Fatal(err)
	}
	readRules(claude, imported)
	panel.Close()
	for _, tool := range claude.Tools {
		if tool.Name != collector.PublishName {
			continue
		}
		said, failed := tool.Call(t.Context(), nil, json.RawMessage(`{}`))
		if failed || !strings.Contains(said, "questions:") || !strings.Contains(said, "not read from the panel this time") || strings.Contains(said, imported) {
			t.Fatalf("an unavailable panel did not return shipped rules with a notice: %.160q", said[max(0, len(said)-300):])
		}
	}
}

func TestAnUnfinishedBriefGuideImportRetriesAndAnAbsentFileStaysAbsentPG(t *testing.T) {
	db := journalDB(t)
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM brief_guide"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM brief_guide") })
	agent := startAgentSeq(t,
		map[string]any{"ok": true},
		map[string]any{"ok": false, "error": "the brief guide was not read"},
		map[string]any{"ok": true, "briefGuide": ""})
	srv := &Server{db: db, chat: chat.New(agent.path)}
	for i := 0; i < 2; i++ {
		if _, err := srv.briefGuide(t.Context()); err == nil {
			t.Fatal("a failed import was accepted")
		}
		if _, found, err := db.BriefGuide(t.Context()); err != nil || found {
			t.Fatalf("failed import marked done: %v %v", found, err)
		}
	}
	if text, err := srv.briefGuide(t.Context()); err != nil || text != "" {
		t.Fatalf("absent file: %q %v", text, err)
	}
	srv.chat = nil
	if text, err := srv.briefGuide(t.Context()); err != nil || text != "" {
		t.Fatalf("an absent file was read again: %q %v", text, err)
	}
}

func TestTheBriefGuideReportsFailedReadsAndSaves(t *testing.T) {
	srv := &Server{}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		w := httptest.NewRecorder()
		srv.routes(srv.localGate()).ServeHTTP(w, httptest.NewRequest(method, "/api/settings/brief-guide", strings.NewReader(`{"text":"guide"}`)))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s without a database: %d", method, w.Code)
		}
	}
	for _, body := range []string{`{}`, `{"text":null}`, `{"text":3}`, `broken`, `{"text":"` + strings.Repeat("x", 4<<20) + `"}`} {
		w := httptest.NewRecorder()
		srv.apiBriefGuideSave(w, httptest.NewRequest(http.MethodPut, "/api/settings/brief-guide", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid guide: %d", w.Code)
		}
	}
}
