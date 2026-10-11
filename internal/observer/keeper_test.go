package observer

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// quick takes the keeper's waits down to what a test sits through: a look
// every ten milliseconds, a second of gathering in fifty.
func quick(t *testing.T) {
	t.Helper()
	was := []time.Duration{step, second, drainWait}
	step, second, drainWait = 10*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { step, second, drainWait = was[0], was[1], was[2] })
}

// fakeUnits runs an observer the way its unit does, in the test's own
// process: Run goes on when the keeper that started it stops, as a unit
// outlives the executor.
type fakeUnits struct {
	mu      sync.Mutex
	running map[string]context.CancelFunc
	done    map[string]chan struct{}
	stopped []string
	// strays are units the manager has that no record names.
	strays []string
}

func newUnits() *fakeUnits {
	return &fakeUnits{running: map[string]context.CancelFunc{}, done: map[string]chan struct{}{}}
}

func (f *fakeUnits) Start(_ context.Context, unit, dir string, _ Spec) error {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	f.mu.Lock()
	f.running[unit], f.done[unit] = cancel, done
	f.mu.Unlock()
	go func() {
		_ = Run(ctx, dir)
		close(done)
	}()
	return nil
}

func (f *fakeUnits) Stop(_ context.Context, unit string) error {
	f.mu.Lock()
	cancel, done := f.running[unit], f.done[unit]
	delete(f.running, unit)
	f.stopped = append(f.stopped, unit)
	f.strays = slices.DeleteFunc(f.strays, func(s string) bool { return s == unit })
	f.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return nil
}

func (f *fakeUnits) Units(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string{}, f.strays...)
	for u := range f.running {
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeUnits) wasStopped(unit string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.stopped, unit)
}

// fakeOwners stands in for the sessions of the machine: one session, alive
// while alive says so, that keeps every letter it is given.
type fakeOwners struct {
	mu      sync.Mutex
	alive   bool
	unknown bool
	refuse  error
	wakeErr error
	wakes   []string
	from    []string
}

func (f *fakeOwners) Resolve(_ context.Context, c Caller) (Owner, error) {
	if f.refuse != nil {
		return Owner{}, f.refuse
	}
	return Owner{Session: "lab", Conversation: c.Conversation, PID: c.PID, Start: "77", Codex: c.Codex}, nil
}

func (f *fakeOwners) Alive(Owner) (bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive, !f.unknown
}

func (f *fakeOwners) Wake(_ context.Context, _ Owner, from, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wakes, f.from = append(f.wakes, text), append(f.from, from)
	return f.wakeErr
}

func (f *fakeOwners) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.wakes...)
}

func (f *fakeOwners) die() {
	f.mu.Lock()
	f.alive = false
	f.mu.Unlock()
}

var lab = Caller{Conversation: "567f4d24-cd5f-48fa-bdc1-04c89d203494", PID: 4242, Name: "lab"}

// keeping starts the keeper's looks until the test ends, and returns how to
// stop them sooner: the way an executor stops.
func keeping(t *testing.T, k *Keeper) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	k.Load(ctx)
	go func() {
		k.Run(ctx)
		close(done)
	}()
	stop := func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return sync.OnceFunc(stop)
}

