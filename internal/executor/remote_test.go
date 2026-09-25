package executor

import (
	"context"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

// remoteScreen is a terminal with claude's /remote-control on it, as claude
// 2.1.283 draws it: typed without the bridge it brings the bridge up; typed
// with it, it opens a dialog whose cursor stands on the line that keeps it.
type remoteScreen struct {
	up      bool
	dialog  bool
	at      int
	lines   []string
	blind   bool
	deaf    bool
	busyDlg bool
	typed   string
	sent    []string
}

func newRemoteScreen(up bool) *remoteScreen {
	return &remoteScreen{up: up, lines: []string{
		"Disconnect this session",
		"Show QR code   Scan with your phone to open this session",
		"Continue",
	}}
}

func (r *remoteScreen) kind() string  { return "tmux" }
func (r *remoteScreen) attempts() int { return 1 }

func (r *remoteScreen) send(_ context.Context, payload string) error {
	r.sent = append(r.sent, payload)
	switch {
	case r.dialog && payload == workUpKey:
		if r.at > 0 {
			r.at--
		}
	case r.dialog && payload == workDownKey:
		if r.at < len(r.lines)-1 {
			r.at++
		}
	case r.dialog && payload == escKey:
		r.dialog = false
	case r.dialog && payload == enterKey:
		r.dialog = false
		if strings.HasPrefix(r.lines[r.at], remoteDisconnect) {
			r.up = false
		}
	case payload == clearLine:
		r.typed = ""
	case strings.HasPrefix(payload, pasteStart):
		r.typed = strings.TrimSuffix(strings.TrimPrefix(payload, pasteStart), pasteEnd)
	case payload == enterKey && r.typed == remoteCommand && !r.deaf:
		r.typed = ""
		if r.up {
			r.dialog, r.at = true, len(r.lines)-1
		} else {
			r.up = true
		}
	}
	return nil
}

func (r *remoteScreen) screen(context.Context) (string, bool) {
	if r.blind {
		return "", false
	}
	if r.busyDlg {
		return "Do you want to proceed?\n❯ 1. Yes\n  2. No\n\nEsc to cancel\n", true
	}
	if !r.dialog {
		return "❯ " + r.typed + "\n", true
	}
	var b strings.Builder
	b.WriteString("▔▔▔▔▔▔▔▔\n   Remote Control\n\n")
	b.WriteString("   This session is available in the Claude mobile app and at\n")
	b.WriteString("   https://claude.ai/code/session_015aXCcdHpKReTaPGrYZVxrT.\n\n")
	for i, line := range r.lines {
		mark := "  "
		if i == r.at {
			mark = "❯ "
		}
		b.WriteString("   " + mark + line + "\n")
	}
	b.WriteString("\n   Enter to select · Esc to continue\n")
	return b.String(), true
}

func (r *remoteScreen) bridge() string {
	if r.up {
		return "session_015aXCcdHpKReTaPGrYZVxrT"
	}
	return ""
}

func (r *remoteScreen) pressed(key string) int {
	n := 0
	for _, s := range r.sent {
		if s == key {
			n++
		}
	}
	return n
}

func TestRemoteControlComesUpWithTheCommand(t *testing.T) {
	r := newRemoteScreen(false)
	bridge, err := switchRemote(t.Context(), r, true, r.bridge)
	if err != nil {
		t.Fatalf("remote control did not come up: %v", err)
	}
	if bridge != "session_015aXCcdHpKReTaPGrYZVxrT" {
		t.Errorf("the bridge read back is %q", bridge)
	}
	if len(r.sent) != 3 || r.sent[0] != clearLine || r.sent[1] != pasteStart+remoteCommand+pasteEnd || r.sent[2] != enterKey {
		t.Errorf("the terminal was sent %q", r.sent)
	}
}

func TestRemoteControlGoesThroughTheLineThatDisconnects(t *testing.T) {
	r := newRemoteScreen(true)
	bridge, err := switchRemote(t.Context(), r, false, r.bridge)
	if err != nil {
		t.Fatalf("remote control did not go: %v", err)
	}
	if bridge != "" || r.up {
		t.Fatalf("the bridge is still up: %q", bridge)
	}
	if r.pressed(workUpKey) != 2 || r.pressed(escKey) != 0 {
		t.Errorf("the cursor went up %d times and Esc was pressed %d times: %q",
			r.pressed(workUpKey), r.pressed(escKey), r.sent)
	}
}

// A dialog of other words is not guessed at: Esc keeps the bridge, and the
// refusal says so.
func TestAnUnknownDialogIsClosedAndTheBridgeStays(t *testing.T) {
	r := newRemoteScreen(true)
	r.lines = []string{"Leave this session", "Continue"}
	_, err := switchRemote(t.Context(), r, false, r.bridge)
	if err == nil || !strings.Contains(err.Error(), "bridge stays") {
		t.Fatalf("a dialog without the line to disconnect with went through: %v", err)
	}
	if !r.up || r.dialog || r.pressed(workUpKey) != 0 {
		t.Errorf("the bridge is up %v, the dialog open %v, keys %q", r.up, r.dialog, r.sent)
	}
}

func TestRemoteControlIsNotTypedBlind(t *testing.T) {
	r := newRemoteScreen(false)
	r.blind = true
	if _, err := switchRemote(t.Context(), r, true, r.bridge); err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("a blind switch went through: %v", err)
	}
	if len(r.sent) != 0 {
		t.Errorf("keys went into a screen nobody read: %q", r.sent)
	}
}

