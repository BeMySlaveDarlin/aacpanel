package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func pausedScene(t *testing.T, noEnd bool) (*reviveScene, store.SessionPause) {
	t.Helper()
	s := newReviveScene(t)
	row := s.row(1)
	s.snap.Sessions = []livingRow{{LiveSession: host.LiveSession{Name: row.Session, SessionID: row.Conversation, CWD: s.dir, Transport: "stream"}, Status: "idle", StatusAt: time.Now().Add(-time.Minute).UnixMilli()}}
	s.publish()
	s.remember([]store.LivingSession{row})
	p := action.Pause{Project: s.project.ID, Session: row.Session, NoEnd: noEnd}
	if !noEnd {
		p.Until = time.Now().Add(time.Hour).Format(time.RFC3339)
	}
	sendPause(t, s.srv, p, 1)
	s.drain()
	rows, err := s.srv.sessionPauses(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("saved pause: %+v %v", rows, err)
	}
	if strings.Contains(s.requests[0].Text, "alarm_set") {
		t.Fatal("Claude was asked to keep a process alarm")
	}
	return s, rows[0]
}

func savePause(t *testing.T, s *reviveScene, p store.SessionPause) {
	t.Helper()
	id, err := s.srv.db.HostID(t.Context(), s.srv.hostName)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.srv.db.PutSessionPause(t.Context(), id, p); err != nil {
		t.Fatal(err)
	}
}

func finishPausedTurn(t *testing.T, s *reviveScene, p store.SessionPause) store.SessionPause {
	t.Helper()
	p.CommandAt = time.Now().Add(-3 * time.Second)
	savePause(t, s, p)
	for i := range s.snap.Sessions {
		if s.snap.Sessions[i].SessionID == p.Conversation {
			s.snap.Sessions[i].Status = "idle"
			s.snap.Sessions[i].StatusAt = time.Now().Add(-time.Second).UnixMilli()
		}
	}
	s.snap.At = time.Now().Unix()
	s.publish()
	return p
}

func sleepTurn(t *testing.T, s *reviveScene, p store.SessionPause) store.SessionPause {
	t.Helper()
	finishPausedTurn(t, s, p)
	if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drain()
	s.publish()
	last := s.requests[len(s.requests)-1]
	if last.Kind != action.SessionClose || !last.Sleeping || last.Conversation != p.Conversation {
		t.Fatalf("sleep did not preserve the conversation's alarms: %+v", last)
	}
	rows, err := s.srv.sessionPauses(t.Context())
	if err != nil || len(rows) != 1 || !rows[0].Sleeping || len(s.snap.Sessions) != 0 {
		t.Fatalf("sleep: %+v live %+v %v", rows, s.snap.Sessions, err)
	}
	rows[0].CommandAt = time.Now().Add(-2 * time.Second)
	savePause(t, s, rows[0])
	return rows[0]
}

func TestPauseSleepsOnlyAfterTheTurnAndBackgroundEndPG(t *testing.T) {
	for _, kind := range []string{"old census", "turn", "agents", "tasks", "workflows", "observers", "wake only"} {
		t.Run(kind, func(t *testing.T) {
			s, p := pausedScene(t, false)
			if kind != "old census" {
				p = finishPausedTurn(t, s, p)
			}
			switch kind {
			case "turn":
				s.snap.Sessions[0].Status = "busy"
			case "agents", "tasks", "workflows", "observers":
				s.snap.Sessions[0].Work = json.RawMessage(fmt.Sprintf(`{"%s":1}`, kind))
			case "wake only":
				s.snap.Sessions[0].Work = json.RawMessage(`{"tasks":1,"wakes":1,"alarms":1}`)
			}
			s.publish()
			if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
				t.Fatal(err)
			}
			s.drain()
			rows, err := s.srv.sessionPauses(t.Context())
			if err != nil || len(rows) != 1 || rows[0].Sleeping != (kind == "wake only") {
				t.Fatalf("%s: %+v %v", kind, rows, err)
			}
			if kind != "wake only" {
				s.snap.Sessions[0].Work = nil
				sleepTurn(t, s, p)
			}
		})
	}
}