// quoted is what a wake quotes of the command's output.
func quoted(wake string) string {
	for _, head := range []string{"What it printed:\n", "The lines that matched:\n", "The last lines it printed:\n"} {
		if _, rest, ok := strings.Cut(wake, head); ok {
			lines, _, _ := strings.Cut(rest, "\n\n")
			return lines
		}
	}
	return ""
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A session is woken by its observer every time a line it waits for comes,
// each time with that line alone, the lines it does not wait for left out,
// and once more when the command ends — with what it told itself to do and
// signed by the observer.
func TestAnObserverWakesItsSessionEveryTimeALineItWaitsForComes(t *testing.T) {
	quick(t)
	units, owners := newUnits(), &fakeOwners{alive: true}
	k := NewKeeper(filepath.Join(t.TempDir(), "observers"), units, owners)
	keeping(t, k)
	_, err := k.Start(context.Background(), lab, Spec{Name: "ci", Dir: t.TempDir(), Wake: Matching,
		Pattern: `^hit`, Gather: 1, Text: "Read the run and fix what failed.",
		Command: "echo hit one; sleep 0.4; echo noise; echo hit two; sleep 0.4; exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "three wakes", func() bool { return len(owners.got()) == 3 })
	wakes := owners.got()
	for i, want := range []string{"hit one", "hit two", ""} {
		if quoted(wakes[i]) != want || !strings.Contains(wakes[i], "Read the run and fix what failed.") {
			t.Errorf("wake %d does not quote %q alone with the words of the session:\n%s", i+1, want, wakes[i])
		}
	}
	if !strings.Contains(wakes[2], "The command ended: exit code 3.") {
		t.Errorf("the last wake does not tell the end:\n%s", wakes[2])
	}
	if !slices.Equal(owners.from, []string{"observer ci", "observer ci", "observer ci"}) {
		t.Errorf("the wakes are signed %v", owners.from)
	}
	time.Sleep(10 * step)
	if n := len(owners.got()); n != 3 {
		t.Errorf("an ended observer woke the session %d times", n)
	}
	list, err := k.List(context.Background(), lab)
	if err != nil || len(list) != 1 || list[0].Woken != 3 || list[0].Ended == nil || list[0].Ended.Code != 3 {
		t.Errorf("the list after the end is %+v, %v", list, err)
	}
}

// Lines a command prints together reach the session as one letter, and a
// line every line wakes for takes the words of the session as they are.
func TestLinesThatComeTogetherAreOneWake(t *testing.T) {
	quick(t)
	units, owners := newUnits(), &fakeOwners{alive: true}
	k := NewKeeper(filepath.Join(t.TempDir(), "observers"), units, owners)
	keeping(t, k)
	if _, err := k.Start(context.Background(), lab, Spec{Name: "burst", Dir: t.TempDir(), Wake: EveryLine,
		Gather: 6, Command: "echo line 1; sleep 0.1; echo line 2; sleep 0.1; echo line 3; sleep 30"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a wake", func() bool { return len(owners.got()) == 1 })
	if w := owners.got()[0]; quoted(w) != "line 1\nline 2\nline 3" {
		t.Errorf("the burst woke the session as\n%s", w)
	}
}

// An executor started again goes on where the last one stopped: the command
// runs on in its unit, a line the last executor woke the session for wakes it
// no second time, and the lines that came while no executor read reach the
// session. A unit of an observer no record names is stopped.
func TestAnExecutorStartedAgainReadsOnWhereTheLastStopped(t *testing.T) {
	quick(t)
	dir := filepath.Join(t.TempDir(), "observers")
	units, first := newUnits(), &fakeOwners{alive: true}
	k := NewKeeper(dir, units, first)
	stop := keeping(t, k)
	if _, err := k.Start(context.Background(), lab, Spec{Name: "deploy", Dir: t.TempDir(), Wake: EveryLine,
		Gather: 1, Command: "echo first; sleep 0.8; echo second; sleep 0.2"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first wake", func() bool { return len(first.got()) == 1 })
	stop()
	stray := Unit("00000000000000ff")
	units.mu.Lock()
	units.strays = append(units.strays, stray)
	units.mu.Unlock()

	again := &fakeOwners{alive: true}
	keeping(t, NewKeeper(dir, units, again))
	waitFor(t, "the wakes after the start", func() bool { return len(again.got()) == 2 })
	if w := first.got(); len(w) != 1 || quoted(w[0]) != "first" {
		t.Errorf("the first executor woke the session with %q", w)
	}
	w := again.got()
	if quoted(w[0]) != "second" || !strings.Contains(w[1], "exit code 0") {
		t.Errorf("the executor started again woke the session with\n%s\n---\n%s", w[0], w[1])
	}
	if !units.wasStopped(stray) {
		t.Error("the unit no record names is left running")
	}
}

// An observer ends with its session: once the session is gone, its command
// is stopped and its files go, and nothing is woken.
func TestAnObserverEndsWithItsSession(t *testing.T) {
	quick(t)
	dir := filepath.Join(t.TempDir(), "observers")
	units, owners := newUnits(), &fakeOwners{alive: true}
	k := NewKeeper(dir, units, owners)
	keeping(t, k)
	v, err := k.Start(context.Background(), lab, Spec{Name: "log", Dir: t.TempDir(), Wake: AtExit,
		Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	owners.die()
	waitFor(t, "the files of the observer to go", func() bool { entries, _ := os.ReadDir(dir); return len(entries) == 0 })
	if !units.wasStopped(v.Unit) {
		t.Error("the observer of a session that ended left its unit running")
	}
	if w := owners.got(); len(w) != 0 {
		t.Errorf("a session that ended was woken: %q", w)
	}
}

// A session keeps a bounded number of observers, each under a name of its
// own, and the panel refuses what it does not take with what to change; a
// call the executor refuses starts nothing.
func TestAnObserverIsRefusedWhatThePanelDoesNotTake(t *testing.T) {
	quick(t)
	units, owners := newUnits(), &fakeOwners{alive: true}
	k := NewKeeper(filepath.Join(t.TempDir(), "observers"), units, owners)
	ctx, work := context.Background(), t.TempDir()
	for i := range PerSession {
		if _, err := k.Start(ctx, lab, Spec{Name: "w" + string(rune('a'+i)), Dir: work, Wake: AtExit,
			Command: "sleep 30"}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, u := range func() []string { l, _ := units.Units(ctx); return l }() {
			_ = units.Stop(ctx, u)
		}
	})
	for _, c := range []struct {
		spec Spec
		says string
	}{
		{Spec{Name: "more", Dir: work, Wake: AtExit, Command: "true"}, "as many as a session keeps"},
		{Spec{Name: "wa", Dir: work, Wake: AtExit, Command: "true"}, "has an observer wa already"},
		{Spec{Name: "-x", Dir: work, Wake: AtExit, Command: "true"}, "is not one"},
		{Spec{Name: "m", Dir: work, Wake: Matching, Command: "true"}, "needs the pattern"},
		{Spec{Name: "m", Dir: work, Wake: EveryLine, Pattern: "x", Command: "true"}, "takes none"},
		{Spec{Name: "m", Dir: work, Wake: Matching, Pattern: "(", Command: "true"}, "not one Go's regexp takes"},
		{Spec{Name: "m", Dir: "relative", Wake: AtExit, Command: "true"}, "not absolute"},
		{Spec{Name: "m", Dir: work, Wake: "often", Command: "true"}, "is not one"},
	} {
		if _, err := k.Start(ctx, lab, c.spec); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%+v: %v", c.spec, err)
		}
	}
	owners.refuse = errors.New("session lab is not on the stream")
	if _, err := k.Start(ctx, lab, Spec{Name: "z", Dir: work, Wake: AtExit, Command: "true"}); err == nil ||
		err.Error() != "session lab is not on the stream" {
		t.Errorf("a refused session was answered %v", err)
	}
}

// What a session reads of its observer is its own: the list, the tail of the
// log and the stop. A stop ends the command and takes the log with it.
func TestASessionReadsAndStopsItsOwnObservers(t *testing.T) {
	quick(t)
	dir := filepath.Join(t.TempDir(), "observers")
	units, owners := newUnits(), &fakeOwners{alive: true}
	k := NewKeeper(dir, units, owners)
	keeping(t, k)
	ctx := context.Background()
	v, err := k.Start(ctx, lab, Spec{Name: "tailing", Dir: t.TempDir(), Wake: AtExit,
		Command: "for i in $(seq 1 50); do echo row $i; done; sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the log", func() bool { _, l, _ := k.Tail(ctx, lab, "tailing", 3); return len(l) == 3 })
	if _, lines, _ := k.Tail(ctx, lab, "tailing", 3); !slices.Equal(lines, []string{"row 48", "row 49", "row 50"}) {
		t.Errorf("the tail is %q", lines)
	}
	if _, _, err := k.Tail(ctx, Caller{Conversation: "other", PID: 1}, "tailing", 3); err == nil {
		t.Error("another session read the log")
	}
	if _, err := k.Stop(ctx, lab, "nothing"); err == nil || !strings.Contains(err.Error(), "it has tailing") {
		t.Errorf("a stop of no observer answered %v", err)
	}
	if _, err := k.Stop(ctx, lab, "tailing"); err != nil {
		t.Fatal(err)
	}
	if !units.wasStopped(v.Unit) {
		t.Error("the stop left the unit running")
	}
	if list, _ := k.List(ctx, lab); len(list) != 0 {
		t.Errorf("a stopped observer is listed: %+v", list)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a stopped observer left %v", entries)
	}
}

// Unknown owners get one hour, including across an executor restart. A
// daemon that answers again clears the deadline; each absence has its own.
func TestACodexDaemonsAbsenceIsBoundedForObserversAndAlarms(t *testing.T) {
	var journal bytes.Buffer
	before := log.Writer()
	log.SetOutput(&journal)
	t.Cleanup(func() { log.SetOutput(before) })
	for _, scene := range []string{"expired", "exact hour", "executor restarted", "daemon returned", "second absence"} {
		t.Run(scene, func(t *testing.T) {
			k, owners, clock, dir := alarmed(t)
			ctx := t.Context()
			caller := Caller{Conversation: lab.Conversation, Codex: true}
			units := k.runner.(*fakeUnits)
			v, err := k.Start(ctx, caller, Spec{Name: "ci", Command: "sleep 300", Dir: t.TempDir(), Wake: AtExit})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = units.Stop(context.Background(), v.Unit) })
			if _, err := k.SetAlarm(ctx, caller, AlarmAsk{Name: "poll", Every: "10m"}); err != nil {
				t.Fatal(err)
			}
			owners.mu.Lock()
			owners.unknown = true
			owners.mu.Unlock()
			k.look(ctx)
			clock.move(59 * time.Minute)
			k.look(ctx)
			assertKept := func(want int) {
				t.Helper()
				observers, err := k.List(ctx, caller)
				if err != nil || len(observers) != want {
					t.Errorf("observers: %+v, %v; want %d", observers, err, want)
				}
				alarms, err := k.Alarms(ctx, caller)
				if err != nil || len(alarms) != want || len(files(dir)) != want {
					t.Errorf("alarms: %+v, %v; files %v; want %d", alarms, err, files(dir), want)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != want {
					t.Errorf("observer directories: %v, %v; want %d", entries, err, want)
				}
			}
			assertKept(1)
			if len(owners.got()) != 0 {
				t.Fatal("a wake was tried while the daemon was unavailable")
			}
			if scene == "executor restarted" {
				k = NewKeeper(dir, units, owners)
				k.now = clock.now
				k.Load(ctx)
			}
			if scene == "daemon returned" || scene == "second absence" {
				// Wait for the real command's pid, so a synthetic hour does
				// not turn its ordinary startup into a lost unit.
				entries, _ := os.ReadDir(dir)
				waitFor(t, "the observer to start", func() bool { alive, _ := running(filepath.Join(dir, entries[0].Name())); return alive })
				owners.mu.Lock()
				owners.unknown = false
				owners.mu.Unlock()
				k.look(ctx)
				if len(owners.got()) != 1 {
					t.Fatalf("the due alarm was not delivered after reconnection: %v", owners.got())
				}
				// Reload also checks the cleared deadline reached the disk.
				k = NewKeeper(dir, units, owners)
				k.now = clock.now
				k.Load(ctx)
				if scene == "second absence" {
					owners.mu.Lock()
					owners.unknown = true
					owners.mu.Unlock()
					k.look(ctx)
					clock.move(59 * time.Minute)
					k.look(ctx)
					assertKept(1)
				} else {
					clock.move(2 * time.Minute)
					k.look(ctx)
					assertKept(1)
					return
				}
			}
			advance := 2 * time.Minute
			if scene == "exact hour" {
				advance = time.Minute
			}
			journal.Reset()
			clock.move(advance)
			k.look(ctx)
			assertKept(0)
			k.look(ctx)
			for _, work := range []string{"observer ci", "alarm poll"} {
				want := work + " of lab is gone: its codex daemon was unavailable for one hour"
				if strings.Count(journal.String(), want) != 1 {
					t.Errorf("the journal must name each expiry once: %s", journal.String())
				}
			}
			if !units.wasStopped(v.Unit) {
				t.Error("the expired observer's command still runs")
			}
		})
	}
}

// A dialog refusal uses the same budget as every failed delivery. Lines and
// a due ring stay pending until delivery succeeds or the attempt limit is
// spent; a failure never counts as a wake the session received.
func TestRefusedWakesKeepTheirLinesAndRingsUntilTheAttemptLimit(t *testing.T) {
	for _, recover := range []bool{false, true} {
		name := "the dialog stays open"
		if recover {
			name = "the composer comes back"
		}
		t.Run(name, func(t *testing.T) {
			k, owners, clock, _ := alarmed(t)
			owners.wakeErr = errors.New("the dialog has the keyboard")
			if _, err := k.SetAlarm(t.Context(), lab, AlarmAsk{Name: "poll", Every: "1m"}); err != nil {
				t.Fatal(err)
			}
			var ring *ringing
			for _, r := range k.alarms {
				ring = r
			}
			clock.move(time.Minute)
			due := ring.rec.Next
			w := &watch{rec: Record{Owner: ring.rec.Owner, Spec: Spec{Name: "ci", Wake: EveryLine}},
				batch: []string{"the run finished"}, end: &Ending{}}
			for attempt := 1; attempt < wakeTries; attempt++ {
				k.wake(t.Context(), w, &w.rec, true)
				k.lookAtAlarm(t.Context(), ring)
				if w.tries != attempt || w.rec.Woken != 0 || !w.rec.LastWake.IsZero() || w.rec.Ended != nil ||
					!slices.Equal(w.batch, []string{"the run finished"}) {
					t.Fatalf("attempt %d consumed the refused observer wake: %+v", attempt, w)
				}
				if ring.tries != attempt || ring.rec.Rang != 0 || !ring.rec.LastRing.IsZero() || !ring.rec.Next.Equal(due) {
					t.Fatalf("attempt %d consumed the refused ring: %+v", attempt, ring)
				}
			}
			if recover {
				owners.wakeErr = nil
			}
			k.wake(t.Context(), w, &w.rec, true)
			k.lookAtAlarm(t.Context(), ring)
			if w.tries != 0 || len(w.batch) != 0 || w.rec.Ended == nil || ring.tries != 0 || !ring.rec.Next.After(due) {
				t.Fatal("the last attempt did not retire the wake and move the repeat on")
			}
			want := 0
			if recover {
				want = 1
			}
			if w.rec.Woken != want || ring.rec.Rang != want || w.rec.LastWake.IsZero() == recover || ring.rec.LastRing.IsZero() == recover {
				t.Errorf("received wakes: observer %d, alarm %d; want %d", w.rec.Woken, ring.rec.Rang, want)
			}
			if len(owners.got()) != 2*wakeTries {
				t.Errorf("the two wakes made %d attempts, want %d", len(owners.got()), 2*wakeTries)
			}
		})
	}
}
