package host

import "testing"

func TestLiveCodexSessionIsFoundByTitleAndStableAddress(t *testing.T) {
	r := readerWith(t, `{"sessions":[{"session":"codex-00000001","sessionId":"thread-one","agent":"codex","title":"review"}]}`)
	for _, name := range []string{"review", "codex-00000001"} {
		if got, ok := r.LiveSession(name); !ok || got.SessionID != "thread-one" || got.Name != "codex-00000001" {
			t.Errorf("%s resolved to %+v, %t", name, got, ok)
		}
	}
	for _, rows := range []string{
		`{"session":"codex-00000002","sessionId":"thread-two","agent":"codex","title":"review"}`,
		`{"session":"review","sessionId":"claude-one"}`,
	} {
		r := readerWith(t, `{"sessions":[{"session":"codex-00000001","sessionId":"thread-one","agent":"codex","title":"review"},`+rows+`]}`)
		if got, ok := r.LiveSession("review"); ok {
			t.Errorf("an ambiguous name resolved to %+v", got)
		}
	}
}
