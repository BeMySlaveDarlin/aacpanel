package heavy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// clientEnv makes the test binary the client of the queue: a run of it with
// the variable set is aacpanel-exec -heavy with the arguments after the
// test's own, and becomes the command as the real one does.
const clientEnv = "AACP_HEAVY_TEST_CLIENT"

// clientCgroupEnv names the file a client of the test reads its cgroup from.
const clientCgroupEnv = "AACP_HEAVY_TEST_CGROUP"

func TestMain(m *testing.M) {
	// The tests may run inside a heavy run of their own; the client they
	// start must still go to the queue the test keeps.
	cgroupFile = filepath.Join(os.TempDir(), "aacp-heavy-test-no-cgroup")
	if file := os.Getenv(clientCgroupEnv); file != "" {
		cgroupFile = file
	}
	if socket := os.Getenv(clientEnv); socket != "" {
		args := os.Args[slices.Index(os.Args, "--")+1:]
		os.Exit(Run(args, socket, os.Stderr))
	}
	lookEvery, sayEvery = 20*time.Millisecond, 50*time.Millisecond
	dialWait, rejoinWait = 200*time.Millisecond, 600*time.Millisecond
	os.Exit(m.Run())
}

// fakeHost is a machine whose measure the test sets, and whose manager
// starts a scope by putting it among the runs going until the test ends it.
type fakeHost struct {
	mu      sync.Mutex
	m       Measure
	started []Scope
	refuse  error
}

func (h *fakeHost) Measure(context.Context) (Measure, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.m
	m.Runs = slices.Clone(h.m.Runs)
	return m, nil
}

func (h *fakeHost) Start(_ context.Context, _ int, s Scope) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.refuse != nil {
		return h.refuse
	}
	h.started = append(h.started, s)
	h.m.Runs = append(h.m.Runs, Running{Unit: s.Unit, Ask: s.Mem, Taken: s.Mem})
	return nil
}

// end ends the run of the scope named after name.
func (h *fakeHost) end(t *testing.T, name string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.started {
		if s.Name == name {
			h.m.Runs = slices.DeleteFunc(h.m.Runs, func(r Running) bool { return r.Unit == s.Unit })
			return
		}
	}
	t.Fatalf("no run named %q was started", name)
}

// going sets the runs going.
func (h *fakeHost) going(runs ...Running) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.m.Runs = runs
}

func (h *fakeHost) scopes() []Scope {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.started)
}

func (h *fakeHost) names() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var names []string
	for _, s := range h.started {
		names = append(names, s.Name)
	}
	return names
}

// scene is a queue on a socket in a temporary directory: socket is the
// executor's socket the client is pointed at, and the queue's is next to it.
type scene struct {
	t      *testing.T
	host   *fakeHost
	q      *Queue
	socket string
}

