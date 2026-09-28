package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
)

// A call without a document returns how a brief is written: the fields, the
// rules of its text, the ceilings — and nothing of a command line, which a
// session with the tool has no use for. Nothing reaches the collector.
func TestACallWithoutADocumentReturnsTheRules(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "id": "a"}))
	tool := BriefPublish(f.path, "")
	for _, args := range []string{``, `{}`, `{"doc":null}`, `{"check":true}`} {
		said, failed := callTool(t, tool, bound(sid), args)
		if failed || said != rules {
			t.Errorf("a call with %q answered %.80q (error %v)", args, said, failed)
		}
	}
	for _, want := range []string{"questions:", "kind", "facts", "Why, What, Choice", "100 questions", "4 MB", "brief_delete"} {
		if !strings.Contains(rules, want) {
			t.Errorf("the rules do not say %q", want)
		}
	}
	for _, gone := range []string{"brief.py", "--check", "--delete", "SKILL", "STOP"} {
		if strings.Contains(rules, gone) {
			t.Errorf("the rules speak of a command line: %q", gone)
		}
	}
	f.quiet(t)
}

// A machine may keep rules of its own for the text of a brief — the words
// its briefs avoid, samples of briefs taken and refused. They follow the
// shipped rules when the file is there, and nothing is added when it is not.
func TestTheRulesOfTheHostFollowTheShippedOnes(t *testing.T) {
	dir := t.TempDir()
	guide := filepath.Join(dir, "brief-guide.md")

	if got := Rules(guide); got != rules {
		t.Errorf("with no file of the host the rules are %d bytes, not the shipped %d", len(got), len(rules))
	}
	if err := os.WriteFile(guide, []byte("\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Rules(guide); got != rules {
		t.Error("an empty file of the host added a heading with nothing under it")
	}

	own := "Never write \"leverage\".\nThe sample of a brief refused: see the archive."
	if err := os.WriteFile(guide, []byte(own+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	said, failed := callTool(t, BriefPublish(nowhere(t), guide), bound(sid), `{}`)
	if failed || !strings.HasPrefix(said, rules) || !strings.HasSuffix(said, own+"\n") ||
		!strings.Contains(said, "## The rules of this machine") {
		t.Errorf("with the host's own rules the call answered the tail %q", said[len(said)-min(len(said), 300):])
	}
}

// The host's rules are found where the config of the user lives, so a
// machine keeps them beside the rest of its panel settings.
func TestTheRulesOfTheHostLiveInTheConfigOfTheUser(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/home/u/.config")
	if got := HostGuide(); got != "/home/u/.config/aacpanel/brief-guide.md" {
		t.Errorf("the host's rules are looked for at %q", got)
	}
}

// The brief goes out under the session and the directory of the claude the
// server serves, whatever the model put in the document or beside it: the
// collector sends the answers back to that session and holds the brief to
// that directory, and a document naming another would land its answers in a
// stranger's conversation. The document itself reaches the collector as the
// model wrote it.
func TestTheBriefGoesOutUnderTheSessionTheServerServes(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "id": "seven-questions"}))
	doc := map[string]any{
		"id": "seven-questions", "title": "Seven questions",
		"sessionId": "6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e", "cwd": "/srv/other",
		"questions": []any{map[string]any{"id": "r1", "title": "Which", "kind": "text"}},
	}
	args, _ := json.Marshal(map[string]any{"doc": doc, "sessionId": "forged", "cwd": "/srv/forged"})

	said, failed := callTool(t, BriefPublish(f.path, ""), bound(sid), string(args))
	if failed || !strings.HasPrefix(said, "Published as seven-questions.") || !strings.Contains(said, "do not wait") {
		t.Errorf("the call answered %q (error %v)", said, failed)
	}
	got := f.request(t)
	if got["sessionId"] != sid || got["cwd"] != "/srv/proj" || len(got) != 3 {
		t.Errorf("the collector was handed %v", got)
	}
	if !reflect.DeepEqual(got["doc"], doc) {
		t.Errorf("the document reached the collector as %v", got["doc"])
	}
}

// A brief that asks nothing is a piece of reading: there are no answers to
// wait for, and the reply does not promise any.
func TestAReportPromisesNoAnswers(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "id": "findings"}))
	said, failed := callTool(t, BriefPublish(f.path, ""), bound(sid),
		`{"doc":{"id":"findings","title":"Findings","questions":[{"id":"r1","title":"Where it came from","kind":"none"}]}}`)
	if failed || said != "Published as findings: the person reads it in the panel." {
		t.Errorf("a report was answered %q (error %v)", said, failed)
	}
}

// A client that hands a nested object over as a string of JSON is taken at
// its word; anything else that is not an object is refused before it reaches
// the collector.
func TestADocumentIsAnObject(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "id": "a"}))
	tool := BriefPublish(f.path, "")
	if said, failed := callTool(t, tool, bound(sid), `{"doc":"{\"id\":\"a\",\"title\":\"A\"}"}`); failed {
		t.Errorf("a document sent as a string of JSON was answered %q", said)
	}
	if got := f.request(t); !reflect.DeepEqual(got["doc"], map[string]any{"id": "a", "title": "A"}) {
		t.Errorf("the document sent as a string reached the collector as %v", got["doc"])
	}
	for _, args := range []string{`{"doc":"id: a"}`, `{"doc":[1]}`, `{"doc":7}`, `{"doc":{"id":"a"`} {
		if said, failed := callTool(t, tool, bound(sid), args); !failed {
			t.Errorf("%s was answered %q", args, said)
		}
	}
	f.quiet(t)
}

