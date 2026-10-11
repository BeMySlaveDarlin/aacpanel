package observer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Owners is how the keeper reaches the sessions of the machine. The executor
// is the one that knows them: which session runs a conversation, which is on
// the stream, which thread a daemon of codex holds.
type Owners interface {
	// Resolve finds the live session a call comes from, or refuses one an
	// observer cannot wake, with the reason.
	Resolve(ctx context.Context, c Caller) (Owner, error)
	// Alive says whether the session still runs as it ran when the observer
	// was started: the same process of claude, the thread still held. known
	// is false while that cannot be told — a daemon of codex not read yet.
	Alive(o Owner) (alive, known bool)
	// Wake gives the session a letter signed by from, not as the words of its
	// person.
	Wake(ctx context.Context, o Owner, from, text string) error
}

// Runner starts and stops the units observers run in.
type Runner interface {
	// Start starts the observer kept in dir in the unit named, held to the
	// ceilings of an observer.
	Start(ctx context.Context, unit, dir string, spec Spec) error
	// Stop stops a unit and whatever runs in it; a unit that is gone already
	// is no failure.
	Stop(ctx context.Context, unit string) error
	// Units are the units of observers the user's manager has.
	Units(ctx context.Context) ([]string, error)
}

// How the keeper looks at its observers.
var (
	// step is how often the logs are read and the sessions looked at.
	step = time.Second
	// startGrace is how long an observer whose unit has not run it yet is
	// waited for before its unit counts as lost.
	startGrace = 15 * time.Second
	// goneSteps is how many looks in a row a session must be gone on before
	// its observers are ended: a daemon of codex can miss a thread for one
	// read and list it the next.
	goneSteps = 3
	// wakeTries is how many times a wake the session did not take is tried
	// before its lines are dropped with a line in the log.
	wakeTries = 10
	// second is what a second of gathering lasts: a test takes less.
	second = time.Second
)

// The bounds of one wake: the lines it quotes and how much of each. What is
// past them is counted and left in the log for observer_tail.
const (
	// A daemon that cannot answer cannot confirm a thread is gone either.
	// Bound that uncertainty so nobody waits on abandoned work forever.
	ownerWait   = time.Hour
	wakeLines   = 20
	wakeLineMax = 400
	// endLines is how much of the log the end of an observer that waits for
	// nothing else quotes.
	endLines = 10
)

// Keeper keeps the observers of the host: it starts their units, reads their
// logs, wakes their sessions and ends them with the sessions. It keeps the
// alarms of the sessions the same way, with no unit: it rings them itself.
// Its records outlive it, and the keeper of an executor started again goes on
// where the last one stopped.
type Keeper struct {
	dir      string
	alarmDir string
	runner   Runner
	owners   Owners
	now      func() time.Time

	// starting serialises the starts, so two calls of one session cannot
	// both take the last place.
	starting sync.Mutex

	mu     sync.Mutex
	watch  map[string]*watch
	alarms map[string]*ringing
}

// watch is one observer as the keeper follows it. A look at it and its end
// hold busy, so a stop does not take the files from under a look; rec
// changes under the keeper's lock, the rest only in a look.
type watch struct {
	busy sync.Mutex
	rec  Record
	dir  string
	re   *regexp.Regexp
	// ready is false while the unit is being started.
	ready bool
	// since is when the keeper took it: its start, or the start of the
	// keeper for one it found on the disk.
	since time.Time
	// batch are the lines waiting for a wake, first is when the first came,
	// more counts the lines past what a wake quotes.
	batch []string
	first time.Time
	more  int
	// skipped says lines of the log were never read.
	skipped bool
	// end is how the command ended, read and not told yet.
	end *Ending
	// tries counts the wakes the session did not take, gone the looks the
	// session was not there.
	tries int
	gone  int
}

// NewKeeper makes the keeper of the observers kept in dir, and of the alarms
// kept beside them.
func NewKeeper(dir string, runner Runner, owners Owners) *Keeper {
	return &Keeper{dir: dir, alarmDir: alarmDir(dir), runner: runner, owners: owners, now: time.Now,
		watch: map[string]*watch{}, alarms: map[string]*ringing{}}
}