func newScene(t *testing.T, l Limits) *scene {
	t.Helper()
	journal := &bytes.Buffer{}
	before := log.Writer()
	log.SetOutput(journal)
	t.Cleanup(func() { log.SetOutput(before) })
	dir, err := os.MkdirTemp("", "hq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := &scene{t: t, host: &fakeHost{m: Measure{Available: 40 * gib}}, socket: filepath.Join(dir, "sock")}
	s.q = NewQueue(s.host, l)
	ln, err := Listen(Socket(s.socket))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.q.Serve(ctx, ln)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return s
}

// until waits for what the test needs of the queue to come true.
func (s *scene) until(what string, ok func(View) bool) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, err := s.q.View(context.Background())
		if err == nil && ok(v) {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("%s: the queue stands at %+v (%v)", what, v, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitingNames(v View) []string {
	var names []string
	for _, w := range v.Waiting {
		names = append(names, w.Name)
	}
	return names
}

// a run is one client of the queue, the test binary standing in for
// aacpanel-exec, with its outputs apart.
type run struct {
	cmd            *exec.Cmd
	stdout, stderr *output
	done           chan struct{}
}

// output is what a run writes, read while it writes.
type output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

func (o *output) Len() int { return len(o.String()) }

// told waits for the run to have been told what holds.
func (r *run) told(t *testing.T, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(r.stderr.String(), what) {
		if time.Now().After(deadline) {
			t.Fatalf("the run was never told %q; it was told %q", what, r.stderr.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *scene) start(args ...string) *run {
	s.t.Helper()
	return startClient(s.t, s.socket, args...)
}

func startClient(t *testing.T, socket string, args ...string) *run {
	t.Helper()
	return startClientIn(t, socket, "", args...)
}

// startClientIn is startClient for a client that lives in the cgroup the
// file names; empty is none of the queue's.
func startClientIn(t *testing.T, socket, cgroup string, args ...string) *run {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), clientEnv+"="+socket, clientCgroupEnv+"="+cgroup)
	r := &run{cmd: cmd, stdout: &output{}, stderr: &output{}, done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = r.stdout, r.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = cmd.Wait()
		close(r.done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-r.done
	})
	return r
}

func (r *run) wait(t *testing.T) int {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the run did not end; it said %q", r.stderr.String())
	}
	return r.cmd.ProcessState.ExitCode()
}

// queueLines are the lines of stderr the queue wrote; the rest is the
// command's.
func (r *run) queueLines() (queue, rest []string) {
	for _, line := range strings.Split(strings.TrimRight(r.stderr.String(), "\n"), "\n") {
		if strings.HasPrefix(line, Prefix) {
			queue = append(queue, strings.TrimPrefix(line, Prefix))
		} else if line != "" {
			rest = append(rest, line)
		}
	}
	return queue, rest
}

// To whoever runs it, a run through the queue is the command: its output and
// its exit are the command's, and the queue says what it says on stderr alone.
func TestARunIsTheCommandItself(t *testing.T) {
	s := newScene(t, Limits{Slots: 3, CPUs: 4, Ask: 6 * gib})
	r := s.start("-name", "probe", "--", "sh", "-c", "echo out; echo err >&2; exit 7")
	if code := r.wait(t); code != 7 {
		t.Errorf("the run exits with %d, want the command's 7; it said %q", code, r.stderr.String())
	}
	if r.stdout.String() != "out\n" {
		t.Errorf("the run writes %q to stdout, want the command's alone", r.stdout.String())
	}
	queue, rest := r.queueLines()
	if !slices.Equal(rest, []string{"err"}) {
		t.Errorf("stderr holds %q besides the queue's lines, want the command's alone", rest)
	}
	if len(queue) < 2 || !strings.Contains(queue[0], "place 1") || !strings.Contains(queue[len(queue)-1], "admitted") {
		t.Errorf("the queue said %q, want the place first and the admission last", queue)
	}
	if got := s.host.scopes(); len(got) != 1 || got[0].Mem != 6*gib || got[0].Swap != 6*gib/8 || got[0].CPUs != 4 ||
		got[0].Name != "probe" || !strings.HasPrefix(got[0].Unit, UnitPrefix) {
		t.Errorf("the run is held to %+v", got)
	}
}

// The queue lets runs go strictly in the order they came, and the one behind
// a run that waits waits too.
func TestRunsGoInTheOrderTheyCame(t *testing.T) {
	s := newScene(t, Limits{Slots: 1})
	first := s.start("-name", "first", "--", "true")
	if code := first.wait(t); code != 0 {
		t.Fatalf("the first run exits with %d: %q", code, first.stderr.String())
	}
	second := s.start("-name", "second", "--", "true")
	s.until("the second run waits", func(v View) bool { return len(v.Waiting) == 1 })
	third := s.start("-name", "third", "-mem", "1G", "--", "true")
	s.until("the third run waits behind it", func(v View) bool {
		return slices.Equal(waitingNames(v), []string{"second", "third"})
	})
	v, _ := s.q.View(context.Background())
	if len(v.Running) != 1 || v.Running[0].Name != "first" || !strings.Contains(v.Head, "a slot") {
		t.Errorf("the queue shows %+v", v)
	}
	third.told(t, "1 run ahead after")

	s.host.end(t, "first")
	if code := second.wait(t); code != 0 {
		t.Fatalf("the second run exits with %d: %q", code, second.stderr.String())
	}
	s.until("the third run waits for the second", func(v View) bool {
		return slices.Equal(waitingNames(v), []string{"third"})
	})
	s.host.end(t, "second")
	if code := third.wait(t); code != 0 {
		t.Fatalf("the third run exits with %d: %q", code, third.stderr.String())
	}
	if got := s.host.names(); !slices.Equal(got, []string{"first", "second", "third"}) {
		t.Errorf("the runs went in the order %q", got)
	}
	queue, _ := third.queueLines()
	if len(queue) < 3 || !strings.Contains(queue[0], "place 2, 1 run ahead") ||
		!slices.ContainsFunc(queue, func(l string) bool { return strings.Contains(l, "the head waits for a slot") }) ||
		!strings.Contains(queue[len(queue)-1], "admitted") {
		t.Errorf("the third run was told %q", queue)
	}
}

// A client that goes away while it waits — killed, or its command timed out
// — takes its place with it, and the run behind it moves up.
func TestARunThatGoesAwayLeavesTheQueue(t *testing.T) {
	s := newScene(t, Limits{Slots: 1})
	s.host.going(Running{Unit: UnitPrefix + "1.scope"})
	gone := s.start("-name", "gone", "--", "true")
	s.until("the run waits", func(v View) bool { return len(v.Waiting) == 1 })
	behind := s.start("-name", "behind", "--", "true")
	s.until("the run behind it waits", func(v View) bool { return len(v.Waiting) == 2 })

	_ = gone.cmd.Process.Kill()
	gone.wait(t)
	s.until("the run killed leaves its place", func(v View) bool {
		return slices.Equal(waitingNames(v), []string{"behind"})
	})
	s.host.going()
	if code := behind.wait(t); code != 0 {
		t.Fatalf("the run behind exits with %d: %q", code, behind.stderr.String())
	}
	if got := s.host.names(); !slices.Equal(got, []string{"behind"}) {
		t.Errorf("the scopes started are %q", got)
	}
}

// An executor started while runs another one admitted go names its own runs
// after none of them.
func TestANewRunIsNotNamedAfterOneGoing(t *testing.T) {
	s := newScene(t, Limits{})
	s.host.going(Running{Unit: UnitPrefix + "1.scope"}, Running{Unit: UnitPrefix + "2.scope"})
	if code := s.start("--", "true").wait(t); code != 0 {
		t.Fatal(code)
	}
	if got := s.host.scopes()[0].Unit; got != UnitPrefix+"3.scope" {
		t.Errorf("the new run is named %s beside the two going", got)
	}
}

// A run that could never be admitted is refused at once, and its command
// does not run.
func TestARunTooBigIsRefusedAtOnce(t *testing.T) {
	s := newScene(t, Limits{Budget: 20 * gib, Total: 62 * gib})
	mark := filepath.Join(t.TempDir(), "ran")
	r := s.start("-mem", "24G", "--", "touch", mark)
	if code := r.wait(t); code != exitRefused {
		t.Errorf("the run exits with %d, want %d", code, exitRefused)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Error("the command of a refused run ran")
	}
	if queue, _ := r.queueLines(); len(queue) != 1 || !strings.Contains(queue[0], "refused: it asks for 24G") {
		t.Errorf("the refusal says %q", queue)
	}
	if r.stdout.Len() != 0 {
		t.Errorf("the refusal wrote %q to stdout", r.stdout.String())
	}
}

// With no executor to queue at, the command runs at once, said in one line:
// the queue is never the reason a session cannot run its tests.
func TestWithNoQueueTheCommandRunsAtOnce(t *testing.T) {
	r := startClient(t, filepath.Join(t.TempDir(), "sock"), "--", "sh", "-c", "echo out; exit 3")
	if code := r.wait(t); code != 3 {
		t.Errorf("the run exits with %d, want the command's 3", code)
	}
	queue, rest := r.queueLines()
	if len(queue) != 1 || !strings.Contains(queue[0], "running now, without the queue") || len(rest) != 0 {
		t.Errorf("stderr holds %q", r.stderr.String())
	}
	if r.stdout.String() != "out\n" {
		t.Errorf("stdout holds %q", r.stdout.String())
	}
}

// A scope the manager would not start leaves the run to go on without it.
func TestARunWhoseScopeDoesNotStartGoesOn(t *testing.T) {
	s := newScene(t, Limits{})
	s.host.mu.Lock()
	s.host.refuse = errors.New("Access denied")
	s.host.mu.Unlock()
	r := s.start("--", "true")
	if code := r.wait(t); code != 0 {
		t.Errorf("the run exits with %d: %q", code, r.stderr.String())
	}
	if queue, _ := r.queueLines(); len(queue) < 2 || !strings.Contains(queue[len(queue)-1], "Access denied") {
		t.Errorf("the queue said %q", queue)
	}
}

// A run whose executor goes away while it waits joins the queue of the next
// one at its end, with a line, and is not run without it.
func TestARunWhoseQueueGoesAwayJoinsTheNext(t *testing.T) {
	journal := &bytes.Buffer{}
	before := log.Writer()
	log.SetOutput(journal)
	t.Cleanup(func() { log.SetOutput(before) })
	dir, err := os.MkdirTemp("", "hq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "sock")
	host := &fakeHost{m: Measure{Available: 40 * gib, Runs: []Running{{Unit: UnitPrefix + "1.scope"}}}}
	serve := func() (*Queue, func()) {
		q := NewQueue(host, Limits{Slots: 1})
		ln, err := Listen(Socket(socket))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			q.Serve(ctx, ln)
			close(done)
		}()
		return q, func() { cancel(); <-done }
	}
	_, stop := serve()
	r := startClient(t, socket, "-name", "survivor", "--", "true")
	r.told(t, "place 1")
	stop()
	q, stop := serve()
	defer stop()
	sc := &scene{t: t, host: host, q: q}
	sc.until("the run joins the new queue", func(v View) bool {
		return slices.Equal(waitingNames(v), []string{"survivor"})
	})
	host.going()
	if code := r.wait(t); code != 0 {
		t.Fatalf("the run exits with %d: %q", code, r.stderr.String())
	}
	if queue, _ := r.queueLines(); !slices.ContainsFunc(queue, func(l string) bool {
		return strings.Contains(l, "joining the queue again at its end")
	}) {
		t.Errorf("the run was told %q", queue)
	}
}

// A run started inside a heavy run goes at once, with every slot taken: its
// slot and its memory are the run's around it, and queued it would wait for
// the slot its own parent holds.
func TestARunInsideARunDoesNotQueue(t *testing.T) {
	s := newScene(t, Limits{Slots: 1})
	s.host.going(Running{Unit: UnitPrefix + "3.scope"})
	file := filepath.Join(t.TempDir(), "cgroup")
	if err := os.WriteFile(file, []byte("0::/user.slice/u/aacp-agents.slice/"+UnitPrefix+"3.scope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := startClientIn(t, s.socket, file, "--", "sh", "-c", "echo inner; exit 4")
	if code := r.wait(t); code != 4 || r.stdout.String() != "inner\n" {
		t.Errorf("the run inside exits with %d and writes %q", code, r.stdout.String())
	}
	queue, _ := r.queueLines()
	if len(queue) != 1 || !strings.Contains(queue[0], "inside the heavy run of "+UnitPrefix+"3.scope") {
		t.Errorf("the run inside said %q", queue)
	}
	if got := s.host.scopes(); len(got) != 0 {
		t.Errorf("the run inside got a scope of its own: %+v", got)
	}
}

// The view lists the runs going and waiting as lists, empty ones included.
func TestTheViewKeepsItsShape(t *testing.T) {
	s := newScene(t, Limits{Slots: 2})
	conn, err := net.Dial("unix", Socket(s.socket))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(Request{Op: OpView}); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"running":[]`, `"waiting":[]`, `"slots":2`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the view %s does not hold %s", raw, want)
		}
	}
}

func TestARunIsNamedByItsCommand(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want string
	}{
		{[]string{"make", "check", "GOTEST_PARALLEL=2", "CHECK_JOBS=4"}, "make check GOTEST_PARALLEL=2"},
		{[]string{"bash", "-c", "go test ./internal/... -run X"}, "go test ./internal/..."},
		{[]string{"/bin/sh", "-c", "make\ncheck"}, "make check"},
		{[]string{"python3", "-c", "print(1)"}, "python3 -c print(1)"},
	} {
		if got := firstWords(c.argv); got != c.want {
			t.Errorf("%q is named %q, want %q", c.argv, got, c.want)
		}
	}
	if got := cleanName(strings.Repeat("x", 100)); len([]rune(got)) != nameMax {
		t.Errorf("a long name is cut to %d characters", len([]rune(got)))
	}
}
