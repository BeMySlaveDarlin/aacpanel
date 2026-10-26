package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/hostcfg"
)

func TestTheSharedCasesOfTheTurnReminder(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "turn-reminder.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c checklistCases
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("the fixture has no cases")
	}
	c.Setup = turnSources
	runReminderCases(t, c, "turn-reminder")
}

func turnSources(t *testing.T, dir string, stop checklistStop) {
	t.Helper()
	t.Setenv(hostcfg.StateDirEnv, filepath.Join(dir, "panel"))
	writeJSON(t, filepath.Join(dir, "panel", "state.json"), map[string]any{"sessions": []any{}})
	for file, value := range stop.Files {
		writeJSON(t, filepath.Join(dir, file), value)
	}
	for _, file := range stop.Missing {
		if err := os.Remove(filepath.Join(dir, file)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheTwoRemindersShareOneStopRound(t *testing.T) {
	c := readChecklistCases(t)
	dir, proc, config := inPlace(t)
	c.Args["withTool"] = append(c.Args["withTool"], turnTool)
	placeClaude(t, c, proc, config, nil)
	parentPID = func() int { return c.Claude.PID }
	writeChecklist(t, c, config, map[string]any{"statuses": []any{"active"}})
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(c.Transcripts["worked"], "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	for _, active := range []bool{false, true} {
		raw, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "session_id": c.Session,
			"transcript_path": path, "stop_hook_active": active})
		for _, name := range []string{"turn-reminder", "checklist-reminder"} {
			reply := handlers[name].Answer("Stop", raw, time.Now())
			if (reply.Block == "") != active {
				t.Errorf("%s with active %v: %+v", name, active, reply)
			}
		}
	}
}
