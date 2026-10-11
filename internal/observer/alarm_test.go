package observer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// testClock is the keeper's clock in a test, moved by hand: an alarm rings
// when the clock says its moment came, not when the test has sat it out.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) move(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// alarmed is a keeper of alarms in a directory of the test, on a clock that
// stands at 21:30 until the test moves it.
func alarmed(t *testing.T) (*Keeper, *fakeOwners, *testClock, string) {
	t.Helper()
	quick(t)
	dir := filepath.Join(t.TempDir(), "observers")
	owners := &fakeOwners{alive: true}
	clock := &testClock{t: time.Date(2026, 10, 11, 21, 30, 0, 0, time.Local)}
	k := NewKeeper(dir, newUnits(), owners)
	k.now = clock.now
	return k, owners, clock, dir
}

// files are the alarms kept on the disk beside the observers of dir.
func files(dir string) []string {
	entries, _ := os.ReadDir(alarmDir(dir))
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// still waits out a few looks and says how many letters the session has then.
func still(owners *fakeOwners) int {
	time.Sleep(10 * step)
	return len(owners.got())
}

// An alarm set to ring once rings at its moment and not before, signed by
// the alarm and with what the session told itself to do, and then it is
// gone: from the list, from the disk, and it rings no second time.
func TestAnAlarmRingsOnceAtItsMoment(t *testing.T) {
	k, owners, clock, dir := alarmed(t)
	keeping(t, k)
	v, err := k.SetAlarm(context.Background(), lab, AlarmAsk{Name: "build", In: "10m", Note: "Look at the build."})
	if err != nil {
		t.Fatal(err)
	}
	if want := clock.now().Add(10 * time.Minute); !v.Next.Equal(want) || v.Every != 0 {
		t.Errorf("the alarm is set for %v every %v, meant %v once", v.Next, v.Every, want)
	}
	clock.move(9*time.Minute + 59*time.Second)
	if n := still(owners); n != 0 {
		t.Fatalf("the alarm rang %d times before its moment", n)
	}
	clock.move(time.Second)
	waitFor(t, "the ring", func() bool { return len(owners.got()) == 1 })
	ring := owners.got()[0]
	for _, say := range []string{"Alarm build, which this session set with alarm_set for 21:40:00.",
		"Look at the build.", "It rings no more."} {
		if !strings.Contains(ring, say) {
			t.Errorf("the ring does not say %q:\n%s", say, ring)
		}
	}
	if strings.Contains(ring, "late") || !slices.Equal(owners.from, []string{"alarm build"}) {
		t.Errorf("the ring signed %v reads\n%s", owners.from, ring)
	}
	clock.move(time.Hour)
	if n := still(owners); n != 1 {
		t.Errorf("an alarm set to ring once rang %d times", n)
	}
	if list, err := k.Alarms(context.Background(), lab); err != nil || len(list) != 0 {
		t.Errorf("the list after the ring is %+v, %v", list, err)
	}
	if left := files(dir); len(left) != 0 {
		t.Errorf("an alarm that rang left %v", left)
	}
}

// A repeat rings every span, a letter a ring that says when the next comes,
// and is listed with how often it rang; a cancel ends it and its file.
func TestARepeatRingsEverySpanUntilCancelled(t *testing.T) {
	k, owners, clock, dir := alarmed(t)
	keeping(t, k)
	ctx := context.Background()
	if _, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "poll", Every: "5m", Note: "Look at the queue."}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		clock.move(5 * time.Minute)
		waitFor(t, "a ring", func() bool { return len(owners.got()) == i })
	}
	rings := owners.got()
	for i, next := range []string{"21:40:00", "21:45:00", "21:50:00"} {
		if !strings.Contains(rings[i], "set with alarm_set to ring every 5m.\nLook at the queue.") ||
			!strings.Contains(rings[i], "It rings again at "+next+"; alarm_cancel poll ends it.") {
			t.Errorf("ring %d does not say the next at %s:\n%s", i+1, next, rings[i])
		}
	}
	list, err := k.Alarms(ctx, lab)
	if err != nil || len(list) != 1 || list[0].Rang != 3 || !list[0].Next.Equal(clock.now().Add(5*time.Minute)) {
		t.Fatalf("the list is %+v, %v", list, err)
	}
	if line := describeAlarm(list[0], clock.now()); !strings.HasPrefix(line,
		"poll: rings at 21:50:00 and every 5m after; set at 21:30:00, rang 3 times, last at 21:45:00") {
		t.Errorf("the alarm is described as %q", line)
	}
	if _, err := k.CancelAlarm(ctx, lab, "nothing"); err == nil || !strings.Contains(err.Error(), "it has poll") {
		t.Errorf("a cancel of no alarm answered %v", err)
	}
	if _, err := k.CancelAlarm(ctx, Caller{Conversation: "other", PID: 1}, "poll"); err == nil {
		t.Error("another session cancelled the alarm")
	}
	if _, err := k.CancelAlarm(ctx, lab, "poll"); err != nil {
		t.Fatal(err)
	}
	clock.move(time.Hour)
	if n := still(owners); n != 3 {
		t.Errorf("a cancelled alarm rang: %d rings", n)
	}
	if left := files(dir); len(left) != 0 {
		t.Errorf("a cancelled alarm left %v", left)
	}
}

