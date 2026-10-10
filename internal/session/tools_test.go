package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

const (
	mine  = "9e3f0a4b-5c6d-4e7f-8a9b-0c1d2e3f4a5b"
	other = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
)

func bound(id string) mcp.Bind {
	return func() (mcp.Binding, error) {
		return mcp.Binding{Place: mcp.Place{ConfigDir: "/home/u/.claude", Dir: "/srv/proj/lab"}, SessionID: id, PID: 4242}, nil
	}
}

func lost() (mcp.Binding, error) {
	return mcp.Binding{}, errors.New("where claude process 9 works is not known")
}

// panel is the panel's local listener as a test sees it: what it was asked,
// and what it answers.
type panel struct {
	mu     sync.Mutex
	asked  []map[string]any
	status int
	body   string
	// hold keeps the answer back, the way the panel holds it while a session
	// on the stream finishes the turn the restart was asked in.
	hold chan struct{}
}

func (p *panel) calls() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any{}, p.asked...)
}

func startPanel(t *testing.T, status int, body string) (*panel, string) {
	t.Helper()
	p := &panel{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/actions" {
			http.Error(w, "not here", http.StatusNotFound)
			return
		}
		var asked map[string]any
		_ = json.NewDecoder(r.Body).Decode(&asked)
		p.mu.Lock()
		p.asked = append(p.asked, asked)
		hold := p.hold
		p.mu.Unlock()
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(p.status)
		_, _ = w.Write([]byte(p.body))
	}))
	t.Cleanup(srv.Close)
	return p, srv.URL
}

// hostWith is a host with a snapshot and a panel, and a clock that moves only when
// the tool sleeps.
func hostWith(t *testing.T, state, panelURL string) Host {
	t.Helper()
	h := hostAt(t, &clock{now: time.Unix(2_000, 0)})
	h.Panel = panelURL
	if state != "" {
		writeState(t, h.State, state)
	}
	return h
}

func call(t *testing.T, tool mcp.Tool, bind mcp.Bind, args any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return tool.Call(context.Background(), bind, raw)
}

// The restart is allowed, since the restart past the context cap is done with
// nobody at the screen; the letter asks the person, as claude asks of any
// tool. Both say when to reach for them in a line the model reads without the
// description, since the tools of a server are deferred.
func TestTheLetterAsksThePersonAndBothSayWhenToReachForThem(t *testing.T) {
	h := Host{}
	if !Restart(h).Allowed {
		t.Error("session_restart asks the person: the restart past the context cap would wait on a prompt")
	}
	if Letter(h).Allowed {
		t.Error("send_to_session is allowed: it would put words before another agent without asking the person")
	}
	for _, tool := range []mcp.Tool{Restart(h), Letter(h)} {
		if tool.Instructions == "" || strings.Contains(tool.Instructions, "\n") || !strings.Contains(tool.Instructions, tool.Name) {
			t.Errorf("%s says when to reach for it as %q", tool.Name, tool.Instructions)
		}
		if n := len(tool.Description); n > 1536 {
			t.Errorf("the description of %s is %d bytes", tool.Name, n)
		}
		raw, _ := json.Marshal(tool.InputSchema)
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil || schema["type"] != "object" || schema["required"] != nil {
			t.Errorf("the schema of %s is %s", tool.Name, raw)
		}
	}
	if Restart(h).Name != "session_restart" || Letter(h).Name != "send_to_session" {
		t.Error("the tools are not named as the sessions call them")
	}
}