// Load takes back the observers and the alarms an executor before this one
// left and stops the units no record names. It is done before any call is
// taken: a record being written for a start would read as one not read and go.
func (k *Keeper) Load(ctx context.Context) {
	k.load(ctx)
	k.loadAlarms()
}

// Run looks at the observers and the alarms until ctx ends. The units go on
// after it, and the alarms wait on the disk: an executor started again takes
// them back with Load.
func (k *Keeper) Run(ctx context.Context) {
	tick := time.NewTicker(step)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			k.look(ctx)
		}
	}
}

// load takes back the observers kept on the disk. Their reading goes on from
// where it stands, so a line the last executor read and woke the session
// for wakes it no second time, and one it had not read yet wakes it now.
func (k *Keeper) load(ctx context.Context) {
	entries, _ := os.ReadDir(k.dir)
	kept := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() || !idPattern.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(k.dir, e.Name())
		rec, err := ReadRecord(dir)
		if err != nil || rec.ID != e.Name() {
			log.Printf("observer %s: the record is not read, the observer goes: %v", e.Name(), err)
			k.stopUnit(ctx, e.Name())
			_ = os.RemoveAll(dir)
			continue
		}
		spec := rec.Spec
		re, err := spec.Check()
		if err != nil {
			log.Printf("observer %s: %v; the observer goes", rec.Spec.Name, err)
			k.stopUnit(ctx, rec.ID)
			_ = os.RemoveAll(dir)
			continue
		}
		k.mu.Lock()
		k.watch[rec.ID] = &watch{rec: rec, dir: dir, re: re, ready: true, since: k.now()}
		k.mu.Unlock()
		kept[Unit(rec.ID)] = true
	}
	units, err := k.runner.Units(ctx)
	if err != nil {
		log.Printf("observers: the units of the user's manager are not listed: %v", err)
		return
	}
	for _, u := range units {
		if !kept[u] {
			log.Printf("observers: unit %s has no record, it is stopped", u)
			_ = k.runner.Stop(ctx, u)
		}
	}
}

// Start starts an observer for the session a call comes from.
func (k *Keeper) Start(ctx context.Context, c Caller, spec Spec) (View, error) {
	owner, err := k.owners.Resolve(ctx, c)
	if err != nil {
		return View{}, err
	}
	re, err := spec.Check()
	if err != nil {
		return View{}, err
	}
	if info, err := os.Stat(spec.Dir); err != nil || !info.IsDir() {
		return View{}, fmt.Errorf("the directory %s is not there to run the command in", spec.Dir)
	}

	k.starting.Lock()
	defer k.starting.Unlock()
	k.mu.Lock()
	own, total := 0, len(k.watch)
	for _, w := range k.watch {
		if !w.rec.Owner.Same(owner) {
			continue
		}
		own++
		if w.rec.Spec.Name == spec.Name {
			k.mu.Unlock()
			return View{}, fmt.Errorf("this session has an observer %s already: stop it with observer_stop, "+
				"or give the new one another name", spec.Name)
		}
	}
	k.mu.Unlock()
	switch {
	case own >= PerSession:
		return View{}, fmt.Errorf("this session has %d observers, as many as a session keeps: "+
			"stop one with observer_stop first", own)
	case total >= Total:
		return View{}, fmt.Errorf("the host keeps %d observers, as many as it takes: "+
			"one has to end before another starts", total)
	}

	id := newID()
	dir := filepath.Join(k.dir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return View{}, fmt.Errorf("the directory of the observer was not made: %w", err)
	}
	rec := Record{ID: id, Spec: spec, Owner: owner, Started: k.now()}
	if err := writeRecord(dir, rec); err != nil {
		_ = os.RemoveAll(dir)
		return View{}, fmt.Errorf("the record of the observer was not written: %w", err)
	}
	w := &watch{rec: rec, dir: dir, re: re, since: k.now()}
	k.mu.Lock()
	k.watch[id] = w
	k.mu.Unlock()
	if err := k.runner.Start(ctx, Unit(id), dir, spec); err != nil {
		k.mu.Lock()
		delete(k.watch, id)
		k.mu.Unlock()
		_ = os.RemoveAll(dir)
		return View{}, fmt.Errorf("the unit of the observer did not start: %w", err)
	}
	k.mu.Lock()
	w.ready = true
	view := w.view()
	k.mu.Unlock()
	log.Printf("observer %s of %s started in unit %s: %s", spec.Name, owner.Session, Unit(id), oneLine(spec.Command, 200))
	return view, nil
}

