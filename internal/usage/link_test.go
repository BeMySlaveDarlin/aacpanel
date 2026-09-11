package usage

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTruncatedStreamIsNotSuccess(t *testing.T) {
	client := fakeAgent(t, func(c net.Conn) {
		c.Write([]byte(`{"file":{"path":"/p/a.jsonl","contour":"home","size":10}}` + "\n"))
	})
	_, err := client.List(context.Background())
	if err == nil {
		t.Fatal("a truncated stream must be an error: without that half the work would pass for all of it")
	}
	if !strings.Contains(err.Error(), "broke off") {
		t.Errorf("the error is %q — it must name the break", err)
	}
}

func TestAgentRefusalReachesTheCaller(t *testing.T) {
	client := fakeAgent(t, func(c net.Conn) {
		c.Write([]byte(`{"error":"transcript parsing is not installed"}` + "\n"))
		c.Write([]byte(`{"end":true}` + "\n"))
	})
	_, err := client.List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "is not installed") {
		t.Fatalf("the error is %v, expected the reason from the agent", err)
	}
}

func TestWriteFailureStopsTheRound(t *testing.T) {
	client := fakeAgent(t, func(c net.Conn) {
		for i := 0; i < 100; i++ {
			c.Write([]byte(`{"result":{"path":"/p/a.jsonl","offset":10}}` + "\n"))
		}
		c.Write([]byte(`{"end":true}` + "\n"))
	})
	seen := 0
	boom := errors.New("the database refuses writes")
	err := client.Parse(context.Background(), []Task{{Path: "/p/a.jsonl"}}, func(Result) error {
		seen++
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("the error is %v, expected the one the writer returned", err)
	}
	if seen != 1 {
		t.Errorf("the handler was called %d times, expected one: after a refusal there is no point reading on", seen)
	}
}

func TestRealAgentDeliversNumbers(t *testing.T) {
	client, _ := agentUsage(t)

	files, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("the listing: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("%d files, expected one", len(files))
	}
	f := files[0]
	if f.Contour != "home" {
		t.Errorf("contour %q, expected home", f.Contour)
	}
	if len(f.Head) != 64 {
		t.Errorf("the file head is %q — expected sha256 as a hex string: it tells "+
			"a moved directory from a rewritten file", f.Head)
	}
	if f.First == "" {
		t.Error("there is no time of the first record — the tasks have nothing to be ordered by, and BRIN will not pay off")
	}
	if f.Size == 0 || f.Inode == 0 {
		t.Errorf("size %d, inode %d — they decide whether the file is read", f.Size, f.Inode)
	}

	var got []Result
	err = client.Parse(context.Background(), []Task{{Path: f.Path}}, func(r Result) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if len(got) != 1 || got[0].Error != "" {
		t.Fatalf("the answer is %+v, expected one parsed file", got)
	}
	res := got[0]
	if res.Session.SessionID == "" {
		t.Fatal("a session without an id: the rows have nothing to attach to")
	}
	if res.Session.Contour != "home" {
		t.Errorf("the session contour is %q, expected home: the host sets it from the root, not the parsing",
			res.Session.Contour)
	}
	if len(res.Rows) == 0 {
		t.Fatal("the transcript parsed without a single hour row")
	}

	var answers int
	var input, output, cacheRead int64
	hours := map[string]bool{}
	for _, r := range res.Rows {
		answers += r.Answers
		input += r.InputTokens
		output += r.OutputTokens
		cacheRead += r.CacheRead
		hours[r.Bucket] = true
		if r.Model == "" {
			t.Error("an hour row without a model: the model is part of the key")
		}
	}
	if res.Offset == 0 && len(hours) > 1 {
		t.Errorf("%d hours in the file while the boundary is at zero — the file would be reread whole", len(hours))
	}
	t.Logf("parsed: %d rows in %d hours, %d answers, boundary %d",
		len(res.Rows), len(hours), answers, res.Offset)
	if answers == 0 || output == 0 || input == 0 {
		t.Fatalf("%d answers, %d input tokens, %d output — the usage arrived as zeros",
			answers, input, output)
	}
	if cacheRead == 0 {
		t.Error("cache read came back zero: on live transcripts it is 95%% of the input")
	}

	file, err := toStore(res, f, 0)
	if err != nil {
		t.Fatalf("the conversion for the store: %v", err)
	}
	if len(file.Rows) != len(res.Rows) {
		t.Fatalf("%d rows of %d reached the store", len(file.Rows), len(res.Rows))
	}
	for _, r := range file.Rows {
		if r.Bucket.IsZero() {
			t.Fatal("the row hour did not parse — the row would land at the start of the epoch")
		}
	}
	if file.Session.StartedAt.IsZero() || file.Session.EndedAt.IsZero() {
		t.Error("the session time boundaries are empty")
	}
	if len(file.Scan.HeadSum) == 0 {
		t.Error("a scan point without the file head: a moved directory would become a full reread")
	}
	if file.Scan.Size == 0 || file.Scan.Inode == 0 {
		t.Error("a scan point without size and inode — they decide what is read next time")
	}
	var messages int
	for _, e := range file.Events {
		messages += e.Messages
	}
	if messages == 0 {
		t.Error("no session events arrived: there is nothing to count messages, compactions and interrupts by")
	}
}

func TestPathOutsideRootsIsRefused(t *testing.T) {
	client, dirs := agentUsage(t)

	outside := filepath.Join(dirs, "secret.jsonl")
	if err := os.WriteFile(outside, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dirs, "home", "projects", "-p", "link.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{outside, link, "/etc/passwd"} {
		var got []Result
		err := client.Parse(context.Background(), []Task{{Path: path}}, func(r Result) error {
			got = append(got, r)
			return nil
		})
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		if len(got) != 1 {
			t.Fatalf("%d answers for %s, expected one", len(got), path)
		}
		if got[0].Error == "" {
			t.Errorf("%s was parsed while it must have been refused: %+v", path, got[0])
		}
		if len(got[0].Rows) != 0 {
			t.Errorf("%s brought rows: %+v", path, got[0].Rows)
		}
	}
}

func fakeAgent(t *testing.T, reply func(net.Conn)) *Client {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "u.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("the socket in %s did not come up: %v", dir, err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 1<<20)
				c.SetReadDeadline(time.Now().Add(5 * time.Second))
				for {
					n, err := c.Read(buf)
					if n == 0 || err != nil {
						break
					}
				}
				reply(c)
			}()
		}
	}()
	return New(path)
}