// The restart names the conversation of the session that calls it — the one
// its server finds, never one of the model's choosing — and goes on with it
// only when asked to.
func TestARestartNamesTheConversationOfTheSessionCallingIt(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"session lab closed; session lab started"}`)
	h := hostWith(t, "", url)

	said, failed := call(t, Restart(h), bound(mine), map[string]any{})
	if failed || !strings.Contains(said, "session lab closed; session lab started") || !strings.Contains(said, "End the turn") {
		t.Errorf("the restart answered %q (error %v)", said, failed)
	}
	call(t, Restart(h), bound(mine), map[string]any{"continue": true})

	asked := p.calls()
	if len(asked) != 2 {
		t.Fatalf("the panel was asked %v", asked)
	}
	for i, resume := range []any{nil, true} {
		params, _ := asked[i]["params"].(map[string]any)
		if asked[i]["kind"] != "session.restart" || asked[i]["target"] != "" || params["conversation"] != mine ||
			params["resume"] != resume {
			t.Errorf("call %d asked the panel %v", i, asked[i])
		}
	}
}

// The restart is taken when the panel does not answer in time: a restart it
// took answers only once the session is closed, which is after this turn.
func TestARestartThePanelKeepsAnsweringIsUnderWay(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true}`)
	p.hold = make(chan struct{})
	t.Cleanup(func() { close(p.hold) })
	was := restartWait
	restartWait = 100 * time.Millisecond
	t.Cleanup(func() { restartWait = was })

	said, failed := call(t, Restart(hostWith(t, "", url)), bound(mine), map[string]any{})
	if failed || !strings.Contains(said, "restarting this session") || !strings.Contains(said, "end the turn") {
		t.Errorf("a restart under way answered %q (error %v)", said, failed)
	}
}

