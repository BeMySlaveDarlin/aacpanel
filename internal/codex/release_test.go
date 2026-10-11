package codex

import (
	"encoding/json"
	"os"
	"testing"

	"aacpanel/internal/codex/contract"
	"aacpanel/internal/stream"
)

func releaseOfFile(t *testing.T) (Release, bool) {
	t.Helper()
	raw, err := os.ReadFile(stream.CodexReleasePath("acme"))
	if err != nil {
		return Release{}, false
	}
	var out Release
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the release does not read: %v\n%s", err, raw)
	}
	return out, true
}

// The release the daemon names at the handshake is written by the contour
// beside the release of the reference, and a daemon that updated itself and
// came back is written again: the service compares the two.
func TestTheReleaseOfTheDaemonIsWrittenAtEveryHandshake(t *testing.T) {
	srv, _ := linked(t)
	until(t, "the release to be written", func() bool { _, ok := releaseOfFile(t); return ok })

	ref, err := contract.Reference()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := releaseOfFile(t)
	if got.Release != "0.162.0" || got.Reference != ref.Release() || got.Reference == "" ||
		got.Contour != "acme" || got.CodexHome != srv.Home || got.At == 0 {
		t.Errorf("the release reads as %+v, the reference is %q", got, ref.Release())
	}

	srv.Close()
	srv.Agent("aacpanel/0.163.0 (Debian 13.0.0; x86_64) xterm-256color (aacpanel; 1)")
	srv.Start(t)
	until(t, "the release the daemon came back with", func() bool {
		got, _ := releaseOfFile(t)
		return got.Release == "0.163.0"
	})
}

// The release is the number in the first word of the user agent, whichever
// client named the daemon first; a user agent of another shape names none.
func TestTheReleaseIsReadOffTheUserAgent(t *testing.T) {
	for agent, want := range map[string]string{
		"aacpanel/0.162.1 (Debian 13.0.0; x86_64) xterm-256color (aacpanel; 1)":     "0.162.1",
		"codex_vscode/0.163.0 (Mac OS 15.0.0; arm64) vscode/1.99 (codex_vscode; 2)": "0.163.0",
		"codex-tui/0.164.0-alpha.1": "0.164.0-alpha.1",
		"codex":                     "",
		"":                          "",
	} {
		if got := releaseOf(agent); got != want {
			t.Errorf("releaseOf(%q) = %q, expected %q", agent, got, want)
		}
	}
}