// An executor started again keeps the alarms the last one left: a repeat
// goes on from where it rang, and a ring due while no executor ran comes
// once, late and saying so, the rings a repeat missed skipped rather than
// made up — and the repeat keeps its beat.
func TestAnExecutorStartedAgainRingsWhatCameDueOnce(t *testing.T) {
	k, owners, clock, dir := alarmed(t)
	stop := keeping(t, k)
	ctx := context.Background()
	if _, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "poll", Every: "5m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "once", At: "21:45"}); err != nil {
		t.Fatal(err)
	}
	clock.move(5 * time.Minute)
	waitFor(t, "the first ring", func() bool { return len(owners.got()) == 1 })
	stop()

	clock.move(17 * time.Minute)
	again := &fakeOwners{alive: true}
	k2 := NewKeeper(dir, newUnits(), again)
	k2.now = clock.now
	keeping(t, k2)
	waitFor(t, "the rings due while no executor ran", func() bool { return len(again.got()) == 2 })
	if n := still(again); n != 2 {
		t.Fatalf("the executor started again rang %d times for two alarms", n)
	}
	byName := map[string]string{}
	for i, from := range again.from {
		byName[from] = again.got()[i]
	}
	if ring := byName["alarm poll"]; !strings.Contains(ring, "It rings late: it was due at 21:40:00") ||
		!strings.Contains(ring, "The 2 rings it missed since are not made up.") ||
		!strings.Contains(ring, "It rings again at 21:55:00") {
		t.Errorf("the repeat rang late as\n%s", ring)
	}
	if ring := byName["alarm once"]; !strings.Contains(ring, "It rings late: it was due at 21:45:00") ||
		!strings.Contains(ring, "It rings no more.") {
		t.Errorf("the alarm due once rang late as\n%s", ring)
	}
	list, err := k2.Alarms(ctx, lab)
	if err != nil || len(list) != 1 || list[0].Name != "poll" || list[0].Rang != 2 {
		t.Fatalf("the list after the start is %+v, %v", list, err)
	}
	clock.move(3 * time.Minute)
	waitFor(t, "the repeat on its beat", func() bool { return len(again.got()) == 3 })
	if ring := again.got()[2]; strings.Contains(ring, "late") || !strings.Contains(ring, "It rings again at 22:00:00") {
		t.Errorf("the repeat lost its beat:\n%s", ring)
	}
}

