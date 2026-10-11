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
	r := req(action.SessionParent, "term")
	r.Parent = "lead"
	if _, err := e.Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	agent, err := filepath.Abs(filepath.Join("..", "..", "agent"))
	if err != nil {
		t.Fatal(err)
	}
	for start, want := range map[string]string{"5556": "lead", "5557": ""} {
		read := exec.Command(python, "-I", "-B", "-c", "import sys; sys.path.insert(0, sys.argv[1]); import held; "+
			"print(held.process_parent(5002, sys.argv[2]))", agent, start)
		read.Env = append(os.Environ(), "XDG_STATE_HOME="+state)
		out, err := read.Output()
		if err != nil {
			t.Fatalf("the collector did not read the parent: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("the collector reads the process started at %s as standing under %q, meant %q", start, got, want)
		}
	}
}

// A session that does not live is stood under nothing, and nothing is kept
// for a process that is not there.
func TestASessionThatDoesNotLiveIsStoodUnderNothing(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	r := req(action.SessionParent, "gone")
	r.Parent = "lead"
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "no live session gone") {
		t.Errorf("a session that does not live was stood under another: %v", err)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Errorf("a parent was kept for nobody: %v", entries)
	}
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
