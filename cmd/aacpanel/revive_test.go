package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/heavy"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

type reviveScene struct {
	t                 *testing.T
	srv               *Server
	exec              *fakeExec
	r                 *reviver
	project           store.ProfileProject
	dir               string
	snap              livingSnapshot
	requests          []action.Request
	started           []store.LivingSession
	hold, refusePause bool
	reads             int
	measurements      int
	measure           func() *action.Headroom
	journalFrom       int64
}

func newReviveScene(t *testing.T) *reviveScene {
	t.Helper()
	srv, fake, dir := switchServer(t, "stream", "stream")
	srv.hostName = t.Name()
	profiles, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s := &reviveScene{t: t, srv: srv, exec: fake, dir: dir, project: profiles[0].Groups[0].Projects[0], snap: livingSnapshot{BootID: "new", At: time.Now().Unix(), Sessions: []livingRow{}}}
	pool, _ := srv.db.Pool()
	if err := pool.QueryRow(t.Context(), `SELECT coalesce(max(id),0) FROM actions`).Scan(&s.journalFrom); err != nil {
		t.Fatal(err)
	}
	s.r = &reviver{srv: srv, boot: "new", config: reviveConfig{Reserve: 1, Session: 1, SlicePercent: 90, PSI: 5, Wait: 35 * time.Millisecond, Alive: 35 * time.Millisecond, Poll: time.Millisecond}}
	s.r.read = func(time.Time) (livingSnapshot, error) { s.drain(); s.reads++; s.publish(); return s.snap, nil }
	s.r.measure = func(context.Context) (*action.Headroom, error) {
		s.drain()
		s.measurements++
		if s.measure != nil {
			return s.measure(), nil
		}
		return &action.Headroom{BootID: "new", High: 1000, Measure: heavy.Measure{Available: 1000}}, nil
	}
	s.srv.sleeper = &reviver{srv: srv, config: s.r.config, read: s.r.read, measure: s.r.measure, sleep: true}
	s.publish()
	return s
}

func (s *reviveScene) row(n int) store.LivingSession {
	return store.LivingSession{Conversation: fmt.Sprintf("%08d-0000-4000-8000-%012d", n, n), Session: fmt.Sprintf("lab-%d", n), CWD: s.dir, ProjectID: s.project.ID, Agent: "claude", Transport: "stream"}
}

func (s *reviveScene) publish() {
	s.t.Helper()
	raw, err := json.Marshal(s.snap)
	if err != nil {
		s.t.Fatal(err)
	}
	s.srv.host = host.NewReader(snapshotWith(s.t, string(raw)))
}

func (s *reviveScene) drain() {
	s.t.Helper()
	for {
		select {
		case req := <-s.exec.got:
			if req.Ask != "" {
				continue
			}
			s.requests = append(s.requests, req)
			if req.Kind == action.SessionResume {
				if (req.Device == "reboot recovery" || req.Device == "session wake") && (!req.Silent || req.Text != "") {
					s.t.Errorf("recovery sent an opening message: %+v", req)
				}
				row := store.LivingSession{Conversation: req.Resume, Session: req.Target, CWD: s.dir}
				s.started = append(s.started, row)
				if !s.hold {
					s.snap.Sessions = append(s.snap.Sessions, livingRow{LiveSession: host.LiveSession{Name: req.Target, SessionID: req.Resume, CWD: s.dir}})
				}
			}
			if req.Kind == action.SessionClose {
				live := s.snap.Sessions[:0]
				for _, row := range s.snap.Sessions {
					if row.SessionID != req.Conversation {
						live = append(live, row)
					}
				}
				s.snap.Sessions = live
			}
			if req.Kind == action.SessionSend && !s.refusePause {
				for i := range s.snap.Sessions {
					if s.snap.Sessions[i].Name != req.Target {
						continue
					}
					switch {
					case strings.Contains(req.Text, `from="pause over"`):
						s.snap.Sessions[i].Pause = nil
					case strings.Contains(req.Text, `from="pause with no end"`):
						s.snap.Sessions[i].Pause = &pauseAlarm{ID: "0123456789abcdef", Name: action.PauseNoEndAlarm, Next: time.Now().Add(time.Hour).Format(time.RFC3339)}
					case strings.Contains(req.Text, `from="pause"`):
						_, tail, _ := strings.Cut(req.Text, `until="`)
						until, _, _ := strings.Cut(tail, `"`)
						s.snap.Sessions[i].Pause = &pauseAlarm{ID: "0123456789abcdef", Name: action.PauseAlarm, Next: until}
					}
				}
			}
		default:
			return
		}
	}
}

