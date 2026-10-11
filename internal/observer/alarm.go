package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// An alarm is the plainest thing a session waits on: a moment. It runs no
// command, so it needs no unit: the keeper holds it itself, keeps it on the
// disk and rings it — wakes the session with a letter, as an observer wakes
// one — at its moment, and every so long after when it repeats. It belongs to
// the session that set it and ends as an observer ends, with the session: a
// session started again knows nothing of what the one before it meant to be
// woken for, and sets its own.

// The bounds of the alarms. A session keeps AlarmsPerSession and the host
// AlarmsTotal. A ring is a turn of the session's model, and EveryMin keeps a
// repeat from becoming a loop that wakes the session faster than it works: a
// session that has to look that often wants an observer of a command that
// looks for it. AheadDays bounds how far ahead a ring is set: a moment past it
// is a slip of the date, and no session runs that long without the restart
// that ends its alarms.
const (
	AlarmsPerSession = 10
	AlarmsTotal      = 100
	EveryMin         = time.Minute
	AheadDays        = 7
	aheadMax         = AheadDays * 24 * time.Hour
)

// lateBy is how late a ring has to be to say so in its letter: an executor
// that was down at the moment rings once it is back, and the session learns
// the moment it was meant for.
const lateBy = time.Minute

// alarmDir is where the keeper of the observers kept in dir keeps the
// alarms: a directory beside the observers', a file an alarm.
func alarmDir(dir string) string { return filepath.Join(filepath.Dir(dir), "alarms") }

// AlarmAsk is what a session asks an alarm for, in the words of the tool: the
// keeper reads the moments on its own clock.
type AlarmAsk struct {
	// Name is what the session calls the alarm by: the name its letters are
	// signed with and the one it cancels it by.
	Name string `json:"name"`
	// In is the first ring as a span from now, At as a moment — a time of the
	// day on the host's clock, the next to come, or RFC 3339. Every is the span
	// between the rings of a repeat; alone, it is the span to the first ring
	// as well.
	In    string `json:"in,omitempty"`
	At    string `json:"at,omitempty"`
	Every string `json:"every,omitempty"`
	// Note is what the session told itself to do when woken, said first in
	// every letter.
	Note string `json:"note,omitempty"`
}

// schedule reads when an alarm asked for at now rings first and how long
// after each ring the next comes, none for an alarm that rings once. It
// refuses what the panel does not take, with what to change.
func (a AlarmAsk) schedule(now time.Time) (first time.Time, every time.Duration, err error) {
	switch {
	case !namePattern.MatchString(a.Name) || len(a.Name) > NameMax:
		return first, 0, fmt.Errorf("the name %q is not one: up to %d letters, digits, dots, dashes and underscores, "+
			"starting with a letter or a digit", a.Name, NameMax)
	case utf8.RuneCountInString(a.Note) > TextMax:
		return first, 0, fmt.Errorf("the note is longer than %d characters", TextMax)
	case a.In != "" && a.At != "":
		return first, 0, errors.New("in and at both name the first ring: give one of them")
	case a.In == "" && a.At == "" && a.Every == "":
		return first, 0, errors.New("nothing says when the alarm rings: give in, at or every")
	}
	if a.Every != "" {
		if every, err = readSpan("every", a.Every); err != nil {
			return first, 0, err
		}
		if every < EveryMin {
			return first, 0, fmt.Errorf("every %s is less than %s: a ring is a turn of this session, and a "+
				"session that has to look that often wants an observer of a command that looks for it",
				spanWords(every), spanWords(EveryMin))
		}
		if every > aheadMax {
			return first, 0, fmt.Errorf("every %s is more than %d days", spanWords(every), AheadDays)
		}
	}
	switch {
	case a.In != "":
		in, err := readSpan("in", a.In)
		if err != nil {
			return first, 0, err
		}
		first = now.Add(in)
	case a.At != "":
		if first, err = readMoment(a.At, now); err != nil {
			return first, 0, err
		}
		if !first.After(now) {
			return first, 0, fmt.Errorf("the moment %s has passed: it is %s now", a.At, now.Format(time.RFC3339))
		}
	default:
		first = now.Add(every)
	}
	if first.Sub(now) > aheadMax {
		return first, 0, fmt.Errorf("the first ring, %s, is more than %d days ahead", first.Format(time.RFC3339),
			AheadDays)
	}
	return first, every, nil
}

// readSpan reads a span ahead in the syntax of Go's durations.
func readSpan(field, s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s %q is not a span ahead: 90s, 10m, 1h30m", field, s)
	}
	return d, nil
}

