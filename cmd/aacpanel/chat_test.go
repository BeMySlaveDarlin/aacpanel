package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

type fakeAgent struct {
	path string
	got  chan chat.Req
	raw  chan []byte
}

func startAgent(t *testing.T, reply any) *fakeAgent {
	t.Helper()
	return startAgentSeq(t, reply)
}

// startAgentSeq answers the requests in turn; the last answer stays for the rest.
func startAgentSeq(t *testing.T, replies ...any) *fakeAgent {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("the agent socket: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	a := &fakeAgent{path: path, got: make(chan chat.Req, 8), raw: make(chan []byte, 8)}
	var mu sync.Mutex
	turn := 0
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				body, _ := io.ReadAll(conn)
				var req chat.Req
				_ = json.Unmarshal(body, &req)
				a.raw <- body
				a.got <- req
				mu.Lock()
				reply := replies[min(turn, len(replies)-1)]
				turn++
				mu.Unlock()
				out, _ := json.Marshal(reply)
				_, _ = conn.Write(out)
			}()
		}
	}()
	return a
}

func hostWith(t *testing.T, body string) *host.Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("the snapshot: %v", err)
	}
	return host.NewReader(path)
}

const liveSnapshot = `{"at":1,"sessions":[{"session":"sentinel","sessionId":"567f4d24-cd5f-48fa-bdc1-04c89d203494","cwd":"/opt/x"}]}`

func okReply() map[string]any {
	return map[string]any{
		"ok":         true,
		"items":      []map[string]any{{"role": "me", "text": "hi", "pos": 0}},
		"total":      1,
		"moreBefore": false,
		"first":      0,
		"last":       0,
	}
}

