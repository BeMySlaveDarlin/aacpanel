package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/guards"
	"aacpanel/internal/hook"
	"aacpanel/internal/host"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

func codexRestartServer(t *testing.T) (*Server, *fakeExec, restartSession) {
	t.Helper()
	srv, fake, dir := restartServer(t)
	profiles, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.UpdateProfile(t.Context(), profiles[0].ID, store.ProfileEdit{LaunchSet: map[string]any{
		"agent": schema.AgentCodex, "autoRestart": true, "contextCap": 80,
		"restartIntent": "Read the project handoff", "intent": "Start new work",
	}}); err != nil {
		t.Fatal(err)
	}
	pct, known := 79.0, true
	return srv, fake, restartSession{LiveSession: host.LiveSession{Name: "codex-0000abcd", SessionID: closeThread,
		CWD: dir, Agent: schema.AgentCodex, Transport: "stream"}, Status: "idle", Pct: &pct, Limit: 1000000, LimitKnown: &known}
}

func restartRequest(t *testing.T, fake *fakeExec) action.Request {
	t.Helper()
	select {
	case req := <-fake.got:
		return req
	default:
		t.Fatal("the restart cycle sent no action")
		return action.Request{}
	}
}

func noRestartRequest(t *testing.T, fake *fakeExec) {
	t.Helper()
	select {
	case req := <-fake.got:
		t.Fatalf("the restart cycle sent an unexpected action: %+v", req)
	default:
	}
}

func keepRestartPause(t *testing.T, srv *Server, row restartSession, now time.Time) {
	t.Helper()
	id, err := srv.db.HostID(t.Context(), srv.hostName)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.db.PutSessionPause(t.Context(), id, store.SessionPause{LivingSession: store.LivingSession{
		Conversation: row.SessionID, Session: row.Name, CWD: row.CWD, Agent: row.Agent,
	}, Snapshot: json.RawMessage(`{}`), CommandAt: now}); err != nil {
		t.Fatal(err)
	}
}

