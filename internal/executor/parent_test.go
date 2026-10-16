package executor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
	"aacpanel/internal/stream"
)

// A live claude session is stood under another by a file of its process: the
// name of the parent and the start of the process, which the collector holds
// against the process it reads, so a pid used again by another process takes
// nothing over. A session in a terminal and one on the stream alike.
func TestALiveSessionIsStoodUnderAnother(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	procFS(t,
		fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5555",
			args: []string{"claude", "-p", "--input-format", "stream-json", "-n", "demo", "--session-id", streamSID}},
		fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
			args: []string{"claude", "-n", "term"}})
	sessionFiles(t,
		fakeSession{pid: 5001, name: "demo", start: "5555", sid: streamSID},
		fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")

	for _, c := range []struct {
		target string
		pid    int
		start  string
	}{{"term", 5002, "5556"}, {"demo", 5001, "5555"}} {
		r := req(action.SessionParent, c.target)
		r.Parent = "lead"
		detail, err := e.Execute(context.Background(), r)
		if err != nil || !strings.Contains(detail, c.target+" stands under lead") {
			t.Fatalf("%s: %q, %v", c.target, detail, err)
		}
		raw, err := os.ReadFile(stream.ProcessParentPath(c.pid))
		if err != nil {
			t.Fatalf("%s: the parent was not kept: %v", c.target, err)
		}
		var kept stream.ProcessParent
		if err := json.Unmarshal(raw, &kept); err != nil || kept.Session != "lead" || kept.Start != c.start {
			t.Errorf("%s: the file of its process holds %s, meant lead for the process started at %s",
				c.target, raw, c.start)
		}
	}
}

// The collector reads the parent where the executor keeps it, by the process
// it reads: the parent of the process kept for, and none for another process
// under the same pid.
func TestTheCollectorReadsTheParentTheExecutorKept(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 was not found: the collector is read in its own language")
	}
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	agent, err := filepath.Abs(filepath.Join("..", "..", "agent"))
	if err != nil {
		t.Fatal(err)
	}
	// The collector tells a session taken from under its parent, '', from a
	// process the executor kept no word of, None: only the second goes on to
	// the parent in the environment.
	for _, c := range []struct {
		kind   action.Kind
		parent string
		read   string
	}{{action.SessionParent, "lead", "'lead'"}, {action.SessionDetach, "", "''"}} {
		r := req(c.kind, "term")
		r.Parent = c.parent
		if _, err := e.Execute(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		for start, want := range map[string]string{"5556": c.read, "5557": "None"} {
			read := exec.Command(python, "-I", "-B", "-c", "import sys; sys.path.insert(0, sys.argv[1]); import held; "+
				"print(repr(held.process_parent(5002, sys.argv[2])))", agent, start)
			read.Env = append(os.Environ(), "XDG_STATE_HOME="+state)
			out, err := read.Output()
			if err != nil {
				t.Fatalf("the collector did not read the parent: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != want {
				t.Errorf("after %s the collector reads the process started at %s as standing under %s, meant %s",
					c.kind, start, got, want)
			}
		}
	}
}

// A session that does not live is stood under nothing and taken from under
// nothing, and nothing is kept for a process that is not there.
func TestASessionThatDoesNotLiveIsStoodUnderNothing(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	under := req(action.SessionParent, "gone")
	under.Parent = "lead"
	for _, r := range []action.Request{under, req(action.SessionDetach, "gone")} {
		if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "no live session gone") {
			t.Errorf("%s of a session that does not live went through: %v", r.Kind, err)
		}
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Errorf("a parent was kept for nobody: %v", entries)
	}
}

// A live claude session is taken from under its parent by the same file of
// its process, which names none then: in place of the parent it was stood
// under later, and over the one it was opened with, which stays in its
// environment.
func TestALiveSessionIsTakenFromUnderItsParent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	under := req(action.SessionParent, "term")
	under.Parent = "lead"
	if _, err := e.Execute(context.Background(), under); err != nil {
		t.Fatal(err)
	}
	detail, err := e.Execute(context.Background(), req(action.SessionDetach, "term"))
	if err != nil || !strings.Contains(detail, "term stands on its own") {
		t.Fatalf("%q, %v", detail, err)
	}
	raw, err := os.ReadFile(stream.ProcessParentPath(5002))
	if err != nil {
		t.Fatalf("nothing was kept for the process: %v", err)
	}
	var kept stream.ProcessParent
	if err := json.Unmarshal(raw, &kept); err != nil || kept.Session != "" || kept.Start != "5556" {
		t.Errorf("the file of the process holds %s, meant no parent for the process started at 5556", raw)
	}
}