func TestChatSendsIDNotName(t *testing.T) {
	agent := startAgent(t, okReply())
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=sentinel", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}

	req := <-agent.got
	if req.Session != "567f4d24-cd5f-48fa-bdc1-04c89d203494" {
		t.Errorf("%q went to the agent — the phone sends a name, and the service substitutes the id", req.Session)
	}

	var reply struct {
		Session string `json:"session"`
		Items   []struct {
			Role string `json:"role"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("the response was not parsed: %v", err)
	}
	if reply.Session != "sentinel" {
		t.Errorf("%q came back outward, and the client pages by name", reply.Session)
	}
	if len(reply.Items) != 1 || reply.Items[0].Role != "me" {
		t.Errorf("the feed did not arrive: %+v", reply.Items)
	}
}

func TestChatTakesOnlyUUIDFromQuery(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"

	t.Run("an archive uuid reaches the agent", func(t *testing.T) {
		agent := startAgent(t, okReply())
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

		w := httptest.NewRecorder()
		srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=april&id="+id, nil))

		req := <-agent.got
		if req.Session != id {
			t.Errorf("%q went to the agent instead of the id from the archive", req.Session)
		}
	})

	t.Run("anything but an id is rejected", func(t *testing.T) {
		for _, bad := range []string{"../../etc/passwd", "sentinel", "11111111-2222", strings.Repeat("a", 36)} {
			agent := startAgent(t, okReply())
			srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

			w := httptest.NewRecorder()
			srv.apiChat(w, httptest.NewRequest(http.MethodGet,
				"/api/chat?session=sentinel&id="+url.QueryEscape(bad), nil))

			if w.Code != http.StatusNotFound {
				t.Errorf("%q: response %d instead of a refusal", bad, w.Code)
			}
			select {
			case req := <-agent.got:
				t.Errorf("%q: went to the agent as %q", bad, req.Session)
			default:
			}
		}
	})
}

func TestChatOpensSubagentFeed(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	const agentID = "aaudit-rules-0123456789abcdef"

	t.Run("the address is split into a conversation and an agent", func(t *testing.T) {
		reply := okReply()
		reply["subagent"] = agentID
		agent := startAgent(t, reply)
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

		w := httptest.NewRecorder()
		srv.apiChat(w, httptest.NewRequest(http.MethodGet,
			"/api/chat?session=sentinel&id="+url.QueryEscape(id+":"+agentID), nil))
		if w.Code != http.StatusOK {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}

		req := <-agent.got
		if req.Session != id {
			t.Errorf("the conversation went out as %q", req.Session)
		}
		if req.Subagent != agentID {
			t.Errorf("the agent went out as %q — the feed will be read from the parent", req.Subagent)
		}
		if req.State {
			t.Error("the session state was asked for an agent feed — the state does not belong to it")
		}
	})

	t.Run("anything but an agent id is rejected", func(t *testing.T) {
		for _, bad := range []string{"../../etc/passwd", "sentinel", "a/b", "",
			strings.Repeat("a", 80)} {
			agent := startAgent(t, okReply())
			srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

			w := httptest.NewRecorder()
			srv.apiChat(w, httptest.NewRequest(http.MethodGet,
				"/api/chat?session=sentinel&id="+url.QueryEscape(id+":"+bad), nil))

			if w.Code != http.StatusNotFound {
				t.Errorf("%q: response %d instead of a refusal", bad, w.Code)
			}
			select {
			case req := <-agent.got:
				t.Errorf("%q: went to the agent as %q", bad, req.Subagent)
			default:
			}
		}
	})

	t.Run("an attachment and a call are read in the agent feed", func(t *testing.T) {
		reply := okReply()
		reply["subagent"] = agentID
		reply["tool"] = "Bash"
		agent := startAgent(t, reply)
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

		w := httptest.NewRecorder()
		srv.apiChatCall(w, httptest.NewRequest(http.MethodGet,
			"/api/chat/call?session=sentinel&id="+url.QueryEscape(id+":"+agentID)+"&pos=4096&i=3", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}
		req := <-agent.got
		if req.Subagent != agentID {
			t.Errorf("the call details were asked without an addressee: %+v", req)
		}
		if req.CallRef == nil || req.CallRef.Pos != 4096 || req.CallRef.Index != 3 {
			t.Errorf("the call address went out as %+v, and pos=4096 i=3 was asked for", req.CallRef)
		}
	})
}

func TestChatRefusesSubagentFeedFromOldAgent(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	const agentID = "aaudit-rules-0123456789abcdef"

	agent := startAgent(t, okReply())
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet,
		"/api/chat?session=sentinel&id="+url.QueryEscape(id+":"+agentID), nil))

	if w.Code == http.StatusOK {
		t.Fatalf("the parent feed is shown under the name of the agent: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "aacpanel-agent") {
		t.Errorf("the refusal does not name what to fix: %s", strings.TrimSpace(w.Body.String()))
	}
}

func TestChatWindowPassesPosition(t *testing.T) {
	agent := startAgent(t, okReply())
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet,
		"/api/chat?session=sentinel&before=4096&limit=7", nil))

	req := <-agent.got
	if req.Before == nil || *req.Before != 4096 {
		t.Errorf("the paging position did not arrive: %+v", req.Before)
	}
	if req.Limit != 7 {
		t.Errorf("window size %d instead of 7", req.Limit)
	}
}

func TestChatFileAsksNextChunkAndKeepsFields(t *testing.T) {
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "text", "name": "log.txt",
		"text": "tail", "cut": true, "size": 131072,
		"offset": 65494, "next": 131000,
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatFile(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file?session=sentinel&path=log.txt&offset=65494", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}

	req := <-agent.got
	if req.Offset != 65494 {
		t.Errorf("offset %d instead of 65494 went to the agent — show more will return the same beginning", req.Offset)
	}
	if req.File != "log.txt" {
		t.Errorf("the path did not arrive: %q", req.File)
	}

	var reply struct {
		Kind   string `json:"kind"`
		Offset int64  `json:"offset"`
		Next   int64  `json:"next"`
		Size   int64  `json:"size"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("the response was not parsed: %v", err)
	}
	if reply.Kind != "text" || reply.Offset != 65494 || reply.Next != 131000 || reply.Size != 131072 {
		t.Errorf("the bounds of the chunk did not reach the client: %+v", reply)
	}
}

func TestChatFileCarriesImageBytes(t *testing.T) {
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "image", "name": "shot.png",
		"media": "image/png", "data": "iVBORw0KGgo=", "size": 42000,
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatFile(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file?session=sentinel&path=shot.png", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}

	var reply struct {
		Media string `json:"media"`
		Data  string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("the response was not parsed: %v", err)
	}
	if reply.Media != "image/png" || reply.Data != "iVBORw0KGgo=" {
		t.Errorf("the bytes of the image did not arrive: %+v", reply)
	}
}

func TestChatSeparatesSilenceFromMissing(t *testing.T) {
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(filepath.Join(t.TempDir(), "missing.sock"))}
	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=sentinel", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("a silent agent gave %d, and this is a temporary refusal", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "aacpanel-agent") {
		t.Errorf("the refusal does not explain what to fix: %q", body)
	}

	agent := startAgent(t, okReply())
	srv2 := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
	w2 := httptest.NewRecorder()
	srv2.apiChat(w2, httptest.NewRequest(http.MethodGet, "/api/chat?session=nosuch", nil))
	if w2.Code != http.StatusNotFound {
		t.Errorf("an unknown session gave %d, and this is not a temporary refusal", w2.Code)
	}
}