func (s *reviveScene) remember(rows []store.LivingSession) {
	s.t.Helper()
	hostID, err := s.srv.db.HostID(s.t.Context(), s.srv.hostName)
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.srv.db.KeepLiving(s.t.Context(), hostID, "old", time.Now().Add(-time.Hour), rows); err != nil {
		s.t.Fatal(err)
	}
}

func TestRecoveryFiltersTheLastBootCensusPG(t *testing.T) {
	s := newReviveScene(t)
	root, child, outside, alive, thread := s.row(1), s.row(2), s.row(3), s.row(4), s.row(5)
	child.Parent = "lead"
	outside.Outside = true
	thread.Agent = "codex"
	s.snap.Sessions = append(s.snap.Sessions, livingRow{LiveSession: host.LiveSession{Name: "renamed", SessionID: alive.Conversation, CWD: s.dir}})
	s.publish()
	rows := []store.LivingSession{root, child, outside, alive, thread}
	s.remember(rows)
	pool, _ := s.srv.db.Pool()
	// The service persists exactly the rows the host supplied, including pauses.
	if err := s.srv.keepLiving(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int
	hostID, _ := s.srv.db.HostID(t.Context(), s.srv.hostName)
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM session_living WHERE boot_id='new' AND host_id=$1`, hostID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("fresh census: %d %v", count, err)
	}
	total, raised, failed := s.r.recover(t.Context(), rows)
	if total != 2 || raised != 1 || len(failed) != 1 || !strings.Contains(failed[0], "codex") {
		t.Fatalf("filtered recovery: %d %d %v", total, raised, failed)
	}
	profiles, _ := s.srv.db.Profiles(t.Context())
	if _, err := s.srv.db.UpdateProfile(t.Context(), profiles[0].ID, store.ProfileEdit{LaunchSet: map[string]any{"reviveAfterReboot": false}}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.r.candidates(t.Context(), rows); err != nil || len(got) != 0 {
		t.Fatalf("disabled contour: %+v %v", got, err)
	}
	unmapped := s.row(10)
	unmapped.ProjectID = 0
	unmapped.ConfigDir = profiles[0].ConfigDir
	if got, err := s.r.candidates(t.Context(), []store.LivingSession{unmapped}); err != nil || len(got) != 0 {
		t.Fatalf("disabled contour without a mapped project: %+v %v", got, err)
	}
	if _, err := s.srv.db.UpdateProject(t.Context(), s.project.ID, store.ProjectEdit{LaunchSet: map[string]any{"reviveAfterReboot": true}}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.r.candidates(t.Context(), []store.LivingSession{thread}); err != nil || len(got) != 1 {
		t.Fatalf("project override: %+v %v", got, err)
	}
}

func TestLivingCensusKeepsParentsAndPausesAndRejectsPartialReadsPG(t *testing.T) {
	s := newReviveScene(t)
	root, kid := s.row(1), s.row(2)
	s.snap.Sessions = []livingRow{
		{LiveSession: host.LiveSession{Name: root.Session, SessionID: root.Conversation, CWD: s.dir, Transport: "stream"}, Pause: &pauseAlarm{ID: "0123456789abcdef", Name: action.PauseNoEndAlarm, Next: time.Now().Add(time.Hour).Format(time.RFC3339)}},
		{LiveSession: host.LiveSession{Name: kid.Session, SessionID: kid.Conversation, CWD: s.dir, Parent: &host.SessionRef{Session: root.Session}}},
	}
	s.publish()
	if err := s.srv.keepLiving(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	hostID, err := s.srv.db.HostID(t.Context(), s.srv.hostName)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.srv.db.LivingConversation(t.Context(), hostID, root.Conversation)
	if err != nil || kept == nil || !strings.Contains(string(kept.Pause), "pause-no-end") || kept.ProjectID != s.project.ID || kept.Transport != "stream" {
		t.Fatalf("the saved pause or place: %+v %v", kept, err)
	}
	child, err := s.srv.db.LivingConversation(t.Context(), hostID, kid.Conversation)
	if err != nil || child == nil || child.ParentConversation != root.Conversation {
		t.Fatalf("the saved parent: %+v %v", child, err)
	}
	good := s.snap
	for _, change := range []func(*livingSnapshot){
		func(s *livingSnapshot) { s.At = 0 }, func(s *livingSnapshot) { s.At -= 60 }, func(s *livingSnapshot) { s.BootID = "" },
		func(s *livingSnapshot) { s.Notes = []string{"the collector failed"} }, func(s *livingSnapshot) { s.Sessions = nil },
	} {
		s.snap = good
		change(&s.snap)
		s.publish()
		if err := s.srv.keepLiving(t.Context(), time.Now()); err == nil {
			t.Fatalf("an incomplete census replaced the live list: %+v", s.snap)
		}
	}
	s.snap = good
	s.snap.At++
	s.snap.Sessions = s.snap.Sessions[:1]
	s.snap.Sessions[0].Pause = nil
	s.snap.Sessions[0].Name = "renamed"
	s.publish()
	if err := s.srv.keepLiving(t.Context(), time.Now().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	kept, err = s.srv.db.LivingConversation(t.Context(), hostID, root.Conversation)
	if err != nil || kept == nil || kept.Session != "renamed" || len(kept.Pause) != 0 {
		t.Fatalf("the updated census: %+v %v", kept, err)
	}
}

func TestRecoveryDoesNotConfuseAReusedSessionNamePG(t *testing.T) {
	s := newReviveScene(t)
	row := s.row(1)
	s.remember([]store.LivingSession{row})
	s.snap.Sessions = append(s.snap.Sessions, livingRow{LiveSession: host.LiveSession{Name: row.Session, SessionID: s.row(9).Conversation, CWD: s.dir}})
	s.publish()
	total, raised, failed := s.r.recover(t.Context(), []store.LivingSession{row})
	if total != 1 || raised != 1 || len(failed) != 0 || len(s.started) != 1 || s.started[0].Conversation != row.Conversation {
		t.Fatalf("a reused name hid its previous conversation: %d %d %v", total, raised, failed)
	}
}

func TestRecoveryWaitsForTheBatchAndHeadroomPG(t *testing.T) {
	s := newReviveScene(t)
	s.hold = true
	rows := []store.LivingSession{}
	for i := 1; i <= 7; i++ {
		rows = append(rows, s.row(i))
	}
	s.remember(rows)
	probes := 0
	s.measure = func() *action.Headroom {
		room := &action.Headroom{BootID: "new", High: 1000, Measure: heavy.Measure{Available: 1000}}
		if len(s.started) == 0 && probes == 0 {
			probes++
			room.Measure.MemoryFull = 20
			return room
		}
		if len(s.started) == 5 {
			if len(s.snap.Sessions) != 5 {
				t.Error("another batch was admitted before the previous one became live")
			}
			if probes == 1 {
				probes++
				room.Measure.Slice = 900
				return room
			}
		}
		return room
	}
	baseRead := s.r.read
	s.r.read = func(now time.Time) (livingSnapshot, error) {
		snap, err := baseRead(now)
		if len(s.started) == 5 && len(s.snap.Sessions) == 0 && s.reads >= 9 {
			for _, row := range s.started {
				s.snap.Sessions = append(s.snap.Sessions, livingRow{LiveSession: host.LiveSession{Name: row.Session, SessionID: row.Conversation, CWD: s.dir}})
			}
			s.hold = false
			s.publish()
			snap = s.snap
		}
		return snap, err
	}
	total, raised, failed := s.r.recover(t.Context(), rows)
	if total != 7 || raised != 7 || len(failed) != 0 || len(s.started) != 7 || probes != 2 {
		t.Fatalf("batches: total %d raised %d started %d pressure probes %d failed %v", total, raised, len(s.started), probes, failed)
	}
	var journaled int
	pool, _ := s.srv.db.Pool()
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM actions WHERE kind='session.resume' AND device_name='reboot recovery' AND result='ok' AND id>$1`, s.journalFrom).Scan(&journaled); err != nil || journaled != 7 {
		t.Fatalf("recovery journal: %d %v", journaled, err)
	}
}

