package checklist

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

// lab is the place most tests keep a checklist in.
var lab = mcp.Place{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab"}

// bound is a session in lab started without a name; named is one started
// with it.
func bound(id string, pid int) mcp.Binding { return mcp.Binding{Place: lab, SessionID: id, PID: pid} }

func named(name, id string, pid int) mcp.Binding {
	return mcp.Binding{Place: lab, Name: name, SessionID: id, PID: pid}
}

func keep(t *testing.T, dir string, at time.Time, items ...Item) *Checklist {
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
		t.Errorf("the checklist is stamped %+v", got)
	}
	if on := Read(dir, lab, ""); on == nil || len(on.Items) != 4 || on.Items[1].Since != t1.Format(Stamp) {
		t.Errorf("the file holds %+v", on)
	}

	// Said otherwise, it is another step: the time starts again.
	got = keep(t, dir, t2.Add(time.Minute), Item{Text: "read the code again", Status: Done})
	if got.Items[0].Since != t2.Add(time.Minute).Format(Stamp) {
		t.Errorf("a step said otherwise kept the time of another: %+v", got.Items[0])
	}
}

// The checklist is the session's — its place and its name — and not the
// conversation's: a session started again under its name — another
// conversation, another process — reads the checklist the one before it left
// and goes on with it, the times of its steps kept, and the checklist then
// says who sent it last. Another session of the directory, under another
// name, starts with none and keeps its own beside it; another directory, or
// the same directory under another account, is another place.
func TestTheChecklistBelongsToTheSessionAndOutlivesItsConversation(t *testing.T) {
	dir := t.TempDir()
	if _, err := Keep(dir, named("lab", sid, 4242), []Item{{Text: "read the code", Status: Done},
		{Text: "write the tests", Status: Active}}, "", t0); err != nil {
		t.Fatal(err)
	}

	const restarted = "6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e"
	if on := Adopt(dir, named("lab", restarted, 5151)); on == nil || on.SessionID != sid || on.Name != "lab" {
		t.Fatalf("the session started again under its name does not find its checklist: %+v", on)
	}
	got, err := Keep(dir, named("lab", restarted, 5151), []Item{{Text: "read the code", Status: Done},
		{Text: "write the tests", Status: Done}, {Text: "mutate", Status: Active}}, "", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != restarted || got.PID != 5151 || got.Name != "lab" || got.Items[0].Since != t0.Format(Stamp) {
		t.Errorf("the checklist after a restart is %+v", got)
	}

	review := named("lab-review", "7c2e9f3a-4d5b-4c6d-8e7f-9a0b1c2d3e4f", 6262)
	if on := Adopt(dir, review); on != nil {
		t.Errorf("another session of the directory found the checklist of this one: %+v", on)
	}
	if _, err := Keep(dir, review, []Item{{Text: "review the change", Status: Active}}, "", t0); err != nil {
		t.Fatal(err)
	}
	if on := Read(dir, lab, "lab"); on == nil || len(on.Items) != 3 || on.SessionID != restarted {
		t.Errorf("the checklist of another session took the place of this one: %+v", on)
	}
	if on := Read(dir, lab, "lab-review"); on == nil || len(on.Items) != 1 || on.Name != "lab-review" {
		t.Errorf("the other session keeps %+v", on)
	}
	if on := Read(dir, lab, ""); on != nil {
		t.Errorf("a session without a name reads the checklist of a named one: %+v", on)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("two sessions keep %d files: %v", len(entries), entries)
	}

	for _, other := range []mcp.Place{{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab-2"}, {ConfigDir: "/srv/claude-work", Dir: "/srv/proj/lab"}} {
		if on := Read(dir, other, "lab"); on != nil {
			t.Errorf("%+v reads the checklist of another place: %+v", other, on)
		}
	}
}

// A session started without a name is told by its place alone: another
// session started there without one — another conversation, another process
// — reads the checklist the one before it left and goes on with it.
func TestASessionWithoutANameKeepsTheChecklistOfItsPlace(t *testing.T) {
	dir := t.TempDir()
	keep(t, dir, t0, Item{Text: "read the code", Status: Done}, Item{Text: "write the tests", Status: Active})

	const restarted = "6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e"
	if on := Read(dir, mcp.Place{ConfigDir: "/srv/claude/", Dir: "/srv/proj/./lab"}, ""); on == nil || on.SessionID != sid {
		t.Fatalf("the place written another way does not find its checklist: %+v", on)
	}
	got, err := Keep(dir, bound(restarted, 5151), []Item{{Text: "read the code", Status: Done},
		{Text: "write the tests", Status: Done}, {Text: "mutate", Status: Active}}, "", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != restarted || got.PID != 5151 || got.Items[0].Since != t0.Format(Stamp) {
		t.Errorf("the checklist after a restart is %+v", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the restart left a second file: %v", entries)
	}

	for _, other := range []mcp.Place{{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab-2"}, {ConfigDir: "/srv/claude-work", Dir: "/srv/proj/lab"}} {
		if on := Read(dir, other, ""); on != nil {
			t.Errorf("%+v reads the checklist of another place: %+v", other, on)
		}
	}
}

// The name of the file is a hash of the place and the name of the session, or
// of the place alone for a session without a name: the collector and the
// hook compute it too, from the same paths and name, so it is pinned here.
func TestTheNameOfTheChecklistIsTheHashOfTheSession(t *testing.T) {
	if got := File(lab, "lab"); got != "f7138acc90830a36ac4a7481c26dfb18.json" {
		t.Errorf("the checklist of the session lab in %+v is named %q", lab, got)
	}
	if got := File(lab, ""); got != "d2ba143628e62863dae2533062199cf6.json" {
		t.Errorf("the checklist of a session without a name in %+v is named %q", lab, got)
	}
	if File(lab, "lab-review") == File(lab, "lab") {
		t.Error("two sessions of one place share a file")
	}
	if File(mcp.Place{ConfigDir: "/srv/claude//", Dir: "/srv/proj/lab/"}, "lab") != File(lab, "lab") {
		t.Error("a place written with slashes to spare is another place")
	}
	for _, p := range []mcp.Place{{ConfigDir: "srv/claude", Dir: "/srv/proj/lab"}, {ConfigDir: "/srv/claude"}, {}} {
		if File(p, "lab") != "" {
			t.Errorf("%+v is taken for a place", p)
		}
	}
}

// A file whose place or session is not the one asked for is not its
// checklist, whatever its name.
func TestAFileOfAnotherSessionIsNotTheChecklist(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		file string
		on   Checklist
	}{
		{File(lab, ""), Checklist{ConfigDir: "/srv/claude", Dir: "/srv/proj/other"}},
		{File(lab, ""), Checklist{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab", Name: "lab"}},
		{File(lab, "lab"), Checklist{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab", Name: "lab-review"}},
		{File(lab, "lab"), Checklist{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab"}},
	} {
		c.on.At, c.on.Items = t0.Format(Stamp), []Item{{Text: "x", Status: Active}}
		body, _ := json.Marshal(c.on)
		if err := os.WriteFile(filepath.Join(dir, c.file), body, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"", "lab"} {
			if on := Read(dir, lab, name); on != nil {
				t.Errorf("the session %q read the checklist of %+v", name, c.on)
			}
		}
	}
}

// A server that does not tell the sessions of a place apart keeps
// writing the one checklist of the place, under the conversation that sent it
// last. A named session with no checklist of its own takes it over while
// that conversation is its own — as its checklist, the file of the place gone
// — and leaves it where it is when the conversation is another's.
func TestANamedSessionTakesOverTheChecklistOfItsPlaceOnlyWhenItSentIt(t *testing.T) {
	dir := t.TempDir()
	const theirs = "7c2e9f3a-4d5b-4c6d-8e7f-9a0b1c2d3e4f"
	if _, err := Keep(dir, bound(theirs, 99), []Item{{Text: "another session's", Status: Active}}, "", t0); err != nil {
		t.Fatal(err)
	}
	for _, b := range []mcp.Binding{named("lab", sid, 4242), named("lab", "", 4242)} {
		if on := Adopt(dir, b); on != nil {
			t.Errorf("%+v took over the checklist another conversation sent: %+v", b, on)
		}
	}
	if Read(dir, lab, "") == nil || Read(dir, lab, "lab") != nil {
		t.Error("the checklist of another conversation was moved")
	}

	if _, err := Keep(dir, bound(sid, 4242), []Item{{Text: "read the code", Status: Active}}, "waits on CI", t0); err != nil {
		t.Fatal(err)
	}
	got := Adopt(dir, named("lab", sid, 4242))
	if got == nil || got.Name != "lab" || got.Note != "waits on CI" || got.Items[0].Since != t0.Format(Stamp) {
		t.Fatalf("the session took over %+v", got)
	}
	if on := Read(dir, lab, "lab"); on == nil || on.SessionID != sid {
		t.Errorf("the checklist taken over is not on disk as the session's: %+v", on)
	}
	if _, err := os.Stat(filepath.Join(dir, File(lab, ""))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file of the place stayed after it was taken over: %v", err)
	}
}

func TestAnEmptyListClearsTheChecklist(t *testing.T) {
	dir := t.TempDir()
	keep(t, dir, t0, Item{Text: "one", Status: Active})
	if p := keep(t, dir, t0); p != nil {
		t.Errorf("an empty list was kept as a checklist: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(dir, File(lab, ""))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file of a cleared checklist is still there: %v", err)
	}
	if p := keep(t, dir, t0); p != nil {
		t.Errorf("clearing a checklist that is not there failed: %+v", p)
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
			if Read(dir, lab, "") != nil {
				t.Error("a refused checklist was written")
			}
		})
	}

	exact := []Item{{Text: strings.Repeat("é", MaxText), Status: Pending}}
	if _, _, err := Clean(exact, strings.Repeat("n", MaxNote)); err != nil {
		t.Errorf("a step at the limit was refused: %v", err)
	}
}

// A place that is not one — a path that is not absolute — keeps no checklist,
// and a conversation id that is not one is not written into a checklist
// either. A session whose conversation is not known yet keeps its checklist
// all the same: the conversation is not the key.
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
			t.Errorf("%s: the checklist was kept", name)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("something was written for them: %v", entries)
	}
	if p, err := Keep(dir, mcp.Binding{Place: lab, PID: 7}, one, "", t0); err != nil || p.SessionID != "" || Read(dir, lab, "") == nil {
		t.Errorf("a session with no conversation yet: %+v %v", p, err)
	}
}

// The file is replaced by a rename, and nothing of the write is left beside it.
func TestKeepLeavesOnlyTheChecklist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "checklists")
	keep(t, dir, t0, Item{Text: "one", Status: Active})
	keep(t, dir, t0, Item{Text: "one", Status: Done})
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != File(lab, "") {
		t.Errorf("the directory holds %v %v", entries, err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the checklists directory is open to others: %v %v", info.Mode(), err)
	}
}

// filed writes a checklist the way a server that keeps one checklist a
// conversation does: named by the conversation, with no place in it.
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

// A checklist filed under a conversation is taken over by its place: the one
// whose project holds the transcript of that conversation, under the same
// account. The newest of them becomes the checklist of the place, with its
// times; the files taken over go, and a checklist of another place stays
// where it is.
func TestAChecklistFiledUnderAConversationIsTakenOverByItsPlace(t *testing.T) {
	dir, config := t.TempDir(), t.TempDir()
	here := mcp.Place{ConfigDir: config, Dir: "/srv/proj/lab"}
	const older, newer, elsewhere = "11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"
	transcriptOf(t, here, older)
	transcriptOf(t, here, newer)
	transcriptOf(t, mcp.Place{ConfigDir: config, Dir: "/srv/proj/other"}, elsewhere)
	filed(t, dir, older, "2026-09-28T09:00:00Z", Item{Text: "the old checklist", Status: Active})
	filed(t, dir, newer, "2026-09-28T10:00:00Z", Item{Text: "read the code", Status: Done, Since: "2026-09-28T09:30:00Z"},
		Item{Text: "write the tests", Status: Active, Since: "2026-09-28T09:45:00Z"})
	filed(t, dir, elsewhere, "2026-09-28T11:00:00Z", Item{Text: "another place", Status: Active})

	got := Adopt(dir, mcp.Binding{Place: here})
	if got == nil || got.SessionID != newer || len(got.Items) != 2 || got.Items[1].Since != "2026-09-28T09:45:00Z" ||
		got.ConfigDir != config || got.Dir != here.Dir {
		t.Fatalf("the place took over %+v", got)
	}
	if on := Read(dir, here, ""); on == nil || on.At != "2026-09-28T10:00:00Z" {
		t.Errorf("the checklist taken over is not on disk as the place's: %+v", on)
	}
	for id, left := range map[string]bool{older: false, newer: false, elsewhere: true} {
		if _, err := os.Stat(filepath.Join(dir, id+".json")); (err == nil) != left {
			t.Errorf("%s: left %v, meant %v", id, err == nil, left)
		}
	}

	// A place with a checklist of its own takes nothing over.
	filed(t, dir, older, "2026-09-28T12:00:00Z", Item{Text: "late", Status: Active})
	if again := Adopt(dir, mcp.Binding{Place: here}); again == nil || again.SessionID != newer {
		t.Errorf("a place with a checklist took another over: %+v", again)
	}
	if _, err := os.Stat(filepath.Join(dir, older+".json")); err != nil {
		t.Errorf("a file the place did not take over went: %v", err)
	}

	// A place with nothing filed under its conversations has no checklist.
	if none := Adopt(dir, mcp.Binding{Place: mcp.Place{ConfigDir: config, Dir: "/srv/proj/empty"}}); none != nil {
		t.Errorf("an empty place took over %+v", none)
	}
}

// A named session takes over only the checklist filed under its own
// conversation: the one filed under another conversation of the place is
// another session's as likely as its own.
func TestANamedSessionTakesOverOnlyWhatItsConversationFiled(t *testing.T) {
	dir, config := t.TempDir(), t.TempDir()
	here := mcp.Place{ConfigDir: config, Dir: "/srv/proj/lab"}
	const mine, theirs = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	transcriptOf(t, here, mine)
	transcriptOf(t, here, theirs)
	filed(t, dir, mine, "2026-09-28T09:00:00Z", Item{Text: "mine", Status: Active})
	filed(t, dir, theirs, "2026-09-28T10:00:00Z", Item{Text: "theirs", Status: Active})

	if got := Adopt(dir, mcp.Binding{Place: here, Name: "lab-review", SessionID: sid}); got != nil {
		t.Errorf("a session with nothing filed under its conversation took over %+v", got)
	}
	got := Adopt(dir, mcp.Binding{Place: here, Name: "lab", SessionID: mine})
	if got == nil || got.Items[0].Text != "mine" || got.Name != "lab" || Read(dir, here, "lab") == nil {
		t.Fatalf("the session took over %+v", got)
	}
	for id, left := range map[string]bool{mine: false, theirs: true} {
		if _, err := os.Stat(filepath.Join(dir, id+".json")); (err == nil) != left {
			t.Errorf("%s: left %v, meant %v", id, err == nil, left)
		}
	}
}

func TestSweepTakesOnlyWhatIsOld(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for name, age := range map[string]time.Duration{
		"old.json":       MaxAge + time.Hour,
		"fresh.json":     time.Hour,
		".checklist-123": MaxAge + time.Hour,
		"old-other.txt":  MaxAge + time.Hour,
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
		t.Errorf("%d files swept, meant the old checklist and the unfinished write", gone)
	}
	for name, left := range map[string]bool{"old.json": false, ".checklist-123": false, "fresh.json": true, "old-other.txt": true} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != left {
			t.Errorf("%s: left %v, meant %v", name, err == nil, left)
		}
	}
	if Sweep(filepath.Join(dir, "none"), now) != 0 {
		t.Error("a directory that is not there swept something")
	}
}