func TestChatNeedsSessionName(t *testing.T) {
	agent := startAgent(t, okReply())
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat", nil))
	if w.Code == http.StatusOK {
		t.Fatal("a request without a session name has to refuse")
	}
}

func TestChatPassesAgentRefusal(t *testing.T) {
	agent := startAgent(t, map[string]any{"ok": false, "error": "there is no conversation with such an id on the host"})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=sentinel", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "conversation") {
		t.Errorf("the text from the agent was lost: %q", w.Body.String())
	}
}

func TestLiveSessionNeverTakesNamesakeFromDB(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	const hostName = "CHAT-NAMESAKE"
	hostID, err := db.HostID(t.Context(), hostName)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	const yesterday = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	// The minute rollup is partitioned by week from the start of this one: an
	// hour ago is last week for the first hour of a Monday.
	testdb.PartitionsBack(t, t.Context(), pool, "sessions_1m", 3*time.Hour)
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO sessions_1m (bucket, host_id, name, tokens_max, pct_avg, pct_max, messages_max, samples, session_id, cwd)
		VALUES ($1, $2, 'sentinel', 1000, 10, 20, 5, 6, $3, '/opt/x'),
		       ($1, $2, 'shop', 1000, 10, 20, 5, 6, $3, '/opt/x') ON CONFLICT DO NOTHING`,
		time.Now().UTC().Add(-time.Hour).Truncate(time.Minute), hostID, yesterday); err != nil {
		t.Fatal(err)
	}

	agent := startAgent(t, okReply())
	const silent = `{"at":1,"sessions":[{"session":"sentinel","cwd":"/opt/x"}]}`
	srv := &Server{host: hostWith(t, silent), chat: chat.New(agent.path), db: db, hostName: hostName}

	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=sentinel", nil))

	select {
	case req := <-agent.got:
		t.Fatalf("conversation %q went to the agent — under the session name its namesake from the database was opened", req.Session)
	default:
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, expected 404. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not said a word") {
		t.Errorf("the response %q does not explain why there is no feed: it will be read as a breakage", strings.TrimSpace(w.Body.String()))
	}

	w = httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=shop", nil))
	select {
	case req := <-agent.got:
		t.Fatalf("conversation %q went to the agent — the name of a closed session was found in the database", req.Session)
	default:
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("status %d, expected 404. body: %s", w.Code, w.Body.String())
	}
}

func TestChatShowsSessionState(t *testing.T) {
	reply := okReply()
	reply["state"] = map[string]any{
		"tasks":  []map[string]any{{"id": "b0g4knhe1", "text": "Waiting for CI", "at": "2026-08-25T10:00:00Z"}},
		"agents": []map[string]any{{"name": "audit-rules", "text": "audit", "at": "2026-08-25T10:00:00Z"}},
	}
	agent := startAgent(t, reply)
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=sentinel", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		State *chat.Work `json:"state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("the response was not parsed: %v", err)
	}
	if got.State == nil {
		t.Fatal("the session state did not reach the panel")
	}
	if len(got.State.Tasks) != 1 || len(got.State.Agents) != 1 {
		t.Fatalf("the state arrived incomplete: %+v", *got.State)
	}
	if got.State.Tasks[0].ID != "b0g4knhe1" {
		t.Errorf("the id of the background task = %q: it is what stops the task later",
			got.State.Tasks[0].ID)
	}

	req := <-agent.got
	if !req.State {
		t.Error("the service did not ask for the state: the panel would show an empty strip above the composer")
	}
}

func TestChatSkipsStateWhenPagingUp(t *testing.T) {
	agent := startAgent(t, okReply())
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=sentinel&before=4096", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if req := <-agent.got; req.State {
		t.Error("the service asked for the state while paging up — extra work on the host")
	}
}

