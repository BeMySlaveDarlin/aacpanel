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
	"aacpanel/internal/store"
)

// A reboot loses the process of a frozen conversation. Its pause stays in
// the database, so its next wake must use resume rather than thaw.
func loseSleepingProcess(t *testing.T, s *reviveScene, conversation string) {
	t.Helper()
	live := s.snap.Sessions[:0]
	for _, row := range s.snap.Sessions {
		if row.SessionID != conversation {
			live = append(live, row)
		}
	}
	s.snap.Sessions = live
	delete(s.frozen, conversation)
	s.publish()
}

func TestSleepingStreamStaysFrozenAcrossTicksAndWakesByTimerPG(t *testing.T) {
	s, p := pausedScene(t, false)
	p = sleepTurn(t, s, p)
	before := len(s.requests)
	for range 2 {
		if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
			t.Fatal(err)
		}
		s.drain()
	}
	if len(s.requests) != before || !s.frozen[p.Conversation] {
		t.Fatalf("a sleeping tick acted on the frozen session: %+v", s.requests[before:])
	}
	expired := time.Now().Add(-time.Second)
	p.Until = &expired
	savePause(t, s, p)
	if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drain()
	sent := s.requests[before:]
	if len(sent) != 2 || sent[0].Kind != action.SessionThaw || sent[0].Conversation != p.Conversation || sent[1].Kind != action.SessionSend || s.frozen[p.Conversation] || len(s.started) != 0 {
		t.Fatalf("the timer did not thaw before continuing: %+v", sent)
	}
	rows, err := s.srv.sessionPauses(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("the timer retained its pause: %+v %v", rows, err)
	}
}

func TestTmuxPauseSleepsByClosingAndResumesOnWakePG(t *testing.T) {
	s, p := pausedScene(t, true)
	s.snap.Sessions[0].Transport = "tmux"
	finishPausedTurn(t, s, p)
	if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drain()
	last := s.requests[len(s.requests)-1]
	if last.Kind != action.SessionClose || !last.Sleeping || len(s.snap.Sessions) != 0 {
		t.Fatalf("tmux sleep froze its shared unit: %+v", last)
	}
	s.publish()
	rows, _ := s.srv.sessionPauses(t.Context())
	p = rows[0]
	p.CommandAt = time.Now().Add(-2 * time.Second)
	savePause(t, s, p)
	before := len(s.requests)
	if err := s.srv.wakePaused(t.Context(), p.Conversation); err != nil {
		t.Fatal(err)
	}
	s.drain()
	if len(s.requests) != before+1 || s.requests[before].Kind != action.SessionResume {
		t.Fatalf("tmux wake did not resume: %+v", s.requests[before:])
	}
}

func TestSleepingCloseWithoutAProcessDropsThePausePG(t *testing.T) {
	s, p := pausedScene(t, true)
	p = sleepTurn(t, s, p)
	loseSleepingProcess(t, s, p.Conversation)
	before := len(s.requests)
	w := post(t, s.srv, fmt.Sprintf(`{"kind":"session.close","target":%q,"params":{}}`, p.Session))
	s.drain()
	pauses, err := s.srv.sessionPauses(t.Context())
	if w.Code != http.StatusOK || err != nil || len(pauses) != 0 || len(s.requests) != before {
		t.Fatalf("an absent sleeper was resumed or kept: %d %s %+v %v", w.Code, w.Body.String(), pauses, err)
	}
}

func TestFreezeAndThawAreOnlyTheSleepersActionsPG(t *testing.T) {
	s := newReviveScene(t)
	for _, kind := range []action.Kind{action.SessionFreeze, action.SessionThaw} {
		w := post(t, s.srv, fmt.Sprintf(`{"kind":%q,"target":"lab","params":{"conversation":%q}}`, kind, s.row(1).Conversation))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s was exposed to a button: %d %s", kind, w.Code, w.Body.String())
		}
	}
	s.drain()
	if len(s.requests) != 0 {
		t.Fatalf("a public sleep action reached the executor: %+v", s.requests)
	}
}

func TestFailedFreezeOrThawKeepsThePausePG(t *testing.T) {
	for _, sleeping := range []bool{false, true} {
		t.Run(fmt.Sprint(sleeping), func(t *testing.T) {
			s, p := pausedScene(t, true)
			if sleeping {
				p = sleepTurn(t, s, p)
			} else {
				p = finishPausedTurn(t, s, p)
			}
			kind := action.SessionFreeze
			if sleeping {
				kind = action.SessionThaw
			}
			s.exec.failures = map[action.Kind]string{kind: "unit refused"}
			before := len(s.requests)
			var err error
			if sleeping {
				err = s.srv.wakePaused(t.Context(), p.Conversation)
			} else {
				err = s.srv.pauseOnce(t.Context(), time.Now())
			}
			s.drain()
			if len(s.requests) != before+1 || s.requests[before].Kind != kind {
				t.Fatalf("the refusal did not reach %s: %+v", kind, s.requests[before:])
			}
			rows, readErr := s.srv.sessionPauses(t.Context())
			if err == nil || readErr != nil || len(rows) != 1 || rows[0].Sleeping != p.Sleeping || !rows[0].CommandAt.Equal(p.CommandAt.Truncate(time.Microsecond)) {
				t.Fatalf("a failed unit action lost the pause: %+v action %v read %v", rows, err, readErr)
			}
		})
	}
}

