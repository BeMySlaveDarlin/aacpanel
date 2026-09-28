package plan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sid = "5a0c7d1e-2b3f-4a5b-8c6d-7e8f9a0b1c2d"

var t0 = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

func keep(t *testing.T, dir string, at time.Time, items ...Item) *Plan {
	t.Helper()
	p, err := Keep(dir, sid, 4242, items, "", at)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A step at work says since when, and a step done says when it ended: the
// time is the one the step took its status at, and it stays while the step
// keeps its text and its status, however often the list is sent again.
func TestAStepKeepsItsTimeWhileItsTextAndStatusStay(t *testing.T) {
	dir := t.TempDir()
	t1, t2 := t0.Add(10*time.Minute), t0.Add(25*time.Minute)

	keep(t, dir, t0, Item{Text: "read the code", Status: Active}, Item{Text: "write the tests", Status: Pending})
	keep(t, dir, t1, Item{Text: "read the code", Status: Done}, Item{Text: "write the tests", Status: Active})
	got := keep(t, dir, t2, Item{Text: "read the code", Status: Done}, Item{Text: "write the tests", Status: Active},
		Item{Text: "mutate", Status: Pending}, Item{Text: "the old idea", Status: Dropped})

	want := []string{t1.Format(Stamp), t1.Format(Stamp), "", ""}
	for i, it := range got.Items {
		if it.Since != want[i] {
			t.Errorf("step %q (%s) says since %q, meant %q", it.Text, it.Status, it.Since, want[i])
		}
	}
	if got.At != t2.Format(Stamp) || got.PID != 4242 || got.SessionID != sid {
		t.Errorf("the plan is stamped %+v", got)
	}
	if on := Read(dir, sid); on == nil || len(on.Items) != 4 || on.Items[1].Since != t1.Format(Stamp) {
		t.Errorf("the file holds %+v", on)
	}

	// Said otherwise, it is another step: the time starts again.
	got = keep(t, dir, t2.Add(time.Minute), Item{Text: "read the code again", Status: Done})
	if got.Items[0].Since != t2.Add(time.Minute).Format(Stamp) {
		t.Errorf("a step said otherwise kept the time of another: %+v", got.Items[0])
	}
}

func TestAnEmptyListClearsThePlan(t *testing.T) {
	dir := t.TempDir()
	keep(t, dir, t0, Item{Text: "one", Status: Active})
	if p := keep(t, dir, t0); p != nil {
		t.Errorf("an empty list was kept as a plan: %+v", p)
	}
	if _, err := os.Stat(Path(dir, sid)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file of a cleared plan is still there: %v", err)
	}
	if p := keep(t, dir, t0); p != nil {
		t.Errorf("clearing a plan that is not there failed: %+v", p)
	}
}

// The text of a step is one line on a phone; what the tool does not take is
// refused with what to change, and nothing is written.
func TestCleanSqueezesTheTextAndRefusesWhatItDoesNotTake(t *testing.T) {
	items, note, err := Clean([]Item{{Text: "  read\n the   code ", Status: Active, Since: "made up"}}, " waits\non CI ")
	if err != nil || items[0].Text != "read the code" || note != "waits on CI" || items[0].Since != "" {
		t.Errorf("clean gave %+v %q %v", items, note, err)
	}

	many := make([]Item, MaxItems+1)
	for i := range many {
		many[i] = Item{Text: "step", Status: Pending}
	}
	for name, c := range map[string]struct {
		items []Item
		note  string
		says  string
	}{
		"too many steps":    {many, "", "at most 40"},
		"an empty step":     {[]Item{{Text: " \n ", Status: Pending}}, "", "step 1 has no text"},
		"a step too long":   {[]Item{{Text: strings.Repeat("é", MaxText+1), Status: Pending}}, "", "longer than 200"},
		"an unknown status": {[]Item{{Text: "x", Status: "in_progress"}}, "", `"in_progress", not one of pending, active, done, dropped`},
		"a note too long":   {[]Item{{Text: "x", Status: Done}}, strings.Repeat("n", MaxNote+1), "note is longer than 300"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := Keep(dir, sid, 1, c.items, c.note, t0)
			var refused Refusal
			if !errors.As(err, &refused) || !strings.Contains(refused.Why, c.says) {
				t.Errorf("refused with %v, meant a refusal saying %q", err, c.says)
			}
			if Read(dir, sid) != nil {
				t.Error("a refused plan was written")
			}
		})
	}

	exact := []Item{{Text: strings.Repeat("é", MaxText), Status: Pending}}
	if _, _, err := Clean(exact, strings.Repeat("n", MaxNote)); err != nil {
		t.Errorf("a step at the limit was refused: %v", err)
	}
}

// The conversation id names a file: one that climbs out of the directory, or
// is not an id at all, is not written anywhere.
func TestKeepWritesOnlyUnderAConversationID(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", "../escape", ".hidden", "a/b"} {
		if _, err := Keep(dir, id, 1, []Item{{Text: "x", Status: Active}}, "", t0); err == nil {
			t.Errorf("%q was taken for a conversation", id)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("something was written for them: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.json")); err == nil {
		t.Error("a plan was written outside the directory of the plans")
	}
}

// The file is replaced by a rename, and nothing of the write is left beside it.
func TestKeepLeavesOnlyThePlan(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plans")
	keep(t, dir, t0, Item{Text: "one", Status: Active})
	keep(t, dir, t0, Item{Text: "one", Status: Done})
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != sid+".json" {
		t.Errorf("the directory holds %v %v", entries, err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the plans directory is open to others: %v %v", info.Mode(), err)
	}
}

func TestSweepTakesOnlyWhatIsOld(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for name, age := range map[string]time.Duration{
		"old.json":      MaxAge + time.Hour,
		"fresh.json":    time.Hour,
		".plan-123":     MaxAge + time.Hour,
		"old-other.txt": MaxAge + time.Hour,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	if gone := Sweep(dir, now); gone != 2 {
		t.Errorf("%d files swept, meant the old plan and the unfinished write", gone)
	}
	for name, left := range map[string]bool{"old.json": false, ".plan-123": false, "fresh.json": true, "old-other.txt": true} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != left {
			t.Errorf("%s: left %v, meant %v", name, err == nil, left)
		}
	}
	if Sweep(filepath.Join(dir, "none"), now) != 0 {
		t.Error("a directory that is not there swept something")
	}
}
