package stream

import (
	"strings"
	"testing"
)

// A letter of the panel goes to claude as a message that names another
// session as its origin, in the permission class of this session, so claude
// frames it as a letter rather than the person's words and a session that
// runs without prompts takes it at once; it is no message of the person's
// and waits in no queue the panel shows. An empty one is refused.
func TestALetterOfThePanelIsNotThePersonsWords(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	if reply := r.ask(Request{Op: OpLetter, Text: " "}); reply.OK {
		t.Error("an empty letter was taken")
	}
	envelope := "<cross-session-message from-name=\"observer ci\">\nhit\n</cross-session-message>"
	if reply := r.ask(Request{Op: OpLetter, Text: envelope}); !reply.OK {
		t.Fatalf("the letter was refused: %+v", reply)
	}
	got := r.eventually(`"origin":{"from":"","fromMode":"prompting","kind":"peer"}`)
	if !strings.Contains(got, `from-name=\"observer ci\"`) {
		t.Errorf("claude read the letter as\n%s", got)
	}
	if s := r.state(); len(s.Queue) != 0 || !s.Said {
		t.Errorf("a letter stands in the queue of the person's messages, or left the conversation unsaid: %+v", s)
	}
}

// The class a letter names is the one claude counts the session in: bypass
// for a session that runs tools without asking, plan mode entered from it
// among them, prompting for any other — by the mode claude named, and before
// it has, by the mode it was started in.
func TestALetterNamesThePermissionClassOfTheSession(t *testing.T) {
	for _, c := range []struct {
		mode string
		argv []string
		want string
	}{
		{"bypassPermissions", nil, "bypass"},
		{"", []string{"claude", "--permission-mode", "bypassPermissions"}, "bypass"},
		{"", []string{"claude", "--permission-mode=bypassPermissions"}, "bypass"},
		{"plan", []string{"claude", "--dangerously-skip-permissions"}, "bypass"},
		{"plan", []string{"claude", "--permission-mode", "bypassPermissions"}, "bypass"},
		{"plan", []string{"claude"}, "prompting"},
		{"auto", []string{"claude", "--allow-dangerously-skip-permissions"}, "prompting"},
		{"default", []string{"claude", "--permission-mode", "bypassPermissions"}, "prompting"},
		{"", []string{"claude"}, "prompting"},
	} {
		if got := peerClass(c.mode, c.argv); got != c.want {
			t.Errorf("mode %q started with %q is %s, meant %s", c.mode, c.argv, got, c.want)
		}
	}
}