// A check says what is in the document — the title, the questions and how
// many of them ask something — and publishes nothing.
func TestACheckCountsAndPublishesNothing(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "id": "a"}))
	said, failed := callTool(t, BriefPublish(f.path, ""), bound(sid), `{"check":true,"doc":{"id":"a","title":"Three",`+
		`"questions":[{"id":"r1","title":"x"},{"id":"r2","title":"y","kind":"text"},{"id":"r3","title":"z","kind":"none"}]}}`)
	if failed || !strings.HasPrefix(said, "Three: 3 questions, 2 of them ask something. Nothing was published") {
		t.Errorf("the check answered %q (error %v)", said, failed)
	}
	if said, _ := callTool(t, BriefPublish(f.path, ""), bound(sid), `{"check":true,"doc":{"id":"a"}}`); !strings.HasPrefix(said, "untitled: 0 questions") {
		t.Errorf("the check of a document without a title answered %q", said)
	}
	f.quiet(t)
}

// A refusal of the collector comes back with its reason, so the model can fix
// the document or say in the conversation why nothing was published.
func TestARefusalOfTheCollectorComesBackWithItsReason(t *testing.T) {
	why := "the id 'a' is taken by a brief from /srv/other: give this one a name of its own"
	f := listen(t, answers(map[string]any{"ok": false, "error": why}))
	said, failed := callTool(t, BriefPublish(f.path, ""), bound(sid), `{"doc":{"id":"a","title":"A"}}`)
	if !failed || said != "The brief was not published: "+why+"." {
		t.Errorf("the refusal came back as %q (error %v)", said, failed)
	}
	said, failed = callTool(t, BriefDelete(f.path), bound(sid), `{"id":"a"}`)
	if !failed || said != "The brief was not removed: "+why+"." {
		t.Errorf("the refusal of a removal came back as %q (error %v)", said, failed)
	}
}

// A document over the collector's ceiling is refused here with the reason:
// the collector stops reading at the ceiling, and the write would break off
// with nothing said.
func TestADocumentOverTheCeilingIsRefusedHere(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "id": "a"}))
	big, _ := json.Marshal(map[string]any{"doc": map[string]any{"id": "a", "title": "A", "lede": strings.Repeat("x", maxBrief)}})
	said, failed := callTool(t, BriefPublish(f.path, ""), bound(sid), string(big))
	if !failed || !strings.Contains(said, "longer than 4 MB") {
		t.Errorf("a document over the ceiling was answered %q", said)
	}
	f.quiet(t)
}

// A brief near the collector's ceiling reaches the tool through the server:
// claude sends a call as one line, and a line longer than the server reads
// would end it, taking every tool from the session.
func TestABriefNearTheCeilingPassesThroughTheServer(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "id": "a"}))
	srv := &mcp.Server{Tools: []mcp.Tool{BriefPublish(f.path, "")}, Bind: bound(sid)}
	doc := map[string]any{"id": "a", "title": "A", "lede": strings.Repeat("x", maxBrief-4096)}
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": PublishName, "arguments": map[string]any{"doc": doc}}})

	var out strings.Builder
	if err := srv.Serve(t.Context(), strings.NewReader(string(line)+"\n"), &out); err != nil {
		t.Fatalf("the server ended on a call of %d bytes: %v", len(line), err)
	}
	if !strings.Contains(out.String(), "Published as a") {
		t.Errorf("the server answered %.200q", out.String())
	}
	if got := f.request(t)["doc"].(map[string]any)["lede"]; len(got.(string)) != maxBrief-4096 {
		t.Errorf("the collector got a lede of %d bytes", len(got.(string)))
	}
}

// A removal is asked under the session and the directory of the claude the
// server serves: the collector removes only the briefs of that directory, and
// one asked without a directory would be taken for the person, who may remove
// any.
func TestARemovalIsHeldToTheDirectoryOfTheSession(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "dropped": "seven-questions"}))
	said, failed := callTool(t, BriefDelete(f.path), bound(sid), `{"id":"seven-questions","cwd":"/srv/other"}`)
	if failed || said != "seven-questions is off the shelf, with the answers given to it." {
		t.Errorf("the removal answered %q (error %v)", said, failed)
	}
	want := map[string]any{"drop": "seven-questions", "sessionId": sid, "cwd": "/srv/proj"}
	if got := f.request(t); !reflect.DeepEqual(got, want) {
		t.Errorf("the collector was asked %v", got)
	}
	if said, failed := callTool(t, BriefDelete(f.path), bound(sid), `{}`); !failed {
		t.Errorf("a removal naming no brief was answered %q", said)
	}
	f.quiet(t)
}