// readMoment reads a moment: RFC 3339, or a time of the day on the clock of
// now, which is the next such time to come — tomorrow's once today's passed.
func readMoment(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"15:04", "15:04:05"} {
		t, err := time.ParseInLocation(layout, s, now.Location())
		if err != nil {
			continue
		}
		at := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, now.Location())
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return at, nil
	}
	return time.Time{}, fmt.Errorf("at %q is not a moment: a time of the day on the host's clock, 23:30 or "+
		"23:30:15, or RFC 3339, as %s", s, now.Format(time.RFC3339))
}

// Alarm is an alarm as the keeper keeps it on the disk: what it was set for,
// whose it is and when it rings next. It outlives the executor, which reads
// it back when it starts again.
type Alarm struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
	// Every is the span between the rings of a repeat, none for an alarm that
	// rings once.
	Every time.Duration `json:"every,omitempty"`
	Owner Owner         `json:"owner"`
	Set   time.Time     `json:"set"`
	// UnknownSince bounds a codex daemon's absence across executor restarts.
	UnknownSince time.Time `json:"unknownSince,omitzero"`
	// Next is the moment it rings next. A ring moves it on by Every past the
	// moment of the ring.
	Next time.Time `json:"next"`
	// Rang counts the rings the session took, LastRing is the last.
	Rang     int       `json:"rang"`
	LastRing time.Time `json:"lastRing,omitzero"`
}

// AlarmView is an alarm as a session reads it.
type AlarmView struct {
	Name     string        `json:"name"`
	Note     string        `json:"note,omitempty"`
	Every    time.Duration `json:"every,omitempty"`
	Set      time.Time     `json:"set"`
	Next     time.Time     `json:"next"`
	Rang     int           `json:"rang"`
	LastRing time.Time     `json:"lastRing,omitzero"`
}

func (a Alarm) view() AlarmView {
	return AlarmView{Name: a.Name, Note: a.Note, Every: a.Every, Set: a.Set, Next: a.Next, Rang: a.Rang,
		LastRing: a.LastRing}
}

func writeAlarm(dir string, a Alarm) error {
	return writeJSON(filepath.Join(dir, a.ID+".json"), a)
}

func readAlarm(path string) (Alarm, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Alarm{}, err
	}
	var a Alarm
	if err := json.Unmarshal(raw, &a); err != nil {
		return Alarm{}, err
	}
	return a, nil
}

// ringing is one alarm as the keeper holds it. A ring and a cancel hold busy,
// so a cancel does not take the file from under a ring; rec changes under the
// keeper's lock.
type ringing struct {
	busy sync.Mutex
	rec  Alarm
	// tries counts the rings the session did not take, gone the looks the
	// session was not there.
	tries int
	gone  int
}

// loadAlarms takes back the alarms an executor before this one left. One
// whose moment passed while no executor ran rings at the first look, once:
// the rings a repeat missed are not made up.
func (k *Keeper) loadAlarms() {
	entries, _ := os.ReadDir(k.alarmDir)
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || !idPattern.MatchString(id) {
			continue
		}
		path := filepath.Join(k.alarmDir, e.Name())
		rec, err := readAlarm(path)
		if err == nil && (rec.ID != id || rec.Next.IsZero() || rec.Every < 0 || !namePattern.MatchString(rec.Name)) {
			err = errors.New("it is not an alarm")
		}
		if err != nil {
			log.Printf("alarm %s: the record is not read, the alarm goes: %v", id, err)
			_ = os.Remove(path)
			continue
		}
		k.mu.Lock()
		k.alarms[id] = &ringing{rec: rec}
		k.mu.Unlock()
	}
}

