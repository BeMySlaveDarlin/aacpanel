package main

import (
	"fmt"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func TestSleepWithoutPausesOrAlarmsSkipsTheCensusPG(t *testing.T) {
	s := newReviveScene(t)
	s.srv.host = host.NewReader(snapshotWith(t, `{}`))
	if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
		t.Fatalf("an empty sleep tick read an unnecessary census: %v", err)
	}
	if len(s.exec.got) != 1 {
		t.Fatalf("an empty sleep tick made %d executor requests", len(s.exec.got))
	}
	if req := <-s.exec.got; req.Ask != action.AskSleepingAlarms {
		t.Fatalf("an empty sleep tick asked for %q", req.Ask)
	}
}

func TestSleepingAlarmWakesBeforeTheDeadlineAndLiftsThePausePG(t *testing.T) {
	for _, noEnd := range []bool{false, true} {
		t.Run(fmt.Sprint(noEnd), func(t *testing.T) {
			s, p := pausedScene(t, noEnd)
			p = sleepTurn(t, s, p)
			due := time.Now().Add(-time.Second)
			s.exec.resp.SleepingAlarms = &action.SleepingAlarms{Alarms: []action.SleepingAlarm{
				{ID: "0123456789abcdef", Session: p.Session, Conversation: p.Conversation, Next: due},
				{ID: "fedcba9876543210", Session: p.Session, Conversation: p.Conversation, Next: due.Add(time.Hour)},
			}}
			before := len(s.requests)
			if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
				t.Fatal(err)
			}
			s.drain()
			pauses, err := s.srv.sessionPauses(t.Context())
			if err != nil || len(pauses) != 0 || len(s.started) != 1 {
				t.Fatalf("alarm wake: %d pauses, %d starts, error %v", len(pauses), len(s.started), err)
			}
			requests := s.requests[before:]
			if len(requests) != 1 || requests[0].Kind != action.SessionResume || !requests[0].Silent {
				t.Fatalf("an alarm wake sent something besides the silent resume: %+v", requests)
			}
		})
	}
}

func TestSleepReconcilesOnlyAbsentOrphansWithOneAlarmReadPG(t *testing.T) {
	for _, sleeping := range []bool{false, true} {
		t.Run(fmt.Sprint(sleeping), func(t *testing.T) {
			s := newReviveScene(t)
			live, orphan, paused := s.row(1), s.row(2), s.row(3)
			s.snap.Sessions = []livingRow{{LiveSession: host.LiveSession{Name: live.Session, SessionID: live.Conversation, CWD: s.dir}}}
			if sleeping {
				snapshot := []byte(`{"session":"lab-3"}`)
				savePause(t, s, store.SessionPause{LivingSession: paused, Snapshot: snapshot, Sleeping: true, CommandAt: time.Now().Add(-time.Minute)})
			}
			s.publish()
			s.exec.resp.SleepingAlarms = &action.SleepingAlarms{Alarms: []action.SleepingAlarm{
				{ID: "1111111111111111", Session: live.Session, Conversation: live.Conversation, Next: time.Now().Add(time.Hour)},
				{ID: "2222222222222222", Session: orphan.Session, Conversation: orphan.Conversation, Next: time.Now().Add(time.Hour)},
			}}
			if sleeping {
				s.exec.resp.SleepingAlarms.Alarms = append(s.exec.resp.SleepingAlarms.Alarms, action.SleepingAlarm{
					ID: "3333333333333333", Session: paused.Session, Conversation: paused.Conversation, Next: time.Now().Add(time.Hour)})
			}
			if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
				t.Fatal(err)
			}
			reads, cancellations := 0, 0
			for len(s.exec.got) > 0 {
				req := <-s.exec.got
				if req.Ask == action.AskSleepingAlarms {
					reads++
				}
				if req.Kind == action.AlarmCancel {
					cancellations++
					if !req.Sleeping || req.Conversation != orphan.Conversation || req.Work.ID != "2222222222222222" {
						t.Fatalf("the reconciliation cancelled a protected alarm: %+v", req)
					}
				}
			}
			if reads != 1 || cancellations != 1 {
				t.Fatalf("reconciliation made %d reads and %d cancellations", reads, cancellations)
			}
		})
	}
}
