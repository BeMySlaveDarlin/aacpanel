package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
	"aacpanel/internal/sessionintent"
)

func TestLaunchUsesTheMessageOfItsFinalName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	config, dir := t.TempDir(), t.TempDir()
	place := mcp.Place{ConfigDir: config, Dir: dir}
	for name, messages := range map[string]sessionintent.Values{
		"person":   {"intent": "Wrong neighbour", "restartIntent": "Wrong neighbour"},
		"person-2": {"intent": "Read the queue", "restartIntent": "Continue"},
	} {
		if err := sessionintent.Keep(sessionintent.Dir(), place, name, messages); err != nil {
			t.Fatal(err)
		}
	}
	start := func(key, inherited string) string {
		t.Helper()
		proc := fakeProc(t, fproc{pid: 300, comm: "claude", args: []string{"claude", "-n", "person"}})
		fakeTmuxLauncher(t, proc, "person-2", 200, 4242)
		machineClaude(t)
		t.Setenv(terminalAutoEnv, "0")
		raw, _ := json.Marshal(map[string]string{"intent": inherited})
		rep, err := Run(context.Background(), Spec{Dir: dir, Session: "person", ConfigDir: config,
			IntentKey: key, Launch: raw})
		if err != nil {
			t.Fatal(err)
		}
		return rep.Intent
	}
	for _, c := range []struct{ key, inherited, want string }{
		{"intent", "Project", "Read the queue"},
		{"restartIntent", "Summary", "Continue"},
		{"", "Model's words", "Model's words"},
		{"", "", ""},
	} {
		if got := start(c.key, c.inherited); got != c.want {
			t.Errorf("%q over %q starts with %q, want %q", c.key, c.inherited, got, c.want)
		}
	}
	if err := sessionintent.Keep(sessionintent.Dir(), place, "person-2", sessionintent.Values{
		"intent": "", "restartIntent": "",
	}); err != nil {
		t.Fatal(err)
	}
	for _, inherited := range []string{"Summary", ""} {
		if got := start("restartIntent", inherited); got != inherited {
			t.Errorf("a cleared override starts with %q instead of %q", got, inherited)
		}
	}
}

// Losing an override must not leave a session closed after a restart. The
// inherited message still reaches the launched session, and the report says
// why its own message could not be used.
func TestUnreadableSessionMessagesWarnWithoutBlockingTheLaunch(t *testing.T) {
	for _, c := range []struct {
		name, key, inherited, raw, why string
	}{
		{"corrupt first", sessionintent.First, "Read the queue", `{"values":`, "unexpected end of JSON input"},
		{"corrupt restart", sessionintent.Restart, "Summary", `{"values":`, "unexpected end of JSON input"},
		{"another session", sessionintent.Restart, "Summary",
			`{"configDir":"/srv/other","repo":"/srv/other","name":"other","values":{"restartIntent":"Wrong"}}`,
			"the messages belong to another session"},
		{"no inherited message", sessionintent.Restart, "", `{"values":`, "unexpected end of JSON input"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			config, dir := t.TempDir(), t.TempDir()
			place := mcp.Place{ConfigDir: config, Dir: dir}
			if err := os.MkdirAll(sessionintent.Dir(), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sessionintent.Dir(), role.File(place, "person")), []byte(c.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			proc := fakeProc(t)
			fakeTmuxLauncher(t, proc, "person", 200, 4242)
			machineClaude(t)
			t.Setenv(terminalAutoEnv, "0")
			raw, err := json.Marshal(map[string]string{"intent": c.inherited})
			if err != nil {
				t.Fatal(err)
			}
			rep, err := Run(context.Background(), Spec{Dir: dir, Session: "person", ConfigDir: config,
				IntentKey: c.key, Launch: raw})
			if err != nil {
				t.Fatalf("the unreadable override blocked the launch: %v", err)
			}
			if rep.Session != "person" || rep.Intent != c.inherited {
				t.Errorf("the launch came back as %q with %q, want person with %q", rep.Session, rep.Intent, c.inherited)
			}
			said := strings.Join(rep.Warnings, "; ")
			if !strings.Contains(said, "the session messages were not read: "+c.why) {
				t.Errorf("the report did not explain the lost override: %q", said)
			}
		})
	}
}
