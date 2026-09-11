package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/launcher"
)

func launchDir(t *testing.T, name string) (root, dir string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(projectRootsEnv, root)
	return root, dir
}

func openWith(kind action.Kind, p *action.Project, resume string) action.Request {
	r := req(kind, p.Session)
	r.Project = p
	r.Resume = resume
	return r
}

func TestSessionOpenTakesProjectFromService(t *testing.T) {
	ctx := context.Background()
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)

	log := fakeLauncher(t, launcher.Report{Session: "aacpanel", Konsole: 1234, Agent: 1235})
	e, _ := newTest(t, "")

	detail, err := e.Execute(ctx, openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel", Launch: json.RawMessage(`{"room":"home"}`),
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	if !strings.Contains(args, `"dir":"`+dir+`"`) {
		t.Errorf("the launcher was called with %q — the path must come from the request of the service", args)
	}
	if !strings.Contains(args, `"session":"aacpanel"`) {
		t.Errorf("the session name did not reach the launcher: %q", args)
	}
	if !strings.Contains(detail, "aacpanel") {
		t.Errorf("the reply %q has no session name", detail)
	}
	if strings.Contains(detail, "WARNING") {
		t.Errorf("the reply complains about nothing at all: %q", detail)
	}
}

func TestSessionOpenChecksPathOnHost(t *testing.T) {
	ctx := context.Background()

	cases := map[string]func(t *testing.T, root string) string{
		"outside the allowed roots": func(t *testing.T, root string) string {
			outside, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return outside
		},
		"there is no such directory": func(t *testing.T, root string) string {
			return filepath.Join(root, "no-such-dir")
		},
		"it is a file, not a directory": func(t *testing.T, root string) string {
			file := filepath.Join(root, "file")
			if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
			return file
		},
		"the link leads outside the root": func(t *testing.T, root string) string {
			away, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "link")
			if err := os.Symlink(away, link); err != nil {
				t.Fatal(err)
			}
			return link
		},
	}

	for name, make := range cases {
		t.Run(name, func(t *testing.T) {
			root, dir := launchDir(t, "aacpanel")
			path := make(t, root)
			trusted := map[string]bool{dir: true, path: true}
			if real, err := filepath.EvalSymlinks(path); err == nil {
				trusted[real] = true
			}
			trustFile(t, trusted)
			t.Setenv(projectRootsEnv, root)
			log := fakeLauncher(t, launcher.Report{Session: "aacpanel", Konsole: 1, Agent: 2})
			e, _ := newTest(t, "")

			if _, err := e.Execute(ctx, openWith(action.SessionOpen, &action.Project{
				Path: path, Session: "aacpanel",
			}, "")); err == nil {
				t.Fatalf("the path %s was accepted though it will not do", path)
			}
			if _, err := os.ReadFile(log); err == nil {
				t.Error("the launcher was called after all: the refusal came after the console had started")
			}
		})
	}
}

func TestProjectRootsDefaultToHomeOnly(t *testing.T) {
	blind := func(t *testing.T) {
		t.Helper()
		t.Setenv(hostcfg.PathEnv, filepath.Join(t.TempDir(), "no-description.env"))
		t.Setenv(projectRootsEnv, "")
	}

	t.Run("the default is home", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		blind(t)

		got := projectRoots()
		if len(got) != 1 || got[0] != home {
			t.Fatalf("default roots %v, expected home %s only", got, home)
		}
	})

	t.Run("neither one nor the other", func(t *testing.T) {
		t.Setenv("HOME", "")
		blind(t)

		_, err := checkProjectDir("/srv/proj/shop")
		if err == nil {
			t.Fatal("a path was accepted with an empty list of roots")
		}
		if !strings.Contains(err.Error(), projectRootsEnv) {
			t.Errorf("the refusal %q does not name the reason — no roots are set at all", err)
		}
	})
}

func TestSessionOpenAcceptsGoodPath(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	log := fakeLauncher(t, launcher.Report{Session: "aacpanel", Konsole: 1, Agent: 2})
	e, _ := newTest(t, "")

	if _, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel",
	}, "")); err != nil {
		t.Fatalf("a valid path was rejected: %v", err)
	}
	if _, err := os.ReadFile(log); err != nil {
		t.Errorf("the launcher was not called: %v", err)
	}
}

func TestSessionLaunchParamsReachLauncher(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	log := fakeLauncher(t, launcher.Report{Session: "aacpanel", Konsole: 1, Agent: 2})
	e, _ := newTest(t, "")

	const params = `{"args":["--add-dir","/opt/x"],"effort":"high","env":{"FOO":"bar"},` +
		`"model":"opus","permissionMode":"plan","remoteControl":true,"room":"work"}`

	if _, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel", Launch: json.RawMessage(params),
	}, "")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"model":"opus"`, `"effort":"high"`, `"permissionMode":"plan"`,
		`"remoteControl":true`, `"env":{"FOO":"bar"}`, `"args":["--add-dir","/opt/x"]`,
		`"room":"work"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the parameter %s did not reach the launcher: %s", want, raw)
		}
	}
}