func TestSleepingCardsAndDeadlinesSurviveServiceReplacementPG(t *testing.T) {
	for _, noEnd := range []bool{false, true} {
		t.Run(fmt.Sprint(noEnd), func(t *testing.T) {
			s, p := pausedScene(t, noEnd)
			p = sleepTurn(t, s, p)
			if noEnd && p.Until != nil {
				t.Fatal("an indefinite pause acquired a deadline")
			}
			// The snapshot kept for the card was taken while the process lived and
			// carries its readings; asleep, the session uses neither memory nor CPU.
			var kept map[string]any
			if err := json.Unmarshal(p.Snapshot, &kept); err != nil {
				t.Fatal(err)
			}
			kept["res"] = map[string]any{"mem": 218 << 20, "cpu": 1.5}
			withRes, err := json.Marshal(kept)
			if err != nil {
				t.Fatal(err)
			}
			p.Snapshot = withRes
			savePause(t, s, p)
			replacement := &Server{db: s.srv.db, host: s.srv.host, hostName: s.srv.hostName, exec: s.srv.exec, home: s.srv.home, sleepConfig: s.r.config}
			s.srv = replacement
			replacement.sleeper = &reviver{srv: replacement, config: s.r.config, read: s.r.read, measure: s.r.measure, sleep: true}
			if err := replacement.pauseOnce(t.Context(), time.Now()); err != nil {
				t.Fatal(err)
			}
			s.drain()
			if len(s.started) != 0 {
				t.Fatal("an indefinite pause woke itself")
			}
			raw, err := replacement.hostSnapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var snap struct {
				Sessions []struct {
					Sleeping           bool
					Session, SessionID string
					Pause              *pauseAlarm     `json:"pauseAlarm"`
					Res                json.RawMessage `json:"res"`
				}
			}
			if err := json.Unmarshal(raw, &snap); err != nil || len(snap.Sessions) != 1 || !snap.Sessions[0].Sleeping || snap.Sessions[0].Res != nil || (noEnd && snap.Sessions[0].Pause.Name != action.PauseNoEndAlarm) || (!noEnd && snap.Sessions[0].Pause.Next != p.Until.Format(time.RFC3339)) {
				t.Fatalf("sleeping card: %s %v", raw, err)
			}
			sendPause(t, replacement, action.Pause{Project: s.project.ID, Session: p.Session, Resume: true}, 1)
			s.drain()
			rows, _ := replacement.sessionPauses(t.Context())
			if len(rows) != 0 || len(s.started) != 1 {
				t.Fatalf("Continue: pauses %+v started %+v", rows, s.started)
			}
			last := s.requests[len(s.requests)-1]
			if last.Kind != action.SessionSend || !strings.Contains(last.Text, `from="pause over"`) {
				t.Fatalf("Continue message: %+v", last)
			}
		})
	}
}