// Nothing is restarted while the background of the session is at work; the
// answer says what is at work, and anyway, on the person's word, restarts all
// the same. The work of another session holds nothing.
func TestARestartWaitsForTheWorkOfTheSession(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"restarted"}`)
	h := hostWith(t, `{"sessionsAt":9999999999,"sessions":[{"sessionId":"`+mine+`","work":{"agents":2,"tasks":1}},`+
		`{"sessionId":"`+other+`","work":{"agents":5}}]}`, url)

	said, failed := call(t, Restart(h), bound(mine), map[string]any{"continue": true})
	if !failed || !strings.HasPrefix(said, "WAIT") || !strings.Contains(said, "2 agents and 1 background task") ||
		!strings.Contains(said, "anyway") {
		t.Errorf("a restart with work at work answered %q (error %v)", said, failed)
	}
	if len(p.calls()) != 0 {
		t.Fatalf("the panel was asked to restart with work at work: %v", p.calls())
	}

	if said, failed := call(t, Restart(h), bound(mine), map[string]any{"anyway": true}); failed {
		t.Errorf("anyway answered %q", said)
	}
	if said, failed := call(t, Restart(h), bound(other+"-not"), map[string]any{}); failed {
		t.Errorf("the work of another session held the restart: %q", said)
	}
	if len(p.calls()) != 2 {
		t.Errorf("the panel was asked %v", p.calls())
	}
}

// What stops a restart comes back as the tool's error, and nothing more is
// asked: a session not known yet, a panel that refuses or is not there.
func TestWhatStopsARestartIsTheToolsError(t *testing.T) {
	refusing, refusingURL := startPanel(t, http.StatusBadRequest, "no live session runs conversation "+mine+"\n")
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	for name, c := range map[string]struct {
		bind  mcp.Bind
		panel string
		args  any
		says  string
	}{
		"no conversation yet":    {bound(""), refusingURL, map[string]any{}, "not known yet"},
		"no place":               {lost, refusingURL, map[string]any{}, "where claude process 9"},
		"arguments of another":   {bound(mine), refusingURL, map[string]any{"continue": "yes"}, "not the tool's"},
		"the panel refuses":      {bound(mine), refusingURL, map[string]any{}, "no live session runs conversation"},
		"the panel is not there": {bound(mine), goneURL, map[string]any{}, "does not answer"},
	} {
		t.Run(name, func(t *testing.T) {
			said, failed := call(t, Restart(hostWith(t, "", c.panel)), c.bind, c.args)
			if !failed || !strings.Contains(said, c.says) {
				t.Errorf("answered %q (error %v), meant an error saying %q", said, failed, c.says)
			}
		})
	}
	if n := len(refusing.calls()); n != 1 {
		t.Errorf("the panel was asked %d times, only by the call it refused", n)
	}
}

const machine = `{"sessionsAt":1,"sessions":[` +
	`{"session":"lab","sessionId":"` + mine + `","profile":"personal","cwd":"/srv/proj/lab"},` +
	`{"session":"shop","sessionId":"` + other + `","profile":"work","cwd":"/srv/work/shop"},` +
	`{"session":"docs","sessionId":"a","profile":"personal","cwd":"/srv/proj/docs"},` +
	`{"session":"docs","sessionId":"b","profile":"work","cwd":"/srv/work/docs"},` +
	`{"session":"","sessionId":"c","profile":"work","cwd":"/srv/work/nameless"}]}`

// Without a recipient the tool lists the live sessions of every account
// besides the one calling: the name, the account and the directory, and a
// name two of them answer to is marked, since a letter to it is refused.
func TestALetterWithoutARecipientListsTheSessions(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true}`)
	said, failed := call(t, Letter(hostWith(t, machine, url)), bound(mine), map[string]any{})
	if failed {
		t.Fatalf("the list answered %q", said)
	}
	for _, want := range []string{"- shop — work — /srv/work/shop", "- docs — personal — /srv/proj/docs",
		"- docs — work — /srv/work/docs (two live sessions answer to this name"} {
		if !strings.Contains(said, want) {
			t.Errorf("the list does not say %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "/srv/proj/lab") || strings.Contains(said, "nameless") {
		t.Errorf("the list names the calling session or one with no name:\n%s", said)
	}
	if len(p.calls()) != 0 {
		t.Errorf("listing asked the panel %v", p.calls())
	}

	if said, failed := call(t, Letter(hostWith(t, "", url)), bound(mine), map[string]any{}); !failed ||
		!strings.Contains(said, "no snapshot") {
		t.Errorf("a machine with no snapshot listed %q", said)
	}
}

const codexThread = "019a1f00-0000-7000-8000-00000000abcd"

// withCodex is a machine with codex threads beside its claude sessions: one a
// daemon holds, one the panel named, and one running on its own.
const withCodex = `{"sessionsAt":1,"sessions":[` +
	`{"session":"lab","sessionId":"` + mine + `","profile":"personal","cwd":"/srv/proj/lab"},` +
	`{"session":"codex-0000abcd","sessionId":"` + codexThread + `","profile":"acme","cwd":"/srv/acme/shop","agent":"codex","transport":"stream"},` +
	`{"session":"codex-0000beef","sessionId":"019a1f00-0000-7000-8000-00000000beef","profile":"personal","cwd":"/srv/proj/docs","agent":"codex","title":"docs review"},` +
	`{"session":"codex-0000f00d","sessionId":"019a1f00-0000-7000-8000-00000000f00d","profile":"acme","cwd":"/srv/acme/own","agent":"codex","outside":true}]}`

// codexBound is the server under codex at a call of the thread it names.
func codexBound() (mcp.Binding, error) {
	return mcp.Binding{Place: mcp.Place{ConfigDir: "/home/u/.codex-profiles/acme", Dir: "/srv/acme/shop"},
		Name: "codex-0000abcd", SessionID: codexThread, PID: 800, Codex: true}, nil
}

// The list names a codex thread as the panel does, with what it is and where
// it works, so the model can tell it from a claude session; a codex running
// on its own takes no letters and is left out. A codex thread calling is not
// in its own list.
func TestTheListOfSessionsNamesTheCodexThreads(t *testing.T) {
	_, url := startPanel(t, http.StatusOK, `{"ok":true}`)
	said, failed := call(t, Letter(hostWith(t, withCodex, url)), bound(mine), map[string]any{})
	if failed {
		t.Fatalf("the list answered %q", said)
	}
	for _, want := range []string{"- codex-0000abcd (Codex, shop) — acme — /srv/acme/shop",
		"- codex-0000beef (Codex, docs review) — personal — /srv/proj/docs"} {
		if !strings.Contains(said, want) {
			t.Errorf("the list does not say %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "codex-0000f00d") {
		t.Errorf("the list names a codex out of the panel's reach:\n%s", said)
	}

	said, failed = call(t, Letter(hostWith(t, withCodex, url)), codexBound, map[string]any{})
	if failed || strings.Contains(said, "codex-0000abcd") || !strings.Contains(said, "- lab — personal — /srv/proj/lab") {
		t.Errorf("the list for the codex thread calling is %q", said)
	}
}

// A letter of a codex thread goes from the thread the server found, with the
// home of its codex and its directory, and with nothing of the model's choice.
func TestALetterOfCodexGoesFromTheThreadCalling(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"a letter from codex-0000abcd to lab"}`)
	said, failed := call(t, Letter(hostWith(t, withCodex, url)), codexBound, map[string]any{"to": "lab", "text": "hello"})
	if failed || !strings.Contains(said, "a letter from codex-0000abcd to lab") {
		t.Fatalf("the letter answered %q (error %v)", said, failed)
	}
	asked := p.calls()
	if len(asked) != 1 {
		t.Fatalf("the panel was asked %v", asked)
	}
	params, _ := asked[0]["params"].(map[string]any)
	sender, _ := params["fromCodex"].(map[string]any)
	if asked[0]["kind"] != "session.letter" || params["from"] != codexThread || params["text"] != "hello" ||
		sender["home"] != "/home/u/.codex-profiles/acme" || sender["dir"] != "/srv/acme/shop" {
		t.Errorf("the panel was asked %v", asked[0])
	}

	if _, failed := call(t, Letter(hostWith(t, withCodex, url)), bound(mine),
		map[string]any{"to": "codex-0000abcd", "text": "hi"}); failed {
		t.Fatal("a letter of claude to a codex thread failed")
	}
	asked = p.calls()
	if params, _ := asked[len(asked)-1]["params"].(map[string]any); params["from"] != mine || params["fromCodex"] != nil {
		t.Errorf("a letter of claude went as %v", asked[len(asked)-1])
	}
}

// A letter goes to the panel as a message to the recipient from the
// conversation of the session calling — the one its server finds, never a
// name of the model's choosing.
func TestALetterGoesFromTheConversationOfTheSessionCalling(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"a letter from lab to shop (free — it will read it right away), 5 characters"}`)
	said, failed := call(t, Letter(hostWith(t, machine, url)), bound(mine), map[string]any{"to": "shop", "text": "hello"})
	if failed || !strings.Contains(said, "a letter from lab to shop") {
		t.Errorf("the letter answered %q (error %v)", said, failed)
	}
	asked := p.calls()
	if len(asked) != 1 {
		t.Fatalf("the panel was asked %v", asked)
	}
	params, _ := asked[0]["params"].(map[string]any)
	if asked[0]["kind"] != "session.letter" || asked[0]["target"] != "shop" || params["from"] != mine || params["text"] != "hello" {
		t.Errorf("the panel was asked %v", asked[0])
	}
}

// What a letter cannot be is refused before the panel is asked, and what the
// panel refuses comes back with its reason.
func TestWhatStopsALetterIsTheToolsError(t *testing.T) {
	refusing, url := startPanel(t, http.StatusBadGateway, `{"error":"session shop takes no letters: its claude publishes no message socket"}`)
	for name, c := range map[string]struct {
		bind mcp.Bind
		args map[string]any
		says string
	}{
		"text without a recipient": {bound(mine), map[string]any{"text": "hello"}, "needs to"},
		"a recipient without text": {bound(mine), map[string]any{"to": "shop"}, "without text"},
		"a letter too long":        {bound(mine), map[string]any{"to": "shop", "text": strings.Repeat("é", action.TextMax+1)}, "longer than"},
		"a control character":      {bound(mine), map[string]any{"to": "shop", "text": "a\x1bb"}, "control character"},
		"no conversation yet":      {bound(""), map[string]any{"to": "shop", "text": "hello"}, "not known yet"},
		"no place":                 {lost, map[string]any{"to": "shop", "text": "hello"}, "where claude process 9"},
		"the panel refuses":        {bound(mine), map[string]any{"to": "shop", "text": "hello"}, "publishes no message socket"},
	} {
		t.Run(name, func(t *testing.T) {
			said, failed := call(t, Letter(hostWith(t, machine, url)), c.bind, c.args)
			if !failed || !strings.Contains(said, c.says) {
				t.Errorf("answered %q (error %v), meant an error saying %q", said, failed, c.says)
			}
		})
	}
	if n := len(refusing.calls()); n != 1 {
		t.Errorf("the panel was asked %d times, only by the letter it refused", n)
	}
}

// A restart the person asks for is a fresh one. Left a free choice, a model
// takes continue to spare itself the notes a fresh start wants first, and the
// person's "restart" comes back as the same conversation; so the server's word,
// the tool's and the flag's each keep continue for their ask to keep it.
func TestARestartThePersonAsksForIsAFreshOne(t *testing.T) {
	flag := restartSchema()["properties"].(map[string]any)["continue"].(map[string]any)["description"].(string)
	for name, text := range map[string]string{
		"the server's word": RestartInstructions,
		"the tool's word":   RestartDescription,
		"the flag's word":   flag,
	} {
		if !strings.Contains(text, "to keep the conversation") {
			t.Errorf("%s leaves continue to the model's choice: %q", name, text)
		}
	}
}

// A fresh start keeps only what is on disk, so before one the session hands
// its work over the way its project does: with the finalize skill where the
// project has one, the way the context guard asks for it too.
func TestAFreshRestartAsksForTheFinalizeSkillFirst(t *testing.T) {
	if !strings.Contains(RestartDescription, "its finalize skill if it has one") {
		t.Errorf("the tool's word does not ask for the project's finalize skill before a fresh start: %q", RestartDescription)
	}
}

// The line of each tool in the server's word stays one short sentence or two:
// it stands in the system prompt of every session.
func TestTheLinesOfTheToolsAreShort(t *testing.T) {
	for _, line := range []string{RestartInstructions, LetterInstructions, OpenInstructions} {
		if n := utf8.RuneCountInString(line); n > 400 {
			t.Errorf("a line of %d characters: %q", n, line)
		}
	}
}

// Opening a session asks the person, as claude asks of any tool: a new session
// starts work in its account and spends its limits. The tool says when to
// reach for it in the server's word, and it takes a directory and a name, never
// words for the new session, which its model would take for its person's.
func TestAnOpenAsksThePersonAndCarriesNoWordsOfItsOwn(t *testing.T) {
	tool := Open(Host{})
	if tool.Allowed {
		t.Error("session_open is allowed: a session would open others in the account without asking the person")
	}
	if tool.Name != "session_open" || tool.Instructions == "" || strings.Contains(tool.Instructions, "\n") ||
		!strings.Contains(tool.Instructions, tool.Name) {
		t.Errorf("%s says when to reach for it as %q", tool.Name, tool.Instructions)
	}
	if n := len(tool.Description); n > 1536 {
		t.Errorf("the description of %s is %d bytes", tool.Name, n)
	}
	for _, want := range []string{"limits", "send_to_session", "not opened"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("the tool's word does not say %q: %s", want, tool.Description)
		}
	}
	raw, _ := json.Marshal(tool.InputSchema)
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
		Additional *bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil || schema.Type != "object" ||
		schema.Additional == nil || *schema.Additional {
		t.Fatalf("the schema of %s is %s", tool.Name, raw)
	}
	if len(schema.Properties) != 2 || schema.Properties["dir"] == nil || schema.Properties["name"] == nil ||
		len(schema.Required) != 1 || schema.Required[0] != "dir" {
		t.Errorf("%s takes %s: a directory it needs and a name it may have, nothing more", tool.Name, raw)
	}
}