// SetAlarm sets an alarm for the session a call comes from.
func (k *Keeper) SetAlarm(ctx context.Context, c Caller, ask AlarmAsk) (AlarmView, error) {
	owner, err := k.owners.Resolve(ctx, c)
	if err != nil {
		return AlarmView{}, err
	}
	now := k.now()
	first, every, err := ask.schedule(now)
	if err != nil {
		return AlarmView{}, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	own := 0
	for _, r := range k.alarms {
		if !r.rec.Owner.Same(owner) {
			continue
		}
		own++
		if r.rec.Name == ask.Name {
			return AlarmView{}, fmt.Errorf("this session has an alarm %s already: cancel it with alarm_cancel, "+
				"or give the new one another name", ask.Name)
		}
	}
	switch {
	case own >= AlarmsPerSession:
		return AlarmView{}, fmt.Errorf("this session has %d alarms, as many as a session keeps: "+
			"cancel one with alarm_cancel first", own)
	case len(k.alarms) >= AlarmsTotal:
		return AlarmView{}, fmt.Errorf("the host keeps %d alarms, as many as it takes: "+
			"one has to end before another is set", len(k.alarms))
	}
	rec := Alarm{ID: newID(), Name: ask.Name, Note: ask.Note, Every: every, Owner: owner, Set: now, Next: first}
	if err := os.MkdirAll(k.alarmDir, 0o700); err != nil {
		return AlarmView{}, fmt.Errorf("the directory of the alarms was not made: %w", err)
	}
	if err := writeAlarm(k.alarmDir, rec); err != nil {
		return AlarmView{}, fmt.Errorf("the record of the alarm was not written: %w", err)
	}
	k.alarms[rec.ID] = &ringing{rec: rec}
	log.Printf("alarm %s of %s set: %s", rec.Name, owner.Session, ringWords(rec.view(), now))
	return rec.view(), nil
}

// Alarms are the alarms of the session a call comes from, the soonest first.
func (k *Keeper) Alarms(ctx context.Context, c Caller) ([]AlarmView, error) {
	owner, err := k.owners.Resolve(ctx, c)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	out := []AlarmView{}
	for _, r := range k.alarms {
		if r.rec.Owner.Same(owner) {
			out = append(out, r.rec.view())
		}
	}
	slices.SortFunc(out, func(a, b AlarmView) int { return a.Next.Compare(b.Next) })
	return out, nil
}

// CancelAlarm ends an alarm of the session a call comes from, by its name.
func (k *Keeper) CancelAlarm(ctx context.Context, c Caller, name string) (AlarmView, error) {
	owner, err := k.owners.Resolve(ctx, c)
	if err != nil {
		return AlarmView{}, err
	}
	k.mu.Lock()
	var found *ringing
	var names []string
	for _, r := range k.alarms {
		switch {
		case !r.rec.Owner.Same(owner):
		case r.rec.Name == name:
			found = r
		default:
			names = append(names, r.rec.Name)
		}
	}
	k.mu.Unlock()
	if found == nil {
		if len(names) == 0 {
			return AlarmView{}, fmt.Errorf("this session has no alarm %s, and no alarms at all", name)
		}
		slices.Sort(names)
		return AlarmView{}, fmt.Errorf("this session has no alarm %s; it has %s", name, strings.Join(names, ", "))
	}
	found.busy.Lock()
	defer found.busy.Unlock()
	k.mu.Lock()
	view := found.rec.view()
	k.mu.Unlock()
	if !k.dropAlarm(found, "the session cancelled it") {
		return AlarmView{}, fmt.Errorf("alarm %s rang its last as it was cancelled", name)
	}
	return view, nil
}

// lookAtAlarms ends the alarms of sessions that are gone and rings the ones
// whose moment came.
func (k *Keeper) lookAtAlarms(ctx context.Context) {
	k.mu.Lock()
	all := make([]*ringing, 0, len(k.alarms))
	for _, r := range k.alarms {
		all = append(all, r)
	}
	k.mu.Unlock()
	for _, r := range all {
		k.lookAtAlarm(ctx, r)
	}
}

func (k *Keeper) lookAtAlarm(ctx context.Context, r *ringing) {
	r.busy.Lock()
	defer r.busy.Unlock()
	k.mu.Lock()
	kept, rec := k.alarms[r.rec.ID] == r, r.rec
	k.mu.Unlock()
	if !kept {
		return
	}
	alive, why := k.ownerStatus(rec.Owner, &r.gone, &rec.UnknownSince)
	if why != "" {
		k.dropAlarm(r, why)
		return
	}
	if rec.UnknownSince != r.rec.UnknownSince {
		k.mu.Lock()
		r.rec = rec
		k.mu.Unlock()
		if err := writeAlarm(k.alarmDir, rec); err != nil {
			log.Printf("alarm %s: the owner's availability was not written: %v", rec.Name, err)
		}
	}
	now := k.now()
	// A thread whose daemon is not read yet is rung once it is: a ring it
	// cannot take would be tried away.
	if !alive || now.Before(rec.Next) {
		return
	}
	var next time.Time
	if rec.Every > 0 {
		next = nextRing(rec.Next, rec.Every, now)
	}
	if err := k.owners.Wake(ctx, rec.Owner, "alarm "+rec.Name, alarmText(rec, now, next)); err != nil {
		r.tries++
		log.Printf("alarm %s of %s: the ring was not taken (%d of %d): %v",
			rec.Name, rec.Owner.Session, r.tries, wakeTries, err)
		if r.tries < wakeTries {
			return
		}
		log.Printf("alarm %s of %s: the ring due at %s is dropped untold", rec.Name, rec.Owner.Session,
			rec.Next.Format(time.RFC3339))
	} else {
		rec.Rang++
		rec.LastRing = now
	}
	r.tries = 0
	if rec.Every == 0 {
		why := "it rang once, as it was set to"
		if rec.Rang == 0 {
			why = "its one ring was never taken"
		}
		k.dropAlarm(r, why)
		return
	}
	rec.Next = next
	k.mu.Lock()
	r.rec = rec
	k.mu.Unlock()
	// A ring that went and was not written goes again from an executor
	// started again: a crash between the two costs a second letter, never a
	// lost one.
	if err := writeAlarm(k.alarmDir, rec); err != nil {
		log.Printf("alarm %s: the record was not written: %v", rec.Name, err)
	}
}

// dropAlarm takes an alarm away with its file, for the one that holds it, and
// says whether it was still there to take.
func (k *Keeper) dropAlarm(r *ringing, why string) bool {
	k.mu.Lock()
	if k.alarms[r.rec.ID] != r {
		k.mu.Unlock()
		return false
	}
	delete(k.alarms, r.rec.ID)
	rec := r.rec
	k.mu.Unlock()
	if err := os.Remove(filepath.Join(k.alarmDir, rec.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("alarm %s: its file was not removed: %v", rec.Name, err)
	}
	log.Printf("alarm %s of %s is gone: %s", rec.Name, rec.Owner.Session, why)
	return true
}

// nextRing is the first moment of a repeat past now: the rings it missed
// while nothing rang are skipped rather than made up, and the repeat keeps
// its beat.
func nextRing(due time.Time, every time.Duration, now time.Time) time.Time {
	return due.Add((now.Sub(due)/every + 1) * every)
}

// alarmText is the letter a session gets from its alarm: what it set the
// alarm for, its note, how late the ring is where it is, and what comes next.
func alarmText(rec Alarm, now, next time.Time) string {
	var b strings.Builder
	if rec.Every > 0 {
		fmt.Fprintf(&b, "Alarm %s, which this session set with alarm_set to ring every %s.\n", rec.Name,
			spanWords(rec.Every))
	} else {
		fmt.Fprintf(&b, "Alarm %s, which this session set with alarm_set for %s.\n", rec.Name, clock(rec.Next, now))
	}
	if rec.Note != "" {
		b.WriteString(rec.Note + "\n")
	}
	b.WriteString("\n")
	if late := now.Sub(rec.Next); late >= lateBy {
		fmt.Fprintf(&b, "It rings late: it was due at %s, when the panel could not ring it.", clock(rec.Next, now))
		if rec.Every > 0 {
			switch missed := int(late / rec.Every); {
			case missed == 1:
				b.WriteString(" The ring it missed since is not made up.")
			case missed > 1:
				fmt.Fprintf(&b, " The %d rings it missed since are not made up.", missed)
			}
		}
		b.WriteString("\n")
	}
	if rec.Every > 0 {
		fmt.Fprintf(&b, "It rings again at %s; alarm_cancel %s ends it.", clock(next, now), rec.Name)
	} else {
		b.WriteString("It rings no more.")
	}
	return b.String()
}

// ringWords say when an alarm rings, as a set and a list say it.
func ringWords(v AlarmView, now time.Time) string {
	if v.Every > 0 {
		return fmt.Sprintf("rings at %s and every %s after", clock(v.Next, now), spanWords(v.Every))
	}
	return "rings once, at " + clock(v.Next, now)
}

// describeAlarm is one alarm on one line.
func describeAlarm(v AlarmView, now time.Time) string {
	s := fmt.Sprintf("%s: %s; set at %s", v.Name, ringWords(v, now), clock(v.Set, now))
	switch v.Rang {
	case 0:
		s += ", has not rung yet"
	case 1:
		s += ", rang once at " + clock(v.LastRing, now)
	default:
		s += fmt.Sprintf(", rang %d times, last at %s", v.Rang, clock(v.LastRing, now))
	}
	if v.Note != "" {
		s += "; note: " + oneLine(v.Note, 120)
	}
	return s
}

// clock is a moment on the clock of now: its time alone on the day of now,
// with its date on another.
func clock(t, now time.Time) string {
	t = t.In(now.Location())
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format(time.TimeOnly)
	}
	return t.Format(time.DateTime)
}

// spanWords is a span the way a person writes it: 5m, 1h30m, 90s as 1m30s.
func spanWords(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}