func TestSleepingDeadlineUsesTheTimerAfterServiceAndHostRestartPG(t *testing.T) {
	s, p := pausedScene(t, false)
	p = sleepTurn(t, s, p)
	expired := time.Now().Add(-time.Minute)
	p.Until = &expired
	savePause(t, s, p)
	candidates, err := s.r.candidates(t.Context(), []store.LivingSession{p.LivingSession})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("reboot recovery selected a sleeping conversation: %+v %v", candidates, err)
	}
	if _, err := s.srv.db.UpdateProject(t.Context(), s.project.ID, store.ProjectEdit{LaunchSet: map[string]any{"transport": "tmux"}}); err != nil {
		t.Fatal(err)
	}
	s.srv.sleeper = nil
	s.srv.sleepConfig = s.r.config
	s.srv.sleeper = &reviver{srv: s.srv, config: s.r.config, read: s.r.read, measure: s.r.measure, sleep: true}
	if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drain()
	rows, _ := s.srv.sessionPauses(t.Context())
	if len(rows) != 0 || len(s.started) != 1 {
		t.Fatalf("timer recovery: pauses %+v starts %+v", rows, s.started)
	}
	var launch map[string]any
	for _, req := range s.requests {
		if req.Kind == action.SessionResume {
			if err := json.Unmarshal(req.Project.Launch, &launch); err != nil {
				t.Fatal(err)
			}
			if launch["transport"] != "stream" || req.Target != p.Session || req.Resume != p.Conversation || req.Project.ConfigDir != p.ConfigDir {
				t.Fatalf("wake changed the saved conversation or transport: %+v %v", req, launch)
			}
		}
	}
	if req := s.requests[len(s.requests)-1]; req.Kind != action.SessionSend || !strings.Contains(req.Text, `from="pause over"`) {
		t.Fatalf("wake message: %+v", req)
	}
}

func TestSleepingHumanMessageBorrowsOneTurnAndLetterLiftsThePausePG(t *testing.T) {
	for _, kind := range []string{"session.send", "session.letter"} {
		t.Run(kind, func(t *testing.T) {
			s, p := pausedScene(t, false)
			p = sleepTurn(t, s, p)
			deadline := *p.Until
			w := post(t, s.srv, fmt.Sprintf(`{"kind":%q,"target":%q,"params":{"text":"Review this change","from":%q}}`, kind, p.Session, s.row(9).Conversation))
			if w.Code != http.StatusOK {
				t.Fatalf("message: %d %s", w.Code, w.Body.String())
			}
			s.drain()
			s.publish()
			last := s.requests[len(s.requests)-1]
			if string(last.Kind) != kind || last.Text != "Review this change" || len(s.started) != 1 {
				t.Fatalf("delivery after wake: %+v starts %+v", last, s.started)
			}
			rows, err := s.srv.sessionPauses(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if kind == "session.letter" {
				if len(rows) != 0 {
					t.Fatalf("a letter left the pause in place: %+v", rows)
				}
				return
			}
			if len(rows) != 1 || rows[0].Sleeping || !rows[0].Until.Equal(deadline) {
				t.Fatalf("human message lost its pause: %+v", rows)
			}
			if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
				t.Fatal(err)
			}
			s.drain()
			if len(s.snap.Sessions) != 1 {
				t.Fatal("the session was closed before answering the human")
			}
			completed := time.Now().Add(3 * time.Second)
			s.snap.Sessions[0].Status = "idle"
			s.snap.Sessions[0].StatusAt = completed.UnixMilli()
			s.snap.At = completed.Unix()
			s.publish()
			if err := s.srv.pauseOnce(t.Context(), completed); err != nil {
				t.Fatal(err)
			}
			s.drain()
			rows, err = s.srv.sessionPauses(t.Context())
			if err != nil || len(rows) != 1 || !rows[0].Sleeping || len(s.snap.Sessions) != 0 {
				t.Fatalf("the one answer did not return to sleep: %+v %v", rows, err)
			}
		})
	}
}

func TestCloseSleepingSessionRemovesItsTimerWithoutResumingPG(t *testing.T) {
	s, p := pausedScene(t, true)
	p = sleepTurn(t, s, p)
	before := len(s.requests)
	w := post(t, s.srv, fmt.Sprintf(`{"kind":"session.close","target":%q,"params":{}}`, p.Session))
	if w.Code != http.StatusOK {
		t.Fatalf("close: %d %s", w.Code, w.Body.String())
	}
	s.drain()
	rows, _ := s.srv.sessionPauses(t.Context())
	if len(rows) != 0 || len(s.requests) != before {
		t.Fatalf("close resumed or retained the sleep: %+v requests %+v", rows, s.requests[before:])
	}
}