// An open goes to the panel as session.open with the directory, and with the
// name only where the model gives one: without it the panel names the session
// after its project. The answer names the session, its account and its
// directory, and says how the new session is given work.
func TestAnOpenNamesTheDirectoryToThePanel(t *testing.T) {
	p, url := startPanel(t, http.StatusOK,
		`{"ok":true,"detail":"session lab-2 started; opening message: read the queue","contour":"work","logged":true}`)
	h := hostWith(t, "", url)

	said, failed := call(t, Open(h), bound(mine), map[string]any{"dir": "/srv/proj/lab"})
	if failed {
		t.Fatalf("the open answered %q", said)
	}
	for _, want := range []string{"session lab-2 started", "account work", "/srv/proj/lab", "send_to_session",
		"Tell the person"} {
		if !strings.Contains(said, want) {
			t.Errorf("the open does not say %q: %s", want, said)
		}
	}
	call(t, Open(h), lost, map[string]any{"dir": "/srv/proj/lab/web", "name": "lab-fix"})

	asked := p.calls()
	if len(asked) != 2 {
		t.Fatalf("the panel was asked %v", asked)
	}
	for i, want := range []struct{ target, path string }{{"", "/srv/proj/lab"}, {"lab-fix", "/srv/proj/lab/web"}} {
		params, _ := asked[i]["params"].(map[string]any)
		if asked[i]["kind"] != "session.open" || asked[i]["target"] != want.target || params["path"] != want.path ||
			len(params) != 1 {
			t.Errorf("call %d asked the panel %v, meant %+v", i, asked[i], want)
		}
	}
}

