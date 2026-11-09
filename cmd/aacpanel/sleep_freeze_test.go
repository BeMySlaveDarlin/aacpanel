package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"aacpanel/internal/action"
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
