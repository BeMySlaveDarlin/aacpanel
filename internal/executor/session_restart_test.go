package executor

import (
	"context"
	"os"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/launcher"
)

func TestSessionRestart(t *testing.T) {
	ctx := context.Background()

	stand := func(t *testing.T) string {
		t.Helper()
		home := t.TempDir()
		t.Setenv("HOME", home)
		procFS(t,
			fakeProc{pid: 300, comm: "konsole", args: []string{"konsole"}, ppid: 1},
			fakeProc{pid: 302, comm: "claude", args: []string{"claude", "-n", "host"}, ppid: 300, cwd: home},
			fakeProc{pid: 402, comm: "claude", args: []string{"claude", "-n", "probe"}, ppid: 1,
				cwd: "/srv/proj/Beta/rnd/probe"},
		)
		return home
	}

	t.Run("closes the main session gently and starts a new one in the home directory", func(t *testing.T) {
		home := stand(t)
		t.Setenv(hostcfg.HomeSessionEnv, "wg-lab")
		log := fakeLauncher(t, launcher.Report{Session: "host", Konsole: 500, Agent: 502})
		tmuxLog := fakeTmux(t, []string{"302 host:0.0"}, "")
		e, _ := newTest(t, "")
		sig := withSignals(t, e, map[int]bool{300: true, 302: true, 402: true}, map[int]int{302: 1})

		detail, err := e.Execute(ctx, req(action.SessionRestart, "host"))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"300:terminated", "302:terminated"}
		if strings.Join(sig.sent, ",") != strings.Join(want, ",") {
			t.Errorf("signals %v, expected %v — the old session has to go the gentle way, and only it", sig.sent, want)
		}
		if called := strings.Join(tmuxArgv(t, tmuxLog), " "); !strings.Contains(called, "kill-session -t host") {
			t.Errorf("the tmux session was not stopped (calls: %s) — the new one would fail to take the name", called)
		}

		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("the launcher was not called: %v", err)
		}
		spec := string(raw)
		if !strings.Contains(spec, `"dir":"`+home+`"`) {
			t.Errorf("the launcher was called with %q — the new session must start in the home directory", spec)
		}
		if !strings.Contains(spec, `"session":"host"`) {
			t.Errorf("the launcher was called with %q — the new session keeps the name of the one closed, "+
				"not the name from the machine description", spec)
		}
		if strings.Contains(spec, "resume") {
			t.Errorf("the launcher was asked to resume: %q — a restart from scratch starts with an empty context", spec)
		}
		for _, word := range []string{"transcript", "started", "empty context"} {
			if !strings.Contains(detail, word) {
				t.Errorf("the reply %q does not say %q", detail, word)
			}
		}
	})

	t.Run("a session outside the home directory is refused before anything is signalled", func(t *testing.T) {
		stand(t)
		log := fakeLauncher(t, launcher.Report{Session: "probe", Konsole: 1, Agent: 2})
		e, _ := newTest(t, "")
		sig := withSignals(t, e, map[int]bool{402: true}, nil)

		_, err := e.Execute(ctx, req(action.SessionRestart, "probe"))
		if err == nil {
			t.Fatal("a project session was restarted without its launch parameters")
		}
		if !strings.Contains(err.Error(), "main session") {
			t.Errorf("the refusal %q does not say the restart is for the main session", err)
		}
		if len(sig.sent) != 0 {
			t.Errorf("signals were sent before the refusal: %v", sig.sent)
		}
		if _, err := os.ReadFile(log); err == nil {
			t.Error("the launcher was called after the refusal")
		}
	})

	t.Run("a project session restarts with the project the panel names", func(t *testing.T) {
		stand(t)
		root := t.TempDir()
		dir := root + "/probe"
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv(projectRootsEnv, root)
		log := fakeLauncher(t, launcher.Report{Session: "probe", Konsole: 1, Agent: 2})
		fakeTmux(t, nil, "")
		e, _ := newTest(t, "")
		sig := withSignals(t, e, map[int]bool{402: true}, map[int]int{402: 1})

		r := req(action.SessionRestart, "probe")
		r.Project = &action.Project{Path: dir, Session: "probe",
			Launch: []byte(`{"model":"opus","remoteControl":true,"intent":"Continue"}`)}
		detail, err := e.Execute(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(sig.sent, ",") != "402:terminated" {
			t.Errorf("signals %v — the old session goes the gentle way, and only it", sig.sent)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("the launcher was not called: %v", err)
		}
		spec := string(raw)
		for _, want := range []string{`"dir":"` + dir + `"`, `"session":"probe"`, `"model":"opus"`, `"intent":"Continue"`} {
			if !strings.Contains(spec, want) {
				t.Errorf("the launcher was called with %s — without %s the session comes up in another setup", spec, want)
			}
		}
		if strings.Contains(spec, "resume") {
			t.Errorf("the launcher was asked to resume: %s", spec)
		}
		if !strings.Contains(detail, "project's parameters") {
			t.Errorf("the reply %q does not say the session came back with its project's parameters", detail)
		}
	})

	t.Run("nothing is started while the old session is still alive", func(t *testing.T) {
		stand(t)
		log := fakeLauncher(t, launcher.Report{Session: "host", Konsole: 1, Agent: 2})
		e, _ := newTest(t, "")
		sig := withSignals(t, e, map[int]bool{300: true, 302: true}, nil)

		_, err := e.Execute(ctx, req(action.SessionRestart, "host"))
		if err == nil {
			t.Fatal("the old session did not close, yet the restart passed for success")
		}
		if !strings.Contains(err.Error(), "nothing was started") || !strings.Contains(err.Error(), "kill -9") {
			t.Errorf("the error %q does not say where the restart stopped and what is left", err)
		}
		for _, s := range sig.sent {
			if strings.HasSuffix(s, ":killed") {
				t.Errorf("the restart moved to KILL on its own: %v", sig.sent)
			}
		}
		if _, err := os.ReadFile(log); err == nil {
			t.Error("the launcher was called next to a session that is still alive — two namesakes would come up")
		}
	})

	t.Run("a failed start is reported together with the close that already happened", func(t *testing.T) {
		stand(t)
		failLauncher(t, "tmux did not start session host")
		e, _ := newTest(t, "")
		withSignals(t, e, map[int]bool{300: true, 302: true}, map[int]int{302: 1})

		_, err := e.Execute(ctx, req(action.SessionRestart, "host"))
		if err == nil {
			t.Fatal("the launcher failed, yet the restart passed for success")
		}
		if !strings.Contains(err.Error(), "closed") || !strings.Contains(err.Error(), "tmux did not start session host") {
			t.Errorf("the error %q hides either that the old session is gone or why the new one did not come up", err)
		}
	})

	t.Run("an unknown session — a refusal with the list", func(t *testing.T) {
		stand(t)
		e, _ := newTest(t, "")
		sig := withSignals(t, e, map[int]bool{}, nil)

		_, err := e.Execute(ctx, req(action.SessionRestart, "nosuchsession"))
		if err == nil {
			t.Fatal("a session that does not exist was restarted")
		}
		if !strings.Contains(err.Error(), "host") || !strings.Contains(err.Error(), "probe") {
			t.Errorf("the refusal %q has no list of running sessions", err)
		}
		if len(sig.sent) != 0 {
			t.Errorf("signals went out to nobody: %v", sig.sent)
		}
	})
}