func TestArchiveAsksForOneProfile(t *testing.T) {
	page := map[string]any{
		"ok":      true,
		"archive": map[string]any{"total": 1, "limit": 5, "offset": 0, "rows": []map[string]any{}},
	}

	t.Run("a named contour travels as it is", func(t *testing.T) {
		agent := startAgent(t, page)
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

		w := httptest.NewRecorder()
		srv.apiSessionsArchive(w, httptest.NewRequest(http.MethodGet,
			"/api/sessions/archive?limit=5&started=1&profile=work", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}

		req := <-agent.got
		if req.Archive == nil {
			t.Fatal("what went to the agent is not an archive request")
		}
		if req.Archive.Profile != "work" {
			t.Errorf("the contour went out as %q: the profile page will show conversations of another", req.Archive.Profile)
		}
		if !req.Archive.Started || req.Archive.Limit != 5 {
			t.Errorf("the other fields of the request drifted apart: %+v", *req.Archive)
		}
		if raw := string(<-agent.raw); !strings.Contains(raw, `"profile":"work"`) {
			t.Errorf("the request to the agent has no profile key: %s", raw)
		}
	})

	t.Run("without a contour we do not substitute our own", func(t *testing.T) {
		agent := startAgent(t, page)
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

		w := httptest.NewRecorder()
		srv.apiSessionsArchive(w, httptest.NewRequest(http.MethodGet, "/api/sessions/archive?limit=5", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}
		if req := <-agent.got; req.Archive.Profile != "" {
			t.Errorf("the service invented contour %q for the client", req.Archive.Profile)
		}
	})
}

func TestArchiveAsksForEveryPickedContour(t *testing.T) {
	page := map[string]any{
		"ok":      true,
		"archive": map[string]any{"total": 1, "limit": 5, "offset": 0, "rows": []map[string]any{}},
	}
	agent := startAgent(t, page)
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiSessionsArchive(w, httptest.NewRequest(http.MethodGet,
		"/api/sessions/archive?limit=5&profile=work&profile=acme", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}

	req := <-agent.got
	if req.Archive == nil {
		t.Fatal("what went to the agent is not an archive request")
	}
	if got := strings.Join(req.Archive.Profiles, ","); got != "work,acme" {
		t.Errorf("the contours went out as %q: the page will select something other than what was picked", got)
	}
	if req.Archive.Profile != "" {
		t.Errorf("a single contour %q went out alongside the list", req.Archive.Profile)
	}
	if raw := string(<-agent.raw); !strings.Contains(raw, `"profiles":["work","acme"]`) {
		t.Errorf("the request to the agent has no profiles key: %s", raw)
	}
}

func TestArchiveRowsCarryProjectFromMap(t *testing.T) {
	list := []store.Profile{{
		Name: "personal",
		Groups: []store.ProfileGroup{
			{Name: "Services", Projects: []store.ProfileProject{
				{ID: 7, Name: "aacpanel", Path: "/srv/proj/Beta/service/aacpanel"},
			}},
			{Name: "Libraries", Projects: []store.ProfileProject{
				{ID: 9, Name: "cc-lib", Path: "/srv/proj/cc-lib/"},
			}},
		},
	}}

	rows := []chat.ArchiveRow{
		{SessionID: "a", CWD: "/srv/proj/cc-lib", Slug: "-srv-proj-cc-lib"},
		{SessionID: "b", CWD: "/srv/proj/cc-lib/", Slug: "-srv-proj-cc-lib"},
		{SessionID: "c", CWD: "/srv/proj/Beta/aacpanel", Slug: "-srv-proj-Beta-service-aacpanel"},
		{SessionID: "d", CWD: "/srv/proj/cc-lib", Slug: "-srv-proj-Beta-service-aacpanel"},
		{SessionID: "e", CWD: "/srv/proj/elsewhere", Slug: "-srv-proj-elsewhere"},
		{SessionID: "f", Home: true},
	}
	placeRows(rows, list)

	want := map[string]string{"a": "cc-lib", "b": "cc-lib", "c": "aacpanel", "d": "cc-lib", "e": "", "f": ""}
	for _, row := range rows {
		name := ""
		if row.Project != nil {
			name = row.Project.Name
		}
		if name != want[row.SessionID] {
			t.Errorf("row %s got project %q, expected %q", row.SessionID, name, want[row.SessionID])
		}
	}
	if rows[2].Project == nil || rows[2].Project.Group != "Services" || rows[2].Project.ID != 7 {
		t.Errorf("the project found by slug lost its group or its id: %+v", rows[2].Project)
	}
	if rows[2].Project.Path != "/srv/proj/Beta/service/aacpanel" {
		t.Errorf("the project path was taken from the conversation, not from the map: %q", rows[2].Project.Path)
	}
}

func TestArchiveSlugMatchesHowClaudeNamesDirs(t *testing.T) {
	cases := map[string]string{
		"/home/u":                         "-home-u",
		"/srv/proj/Beta/service/aacpanel": "-srv-proj-Beta-service-aacpanel",
		"/home/u/.cache/claude-tmp":       "-home-u--cache-claude-tmp",
		"/srv/proj/lab":                   "-srv-proj-lab",
		"/srv/proj/my_lib/":               "-srv-proj-my-lib",
	}
	for dir, want := range cases {
		if got := archiveSlug(dir); got != want {
			t.Errorf("archiveSlug(%q) = %q, expected %q", dir, got, want)
		}
	}
}

func TestArchiveAsksByContourIDPG(t *testing.T) {
	page := map[string]any{
		"ok":      true,
		"archive": map[string]any{"total": 1, "limit": 5, "offset": 0, "rows": []map[string]any{}},
	}
	agent := startAgent(t, page)
	srv, root := profilesServer(t)
	srv.host = hostWith(t, liveSnapshot)
	srv.chat = chat.New(agent.path)
	mux := profilesMux(srv)

	conf := filepath.Join(root, "work-config")
	id := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"work","configDir":"`+conf+`"}`), "profile")

	t.Run("an id turns into a directory", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.apiSessionsArchive(w, httptest.NewRequest(http.MethodGet,
			"/api/sessions/archive?limit=5&contour="+strconv.Itoa(id), nil))
		if w.Code != http.StatusOK {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}
		req := <-agent.got
		if req.Archive == nil {
			t.Fatal("what went to the agent is not an archive request")
		}
		if req.Archive.Profile != conf {
			t.Errorf("the contour went out as %q, expected the directory %q", req.Archive.Profile, conf)
		}
	})

	t.Run("an unknown id is a refusal, not the personal archive", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.apiSessionsArchive(w, httptest.NewRequest(http.MethodGet,
			"/api/sessions/archive?limit=5&contour="+strconv.Itoa(id+1000), nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("response %d, expected 400: %s", w.Code, w.Body.String())
		}
	})
}

func TestDownloadNamesTheFileInAnyAlphabet(t *testing.T) {
	const name = `résumé "mai".md`
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "raw", "name": name, "size": 2,
		"data": base64.StdEncoding.EncodeToString([]byte("hi")),
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=docs/may.md", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}

	head := w.Header().Get("Content-Disposition")
	kind, params, err := mime.ParseMediaType(head)
	if err != nil {
		t.Fatalf("the header did not parse: %v (%q)", err, head)
	}
	if kind != "attachment" {
		t.Errorf("the answer came as %q: the phone opens the file instead of saving it", kind)
	}
	if params["filename"] != name {
		t.Errorf("the file is saved as %q instead of %q", params["filename"], name)
	}

	plain := quotedName(t, head)
	for i := 0; i < len(plain); i++ {
		if c := plain[i]; c < 0x20 || c > 0x7e || c == '"' {
			t.Fatalf("byte %d of the plain name is %#x: a quote or a letter outside ASCII "+
				"parts a browser from the header, and with it from the rest of the answer", i, c)
		}
	}
	if !strings.HasSuffix(plain, ".md") {
		t.Errorf("the plain name %q lost the extension — the phone will not know what opens it", plain)
	}
}

func quotedName(t *testing.T, head string) string {
	t.Helper()
	const key = `filename="`
	at := strings.Index(head, key)
	if at < 0 {
		t.Fatalf("%q has no plain name — a browser that does not read the encoded one saves "+
			"the file under the name of the route", head)
	}
	rest := head[at+len(key):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("the plain name is not closed: %q", head)
	}
	return rest[:end]
}

func TestDownloadWalksTheFileToItsEnd(t *testing.T) {
	agent := startAgentSeq(t,
		map[string]any{"ok": true, "kind": "raw", "name": "log.txt", "size": 12,
			"offset": 0, "data": base64.StdEncoding.EncodeToString([]byte("first ")), "next": 6},
		map[string]any{"ok": true, "kind": "raw", "name": "log.txt", "size": 12,
			"offset": 6, "data": base64.StdEncoding.EncodeToString([]byte("second"))},
	)
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=notes/log.txt", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != "first second" {
		t.Errorf("%q was saved instead of the whole file — the file arrives cut off at the "+
			"first range, and nothing on the phone says so", body)
	}
	if got := w.Header().Get("Content-Length"); got != "12" {
		t.Errorf("Content-Length %q against 12 bytes of the file: a download broken off "+
			"halfway passes for a whole file", got)
	}

	first, second := <-agent.got, <-agent.got
	if first.Raw != "notes/log.txt" || second.Raw != "notes/log.txt" {
		t.Errorf("the path changed on the way: %q, then %q", first.Raw, second.Raw)
	}
	if first.File != "" || second.File != "" {
		t.Errorf("the file was asked for the way the viewer asks (%q) — that reply carries "+
			"no bytes of a binary at all", first.File)
	}
	if first.Offset != 0 || second.Offset != 6 {
		t.Errorf("the ranges were asked for at %d and %d — the second one repeats the beginning "+
			"of the file instead of continuing it", first.Offset, second.Offset)
	}
}

func TestDownloadHandsOverMediaWhole(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02}
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "raw", "name": "shot.png", "media": "image/png",
		"data": base64.StdEncoding.EncodeToString(raw), "size": len(raw),
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=shot.png", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), raw) {
		t.Errorf("the bytes of the picture came out as %x instead of %x — base64 reached "+
			"the phone instead of the file", w.Body.Bytes(), raw)
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(raw)) {
		t.Errorf("Content-Length %q with %d bytes of the file: the download is shown as broken "+
			"off or hangs waiting for the rest", got, len(raw))
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("the type of the saved file is %q — the phone opens it with the wrong thing", got)
	}
}