func TestRecoveryStopsWhenABatchStaysDeadPG(t *testing.T) {
	s := newReviveScene(t)
	s.hold = true
	rows := []store.LivingSession{}
	for i := 1; i <= 6; i++ {
		rows = append(rows, s.row(i))
	}
	s.remember(rows)
	total, raised, failed := s.r.recover(t.Context(), rows)
	if total != 6 || raised != 0 || len(failed) != 6 || len(s.started) != 5 {
		t.Fatalf("dead batch: %d %d %v, starts %d", total, raised, failed, len(s.started))
	}
}

func TestRecoveryStopsWhenHostPressureDoesNotFallPG(t *testing.T) {
	for _, pressure := range []string{"available", "slice", "psi"} {
		t.Run(pressure, func(t *testing.T) {
			s := newReviveScene(t)
			s.measure = func() *action.Headroom {
				room := &action.Headroom{BootID: "new", High: 1000, Measure: heavy.Measure{Available: 1000}}
				switch pressure {
				case "available":
					room.Measure.Available = 0
				case "slice":
					room.Measure.Slice = 900
				case "psi":
					room.Measure.MemoryFull = 5
				}
				return room
			}
			_, raised, failed := s.r.recover(t.Context(), []store.LivingSession{s.row(1)})
			if raised != 0 || len(failed) != 1 || len(s.started) != 0 || !strings.Contains(failed[0], "headroom did not recover") {
				t.Fatalf("host pressure: %d %v, starts %d", raised, failed, len(s.started))
			}
		})
	}
}