// A session taken from under its parent comes back on its own through a
// restart and a move, and one stood under another later comes back under
// that one, whatever parent the request names: the panel names the parent
// of its snapshot, which reads the file of the process a moment after the
// executor wrote it, and a move with the panel down names the parent the
// session was started with. A request about a session the executor kept no
// word of is taken as it is.
func TestTheNextProcessStartsWithTheParentKeptForTheProcess(t *testing.T) {
	for _, c := range []struct {
		name string
		kind action.Kind
		was  string
		want string
	}{
		{"taken from under its parent", action.SessionDetach, "", ""},
		{"stood under another", action.SessionParent, "review", "review"},
		{"left as it was", "", "", "lead"},
	} {
		keep := func(t *testing.T, e *Executor, target string) {
			t.Helper()
			if c.kind == "" {
				return
			}
			r := req(c.kind, target)
			r.Parent = c.was
			if _, err := e.Execute(context.Background(), r); err != nil {
				t.Fatal(err)
			}
		}
		t.Run("a restart of a session "+c.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			_, dir := launchDir(t, "probe")
			procFS(t, fakeProc{pid: 402, comm: "claude", args: []string{"claude", "-n", "probe"}, ppid: 1, cwd: dir,
				start: "4020"})
			sessionFiles(t, fakeSession{pid: 402, name: "probe", start: "4020", cwd: dir})
			log := fakeLauncher(t, launcher.Report{Session: "probe", Konsole: 1, Agent: 2})
			fakeTmux(t, nil, "")
			e, _ := newTest(t, "")
			withSignals(t, e, map[int]bool{402: true}, map[int]int{402: 1})
			keep(t, e, "probe")

			r := req(action.SessionRestart, "probe")
			r.Project = &action.Project{Path: dir, Session: "probe", Parent: "lead", Launch: []byte(`{}`)}
			if _, err := e.Execute(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if got := launchedSpec(t, log).Parent; got != c.want {
				t.Errorf("the restart started the session under %q, meant %q", got, c.want)
			}
		})
		t.Run("a move with the panel down of a session "+c.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			dir, f := streamStand(t, func(*stream.State) {})
			kept, err := json.Marshal(launcher.Spec{Dir: dir, Session: "demo", Parent: "lead"})
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.state.Launched = kept
			f.mu.Unlock()
			log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
			e, _ := newTest(t, "")
			withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})
			keep(t, e, "demo")

			r := req(action.SessionSwitch, "demo")
			r.Switch = &action.Switch{To: action.SwitchConsole}
			if _, err := e.Execute(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if got := launchedSpec(t, log).Parent; got != c.want {
				t.Errorf("the move started the session under %q, meant %q", got, c.want)
			}
		})
	}
}

// launchedSpec is the spec the fake launcher was given on its input.
func launchedSpec(t *testing.T, log string) launcher.Spec {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the launcher was not called: %v", err)
	}
	var spec launcher.Spec
	body := raw[strings.Index(string(raw), "{") : strings.LastIndex(string(raw), "}")+1]
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return spec
}

// A live codex thread is stood under a session as one the panel started for
// it stands: its state names the parent.
func TestALiveCodexThreadIsStoodUnderASession(t *testing.T) {
	_, e := onCodex(t, nil)
	r := action.Request{Kind: action.SessionParent, Target: codexName, Parent: "lead"}
	detail, err := e.Execute(context.Background(), r)
	if err != nil || !strings.Contains(detail, codexName+" stands under lead") {
		t.Fatalf("%q, %v", detail, err)
	}
	waitFor(t, "the state of the thread to name its parent", func() bool {
		raw, err := os.ReadFile(stream.StatePath(codexThread))
		return err == nil && strings.Contains(string(raw), `"parent":"lead"`)
	})
}

// A live codex thread is taken from under its session the same way it is
// stood under one: its state names no parent, and the file an executor started
// later reads its parent from names none either.
func TestALiveCodexThreadIsTakenFromUnderItsSession(t *testing.T) {
	_, e := onCodex(t, nil)
	under := action.Request{Kind: action.SessionParent, Target: codexName, Parent: "lead"}
	if _, err := e.Execute(context.Background(), under); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the state of the thread to name its parent", func() bool {
		raw, err := os.ReadFile(stream.StatePath(codexThread))
		return err == nil && strings.Contains(string(raw), `"parent":"lead"`)
	})
	detail, err := e.Execute(context.Background(), action.Request{Kind: action.SessionDetach, Target: codexName})
	if err != nil || !strings.Contains(detail, codexName+" stands on its own") {
		t.Fatalf("%q, %v", detail, err)
	}
	waitFor(t, "the state of the thread to name no parent", func() bool {
		raw, err := os.ReadFile(stream.StatePath(codexThread))
		return err == nil && !strings.Contains(string(raw), `"parent"`)
	})
	raw, err := os.ReadFile(stream.ParentPath(codexThread))
	if err != nil || strings.Contains(string(raw), "lead") {
		t.Errorf("the parent kept for the thread is %s (%v): an executor started later stands it under lead again", raw, err)
	}
}