func TestSleepWakeAdmitsBatchesAndWaitsForParentsPG(t *testing.T) {
	s := newReviveScene(t)
	pauses := []store.SessionPause{}
	rows := []store.LivingSession{}
	for i := 1; i <= 7; i++ {
		row := s.row(i)
		if i == 1 {
			row.Parent, row.ParentConversation = s.row(7).Session, s.row(7).Conversation
		}
		snapshot, _ := json.Marshal(host.LiveSession{Name: row.Session, SessionID: row.Conversation, CWD: s.dir, Transport: "stream"})
		p := store.SessionPause{LivingSession: row, Snapshot: snapshot, Sleeping: true, CommandAt: time.Now().Add(-time.Minute)}
		savePause(t, s, p)
		pauses = append(pauses, p)
		rows = append(rows, row)
	}
	s.remember(rows)
	probes := 0
	previousStarts := 0
	s.measure = func() *action.Headroom {
		probes++
		room := &action.Headroom{BootID: "new", High: 1000}
		room.Measure.Available = 1000
		if probes == 1 {
			room.Measure.MemoryFull = 20
		}
		if len(s.started)-previousStarts > 5 {
			t.Error("a batch exceeded its admission")
		}
		previousStarts = len(s.started)
		return room
	}
	if err := s.srv.wakePauses(context.Background(), pauses, nil); err != nil {
		t.Fatal(err)
	}
	s.drain()
	if len(s.started) != 7 || probes < 3 {
		t.Fatalf("batch admission: started %d probes %d", len(s.started), probes)
	}
	parent, child := -1, -1
	for i, req := range s.requests {
		if req.Kind != action.SessionResume {
			continue
		}
		if req.Resume == s.row(7).Conversation {
			parent = i
		}
		if req.Resume == s.row(1).Conversation {
			child = i
			if req.Project.Parent != s.row(7).Session {
				t.Fatalf("child resumed without its live parent: %+v", req.Project)
			}
		}
	}
	if parent < 0 || child <= parent {
		t.Fatalf("parent resumed at %d, child at %d", parent, child)
	}
}

func TestSleepingLetterWaitsForAConfirmedLiveConversationPG(t *testing.T) {
	s, p := pausedScene(t, true)
	p = sleepTurn(t, s, p)
	s.hold = true
	before := len(s.requests)
	w := post(t, s.srv, fmt.Sprintf(`{"kind":"session.letter","target":%q,"params":{"text":"A delayed letter","from":%q}}`, p.Session, s.row(9).Conversation))
	if w.Code == http.StatusOK {
		t.Fatalf("the letter was accepted without a live recipient: %s", w.Body.String())
	}
	s.drain()
	for _, req := range s.requests[before:] {
		if req.Kind == action.SessionLetter {
			t.Fatal("the letter was sent before wake-up was confirmed")
		}
	}
	rows, _ := s.srv.sessionPauses(t.Context())
	if len(rows) != 1 || !rows[0].Sleeping {
		t.Fatalf("a failed wake lost its pause: %+v", rows)
	}
}

