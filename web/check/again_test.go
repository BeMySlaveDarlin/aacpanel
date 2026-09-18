package check

import (
	"os"
	"strings"
	"testing"
)

const heldComposer = "session evirma did not accept the input: nothing was typed: the session is " +
	"showing a screen of its own, not its composer; it ends with: \"1 to review · 2 to send · 0 to dismiss\". " +
	"Press Esc in the session to get back to its composer, then send again"

const wentAstray = "session evirma did not accept the input: the session left its composer while the " +
	"message was going in: the session is showing a screen of its own, not its composer — whether it " +
	"arrived is unknown. Press Esc there and send it again"

const savedTo = "; the file itself was saved to /home/x/.local/share/aacpanel-exec/files/20260918-101010-ab12cd-shot.png"

// A second attempt is offered against one refusal only: the one that promises
// the message was never typed. Every other refusal leaves it unknown whether
// the words arrived, and a button that sends them again there says everything
// twice. A file is not asked for a second time either — it lies on the host,
// and what goes out is the path it landed at.
func TestASecondAttemptOnlyFollowsARefusalThatPromisesNothingWentOut(t *testing.T) {
	text := map[string]any{"name": "evirma", "text": "restart the router", "files": 0}
	file := map[string]any{"name": "evirma", "text": "look at this", "files": 1}

	got := runModuleJS(t, "src/screens/chat/again.js", "resend", [][]any{
		{text, heldComposer},
		{text, wentAstray},
		{file, heldComposer + savedTo},
		{file, heldComposer},
	})

	said := func(one any) string {
		row, ok := one.(map[string]any)
		if !ok {
			return ""
		}
		return row["text"].(string)
	}

	if said(got[0]) != "restart the router" {
		t.Errorf("a message refused with nothing typed gives back %v — there is nothing for the button to send", got[0])
	}
	if got[1] != nil {
		t.Errorf("a message that may have arrived is offered again as %v — pressing that says it twice", got[1])
	}
	if want := "look at this\n/home/x/.local/share/aacpanel-exec/files/20260918-101010-ab12cd-shot.png"; said(got[2]) != want {
		t.Errorf("the second attempt at a file sends %q, expected %q — the path is on the host already, "+
			"and the phone has no business uploading it twice", said(got[2]), want)
	}
	if got[3] != nil {
		t.Errorf("a file the host never saved is offered again as %v — there is no path to send", got[3])
	}

	// The panel reads the refusal to tell one from another, and the words are
	// the executor's. A test that only reads the panel would keep passing while
	// the two drift apart.
	host := read(t, "internal/executor/terminal.go")
	if !strings.Contains(host, `"nothing was typed: %s.`) {
		t.Error("the executor no longer refuses with \"nothing was typed\" — the panel offers no second attempt at all now")
	}
	saved := read(t, "internal/executor/file.go")
	if !strings.Contains(saved, `"; the file itself was saved to "`) {
		t.Error("the executor no longer names where a single file landed — the second attempt has no path to send")
	}
	if !strings.Contains(saved, `"; what was saved to disk: %s"`) {
		t.Error("the executor no longer names where several files landed — the second attempt has no paths to send")
	}
}

func read(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(repoPath(rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(raw)
}