// List is the observers of the session a call comes from, the oldest first.
func (k *Keeper) List(ctx context.Context, c Caller) ([]View, error) {
	owner, err := k.owners.Resolve(ctx, c)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	out := []View{}
	for _, w := range k.watch {
		if w.ready && w.rec.Owner.Same(owner) {
			out = append(out, w.view())
		}
	}
	slices.SortFunc(out, func(a, b View) int { return a.Started.Compare(b.Started) })
	return out, nil
}

// Stop ends an observer of the session a call comes from, by its name.
func (k *Keeper) Stop(ctx context.Context, c Caller, name string) (View, error) {
	w, err := k.find(ctx, c, name)
	if err != nil {
		return View{}, err
	}
	k.mu.Lock()
	view := w.view()
	k.mu.Unlock()
	k.end(ctx, w, "the session stopped it")
	return view, nil
}

// Tail is the last lines of the log of an observer of the session a call
// comes from.
func (k *Keeper) Tail(ctx context.Context, c Caller, name string, lines int) (View, []string, error) {
	w, err := k.find(ctx, c, name)
	if err != nil {
		return View{}, nil, err
	}
	k.mu.Lock()
	view := w.view()
	k.mu.Unlock()
	return view, tail(w.dir, lines), nil
}

func (k *Keeper) find(ctx context.Context, c Caller, name string) (*watch, error) {
	owner, err := k.owners.Resolve(ctx, c)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	var names []string
	for _, w := range k.watch {
		if !w.ready || !w.rec.Owner.Same(owner) {
			continue
		}
		if w.rec.Spec.Name == name {
			return w, nil
		}
		names = append(names, w.rec.Spec.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("this session has no observer %s, and no observers at all", name)
	}
	slices.Sort(names)
	return nil, fmt.Errorf("this session has no observer %s; it has %s", name, strings.Join(names, ", "))
}

// look reads the logs, ends the observers and the alarms of sessions that are
// gone and wakes the sessions whose lines or moments are due.
func (k *Keeper) look(ctx context.Context) {
	k.mu.Lock()
	all := make([]*watch, 0, len(k.watch))
	for _, w := range k.watch {
		if w.ready {
			all = append(all, w)
		}
	}
	k.mu.Unlock()
	for _, w := range all {
		k.lookAt(ctx, w)
	}
	k.lookAtAlarms(ctx)
}

func (k *Keeper) lookAt(ctx context.Context, w *watch) {
	w.busy.Lock()
	defer w.busy.Unlock()
	if !k.kept(w) {
		return
	}
	rec := w.rec
	alive, why := k.ownerStatus(rec.Owner, &w.gone, &rec.UnknownSince)
	if why != "" {
		k.endHeld(ctx, w, why)
		return
	}
	if rec.UnknownSince != w.rec.UnknownSince {
		k.mu.Lock()
		w.rec = rec
		k.mu.Unlock()
		if err := writeRecord(w.dir, rec); err != nil {
			log.Printf("observer %s: the owner's availability was not written: %v", rec.Spec.Name, err)
		}
	}
	if !alive || rec.Ended != nil {
		return
	}
	if w.end == nil {
		w.end = k.ended(w)
	}
	at := cursor{Inode: rec.Inode, Offset: rec.Offset}
	lines, next, skipped, more, err := readOn(w.dir, at, w.end != nil)
	if err != nil {
		log.Printf("observer %s of %s: the log is not read: %v", rec.Spec.Name, rec.Owner.Session, err)
	}
	k.gather(w, lines)
	w.skipped = w.skipped || skipped
	rec.Inode, rec.Offset = next.Inode, next.Offset
	now := k.now()
	switch {
	case w.end != nil && !more && err == nil:
		// The end is told once the last lines are read, in the same wake.
		k.wake(ctx, w, &rec, true)
	case len(w.batch) > 0 && !now.Before(w.first.Add(time.Duration(rec.Spec.Gather)*second)):
		k.wake(ctx, w, &rec, false)
	}
	k.mu.Lock()
	changed := rec != w.rec
	w.rec = rec
	k.mu.Unlock()
	// What was read and not woken for yet is read again by an executor
	// started again, so it is kept as read only once its wake went.
	if changed && len(w.batch) == 0 {
		if err := writeRecord(w.dir, rec); err != nil {
			log.Printf("observer %s: the record was not written: %v", rec.Spec.Name, err)
		}
	}
}

// ownerStatus pauses work while its owner cannot be reached. UnknownSince
// is kept in the record: restarting the executor cannot buy another hour.
func (k *Keeper) ownerStatus(owner Owner, gone *int, unknownSince *time.Time) (alive bool, why string) {
	alive, known := k.owners.Alive(owner)
	if !known {
		*gone = 0
		if owner.Codex {
			if unknownSince.IsZero() {
				*unknownSince = k.now()
			}
			if k.now().Sub(*unknownSince) >= ownerWait {
				return false, "its codex daemon was unavailable for one hour"
			}
		}
		return false, ""
	}
	*unknownSince = time.Time{}
	if alive {
		*gone = 0
		return true, ""
	}
	*gone++
	if *gone >= goneSteps {
		return false, "its session ended"
	}
	return false, ""
}

// kept says whether the keeper still keeps an observer: a stop may have
// ended it while a look waited for it.
func (k *Keeper) kept(w *watch) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.watch[w.rec.ID] == w
}

