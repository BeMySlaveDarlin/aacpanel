package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"aacpanel/internal/chat"
)

func searchReply() map[string]any {
	return map[string]any{
		"ok":      true,
		"session": "567f4d24-cd5f-48fa-bdc1-04c89d203494",
		"matches": []map[string]any{
			{"pos": 4096, "at": "2026-10-09T10:00:00Z", "role": "me",
				"snippet": "fix 😀 the café panel", "hit": []int{16, 5}},
			{"pos": 8192, "at": "2026-10-09T10:01:00Z", "role": "card",
				"snippet": "Token of the panel", "hit": []int{13, 5}},
		},
		"total": 412,
		"cut":   true,
	}
}

func searchFor(srv *Server, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.apiChatSearch(w, httptest.NewRequest(http.MethodGet, "/api/chat/search?"+query, nil))
	return w
}

// A question of one character finds half the conversation, and one of a page
// is no question: either is refused before the collector reads a byte. The
// length is in characters, not bytes — two hundred letters with an accent are
// four hundred bytes.
func TestChatSearchRefusesAShortOrALongQuestion(t *testing.T) {
	for _, q := range []string{"", " ", "é", "  é \n", strings.Repeat("é", 201), " " + strings.Repeat("a", 201) + " "} {
		agent := startAgent(t, searchReply())
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

		w := searchFor(srv, "session=sentinel&q="+url.QueryEscape(q))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%q: response %d instead of 400", q, w.Code)
		}
		if !strings.Contains(w.Body.String(), "characters") {
			t.Errorf("%q: the refusal does not say why: %q", q, w.Body.String())
		}
		select {
		case req := <-agent.got:
			t.Errorf("%q went to the collector: %+v", q, req.Search)
		default:
		}
	}
	for _, q := range []string{"pé", " pé ", strings.Repeat("é", 200), "\t" + strings.Repeat("é", 200) + "\n"} {
		agent := startAgent(t, searchReply())
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
		if w := searchFor(srv, "session=sentinel&q="+url.QueryEscape(q)); w.Code != http.StatusOK {
			t.Errorf("%d characters: response %d: %s", len([]rune(strings.TrimSpace(q))), w.Code, w.Body.String())
		}
	}
}

// The question goes to the collector trimmed, for the conversation the feed
// of the same address reads: a live session by its name, an archived one and
// an agent by their ids.
func TestChatSearchAsksAboutTheConversationOfTheFeed(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	const agentID = "aaudit-rules-0123456789abcdef"

	t.Run("a live session", func(t *testing.T) {
		agent := startAgent(t, searchReply())
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
		if w := searchFor(srv, "session=sentinel&q="+url.QueryEscape("  café panel \n")); w.Code != http.StatusOK {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}
		req := <-agent.got
		if req.Session != "567f4d24-cd5f-48fa-bdc1-04c89d203494" || req.Subagent != "" {
			t.Errorf("the search went to %q/%q", req.Session, req.Subagent)
		}
		if req.Search == nil || req.Search.Q != "café panel" {
			t.Fatalf("the question went out as %+v", req.Search)
		}
		if req.Search.Limit != chat.MaxMatches {
			t.Errorf("the collector was asked for %d matches", req.Search.Limit)
		}
		if req.Before != nil || req.After != nil || req.State {
			t.Errorf("the search asked for a window as well: %+v", req)
		}
	})

	t.Run("an agent of an archived conversation", func(t *testing.T) {
		reply := searchReply()
		reply["subagent"] = agentID
		agent := startAgent(t, reply)
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
		w := searchFor(srv, "session=april&id="+url.QueryEscape(id+":"+agentID)+"&q=panel")
		if w.Code != http.StatusOK {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}
		req := <-agent.got
		if req.Session != id || req.Subagent != agentID {
			t.Errorf("the search went to %q/%q", req.Session, req.Subagent)
		}
	})

	t.Run("an address that is no id is refused", func(t *testing.T) {
		agent := startAgent(t, searchReply())
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
		if w := searchFor(srv, "session=sentinel&id=..%2F..%2Fetc&q=panel"); w.Code != http.StatusNotFound {
			t.Errorf("response %d instead of a refusal", w.Code)
		}
		select {
		case req := <-agent.got:
			t.Errorf("went to the collector as %q", req.Session)
		default:
		}
	})
}

// The answer of the collector reaches the screen as it came: the places, the
// snippets and the hits in UTF-16 units, the count of all matches and the
// mark that there were more — and nothing of the socket around it.
func TestChatSearchPassesTheAnswerOfTheCollectorAsItIs(t *testing.T) {
	agent := startAgent(t, searchReply())
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
	w := searchFor(srv, "session=sentinel&q=panel")
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	var got, want map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("the response did not parse: %v", err)
	}
	raw, _ := json.Marshal(searchReply())
	_ = json.Unmarshal(raw, &want)
	delete(want, "ok")
	delete(want, "session")
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("the answer changed on the way:\n got %s\nwant %s", gotJSON, wantJSON)
	}

	none := searchReply()
	none["matches"] = []any{}
	none["total"] = 0
	none["cut"] = false
	empty := startAgent(t, none)
	srv = &Server{host: hostWith(t, liveSnapshot), chat: chat.New(empty.path)}
	w = searchFor(srv, "session=sentinel&q=panel")
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"matches":[],"total":0,"cut":false}` {
		t.Errorf("no match reached the screen as %d %s", w.Code, w.Body.String())
	}
}

// A collector that does not know the question answers with a window of the
// feed, and a window taken for no matches would say the conversation does
// not hold the words. A refusal of the collector and a collector that is down
// answer the way the feed does.
func TestChatSearchTellsACollectorThatCannotSearch(t *testing.T) {
	agent := startAgent(t, okReply())
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
	w := searchFor(srv, "session=sentinel&q=panel")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "aacpanel-agent") {
		t.Errorf("an old collector gave %d: %s", w.Code, w.Body.String())
	}

	refusing := startAgent(t, map[string]any{"ok": false, "error": "there is no transcript with this identifier on disk"})
	srv = &Server{host: hostWith(t, liveSnapshot), chat: chat.New(refusing.path)}
	w = searchFor(srv, "session=sentinel&q=panel")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "no transcript") {
		t.Errorf("a refusal of the collector gave %d: %s", w.Code, w.Body.String())
	}

	srv = &Server{host: hostWith(t, liveSnapshot), chat: chat.New(socketPath(t, "missing.sock"))}
	w = searchFor(srv, "session=sentinel&q=panel")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "aacpanel-agent") {
		t.Errorf("a silent collector gave %d: %s", w.Code, w.Body.String())
	}
}
