package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	callerThread = "019a1f00-0000-7000-8000-00000000abcd"
	otherThread  = "019a1f00-0000-7000-8000-00000000ef01"
)

// holdsLocks gives a codex of the fake /proc the open files a codex holds
// while it writes its threads: a lock per thread under its home, and its
// rollouts beside them.
func holdsLocks(t *testing.T, root string, pid string, home string, threads ...string) {
	t.Helper()
	fds := filepath.Join(root, pid, "fd")
	if err := os.MkdirAll(fds, 0o755); err != nil {
		t.Fatal(err)
	}
	links := []string{"/dev/null", filepath.Join(home, "sessions", "2026", "10", "11", "rollout-x-"+otherThread+".jsonl")}
	for _, thread := range threads {
		links = append(links, filepath.Join(home, codexLocks, thread+".lock"))
	}
	for i, target := range links {
		if err := os.Symlink(target, filepath.Join(fds, string(rune('3'+i)))); err != nil {
			t.Fatal(err)
		}
	}
}

func metaOf(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A daemon holds many threads and starts a server for each: the caller is the
// thread the call names, confirmed by the lock the codex holds on it, with the
// home the lock lies in and the directory codex started the server in. It is
// named as the panel names a codex session.
func TestACodexCallerIsTheThreadTheCallNames(t *testing.T) {
	root := fakeProc(t, fproc{pid: 800, comm: "codex", args: []string{"codex", "app-server", "--listen", "unix://"}})
	home := filepath.Join(t.TempDir(), ".codex-profiles", "acme")
	holdsLocks(t, root, "800", home, otherThread, callerThread)
	dir := t.TempDir()
	t.Chdir(dir)

	if !IsCodex(800) {
		t.Fatal("a codex process is not told for one")
	}
	b, err := CodexCaller(800, metaOf(t, map[string]any{"threadId": callerThread, "callId": "call_1",
		"x-codex-turn-metadata": map[string]any{"thread_id": callerThread}}))
	if err != nil {
		t.Fatal(err)
	}
	if !b.Codex || b.SessionID != callerThread || b.Name != "codex-0000abcd" || b.PID != 800 ||
		b.Place.ConfigDir != home || b.Place.Dir != dir {
		t.Errorf("the caller is %+v", b)
	}
}

// The thread is not taken on the call's word: a call that names none, one that
// names a thread the parent holds no lock of, or something that is no id of a
// thread has no caller — and nothing stands in for it.
func TestACodexCallWithoutItsThreadHasNoCaller(t *testing.T) {
	root := fakeProc(t,
		fproc{pid: 800, comm: "codex", args: []string{"codex", "app-server"}},
		fproc{pid: 900, comm: "claude", args: []string{"claude"}})
	home := filepath.Join(t.TempDir(), ".codex")
	holdsLocks(t, root, "800", home, callerThread)
	holdsLocks(t, root, "900", home, otherThread)
	if err := os.Symlink(filepath.Join(home, "elsewhere", callerThread+".lock"), filepath.Join(root, "900", "fd", "9")); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct {
		parent int
		meta   json.RawMessage
		says   string
	}{
		"no _meta":                {800, nil, "names no codex thread"},
		"a _meta with no thread":  {800, metaOf(t, map[string]any{"callId": "call_1"}), "names no codex thread"},
		"a _meta that is a list":  {800, json.RawMessage(`["` + callerThread + `"]`), "names no codex thread"},
		"a thread of another":     {800, metaOf(t, map[string]any{"threadId": otherThread}), "holds no thread " + otherThread},
		"a path for a thread":     {800, metaOf(t, map[string]any{"threadId": "../" + callerThread}), "named by a uuid"},
		"a parent that has none":  {901, metaOf(t, map[string]any{"threadId": callerThread}), "holds no thread"},
		"a lock out of its place": {900, metaOf(t, map[string]any{"threadId": callerThread}), "holds no thread"},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := CodexCaller(c.parent, c.meta)
			if err == nil || !strings.Contains(err.Error(), c.says) {
				t.Errorf("found %+v, %v; meant an error saying %q", b, err, c.says)
			}
		})
	}
	if IsCodex(900) {
		t.Error("a claude is told for a codex")
	}
}