// ended reads how the command ended, if it has: what the observer in the
// unit wrote, or a unit that is gone with no word.
func (k *Keeper) ended(w *watch) *Ending {
	if e, ok := readEnding(w.dir); ok {
		return e
	}
	alive, known := running(w.dir)
	if alive || (!known && k.now().Sub(w.since) < startGrace) {
		return nil
	}
	if e, ok := readEnding(w.dir); ok {
		return e
	}
	return &Ending{Lost: true, At: k.now()}
}

// gather takes the lines of a look into the wake waiting for them.
func (k *Keeper) gather(w *watch, lines []string) {
	for _, line := range lines {
		switch w.rec.Spec.Wake {
		case AtExit:
			continue
		case Matching:
			if !w.re.MatchString(line) {
				continue
			}
		}
		if len(w.batch) == 0 && w.more == 0 {
			w.first = k.now()
		}
		if len(w.batch) < wakeLines {
			w.batch = append(w.batch, line)
		} else {
			w.more++
		}
	}
}

// wake gives the session its lines, and the end of the command when final,
// and counts it in rec.
func (k *Keeper) wake(ctx context.Context, w *watch, rec *Record, final bool) {
	var end *Ending
	lines := w.batch
	if final {
		end = w.end
		if rec.Spec.Wake == AtExit {
			lines = tail(w.dir, endLines)
		}
	}
	text := wakeText(*rec, lines, w.more, w.skipped, end)
	err := k.owners.Wake(ctx, rec.Owner, "observer "+rec.Spec.Name, text)
	if err != nil {
		w.tries++
		log.Printf("observer %s of %s: the wake was not taken (%d of %d): %v",
			rec.Spec.Name, rec.Owner.Session, w.tries, wakeTries, err)
		if w.tries < wakeTries {
			return
		}
		log.Printf("observer %s of %s: %d lines are dropped untold", rec.Spec.Name, rec.Owner.Session,
			len(w.batch)+w.more)
	} else {
		rec.Woken++
		rec.LastWake = k.now()
	}
	w.tries = 0
	w.batch, w.more, w.skipped = nil, 0, false
	if final {
		rec.Ended = w.end
		log.Printf("observer %s of %s ended: %s", rec.Spec.Name, rec.Owner.Session, w.end)
	}
}