func TestWakeThawsAConversationThatAppearsDuringAdmissionPG(t *testing.T) {
	s, p := pausedScene(t, true)
	p = sleepTurn(t, s, p)
	read := s.srv.sleeper.read
	first := true
	s.srv.sleeper.read = func(now time.Time) (livingSnapshot, error) {
		snap, err := read(now)
		if first {
			first = false
			snap.Sessions = nil
		}
		return snap, err
	}
	before := len(s.requests)
	if err := s.srv.wakePaused(t.Context(), p.Conversation); err != nil {
		t.Fatal(err)
	}
	s.drain()
	if len(s.requests) != before+1 || s.requests[before].Kind != action.SessionThaw || s.frozen[p.Conversation] || len(s.started) != 0 {
		t.Fatalf("the admission census mistook a frozen process for awake: %+v", s.requests[before:])
	}
}

func TestSleepingCardMasksLiveAndSavedActivityPG(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			s, p := pausedScene(t, true)
			p = sleepTurn(t, s, p)
			card := fmt.Sprintf(`{"session":%q,"sessionId":%q,"status":"busy","ask":{},"permission":{},"waitingFor":"person","work":{"tasks":1},"nextAlarm":"later","res":{"mem":123}}`, p.Session, p.Conversation)
			p.Snapshot = []byte(card)
			savePause(t, s, p)
			rows := "[]"
			if live {
				rows = "[" + card + "]"
			}
			raw, err := s.srv.sessionsPaused(t.Context(), []byte(`{"sessions":`+rows+`}`))
			var out struct {
				Sessions []map[string]any `json:"sessions"`
			}
			if err != nil || json.Unmarshal(raw, &out) != nil || len(out.Sessions) != 1 {
				t.Fatalf("the sleeping card was lost or duplicated: %s %v", raw, err)
			}
			row := out.Sessions[0]
			if row["status"] != "idle" || row["sleeping"] != true || row["pauseAlarm"] == nil {
				t.Fatalf("the sleeping card reports activity: %s", raw)
			}
			for _, key := range []string{"ask", "permission", "waitingFor", "work", "nextAlarm", "res"} {
				if _, present := row[key]; present {
					t.Errorf("the sleeping card kept %s: %s", key, raw)
				}
			}
		})
	}
}

func timedOutFreeze(t *testing.T) (*reviveScene, store.SessionPause) {
	t.Helper()
	s, p := pausedScene(t, false)
	p = finishPausedTurn(t, s, p)
	s.exec.silent = action.SessionFreeze
	s.srv.exec = action.NewClient(s.exec.socket, 100*time.Millisecond)
	if err := s.srv.pauseOnce(t.Context(), time.Now()); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("the missing freeze reply did not time out: %v", err)
	}
	s.drain()
	rows, err := s.srv.sessionPauses(t.Context())
	if err != nil || len(rows) != 1 || rows[0].Sleeping || !s.frozen[p.Conversation] || s.requests[len(s.requests)-1].Kind != action.SessionFreeze {
		t.Fatalf("the timed-out freeze did not leave a frozen process with an awake pause: %+v frozen %v error %v", rows, s.frozen, err)
	}
	return s, rows[0]
}

