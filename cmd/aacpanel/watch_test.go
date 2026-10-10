package main

import (
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/notify"
)

func TestWatcherReadsWhatTheAgentWrote(t *testing.T) {
	path := snapshotWith(t, `{
		"at": 1788814000,
		"sessionsAt": 1788814000,
		"sessions": [
			{"session": "aacpanel", "sessionId": "4d89ed41", "cwd": "/srv/proj/aacpanel",
			 "profile": "personal", "status": "waiting", "statusUpdatedAt": 1788813894909,
			 "waitingFor": "input needed", "transport": "stream",
			 "ask": {"header": "Method", "text": "What do we bring the stack down with?", "count": 2, "at": "2026-09-08T03:00:00Z"}}
		],
		"limits": {"profile": "personal",
			"fiveHour": {"pct": 82, "resetsAt": 1788815400},
			"contours": [
				{"profile": "personal", "fiveHour": {"pct": 82, "resetsAt": 1788815400},
				 "sevenDay": {"pct": 48, "resetsAt": 1789059600}},
				{"profile": "work", "fiveHour": {"pct": 13, "resetsAt": 1788816000}}
			]}
	}`)

	w := newWatcher(&Server{host: host.NewReader(path)}, nil)
	var cur notify.World
	w.readSnapshot(t.Context(), &cur)

	if len(cur.Sessions) != 1 {
		t.Fatalf("the sessions were read wrong: %+v", cur.Sessions)
	}
	s := cur.Sessions[0]
	if s.ID != "4d89ed41" || s.Name != "aacpanel" || s.Profile != "personal" || s.CWD != "/srv/proj/aacpanel" {
		t.Errorf("the session was misread: %+v", s)
	}
	if s.Status != "waiting" || s.WaitingFor != "input needed" || s.StatusAt != 1788813894909 {
		t.Errorf("what the session waits for was lost: %+v", s)
	}
	if s.Transport != "stream" {
		t.Errorf("how the session is kept was lost — the push of its end would not say its holder died: %q", s.Transport)
	}
	if s.Ask == nil || s.Ask.Header != "Method" || s.Ask.Text != "What do we bring the stack down with?" ||
		s.Ask.Count != 2 || s.Ask.At != "2026-09-08T03:00:00Z" {
		t.Errorf("the question of the session was lost: %+v", s.Ask)
	}

	if !cur.LimitsSeen {
		t.Fatal("the limits block was not seen — the panel will decide the spend is unknown")
	}
	want := map[string]int{"personal:5h": 82, "personal:7d": 48, "work:5h": 13}
	got := map[string]int{}
	for _, l := range cur.Limits {
		got[l.Contour+":"+l.Window] = l.Pct
	}
	if len(got) != len(want) {
		t.Fatalf("%d windows were read, expected %d: %+v", len(got), len(want), got)
	}
	for key, pct := range want {
		if got[key] != pct {
			t.Errorf("window %s: %d%%, expected %d%%", key, got[key], pct)
		}
	}
}

// A codex thread is named by the tail of its id, and the watcher reads the
// name its card shows beside it, which titles its pushes: the name the panel
// gave the thread, else the session of the project it runs in; a thread no
// project holds and a claude session have none but their own.
func TestWatcherReadsTheNameACodexThreadIsShownBy(t *testing.T) {
	path := snapshotWith(t, `{
		"at": 1788814000,
		"sessionsAt": 1788814000,
		"sessions": [
			{"session": "codex-0000abcd", "sessionId": "019a1f00-0000-7000-8000-00000000abcd", "cwd": "/srv/proj/shop",
			 "agent": "codex", "title": "cart review", "status": "idle"},
			{"session": "codex-0000ef01", "sessionId": "019a1f00-0000-7000-8000-00000000ef01", "cwd": "/srv/elsewhere",
			 "agent": "codex", "status": "idle"},
			{"session": "lab", "sessionId": "4d89ed41", "cwd": "/srv/proj/lab", "title": "not a codex", "status": "idle"}
		]
	}`)
	w := newWatcher(&Server{host: host.NewReader(path)}, nil)
	var cur notify.World
	w.readSnapshot(t.Context(), &cur)
	got := map[string]string{}
	for _, s := range cur.Sessions {
		got[s.Name] = s.Shown
	}
	want := map[string]string{"codex-0000abcd": "cart review", "codex-0000ef01": "", "lab": ""}
	if len(got) != len(want) {
		t.Fatalf("the sessions read are %v", got)
	}
	for name, shown := range want {
		if got[name] != shown {
			t.Errorf("%s is shown as %q, expected %q", name, got[name], shown)
		}
	}

	places := &mapPlaces{list: tree()}
	if got := places.session("/home/u", "codex-0000abcd"); got != "home" {
		t.Errorf("a thread in the directory of a project reads as %q, expected its session", got)
	}
	if got := places.session("/srv/elsewhere", "codex-0000ef01"); got != "" {
		t.Errorf("a thread no project holds reads as %q", got)
	}
}

