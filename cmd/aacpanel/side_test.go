package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

// The commands of a session run to tens of kilobytes with what each does: the
// list crosses the executor's socket whole, however long it is.
func TestSessionCommandsPassesTheListOn(t *testing.T) {
	var list []action.SlashCommand
	for i := range 200 {
		list = append(list, action.SlashCommand{Name: fmt.Sprintf("skill-%d", i),
			Description: strings.Repeat("what the skill does, ", 6), Hint: "[path]"})
	}
	client, fake := startFakeExec(t, action.Response{OK: true,
		Commands: &action.SessionCommands{Transport: "stream", List: list}})
	w := httptest.NewRecorder()
	(&Server{exec: client}).apiSessionCommands(w, httptest.NewRequest(http.MethodGet, "/api/session/commands?name=aacpanel", nil))
	var body struct {
		State     string                `json:"state"`
		Transport string                `json:"transport"`
		List      []action.SlashCommand `json:"list"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not json: %.300s", w.Body.String())
	}
	if body.State != "ok" || body.Transport != "stream" || len(body.List) != 200 || body.List[199].Hint != "[path]" {
		t.Errorf("the list arrived as %.300s", w.Body.String())
	}
	if got := <-fake.got; got.Ask != action.AskCommands || got.Target != "aacpanel" {
		t.Errorf("the executor was asked %+v", got)
	}

	empty, _ := startFakeExec(t, action.Response{OK: true, Commands: &action.SessionCommands{Transport: "console"}})
	w = httptest.NewRecorder()
	(&Server{exec: empty}).apiSessionCommands(w, httptest.NewRequest(http.MethodGet, "/api/session/commands?name=aacpanel", nil))
	if !strings.Contains(w.Body.String(), `"list":[]`) {
		t.Errorf("a terminal reads %s — the composer gets no list to read", w.Body.String())
	}
}

func TestAQuestionAsideGoesToTheSessionAndBack(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Side: &action.Side{Answer: "tangerine"}})
	srv := &Server{exec: client}
	w := httptest.NewRecorder()
	srv.apiSessionSide(w, httptest.NewRequest(http.MethodPost, "/api/session/btw", strings.NewReader(
		`{"name":"aacpanel","question":"and its first letter?","history":[{"question":"which word?","response":"tangerine"}]}`)))
	if !strings.Contains(w.Body.String(), `"answer":"tangerine"`) || !strings.Contains(w.Body.String(), `"state":"ok"`) {
		t.Errorf("the answer arrived as %s", w.Body.String())
	}
	got := <-fake.got
	if got.Ask != action.AskSide || got.Target != "aacpanel" || got.Text != "and its first letter?" ||
		len(got.History) != 1 || got.History[0].Response != "tangerine" {
		t.Errorf("the executor was asked %+v", got)
	}

	w = httptest.NewRecorder()
	srv.apiSessionSide(w, httptest.NewRequest(http.MethodPost, "/api/session/btw", strings.NewReader(`{"name":"aacpanel","question":"  "}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a question with no words went on with %d", w.Code)
	}

	failing, _ := startFakeExec(t, action.Response{OK: false, Error: "session aacpanel runs in a terminal"})
	w = httptest.NewRecorder()
	(&Server{exec: failing}).apiSessionSide(w, httptest.NewRequest(http.MethodPost, "/api/session/btw",
		strings.NewReader(`{"name":"aacpanel","question":"why?"}`)))
	if !strings.Contains(w.Body.String(), `"state":"failed"`) || !strings.Contains(w.Body.String(), "terminal") {
		t.Errorf("a refusal of the executor reads %s", w.Body.String())
	}
}