// end stops the unit of an observer and takes away everything it kept, once
// a look at it in progress is over.
func (k *Keeper) end(ctx context.Context, w *watch, why string) {
	w.busy.Lock()
	defer w.busy.Unlock()
	k.endHeld(ctx, w, why)
}

// endHeld is end for the one that holds the observer already.
func (k *Keeper) endHeld(ctx context.Context, w *watch, why string) {
	k.mu.Lock()
	if k.watch[w.rec.ID] != w {
		k.mu.Unlock()
		return
	}
	delete(k.watch, w.rec.ID)
	k.mu.Unlock()
	k.stopUnit(ctx, w.rec.ID)
	if err := os.RemoveAll(w.dir); err != nil {
		log.Printf("observer %s: its files were not removed: %v", w.rec.Spec.Name, err)
	}
	log.Printf("observer %s of %s is gone: %s", w.rec.Spec.Name, w.rec.Owner.Session, why)
}

func (k *Keeper) stopUnit(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := k.runner.Stop(ctx, Unit(id)); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("observer unit %s was not stopped: %v", Unit(id), err)
	}
}

// wakeText is the letter a session gets from its observer: what it told
// itself to do, the lines that woke it and how the command ended.
func wakeText(rec Record, lines []string, more int, skipped bool, end *Ending) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Observer %s, which this session started with observer_start, watching `%s`", rec.Spec.Name,
		oneLine(rec.Spec.Command, 200))
	if rec.Spec.Wake == Matching {
		fmt.Fprintf(&b, " for lines matching `%s`", rec.Spec.Pattern)
	}
	b.WriteString(".\n")
	if rec.Spec.Text != "" {
		b.WriteString(rec.Spec.Text + "\n")
	}
	if len(lines) > 0 {
		switch {
		case end != nil && rec.Spec.Wake == AtExit:
			b.WriteString("\nThe last lines it printed:\n")
		case rec.Spec.Wake == Matching:
			b.WriteString("\nThe lines that matched:\n")
		default:
			b.WriteString("\nWhat it printed:\n")
		}
		for _, l := range lines {
			b.WriteString(oneLine(l, wakeLineMax) + "\n")
		}
	}
	if more > 0 {
		fmt.Fprintf(&b, "…and %d lines more.\n", more)
	}
	if skipped {
		b.WriteString("Part of its log went before it was read.\n")
	}
	if end != nil {
		fmt.Fprintf(&b, "\nThe command ended: %s. The observer stays listed until observer_stop %s.\n",
			end, rec.Spec.Name)
	} else {
		fmt.Fprintf(&b, "\nobserver_tail %s reads its log, observer_stop %s ends it.\n", rec.Spec.Name, rec.Spec.Name)
	}
	return strings.TrimRight(b.String(), "\n")
}

// oneLine cuts a text to n characters and keeps it on one line.
func oneLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// View is an observer as a session reads it.
type View struct {
	Name     string    `json:"name"`
	Command  string    `json:"command"`
	Dir      string    `json:"dir"`
	Wake     When      `json:"wake"`
	Pattern  string    `json:"pattern,omitempty"`
	Gather   int       `json:"gather"`
	Started  time.Time `json:"started"`
	Woken    int       `json:"woken"`
	LastWake time.Time `json:"lastWake,omitzero"`
	Ended    *Ending   `json:"ended,omitempty"`
	Log      int64     `json:"log"`
	Unit     string    `json:"unit"`
}

func (w *watch) view() View {
	s := w.rec.Spec
	return View{Name: s.Name, Command: s.Command, Dir: s.Dir, Wake: s.Wake, Pattern: s.Pattern, Gather: s.Gather,
		Started: w.rec.Started, Woken: w.rec.Woken, LastWake: w.rec.LastWake, Ended: w.rec.Ended,
		Log: logSize(w.dir), Unit: Unit(w.rec.ID)}
}