func TestTimedOutFreezeThawsBeforeEveryWakePG(t *testing.T) {
	for _, wake := range []string{"timer", "Continue", "alarm", "message", "letter"} {
		t.Run(wake, func(t *testing.T) {
			s, p := timedOutFreeze(t)
			before := len(s.requests)
			kind := action.SessionSend
			switch wake {
			case "timer":
				expired := time.Now().Add(-time.Second)
				p.Until = &expired
				savePause(t, s, p)
				if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
					t.Fatal(err)
				}
			case "Continue":
				sendPause(t, s.srv, action.Pause{Project: s.project.ID, Session: p.Session, Resume: true}, 1)
			case "alarm":
				kind = ""
				s.exec.resp.SleepingAlarms = &action.SleepingAlarms{Alarms: []action.SleepingAlarm{{ID: "0123456789abcdef", Session: p.Session, Conversation: p.Conversation, Next: time.Now().Add(-time.Second)}}}
				if err := s.srv.pauseOnce(t.Context(), time.Now()); err != nil {
					t.Fatal(err)
				}
			case "message", "letter":
				if wake == "letter" {
					kind = action.SessionLetter
				}
				w := post(t, s.srv, fmt.Sprintf(`{"kind":%q,"target":%q,"params":{"text":"Continue this work","from":%q}}`, kind, p.Session, s.row(9).Conversation))
				if w.Code != http.StatusOK {
					t.Fatalf("wake after a timed-out freeze: %d %s", w.Code, w.Body.String())
				}
			}
			s.drain()
			sent := s.requests[before:]
			want := 2
			if kind == "" {
				want = 1
			}
			if len(sent) != want || sent[0].Kind != action.SessionThaw || sent[0].Conversation != p.Conversation || sent[0].Target != p.Session || s.frozen[p.Conversation] || len(s.started) != 0 {
				t.Fatalf("the wake did not thaw the live conversation: %+v frozen %v", sent, s.frozen)
			}
			if kind != "" && sent[1].Kind != kind {
				t.Fatalf("the wake did not deliver after thaw: %+v", sent)
			}
			rows, err := s.srv.sessionPauses(t.Context())
			if err != nil || (wake == "message" && (len(rows) != 1 || rows[0].Sleeping || !rows[0].Until.Equal(*p.Until))) || (wake != "message" && len(rows) != 0) {
				t.Fatalf("the wake lost or retained the wrong pause: %+v %v", rows, err)
			}
		})
	}
}

func TestLiveWakeUsesTheCensusTransportPG(t *testing.T) {
	for _, transport := range []string{"stream", "tmux"} {
		for _, sleeping := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", transport, sleeping), func(t *testing.T) {
				s, p := pausedScene(t, true)
				// The census keeps whole seconds: the sleep is dated by the
				// census itself, which a clock reading may outrun by a second.
				p.Sleeping, p.CommandAt = sleeping, time.Unix(s.snap.At, 0).Add(-time.Second)
				p.Transport = "stream"
				if transport == "stream" {
					p.Transport = "tmux"
				}
				savePause(t, s, p)
				s.snap.Sessions[0].Transport = transport
				s.publish()
				before := len(s.requests)
				if err := s.srv.wakePauses(t.Context(), []store.SessionPause{p}, nil); err != nil {
					t.Fatal(err)
				}
				s.drain()
				sent := s.requests[before:]
				if transport == "tmux" {
					if len(sent) != 0 {
						t.Fatalf("tmux received a thaw: %+v", sent)
					}
				} else if len(sent) != 1 || sent[0].Kind != action.SessionThaw {
					t.Fatalf("a live stream did not receive thaw: %+v", sent)
				}
			})
		}
	}
}

func TestAdmissionThawsLiveStreamsRegardlessOfTheSavedSleepMarkPG(t *testing.T) {
	for _, transport := range []string{"stream", "tmux"} {
		for _, sleep := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/sleep-%t", transport, sleep), func(t *testing.T) {
				s, p := pausedScene(t, true)
				s.snap.Sessions[0].Transport = transport
				s.publish()
				s.frozen[p.Conversation] = transport == "stream"
				r := s.r
				if sleep {
					r = s.srv.sleeper
				}
				r.boot = s.snap.BootID
				before, continued := len(s.requests), false
				_, raised, failed := r.raise(t.Context(), []store.LivingSession{p.LivingSession}, 1, func(_ context.Context, _ store.LivingSession, _ livingRow) error {
					s.drain()
					continued = true
					if s.frozen[p.Conversation] {
						t.Fatal("admission continued before thaw")
					}
					return nil
				})
				if !continued || raised != 1 || len(failed) != 0 {
					t.Fatalf("admission did not continue: raised %d failures %v", raised, failed)
				}
				s.drain()
				sent := s.requests[before:]
				if transport == "tmux" {
					if len(sent) != 0 {
						t.Fatalf("admission thawed tmux: %+v", sent)
					}
				} else if len(sent) != 1 || sent[0].Kind != action.SessionThaw {
					t.Fatalf("admission did not thaw a live stream: %+v", sent)
				}
			})
		}
	}
}

func TestAdmissionDoesNotThawALiveCodexThreadPG(t *testing.T) {
	s, p := pausedScene(t, true)
	s.snap.Sessions[0].Agent = "codex"
	s.publish()
	before := len(s.requests)
	_, raised, failed := s.r.raise(t.Context(), []store.LivingSession{p.LivingSession}, 1, func(context.Context, store.LivingSession, livingRow) error { return nil })
	s.drain()
	if raised != 1 || len(failed) != 0 || len(s.requests) != before {
		t.Fatalf("admission thawed a Codex thread: raised %d failures %v actions %+v", raised, failed, s.requests[before:])
	}
}