// An alarm ends with its session: once the session is gone it rings no more,
// and its file goes.
func TestAnAlarmEndsWithItsSession(t *testing.T) {
	k, owners, clock, dir := alarmed(t)
	keeping(t, k)
	if _, err := k.SetAlarm(context.Background(), lab, AlarmAsk{Name: "poll", Every: "1m"}); err != nil {
		t.Fatal(err)
	}
	owners.die()
	clock.move(time.Minute)
	waitFor(t, "the file of the alarm to go", func() bool { return len(files(dir)) == 0 })
	if n := still(owners); n != 0 {
		t.Errorf("a session that ended was rung %d times", n)
	}
}

// The panel refuses an alarm it does not take with what to change, and a
// session keeps a bounded number of alarms, each under a name of its own.
func TestAnAlarmIsRefusedWhatThePanelDoesNotTake(t *testing.T) {
	k, owners, clock, _ := alarmed(t)
	ctx := context.Background()
	for _, c := range []struct {
		ask  AlarmAsk
		says string
	}{
		{AlarmAsk{Name: "-x", In: "1m"}, "is not one"},
		{AlarmAsk{Name: "a"}, "nothing says when"},
		{AlarmAsk{Name: "a", In: "1m", At: "23:00"}, "give one of them"},
		{AlarmAsk{Name: "a", In: "soon"}, "is not a span ahead"},
		{AlarmAsk{Name: "a", In: "-5m"}, "is not a span ahead"},
		{AlarmAsk{Name: "a", Every: "59s"}, "less than 1m"},
		{AlarmAsk{Name: "a", Every: "169h"}, "more than 7 days"},
		{AlarmAsk{Name: "a", In: "169h"}, "more than 7 days ahead"},
		{AlarmAsk{Name: "a", At: clock.now().Add(-time.Minute).Format(time.RFC3339)}, "has passed"},
		{AlarmAsk{Name: "a", At: "noon"}, "is not a moment"},
		{AlarmAsk{Name: "a", In: "1m", Note: strings.Repeat("x", TextMax+1)}, "longer than"},
	} {
		if _, err := k.SetAlarm(ctx, lab, c.ask); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%+v: %v", c.ask, err)
		}
	}
	for i := range AlarmsPerSession {
		if _, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "a" + string(rune('a'+i)), In: "1h"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "more", In: "1h"}); err == nil ||
		!strings.Contains(err.Error(), "as many as a session keeps") {
		t.Errorf("an alarm past the bound answered %v", err)
	}
	if _, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "aa", In: "1h"}); err == nil ||
		!strings.Contains(err.Error(), "has an alarm aa already") {
		t.Errorf("a second alarm of the name answered %v", err)
	}
	owners.refuse = errors.New("session lab is not on the stream")
	if _, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "z", In: "1m"}); err == nil ||
		err.Error() != "session lab is not on the stream" {
		t.Errorf("a refused session was answered %v", err)
	}
}

// A moment is a time of the day on the clock of the host, the next to come,
// or a moment of RFC 3339 as it is.
func TestAMomentIsTheNextOfItsTimeOfTheDay(t *testing.T) {
	now := time.Date(2026, 10, 11, 21, 30, 0, 0, time.Local)
	for at, want := range map[string]time.Time{
		"23:00":                     time.Date(2026, 10, 11, 23, 0, 0, 0, time.Local),
		"21:30:01":                  time.Date(2026, 10, 11, 21, 30, 1, 0, time.Local),
		"21:30":                     time.Date(2026, 10, 12, 21, 30, 0, 0, time.Local),
		"9:15":                      time.Date(2026, 10, 12, 9, 15, 0, 0, time.Local),
		"2026-10-12T09:00:00+03:00": time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC),
	} {
		first, every, err := AlarmAsk{Name: "a", At: at}.schedule(now)
		if err != nil || !first.Equal(want) || every != 0 {
			t.Errorf("at %s rings first at %v every %v (%v), meant %v", at, first, every, err, want)
		}
	}
	if first, every, err := (AlarmAsk{Name: "a", Every: "1h30m"}).schedule(now); err != nil ||
		!first.Equal(now.Add(90*time.Minute)) || spanWords(every) != "1h30m" {
		t.Errorf("every alone rings first at %v every %v (%v)", first, every, err)
	}
}
