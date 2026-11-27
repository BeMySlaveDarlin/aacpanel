package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/mcp"
	"aacpanel/internal/schema"
	"aacpanel/internal/session"
	"aacpanel/internal/sessionintent"
	"aacpanel/internal/store"
	"aacpanel/internal/taskreg"
)

func handoffRestartServer(t *testing.T, agent, started string) (*Server, *fakeExec, store.ProfileProject) {
	t.Helper()
	srv, fake, dir := switchServer(t, "stream", "stream")
	profiles, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	project := profiles[0].Groups[0].Projects[0]
	if agent == schema.AgentCodex {
		if _, err := srv.db.UpdateProfile(t.Context(), profiles[0].ID, store.ProfileEdit{LaunchSet: map[string]any{"agent": agent}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.db.SwitchTaskWriter(t.Context(), project.ID, dir, taskreg.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"at": 1, "sessions": []host.LiveSession{{
		Name: "aacpanel-2", SessionID: switchSID, CWD: dir, Agent: agent, Transport: "stream", StartedAt: started,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	srv.host = host.NewReader(snapshotWith(t, string(raw)))
	return srv, fake, project
}

func writeRestartHandoff(t *testing.T, srv *Server, project store.ProfileProject, conversation, title string, at time.Time) taskreg.Handoff {
	t.Helper()
	h, err := srv.db.WriteHandoff(t.Context(), project.ID, taskreg.Actor{Conversation: conversation, Name: "aacpanel-2"},
		project.Path, taskreg.HandoffInput{Title: title, Markdown: "Saved the work", Next: "Run the check"})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE project_handoffs SET at=$1 WHERE project_id=$2 AND key=$3`, at, project.ID, h.Key); err != nil {
		t.Fatal(err)
	}
	return h
}

func restartActions(t *testing.T, fake *fakeExec) []action.Request {
	t.Helper()
	var actions []action.Request
	for {
		select {
		case req := <-fake.got:
			if req.Ask != "" {
				if req.Ask != action.AskIntent {
					t.Fatalf("unexpected executor question: %+v", req)
				}
				continue
			}
			actions = append(actions, req)
		default:
			return actions
		}
	}
}

func TestSelfRestartRequiresFreshHandoffPG(t *testing.T) {
	started := time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)
	for _, agent := range []string{schema.AgentClaude, schema.AgentCodex} {
		for _, scene := range []string{"missing", "older", "at start", "other conversation", "other project", "deleted", "unknown start", "invalid start", "latest own"} {
			t.Run(agent+"/"+scene, func(t *testing.T) {
				start := started.Format(time.RFC3339Nano)
				if scene == "unknown start" {
					start = ""
				} else if scene == "invalid start" {
					start = "not a timestamp"
				}
				srv, fake, project := handoffRestartServer(t, agent, start)
				var saved taskreg.Handoff
				if scene != "missing" {
					conversation, at := switchSID, started.Add(time.Minute)
					if scene == "older" {
						at = started.Add(-time.Second)
					} else if scene == "at start" {
						at = started
					} else if scene == "other conversation" {
						conversation = closeThread
					}
					saved = writeRestartHandoff(t, srv, project, conversation, "Saved progress", at)
					pool, _ := srv.db.Pool()
					if scene == "deleted" {
						if _, err := pool.Exec(t.Context(), `UPDATE project_handoffs SET deleted_at=clock_timestamp() WHERE key=$1`, saved.Key); err != nil {
							t.Fatal(err)
						}
					} else if scene == "other project" {
						name, path := "Other", project.Path+"/other"
						other, err := srv.db.CreateProject(t.Context(), project.GroupID, store.ProjectEdit{Name: &name, Path: &path})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := pool.Exec(t.Context(), `UPDATE project_handoffs SET project_id=$1 WHERE key=$2`, other.ID, saved.Key); err != nil {
							t.Fatal(err)
						}
					} else if scene == "latest own" {
						saved = writeRestartHandoff(t, srv, project, switchSID, "Latest work", at.Add(time.Second))
						writeRestartHandoff(t, srv, project, closeThread, "Somebody else's work", at.Add(2*time.Second))
					}
				}
				panel := httptest.NewServer(srv.apiRunAction(true))
				defer panel.Close()
				h := session.Host{Panel: panel.URL, Now: time.Now}
				tool := session.Restart(h)
				if agent == schema.AgentCodex {
					tool = session.CodexRestart(h)
				}
				said, failed := tool.Call(t.Context(), func() (mcp.Binding, error) {
					return mcp.Binding{SessionID: switchSID}, nil
				}, json.RawMessage(`{}`))
				actions := restartActions(t, fake)
				if scene != "latest own" {
					const refusal = "Nothing was restarted: this project's map has no message after a restart, so the new session would start blank. Write a handoff first with handoff_write — the work done and the next step — then call again."
					if !failed || said != refusal || len(actions) != 0 {
						t.Fatalf("unsafe restart: failed=%v, reply=%q, executor actions=%+v", failed, said, actions)
					}
					return
				}
				if failed || len(actions) != 1 || actions[0].Kind != action.SessionRestart || actions[0].Project == nil {
					t.Fatalf("saved restart: failed=%v, reply=%q, executor actions=%+v", failed, said, actions)
				}
				var launch map[string]any
				if err := json.Unmarshal(actions[0].Project.Launch, &launch); err != nil {
					t.Fatal(err)
				}
				want := fmt.Sprintf("Continue from the handoff “%s”: read it with handoff_read key=%s, then take its next step.", saved.Title, saved.Key)
				if launch["intent"] != want {
					t.Fatalf("restart message = %q, want %q", launch["intent"], want)
				}
			})
		}
	}
}

func TestSelfRestartKeepsExplicitMessagesPG(t *testing.T) {
	for _, agent := range []string{schema.AgentClaude, schema.AgentCodex} {
		for _, scene := range []string{"contour message", "project message", "session message", "own intent", "clean", "human", "context cycle"} {
			t.Run(agent+"/"+scene, func(t *testing.T) {
				srv, fake, project := handoffRestartServer(t, agent, "")
				params := map[string]any{"conversation": switchSID}
				target, want, text := "", "", ""
				switch scene {
				case "contour message":
					profiles, err := srv.db.Profiles(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					want = "Continue from the contour"
					if _, err := srv.db.UpdateProfile(t.Context(), profiles[0].ID, store.ProfileEdit{LaunchSet: map[string]any{"restartIntent": want}}); err != nil {
						t.Fatal(err)
					}
				case "project message":
					want = "Continue from the project"
					if _, err := srv.db.UpdateProject(t.Context(), project.ID, store.ProjectEdit{LaunchSet: map[string]any{"restartIntent": want}}); err != nil {
						t.Fatal(err)
					}
				case "session message":
					client, own := startFakeExec(t, action.Response{OK: true, Intents: sessionintent.Values{"restartIntent": "Continue this session"}})
					srv.exec, fake = client, own
				case "own intent":
					text = "Continue the saved work"
					params["intent"] = text
					if agent == schema.AgentClaude {
						params["resume"] = true
					}
				case "clean":
					params["clean"] = true
				case "human":
					target, params = "aacpanel-2", map[string]any{}
				case "context cycle":
					if err := srv.restartAction(t.Context(), store.RestartCycle{Session: "aacpanel-2", Conversation: switchSID}, restartStep{Kind: action.SessionRestart}); err != nil {
						t.Fatal(err)
					}
				}
				if scene != "context cycle" {
					raw, _ := json.Marshal(map[string]any{"kind": action.SessionRestart, "target": target, "params": params})
					if w := post(t, srv, string(raw)); w.Code != http.StatusOK {
						t.Fatalf("restart: %d %s", w.Code, w.Body.String())
					}
				}
				actions := restartActions(t, fake)
				if len(actions) != 1 || actions[0].Kind != action.SessionRestart || actions[0].Project == nil {
					t.Fatalf("restart did not reach the executor: %+v", actions)
				}
				got := actions[0]
				var launch map[string]any
				if err := json.Unmarshal(got.Project.Launch, &launch); err != nil {
					t.Fatal(err)
				}
				if launch["intent"] != want || got.Text != text || got.Clean != (scene == "clean") ||
					(got.Resume != "") != (scene == "own intent" && agent == schema.AgentClaude) || got.AutoRestart != (scene == "context cycle") {
					t.Fatalf("restart options changed: %+v, launch %s", got, got.Project.Launch)
				}
			})
		}
	}
}