func agentUsage(t *testing.T) (*Client, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 was not found: the end-to-end check of the agent is skipped")
	}

	dir := t.TempDir()
	root := filepath.Join(dir, "home", "projects", "-p")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	copyTranscript(t, root)

	sockDir := filepath.Join(dir, "run")
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(sockDir, "usage.sock")
	if len(sock) > 100 {
		t.Skipf("a socket name %d characters long does not fit into a unix path: set a shorter TMPDIR", len(sock))
	}

	cmd := exec.Command(python, "-c", "import usage_link; usage_link.worker()")
	cmd.Env = append(os.Environ(),
		"AACP_USAGE_DIR="+sockDir,
		"AACP_CLAUDE_HOME="+filepath.Join(dir, "home"),
		"AACP_CLAUDE_REGISTRY="+filepath.Join(dir, "no-registry.conf"),
		"AACP_STATE_DIR="+filepath.Join(dir, "state"),
		"PYTHONPATH="+filepath.Join("..", "..", "agent"),
		"HOME="+dir,
		"PYTHONDONTWRITEBYTECODE=1")
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("the agent did not start: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if _, err := os.Stat(sock); err == nil {
			return New(sock), dir
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the agent never brought the socket up in ten seconds")
	return nil, ""
}

func copyTranscript(t *testing.T, into string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("there is no home directory: the end-to-end check on a live transcript is skipped")
	}
	found, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	type candidate struct {
		path string
		size int64
	}
	var list []candidate
	for _, p := range found {
		st, err := os.Stat(p)
		if err == nil && st.Size() > 256<<10 && st.Size() < 4<<20 {
			list = append(list, candidate{p, st.Size()})
		}
	}
	if len(list) == 0 {
		t.Skip("this machine has no transcript of a suitable size: the end-to-end check is skipped")
	}
	slices.SortFunc(list, func(a, b candidate) int { return int(b.size - a.size) })

	body, err := os.ReadFile(list[0].path)
	if err != nil {
		t.Skipf("the transcript did not read: %v", err)
	}
	name := filepath.Base(list[0].path)
	if err := os.WriteFile(filepath.Join(into, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("the end-to-end check on a live transcript: %d KB", len(body)>>10)
}