func TestCodexContextRestartUsesReadinessAndTheRestartMessagePG(t *testing.T) {
	for _, intent := range []string{"", "Read the thread's saved handoff"} {
		t.Run("intent="+intent, func(t *testing.T) {
			srv, fake, row := codexRestartServer(t)
			now := time.Now().Truncate(time.Second)
			restartSnapshot(t, srv, row, now)
			if err := srv.restartOnce(t.Context(), now); err != nil {
				t.Fatal(err)
			}
			command := restartRequest(t, fake)
			if command.Kind != action.SessionSend || command.Target != row.Name || command.Text != restartMessage(restartCommand) {
				t.Fatalf("the thread did not get the save command: %+v", command)
			}
			if w := readyConversationPost(t, srv, true, row.SessionID, intent); w.Code != http.StatusOK {
				t.Fatalf("thread readiness: %d %s", w.Code, w.Body.String())
			}
			cycles, err := srv.db.RestartCycles(t.Context())
			if err != nil || len(cycles) != 1 || cycles[0].ReadyAt == nil {
				t.Fatalf("the thread's readiness was not saved: %+v %v", cycles, err)
			}
			afterReady := cycles[0].ReadyAt.Add(2 * time.Second)
			// Readiness survives the service, but cannot end the tool's turn
			// or use an idle snapshot collected before the tool answered.
			srv = &Server{db: srv.db, host: srv.host, exec: srv.exec}
			if err := srv.restartOnce(t.Context(), now); err != nil {
				t.Fatal(err)
			}
			noRestartRequest(t, fake)
			row.Status = "busy"
			restartSnapshot(t, srv, row, afterReady)
			if err := srv.restartOnce(t.Context(), afterReady); err != nil {
				t.Fatal(err)
			}
			noRestartRequest(t, fake)
			row.Status = "idle"
			restartSnapshot(t, srv, row, afterReady.Add(time.Second))
			if err := srv.restartOnce(t.Context(), afterReady.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			req := restartRequest(t, fake)
			if req.Kind != action.SessionRestart || req.Target != row.Name || req.Conversation != row.SessionID ||
				!req.AutoRestart || req.Anyway || req.Resume != "" || req.Text != intent || req.Project == nil {
				t.Fatalf("the thread did not get a fresh restart: %+v", req)
			}
			var launch map[string]any
			if err := json.Unmarshal(req.Project.Launch, &launch); err != nil || launch["intent"] != "Read the project handoff" {
				t.Fatalf("the project restart message was lost: %s %v", req.Project.Launch, err)
			}
			if err := srv.restartOnce(t.Context(), afterReady.Add(2*time.Second)); err != nil {
				t.Fatal(err)
			}
			noRestartRequest(t, fake)
			cycles, err = srv.db.RestartCycles(t.Context())
			if err != nil || len(cycles) != 1 || !cycles[0].Done || cycles[0].ReadyAt == nil || cycles[0].Intent != intent {
				t.Fatalf("the thread's completed cycle was not saved: %+v %v", cycles, err)
			}
		})
	}
}

func TestCodexContextRestartRemindsOnceAndForcesAfterFiveMinutesPG(t *testing.T) {
	srv, fake, row := codexRestartServer(t)
	now := time.Now().Truncate(time.Second)
	restartSnapshot(t, srv, row, now)
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if req := restartRequest(t, fake); req.Kind != action.SessionSend || req.Text != restartMessage(restartCommand) {
		t.Fatalf("save command: %+v", req)
	}
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	noRestartRequest(t, fake)
	row.LastRequestAt = now.Add(time.Second).Format(time.RFC3339Nano)
	restartSnapshot(t, srv, row, now.Add(2*time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if req := restartRequest(t, fake); req.Kind != action.SessionSend || req.Text != restartMessage(restartReminder) {
		t.Fatalf("readiness reminder: %+v", req)
	}
	if err := srv.restartOnce(t.Context(), now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	noRestartRequest(t, fake)
	cycles, err := srv.db.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 || cycles[0].RemindedAt == nil {
		t.Fatalf("the reminder was not saved: %+v %v", cycles, err)
	}
	deadline := cycles[0].RemindedAt.Add(restartGrace)
	srv = &Server{db: srv.db, host: srv.host, exec: srv.exec}
	row.Status = "busy"
	restartSnapshot(t, srv, row, deadline.Add(-time.Second))
	if err := srv.restartOnce(t.Context(), deadline.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	noRestartRequest(t, fake)
	restartSnapshot(t, srv, row, deadline)
	if err := srv.restartOnce(t.Context(), deadline); err != nil {
		t.Fatal(err)
	}
	req := restartRequest(t, fake)
	if req.Kind != action.SessionRestart || !req.Anyway || !req.AutoRestart || req.Conversation != row.SessionID ||
		req.Target != row.Name || req.Resume != "" || req.Project == nil {
		t.Fatalf("the deadline did not force a fresh thread: %+v", req)
	}
}

func TestCodexContextRestartLeavesIneligibleThreadsAlonePG(t *testing.T) {
	for _, reason := range []string{"paused", "pause alarm", "tmux", "outside the map", "outside the daemon", "running turn"} {
		t.Run(reason, func(t *testing.T) {
			srv, fake, row := codexRestartServer(t)
			now := time.Now()
			switch reason {
			case "paused":
				keepRestartPause(t, srv, row, now)
			case "outside the map":
				row.CWD += "-outside"
			case "pause alarm":
				row.Pause = &pauseAlarm{ID: "pause", Name: action.PauseNoEndAlarm}
			case "tmux":
				row.Transport = "tmux"
			case "outside the daemon":
				row.Outside = true
			case "running turn":
				row.Status = "busy"
			}
			restartSnapshot(t, srv, row, now)
			if err := srv.restartOnce(t.Context(), now); err != nil {
				t.Fatal(err)
			}
			noRestartRequest(t, fake)
			cycles, err := srv.db.RestartCycles(t.Context())
			if err != nil || len(cycles) != 0 {
				t.Fatalf("an ineligible thread entered the restart cycle: %+v %v", cycles, err)
			}
		})
	}
}

func TestRestartPolicyLetsTheStampPromiseOnlyManagedRestartsPG(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"Codex daemon", true}, {"Claude", true}, {"Codex tmux", false}, {"Claude paused", false},
		{"Codex paused", false}, {"outside the map", false}, {"outside the panel", false},
		{"disabled", false}, {"stale snapshot", false}, {"unknown window", false}, {"completed cycle", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, fake, row := codexRestartServer(t)
			now := time.Now().Truncate(time.Second)
			profiles, err := srv.db.Profiles(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Dir(row.CWD)
			launch := map[string]any{}
			switch tc.name {
			case "Claude", "Claude paused":
				row.Agent, launch["agent"] = "", schema.AgentClaude
			case "Codex tmux":
				row.Transport = "tmux"
			case "Codex paused":
				row.Pause = &pauseAlarm{ID: "pause", Name: action.PauseNoEndAlarm}
			case "outside the map":
				row.CWD = filepath.Join(root, "unmapped")
			case "outside the panel":
				row.Outside = true
			case "disabled":
				launch["autoRestart"] = false
			case "stale snapshot":
				now = now.Add(-time.Minute)
			case "unknown window":
				known := false
				row.LimitKnown = &known
			}
			if _, err := srv.db.UpdateProfile(t.Context(), profiles[0].ID, store.ProfileEdit{Prefix: &root, LaunchSet: launch}); err != nil {
				t.Fatal(err)
			}
			if tc.name == "Claude paused" {
				keepRestartPause(t, srv, row, now)
			}
			if tc.name == "completed cycle" {
				if err := srv.db.KeepRestartCycle(t.Context(), store.RestartCycle{Conversation: row.SessionID,
					Session: row.Name, CommandAt: now, Commanded: true, Done: true}); err != nil {
					t.Fatal(err)
				}
			}
			pct := 85.0
			row.Pct = &pct
			restartSnapshot(t, srv, row, now)
			panel := httptest.NewServer(http.HandlerFunc(srv.apiRestartPolicy))
			t.Cleanup(panel.Close)
			t.Setenv(hostcfg.PanelEnv, panel.URL)
			t.Setenv(hook.HeldEnv, "")
			t.Setenv(hook.PIDEnv, "")
			t.Setenv("CLAUDE_PROJECT_DIR", "")
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			state := t.TempDir()
			t.Setenv(hostcfg.StateDirEnv, state)
			if err := os.MkdirAll(guards.Home(), 0o700); err != nil {
				t.Fatal(err)
			}
			// The contour guard is deliberately broader than its project,
			// and can be stale after a map edit. It cannot promise a restart.
			if err := os.WriteFile(guards.Path(), []byte(guards.Lines([]action.Guard{{Path: root, Cap: 80, Restart: true}})), 0o600); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(map[string]any{"at": now.Unix(), "sessions": []map[string]any{{
				"sessionId": row.SessionID, "agent": row.Agent, "transport": row.Transport, "cwd": row.CWD,
				"tokens": 850000, "limit": row.Limit, "pct": pct,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(state, "state.json"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]any{"session_id": row.SessionID, "cwd": row.CWD})
			var out, errs bytes.Buffer
			if code := hook.Run([]string{"prompt-stamp"}, bytes.NewReader(payload), &out, &errs); code != 0 || errs.Len() != 0 {
				t.Fatalf("the stamp failed: %d %s", code, errs.String())
			}
			var reply struct {
				Hook struct {
					Context string `json:"additionalContext"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			stamp := reply.Hook.Context
			if !strings.Contains(stamp, "CONTEXT past the cap") || strings.Contains(stamp, "auto restart at") != tc.want ||
				strings.Contains(stamp, "start no new work") != tc.want || strings.Contains(stamp, "the panel asks") != tc.want {
				t.Fatalf("the stamp's restart promise is %t, want %t:\n%s", !tc.want, tc.want, stamp)
			}
			if err := srv.restartOnce(t.Context(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if tc.want {
				if req := restartRequest(t, fake); req.Kind != action.SessionSend || req.Text != restartMessage(restartCommand) {
					t.Fatalf("the promised restart did not send its save command: %+v", req)
				}
			} else {
				noRestartRequest(t, fake)
			}
		})
	}
}