func TestDownloadCarriesWhatTheViewerCannotShow(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		file string
	}{
		{"a binary", []byte{0x00, 0x01, 0x02, 0xff}, "core.bin"},
		{"an executable", []byte("\x7fELF\x02\x01"), "aacpanel-exec"},
		{"a clip the viewer calls too large", bytes.Repeat([]byte{0x1a}, 64), "clip.mp4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent := startAgent(t, map[string]any{
				"ok": true, "kind": "raw", "name": c.file, "size": len(c.body),
				"data": base64.StdEncoding.EncodeToString(c.body),
			})
			srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

			w := httptest.NewRecorder()
			srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
				"/api/chat/file/download?session=sentinel&path="+c.file, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("response %d: %s", w.Code, w.Body.String())
			}
			if !bytes.Equal(w.Body.Bytes(), c.body) {
				t.Errorf("%x arrived instead of %x — the very file the screen has nothing "+
					"to show with is the one there is a reason to save", w.Body.Bytes(), c.body)
			}
		})
	}
}

func TestDownloadStopsAtACollectorThatCannotDoIt(t *testing.T) {
	// An older collector knows no range mode and answers the feed window instead.
	agent := startAgent(t, okReply())
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=core.bin", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("response %d: %s — an empty file under the name of the one that was asked "+
			"for lands on the phone, and nothing says the host is behind", w.Code, w.Body.String())
	}
	if head := w.Header().Get("Content-Disposition"); head != "" {
		t.Errorf("the refusal is offered for saving as %q", head)
	}
	if !strings.Contains(w.Body.String(), "restarted") {
		t.Errorf("the refusal does not say what to do about it: %q", w.Body.String())
	}
}

