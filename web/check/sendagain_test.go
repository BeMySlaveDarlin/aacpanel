package check

import (
	"strings"
	"testing"
)

// A message refused because the session was showing a dialog of its own. The
// way out of it on a phone used to be a walk to the terminal: press Esc there,
// come back, send again, and for a photo also find it in the gallery once more.
// The row does the three of them, and stops at the first one that fails: a
// dialog Esc did not close is a screen the message must not be typed into.
func TestAMessageRefusedByADialogIsSentAgainFromTheRow(t *testing.T) {
	var got struct {
		Label     string `json:"label"`
		AfterFail string `json:"afterFail"`
		AfterOne  int    `json:"afterOne"`
		Sent      []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
		Patches []map[string]any `json:"patches"`
		Fits    bool             `json:"fits"`
		Height  float64          `json:"height"`
		Cls     string           `json:"cls"`
		Gone    bool             `json:"gone"`
		Failed  string           `json:"failed"`
	}
	runFixture(t, "sendagain.html", &got)

	if got.Failed != "" {
		t.Fatalf("%s: the refusal names Esc, and there is nothing on the row to press it with", got.Failed)
	}
	if !strings.Contains(got.Label, "Esc") {
		t.Errorf("the button says %q — it does not say it presses Esc, and pressing it is a guess", got.Label)
	}

	if !got.Fits {
		t.Errorf("the button is %.0f px tall and does not sit inside the bubble it belongs to — on a phone "+
			"it is either out of the message or too small for a thumb", got.Height)
	}
	if got.AfterOne != 1 {
		t.Fatalf("the first press sent %d requests: %+v — Esc that did not free the composer was followed "+
			"by the message anyway, into whatever screen is standing there", got.AfterOne, got.Sent)
	}
	if got.Sent[0].Kind != "session.escape" || got.Sent[0].Target != "evirma" {
		t.Fatalf("the first request was %s to %q — the press starts with Esc into the session that refused",
			got.Sent[0].Kind, got.Sent[0].Target)
	}
	if !strings.Contains(got.AfterFail, "Discard the draft") {
		t.Errorf("after the refused Esc the row shows %q — it does not carry the screen that is holding the "+
			"keyboard now, and the next press is as blind as the first", got.AfterFail)
	}

	if len(got.Sent) != 3 {
		t.Fatalf("the two presses sent %d requests: %+v", len(got.Sent), got.Sent)
	}
	if got.Sent[2].Kind != "session.send" {
		t.Fatalf("after the composer came back the row sent %s — the second step is the ordinary send",
			got.Sent[2].Kind)
	}
	text, _ := got.Sent[2].Params["text"].(string)
	if !strings.HasSuffix(text, "1000032475.png") {
		t.Errorf("the second attempt carries %q — the file lies on the host already, and its path is what "+
			"goes into the session", text)
	}
	if _, again := got.Sent[2].Params["files"]; again {
		t.Errorf("the second attempt uploads the file again: %+v — the phone sends the bytes twice and the "+
			"host keeps two copies", got.Sent[2].Params)
	}

	if len(got.Patches) != 1 || got.Patches[0]["state"] != "queued" {
		t.Fatalf("the row was told %v about the second attempt — a message that did arrive stays in the feed "+
			"as a failed bubble beside its own echo", got.Patches)
	}
	if strings.Contains(got.Cls, "failed") || !strings.Contains(got.Cls, "queued") {
		t.Errorf("after the send the bubble is %q — it still reads as the message that did not go out", got.Cls)
	}
	if !got.Gone {
		t.Error("the button is still under a message that went out: a second press would say it twice")
	}
}
