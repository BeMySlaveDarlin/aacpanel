package collector

import (
	"reflect"
	"strings"
	"testing"
)

// The call goes out under the conversation of the claude the server serves,
// which is what the push opens, with the line as one line.
func TestTheCallGoesOutUnderTheSessionTheServerServes(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true}))
	said, failed := callTool(t, Notify(f.path), bound(sid),
		`{"text":"  stuck on the migration:\n the checksum does not match  ","sessionId":"forged"}`)
	if failed || said != "The person has been called: the push carries this line and opens this session." {
		t.Errorf("the call answered %q (error %v)", said, failed)
	}
	want := map[string]any{"sessionId": sid, "text": "stuck on the migration: the checksum does not match"}
	if got := f.request(t); !reflect.DeepEqual(got, want) {
		t.Errorf("the collector was handed %v", got)
	}
}

// A line over what a push carries is cut here, where the model hears of it:
// the collector reads a call in one short read and would refuse a page as
// not parsed.
func TestALongLineIsCutAndTheModelToldSo(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true}))
	line := strings.Repeat("ü", maxCall+50)
	said, failed := callTool(t, Notify(f.path), bound(sid), `{"text":"`+line+`"}`)
	if failed || !strings.HasSuffix(said, "The line was 350 characters, and the push carries the first 300.") {
		t.Errorf("a long line was answered %q (error %v)", said, failed)
	}
	if got := f.request(t)["text"]; got != strings.Repeat("ü", maxCall) {
		t.Errorf("the collector was handed a line of %d bytes", len(got.(string)))
	}
}

// An empty line calls nobody: there is nothing in it to act on.
func TestAnEmptyLineCallsNobody(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true}))
	for _, args := range []string{`{"text":"  \n "}`, `{}`} {
		if said, failed := callTool(t, Notify(f.path), bound(sid), args); !failed || !strings.HasPrefix(said, "Nobody was called") {
			t.Errorf("%s was answered %q (error %v)", args, said, failed)
		}
	}
	f.quiet(t)
}

// The collector takes one call a minute from a session, and its refusal comes
// back with the time left, so the model can gather what it has into one line.
func TestARefusalOfTheCallComesBackWithItsReason(t *testing.T) {
	why := "this session called 12 s ago: one call a minute, 48 s left"
	f := listen(t, answers(map[string]any{"ok": false, "error": why}))
	said, failed := callTool(t, Notify(f.path), bound(sid), `{"text":"stuck"}`)
	if !failed || said != "Nobody was called: "+why+"." {
		t.Errorf("the refusal came back as %q (error %v)", said, failed)
	}
}