func TestSleepingChildWakesAloneAndItsLetterWakesTheParentPG(t *testing.T) {
	for _, wake := range []string{"Continue", "message", "manual resume"} {
		t.Run(wake, func(t *testing.T) {
			s := newReviveScene(t)
			parent, child := s.row(2), s.row(1)
			child.Parent, child.ParentConversation = parent.Session, parent.Conversation
			s.remember([]store.LivingSession{parent, child})
			for _, row := range []store.LivingSession{parent, child} {
				card := host.LiveSession{Name: row.Session, SessionID: row.Conversation, CWD: s.dir, Transport: "stream"}
				if row.Parent != "" {
					card.Parent = &host.SessionRef{Session: row.Parent}
				}
				raw, err := json.Marshal(card)
				if err != nil {
					t.Fatal(err)
				}
				savePause(t, s, store.SessionPause{LivingSession: row, Snapshot: raw, Sleeping: true, CommandAt: time.Now().Add(-time.Minute)})
			}
			switch wake {
			case "Continue":
				sendPause(t, s.srv, action.Pause{Project: s.project.ID, Session: child.Session, Resume: true}, 1)
			case "message":
				w := post(t, s.srv, fmt.Sprintf(`{"kind":"session.send","target":%q,"params":{"text":"Answer this question"}}`, child.Session))
				if w.Code != http.StatusOK {
					t.Fatalf("child message: %d %s", w.Code, w.Body.String())
				}
			case "manual resume":
				w := post(t, s.srv, fmt.Sprintf(`{"kind":"session.resume","target":%q,"params":{"session":%q}}`, child.Session, child.Conversation))
				if w.Code != http.StatusOK {
					t.Fatalf("manual child resume: %d %s", w.Code, w.Body.String())
				}
			}
			s.drain()
			s.publish()
			if len(s.started) != 1 || s.started[0].Conversation != child.Conversation {
				t.Fatalf("waking a child also woke its parent: %+v", s.started)
			}
			for _, req := range s.requests {
				if req.Kind == action.SessionResume && (req.Project == nil || req.Project.Parent != parent.Session) {
					t.Fatalf("the sleeping parent's name was lost: %+v", req.Project)
				}
			}
			rows, err := s.srv.sessionPauses(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			kept, ok := pauseConversation(rows, parent.Conversation)
			if !ok || !kept.Sleeping || kept.Until != nil {
				t.Fatalf("waking the child changed the parent's pause: %+v", rows)
			}
			before := len(s.requests)
			w := post(t, s.srv, fmt.Sprintf(`{"kind":"session.letter","target":%q,"params":{"text":"The child finished its work","from":%q}}`, parent.Session, child.Conversation))
			if w.Code != http.StatusOK {
				t.Fatalf("child report: %d %s", w.Code, w.Body.String())
			}
			s.drain()
			rows, err = s.srv.sessionPauses(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := pauseConversation(rows, parent.Conversation); ok {
				t.Fatalf("the child's letter left its parent paused: %+v", rows)
			}
			if len(s.started) != 2 || s.started[1].Conversation != parent.Conversation {
				t.Fatalf("the child's letter did not wake its parent: %+v", s.started)
			}
			delivered := s.requests[before:]
			if len(delivered) != 2 || delivered[0].Kind != action.SessionResume || delivered[1].Kind != action.SessionLetter || delivered[1].Target != parent.Session || delivered[1].From != child.Conversation || delivered[1].Text != "The child finished its work" {
				t.Fatalf("child report delivery: %+v", delivered)
			}
		})
	}
}

func TestSleepingLetterFinishesAfterTheCallerStopsWaitingPG(t *testing.T) {
	s, p := pausedScene(t, true)
	p = sleepTurn(t, s, p)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	read := s.srv.sleeper.read
	s.srv.sleeper.read = func(now time.Time) (livingSnapshot, error) { cancel(); return read(now) }
	raw := fmt.Sprintf(`{"kind":"session.letter","target":%q,"params":{"text":"A report after admission","from":%q}}`, p.Session, s.row(9).Conversation)
	r := httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(raw)).WithContext(ctx)
	w := httptest.NewRecorder()
	s.srv.runAction(w, r, true)
	if w.Code != http.StatusOK {
		t.Fatalf("delivery after caller timeout: %d %s", w.Code, w.Body.String())
	}
	s.drain()
	last := s.requests[len(s.requests)-1]
	if last.Kind != action.SessionLetter || last.Text != "A report after admission" {
		t.Fatalf("the caller timeout abandoned delivery: %+v", last)
	}
	rows, err := s.srv.sessionPauses(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("the completed letter retained a pause: %+v %v", rows, err)
	}
}