func TestWatcherTakesFractionalPercent(t *testing.T) {
	path := snapshotWith(t, `{
		"at": 1788862979,
		"sessionsAt": 1788862979,
		"limits": {"profile": "personal",
			"sevenDay": {"pct": 55.00000000000001, "resetsAt": 1789059600},
			"contours": [
				{"profile": "personal", "sevenDay": {"pct": 55.00000000000001, "resetsAt": 1789059600}},
				{"profile": "work", "fiveHour": {"pct": 28.499, "resetsAt": 1788865200}}
			]}
	}`)

	w := newWatcher(&Server{host: host.NewReader(path)}, nil)
	var cur notify.World
	w.readSnapshot(t.Context(), &cur)

	if cur.AgentErr != "" {
		t.Fatalf("a snapshot with a fractional percent was not read: %s", cur.AgentErr)
	}
	if !cur.LimitsSeen {
		t.Fatal("the limits block was not seen")
	}
	want := map[string]int{"personal:7d": 55, "work:5h": 28}
	got := map[string]int{}
	for _, l := range cur.Limits {
		got[l.Contour+":"+l.Window] = l.Pct
	}
	if len(got) != len(want) {
		t.Fatalf("%d windows were read, expected %d: %+v", len(got), len(want), got)
	}
	for key, pct := range want {
		if got[key] != pct {
			t.Errorf("window %s: %d%%, expected %d%%", key, got[key], pct)
		}
	}
}

func TestWatcherTellsBlindnessFromEmptiness(t *testing.T) {
	w := newWatcher(&Server{host: host.NewReader(snapshotWith(t, "") + ".missing")}, nil)
	var cur notify.World
	w.readSnapshot(t.Context(), &cur)

	if cur.AgentErr == "" {
		t.Fatal("a missing snapshot was read as an empty host")
	}
	if len(cur.Sessions) > 0 {
		t.Errorf("sessions arrived from a snapshot that does not exist: %+v", cur.Sessions)
	}
}

func TestPanelDidCoversTheStackContainers(t *testing.T) {
	w := newWatcher(&Server{}, nil)
	w.stackOf = map[string]string{
		"aacpanel": "aacpanel", "aacpanel-db": "aacpanel", "foreign": "other",
	}

	w.Expect(action.StackDown, "aacpanel")
	did := w.panelDid(time.Now())

	for _, key := range []string{"stack:aacpanel", "container:aacpanel", "container:aacpanel-db"} {
		if !did[key] {
			t.Errorf("our own action did not cover %s — a push will come about what the person did themselves", key)
		}
	}
	if did["container:foreign"] {
		t.Error("bringing one stack down silenced a container of another")
	}
}

func TestPanelDidForgetsOldDoings(t *testing.T) {
	w := newWatcher(&Server{}, nil)
	w.Expect(action.ContainerStop, "aacpanel-db")

	if did := w.panelDid(time.Now().Add(panelDidFor + time.Minute)); did["container:aacpanel-db"] {
		t.Fatal("the panel remembers its own action past the deadline — a fall of the same container will stay silent")
	}
	if len(w.did) != 0 {
		t.Errorf("a stale mark stayed in memory: %v", w.did)
	}
}