func TestRecoveryRestoresEveryKindOfPausePG(t *testing.T) {
	for _, kind := range []string{"future", "expired", "no end", "no process alarm"} {
		t.Run(kind, func(t *testing.T) {
			s := newReviveScene(t)
			row := s.row(1)
			alarm := pauseAlarm{ID: "old-alarm", Name: action.PauseAlarm, Next: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
			switch kind {
			case "expired":
				alarm.Next = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			case "no end":
				alarm.Name = action.PauseNoEndAlarm
			case "no process alarm":
				s.refusePause = true
			}
			row.Pause, _ = json.Marshal(alarm)

			s.remember([]store.LivingSession{row})
			total, raised, failed := s.r.recover(t.Context(), []store.LivingSession{row})
			s.drain()
			if total != 1 || raised != 1 || len(failed) != 0 {
				t.Fatalf("pause recovery: %d %d %v", total, raised, failed)
			}
			if len(s.requests) != 2 || s.requests[0].Kind != action.SessionResume || s.requests[1].Kind != action.SessionSend {
				t.Fatalf("pause actions: %+v", s.requests)
			}
			text := s.requests[1].Text
			switch kind {
			case "future":
				if !strings.Contains(text, `until="`+alarm.Next+`"`) {
					t.Fatal(text)
				}
			case "expired":
				if !strings.Contains(text, `from="pause over"`) || strings.Contains(text, "alarm_set") {
					t.Fatalf("expired pause did not Continue: %s", text)
				}
			case "no end":
				if !strings.Contains(text, `from="pause with no end"`) {
					t.Fatal(text)
				}
			}
			pool, _ := s.srv.db.Pool()
			var resume bool
			if err := pool.QueryRow(t.Context(), `SELECT (params->>'resume')::boolean FROM actions WHERE kind='project.pause' ORDER BY id DESC LIMIT 1`).Scan(&resume); err != nil || resume != (kind == "expired") {
				t.Fatalf("pause journal: resume %v %v", resume, err)
			}
		})
	}
}

func TestArchivedChildResumesUnderItsLiveParentPG(t *testing.T) {
	for _, state := range []string{"live", "dead", "name reused"} {
		t.Run(state, func(t *testing.T) {
			s := newReviveScene(t)
			row := s.row(1)
			row.Parent = "lead"
			row.ParentConversation = s.row(2).Conversation
			s.remember([]store.LivingSession{row})
			if state != "dead" {
				id := row.ParentConversation
				if state == "name reused" {
					id = s.row(3).Conversation
				}
				s.snap.Sessions = append(s.snap.Sessions, livingRow{LiveSession: host.LiveSession{Name: "lead", SessionID: id, CWD: s.dir}})
			}
			s.publish()
			w := post(t, s.srv, fmt.Sprintf(`{"kind":"session.resume","target":"lab-1","params":{"session":%q}}`, row.Conversation))
			if w.Code != http.StatusOK {
				t.Fatalf("resume: %d %s", w.Code, w.Body.String())
			}
			req := <-s.exec.got
			parent := ""
			if state == "live" {
				parent = "lead"
			}
			if req.Project == nil || req.Project.Parent != parent {
				t.Fatalf("resumed child: %+v", req.Project)
			}
			if state != "live" && !strings.Contains(w.Body.String(), "resumed without a parent") {
				t.Fatalf("the missing parent was not reported: %s", w.Body.String())
			}
		})
	}
}

func TestArchivedCodexChildResumesUnderItsLiveParentPG(t *testing.T) {
	s := newReviveScene(t)
	row := s.row(1)
	row.Agent = "codex"
	row.Parent = "lead"
	row.ParentConversation = s.row(2).Conversation
	s.remember([]store.LivingSession{row})
	s.snap.Sessions = append(s.snap.Sessions, livingRow{LiveSession: host.LiveSession{Name: "lead", SessionID: row.ParentConversation, CWD: s.dir}})
	s.publish()
	w := post(t, s.srv, fmt.Sprintf(`{"kind":"session.resume","target":%q,"params":{"session":%q,"agent":"codex","contour":"work"}}`, row.Session, row.Conversation))
	if w.Code != http.StatusOK {
		t.Fatalf("codex resume: %d %s", w.Code, w.Body.String())
	}
	if req := <-s.exec.got; req.Parent != "lead" {
		t.Fatalf("codex resume lost its parent: %+v", req)
	}
}