func TestRemoteControlIsNotTypedIntoADialog(t *testing.T) {
	r := newRemoteScreen(false)
	r.busyDlg = true
	if _, err := switchRemote(t.Context(), r, true, r.bridge); err == nil || !strings.Contains(err.Error(), "dialog") {
		t.Fatalf("the command went into a dialog: %v", err)
	}
	if len(r.sent) != 0 {
		t.Errorf("keys went into the dialog: %q", r.sent)
	}
}

func TestACommandTheSessionSwallowedIsNotReportedAsDone(t *testing.T) {
	r := newRemoteScreen(false)
	r.deaf = true
	if _, err := switchRemote(t.Context(), r, true, r.bridge); err == nil || !strings.Contains(err.Error(), "did not come up") {
		t.Fatalf("a swallowed command was reported as a bridge: %v", err)
	}
}

// On the stream the switch is claude's own request, and its answer carries
// the address of the session.
func TestRemoteControlOnTheStreamIsClaudesRequest(t *testing.T) {
	f := onTheStream(t, false)
	f.answers = map[string]string{"remote_control": `{"subtype":"success","response":` +
		`{"session_url":"https://claude.ai/code/session_011Ri3EESSEnkrtd1REeozCS","bridge_session_id":"cse_011Ri3EESSEnkrtd1REeozCS"}}`}
	e, _ := newTest(t, "")
	on := true
	r := req(action.SessionRemote, "demo")
	r.Remote = &on
	detail, err := e.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	asked := f.asked()
	if len(asked) != 1 || asked[0].Subtype != "remote_control" || asked[0].Fields["enabled"] != true || len(asked[0].Fields) != 1 {
		t.Fatalf("the holder was asked %+v", asked)
	}
	if !strings.Contains(detail, "https://claude.ai/code/session_011Ri3EESSEnkrtd1REeozCS") {
		t.Errorf("the report %q does not give the address of the session", detail)
	}
}

// A switch to where the session already stands asks nothing of it.
func TestRemoteControlAlreadyThereAsksNothing(t *testing.T) {
	f := onTheStream(t, false)
	sessionFiles(t, fakeSession{pid: 5001, name: "demo", start: "5555", sid: streamSID, bridge: "session_0Up"})
	e, _ := newTest(t, "")
	on := true
	r := req(action.SessionRemote, "demo")
	r.Remote = &on
	detail, err := e.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if asked := f.asked(); len(asked) != 0 {
		t.Errorf("claude was asked %+v for a bridge that is up", asked)
	}
	if !strings.Contains(detail, "already on") || !strings.Contains(detail, "session_0Up") {
		t.Errorf("the report %q", detail)
	}
}