func TestLaunchParamsNeverGoThroughArgv(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	fakeLauncher(t, launcher.Report{Session: "aacpanel", Konsole: 1, Agent: 2})
	unit := fakeSystemdRun(t)
	e, _ := newTest(t, "")

	const secret = "s3cr3t-from-the-map"
	if _, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel",
		Launch: json.RawMessage(`{"env":{"GITLAB_TOKEN":"` + secret + `"}}`),
	}, "")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Errorf("a secret from the map went into the systemd-run command line — anyone sees it in ps: %s", raw)
	}
}

func TestSessionReportWarningsAreNotSwallowed(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	fakeLauncher(t, launcher.Report{
		Session: "aacpanel", Konsole: 1, Agent: 2,
		Leaks:    []string{"CLAUDECODE"},
		Warnings: []string{"the launcher does not know these parameters: model"},
	})
	e, _ := newTest(t, "")

	detail, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel",
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "CLAUDECODE") || !strings.Contains(detail, "transcript") {
		t.Errorf("the reply %q says nothing about the leak — the session came up but will write no history", detail)
	}
	if !strings.Contains(detail, "does not know these parameters") {
		t.Errorf("the reply %q lost the warning from the launcher", detail)
	}
}

func TestSessionResumeTakesProjectFromService(t *testing.T) {
	ctx := context.Background()
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	log := fakeLauncher(t, launcher.Report{Session: "aacpanel-2", Konsole: 1, Agent: 2})
	e, _ := newTest(t, "")

	const uuid = "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"
	detail, err := e.Execute(ctx, openWith(action.SessionResume, &action.Project{
		Path: dir, Session: "aacpanel",
	}, uuid))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if args := string(raw); !strings.Contains(args, `"dir":"`+dir+`"`) ||
		!strings.Contains(args, `"resume":"`+uuid+`"`) {
		t.Errorf("the launcher was called with %q — expected the path from the request and the conversation uuid", args)
	}
	if !strings.Contains(detail, uuid) {
		t.Errorf("the reply %q does not show that this continues a conversation", detail)
	}
}

func TestSessionCloseSkipsDaemonFork(t *testing.T) {
	ctx := context.Background()
	fork := []string{"claude", "--session-id", "e6de7b84-c862-4699-b4dc-14af140ca041",
		"--fork-session", "--resume", "/home/x/.claude/projects/-opt-x/d63546eb.jsonl"}
	stand := func(t *testing.T, withConsole bool) {
		t.Helper()
		procs := []fakeProc{
			fakeProc{pid: 100, comm: "claude", args: fork, ppid: 1, cwd: "/srv/proj/Beta/rnd/probe"},
		}
		files := []fakeSession{{pid: 100, name: "probe", start: "1000", kind: "bg"}}
		if withConsole {
			procs = append(procs,
				fakeProc{pid: 1003, comm: "konsole", args: []string{"konsole"}, ppid: 1},
				fakeProc{pid: 1004, comm: "claude", args: []string{"claude", "-n", "probe", "--remote-control", "probe"},
					ppid: 1003, cwd: "/srv/proj/Beta/rnd/probe"},
			)
			files = append(files, fakeSession{pid: 1004, name: "probe", start: "1000"})
		}
		procFS(t, procs...)
		sessionFiles(t, files...)
	}

	t.Run("the signal goes to the console session, not to a namesake fork", func(t *testing.T) {
		stand(t, true)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{100: true, 1003: true, 1004: true}, map[int]int{1004: 1})

		if _, err := e.Execute(ctx, req(action.SessionClose, "probe")); err != nil {
			t.Fatal(err)
		}
		want := []string{"1003:terminated", "1004:terminated"}
		if strings.Join(log.sent, ",") != strings.Join(want, ",") {
			t.Errorf("signals %v, expected %v", log.sent, want)
		}
	})

	t.Run("a lone fork without a console is not a session", func(t *testing.T) {
		stand(t, false)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{100: true}, nil)

		_, err := e.Execute(ctx, req(action.SessionClose, "probe"))
		if err == nil {
			t.Fatal("a fork of the daemon passed for a session")
		}
		if !strings.Contains(err.Error(), "there is no session") {
			t.Errorf("the error %q does not say there is no such session", err)
		}
		if len(log.sent) != 0 {
			t.Errorf("signals %v went out to the fork", log.sent)
		}
	})
}