// A panel that does not answer within the wait may be opening the session all
// the same: the answer says so, and that a second open is not to be tried
// blindly.
func TestAnOpenThePanelDoesNotAnswerIsNotKnown(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true}`)
	p.hold = make(chan struct{})
	t.Cleanup(func() { close(p.hold) })
	was := openWait
	openWait = 100 * time.Millisecond
	t.Cleanup(func() { openWait = was })

	said, failed := call(t, Open(hostWith(t, "", url)), bound(mine), map[string]any{"dir": "/srv/proj/lab"})
	if !failed || !strings.Contains(said, "not known") || !strings.Contains(said, "again blindly") ||
		!strings.Contains(said, "send_to_session") {
		t.Errorf("an open the panel kept answering answered %q (error %v)", said, failed)
	}
}

// What an open cannot be is refused before the panel is asked, and what the
// panel refuses comes back with its reason.
func TestWhatStopsAnOpenIsTheToolsError(t *testing.T) {
	refusing, url := startPanel(t, http.StatusBadRequest,
		"no project of the map holds /srv/stray: the panel knows neither the account to open a session there in\n")
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	for name, c := range map[string]struct {
		panel string
		args  any
		says  string
	}{
		"no directory":           {url, map[string]any{}, "not an absolute directory"},
		"a relative directory":   {url, map[string]any{"dir": "srv/proj/lab"}, "not an absolute directory"},
		"a name with a space":    {url, map[string]any{"dir": "/srv/proj/lab", "name": "lab two"}, "forbidden character"},
		"a name with a slash":    {url, map[string]any{"dir": "/srv/proj/lab", "name": "lab/two"}, "contains /"},
		"a name out of the path": {url, map[string]any{"dir": "/srv/proj/lab", "name": "../lab"}, ".."},
		"arguments of another":   {url, map[string]any{"dir": 7}, "not the tool's"},
		"the panel refuses":      {url, map[string]any{"dir": "/srv/stray"}, "no project of the map holds /srv/stray"},
		"the panel is not there": {goneURL, map[string]any{"dir": "/srv/proj/lab"}, "does not answer"},
	} {
		t.Run(name, func(t *testing.T) {
			said, failed := call(t, Open(hostWith(t, "", c.panel)), bound(mine), c.args)
			if !failed || !strings.Contains(said, c.says) || !strings.HasPrefix(said, "Nothing was opened") {
				t.Errorf("answered %q (error %v), meant an error saying %q", said, failed, c.says)
			}
		})
	}
	if n := len(refusing.calls()); n != 1 {
		t.Errorf("the panel was asked %d times, only by the open it refused", n)
	}
}