func TestDownloadRefusesAFileTooLargeToCross(t *testing.T) {
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "raw", "name": "huge.log", "size": downloadCap + 1,
		"data": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=huge.log", nil))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 || !strings.Contains(w.Body.String(), strconv.Itoa(downloadCap+1)) {
		t.Errorf("the refusal does not say how large the file is: %q", w.Body.String())
	}
}

func TestDownloadLeavesThePathCheckToTheCollector(t *testing.T) {
	const refusal = "the file was not opened: it either does not exist, or lies outside the " +
		"directory of this conversation"
	agent := startAgent(t, map[string]any{"ok": false, "error": refusal})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	const climb = "../../../etc/passwd"
	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path="+url.QueryEscape(climb), nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), refusal) {
		t.Errorf("the refusal of the collector did not reach the phone: %q", w.Body.String())
	}
	if head := w.Header().Get("Content-Disposition"); head != "" {
		t.Errorf("a refused file is still offered for saving: %q", head)
	}

	req := <-agent.got
	if req.Raw != climb {
		t.Errorf("the path reached the collector as %q instead of %q: the one that holds it "+
			"against the directory of the conversation checks something else than what was asked for",
			req.Raw, climb)
	}
	if req.Session != "567f4d24-cd5f-48fa-bdc1-04c89d203494" {
		t.Errorf("the file was asked for in conversation %q — a path is only inside or outside "+
			"of one, and with the wrong one the check means nothing", req.Session)
	}
}
