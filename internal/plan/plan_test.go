package plan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/mcp"
)

const sid = "5a0c7d1e-2b3f-4a5b-8c6d-7e8f9a0b1c2d"

var t0 = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

// lab is the place most tests keep a plan in.
var lab = mcp.Place{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab"}

func bound(id string, pid int) mcp.Binding { return mcp.Binding{Place: lab, SessionID: id, PID: pid} }

func keep(t *testing.T, dir string, at time.Time, items ...Item) *Plan {
	t.Helper()
	p, err := Keep(dir, bound(sid, 4242), items, "", at)
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
	if got.At != t2.Format(Stamp) || got.PID != 4242 || got.SessionID != sid ||
		got.ConfigDir != lab.ConfigDir || got.Dir != lab.Dir {
		t.Errorf("the plan is stamped %+v", got)
	}
	if on := Read(dir, lab); on == nil || len(on.Items) != 4 || on.Items[1].Since != t1.Format(Stamp) {
		t.Errorf("the file holds %+v", on)
	}

	// Said otherwise, it is another step: the time starts again.
	got = keep(t, dir, t2.Add(time.Minute), Item{Text: "read the code again", Status: Done})
	if got.Items[0].Since != t2.Add(time.Minute).Format(Stamp) {
		t.Errorf("a step said otherwise kept the time of another: %+v", got.Items[0])
	}
}

// The plan is the place's, not the conversation's: a session started again in
// the place — another conversation, another process — reads the plan the one
// before it left and goes on with it, the times of its steps kept, and the
// plan then says who sent it last. Another directory, or the same directory
// under another account, is another place.
func TestThePlanBelongsToThePlaceAndOutlivesTheConversation(t *testing.T) {
	dir := t.TempDir()
	keep(t, dir, t0, Item{Text: "read the code", Status: Done}, Item{Text: "write the tests", Status: Active})

	const restarted = "6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e"
	if on := Read(dir, mcp.Place{ConfigDir: "/srv/claude/", Dir: "/srv/proj/./lab"}); on == nil || on.SessionID != sid {
		t.Fatalf("the place written another way does not find its plan: %+v", on)
	}
	got, err := Keep(dir, bound(restarted, 5151), []Item{{Text: "read the code", Status: Done},
		{Text: "write the tests", Status: Done}, {Text: "mutate", Status: Active}}, "", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != restarted || got.PID != 5151 || got.Items[0].Since != t0.Format(Stamp) {
		t.Errorf("the plan after a restart is %+v", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the restart left a second file: %v", entries)
	}

	for _, other := range []mcp.Place{{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab-2"}, {ConfigDir: "/srv/claude-work", Dir: "/srv/proj/lab"}} {
		if on := Read(dir, other); on != nil {
			t.Errorf("%+v reads the plan of another place: %+v", other, on)
		}
	}
}

// The name of the file is a hash of the place: the collector and the hook
// compute it too, from the same two paths, so it is pinned here.
func TestTheNameOfThePlanIsTheHashOfThePlace(t *testing.T) {
	if got := Name(lab); got != "d2ba143628e62863dae2533062199cf6.json" {
		t.Errorf("the plan of %+v is named %q", lab, got)
	}
	if Name(mcp.Place{ConfigDir: "/srv/claude//", Dir: "/srv/proj/lab/"}) != Name(lab) {
		t.Error("a place written with slashes to spare is another place")
	}
	for _, p := range []mcp.Place{{ConfigDir: "srv/claude", Dir: "/srv/proj/lab"}, {ConfigDir: "/srv/claude"}, {}} {
		if Name(p) != "" {
			t.Errorf("%+v is taken for a place", p)
		}
	}
}

// A file whose place is not the one asked for is not its plan, whatever
// its name.
func TestAFileOfAnotherPlaceIsNotThePlan(t *testing.T) {
	dir := t.TempDir()
	body, _ := json.Marshal(Plan{ConfigDir: "/srv/claude", Dir: "/srv/proj/other", At: t0.Format(Stamp),
		Items: []Item{{Text: "x", Status: Active}}})
	if err := os.WriteFile(filepath.Join(dir, Name(lab)), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if on := Read(dir, lab); on != nil {
		t.Errorf("the plan of another place was read: %+v", on)
	}
}

func TestAnEmptyListClearsThePlan(t *testing.T) {
	dir := t.TempDir()
	keep(t, dir, t0, Item{Text: "one", Status: Active})
	if p := keep(t, dir, t0); p != nil {
		t.Errorf("an empty list was kept as a plan: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(dir, Name(lab))); !errors.Is(err, os.ErrNotExist) {
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
			_, err := Keep(dir, bound(sid, 1), c.items, c.note, t0)
			var refused Refusal
			if !errors.As(err, &refused) || !strings.Contains(refused.Why, c.says) {
				t.Errorf("refused with %v, meant a refusal saying %q", err, c.says)
			}
			if Read(dir, lab) != nil {
				t.Error("a refused plan was written")
			}
		})
	}

	exact := []Item{{Text: strings.Repeat("é", MaxText), Status: Pending}}
	if _, _, err := Clean(exact, strings.Repeat("n", MaxNote)); err != nil {
		t.Errorf("a step at the limit was refused: %v", err)
	}
}

// A place that is not one — a path that is not absolute — keeps no plan, and
// a conversation id that is not one is not written into a plan either. A
// session whose conversation is not known yet keeps its plan all the same:
// the place is the key.
func TestKeepWritesOnlyForAPlace(t *testing.T) {
	dir := t.TempDir()
	one := []Item{{Text: "x", Status: Active}}
	for name, b := range map[string]mcp.Binding{
		"a relative directory":       {Place: mcp.Place{ConfigDir: "/srv/claude", Dir: "proj/lab"}, SessionID: sid},
		"no account":                 {Place: mcp.Place{Dir: "/srv/proj/lab"}, SessionID: sid},
		"no place at all":            {SessionID: sid},
		"a conversation that climbs": {Place: lab, SessionID: "../escape"},
	} {
		if _, err := Keep(dir, b, one, "", t0); err == nil {
			t.Errorf("%s: the plan was kept", name)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("something was written for them: %v", entries)
	}
	if p, err := Keep(dir, mcp.Binding{Place: lab, PID: 7}, one, "", t0); err != nil || p.SessionID != "" || Read(dir, lab) == nil {
		t.Errorf("a session with no conversation yet: %+v %v", p, err)
	}
}

// The file is replaced by a rename, and nothing of the write is left beside it.
func TestKeepLeavesOnlyThePlan(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plans")
	keep(t, dir, t0, Item{Text: "one", Status: Active})
	keep(t, dir, t0, Item{Text: "one", Status: Done})
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != Name(lab) {
		t.Errorf("the directory holds %v %v", entries, err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the plans directory is open to others: %v %v", info.Mode(), err)
	}
}

// filed writes a plan the way a server that keeps one plan a conversation
// does: named by the conversation, with no place in it.
func filed(t *testing.T, dir, id, at string, items ...Item) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"sessionId": id, "pid": 99, "at": at, "items": items})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// transcriptOf puts the transcript of a conversation where claude keeps it:
// under the account, in the directory of the project.
func transcriptOf(t *testing.T, place mcp.Place, id string) {
	t.Helper()
	project := filepath.Join(place.ConfigDir, "projects", projectSlug(place.Dir))
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A plan filed under a conversation is taken over by its place: the one
// whose project holds the transcript of that conversation, under the same
// account. The newest of them becomes the plan of the place, with its times;
// the files taken over go, and a plan of another place stays where it is.
func TestAPlanFiledUnderAConversationIsTakenOverByItsPlace(t *testing.T) {
	dir, config := t.TempDir(), t.TempDir()
	here := mcp.Place{ConfigDir: config, Dir: "/srv/proj/lab"}
	const older, newer, elsewhere = "11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"
	transcriptOf(t, here, older)
	transcriptOf(t, here, newer)
	transcriptOf(t, mcp.Place{ConfigDir: config, Dir: "/srv/proj/other"}, elsewhere)
	filed(t, dir, older, "2026-09-28T09:00:00Z", Item{Text: "the old plan", Status: Active})
	filed(t, dir, newer, "2026-09-28T10:00:00Z", Item{Text: "read the code", Status: Done, Since: "2026-09-28T09:30:00Z"},
		Item{Text: "write the tests", Status: Active, Since: "2026-09-28T09:45:00Z"})
	filed(t, dir, elsewhere, "2026-09-28T11:00:00Z", Item{Text: "another place", Status: Active})

	got := Adopt(dir, here)
	if got == nil || got.SessionID != newer || len(got.Items) != 2 || got.Items[1].Since != "2026-09-28T09:45:00Z" ||
		got.ConfigDir != config || got.Dir != here.Dir {
		t.Fatalf("the place took over %+v", got)
	}
	if on := Read(dir, here); on == nil || on.At != "2026-09-28T10:00:00Z" {
		t.Errorf("the plan taken over is not on disk as the place's: %+v", on)
	}
	for id, left := range map[string]bool{older: false, newer: false, elsewhere: true} {
		if _, err := os.Stat(filepath.Join(dir, id+".json")); (err == nil) != left {
			t.Errorf("%s: left %v, meant %v", id, err == nil, left)
		}
	}

	// A place with a plan of its own takes nothing over.
	filed(t, dir, older, "2026-09-28T12:00:00Z", Item{Text: "late", Status: Active})
	if again := Adopt(dir, here); again == nil || again.SessionID != newer {
		t.Errorf("a place with a plan took another over: %+v", again)
	}
	if _, err := os.Stat(filepath.Join(dir, older+".json")); err != nil {
		t.Errorf("a file the place did not take over went: %v", err)
	}

	// A place with nothing filed under its conversations has no plan.
	if none := Adopt(dir, mcp.Place{ConfigDir: config, Dir: "/srv/proj/empty"}); none != nil {
		t.Errorf("an empty place took over %+v", none)
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
