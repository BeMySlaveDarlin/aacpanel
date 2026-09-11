package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The collector writes the state of a session, the panel reads it into typed
// structures, and a field the structure does not declare is dropped on the way
// without a word. The screen then shows the old behaviour and nobody can tell
// why: the collector is right, the feed is right, and the value is gone in
// between. This walks the keys the collector puts into a task and demands that
// each of them has a place to land.
func TestEveryTaskFieldTheCollectorSendsHasAPlace(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "agent", "sesstate", "tasks.py"))
	if err != nil {
		t.Fatalf("the collector source is out of reach: %v", err)
	}

	sent := collectorKeys(t, string(raw))
	if len(sent) < 4 {
		t.Fatalf("only %d keys found in the collector: the test reads the wrong place", len(sent))
	}

	known := map[string]bool{}
	typ := reflect.TypeOf(WorkTask{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			known[name] = true
		}
	}

	for _, key := range sent {
		if !known[key] {
			t.Errorf("the collector sends the field %q of a task and WorkTask has nowhere to put it: "+
				"the value is dropped between the two, silently", key)
		}
	}
}

// collectorKeys returns the names of the fields the collector assigns to a task.
func collectorKeys(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	seen := map[string]bool{}

	// The literal a task is built from: state.tasks[task_id] = { ... }
	start := strings.Index(src, "state.tasks[task_id] = {")
	if start < 0 {
		t.Fatal("the literal of a task is not where the test looks for it")
	}
	end := strings.Index(src[start:], "\n    }")
	if end < 0 {
		t.Fatal("the literal of a task has no end where the test expects one")
	}
	body := src[start : start+end]

	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`"([a-zA-Z]+)":`),            // inside the literal
		regexp.MustCompile(`task\["([a-zA-Z]+)"\]\s*=`), // assigned to it later
	} {
		text := body
		if strings.Contains(re.String(), "task") {
			text = src
		}
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// A finished shell has to survive the trip from the collector to the feed.
func TestAFinishedShellKeepsItsMarkThroughTheAPI(t *testing.T) {
	raw := []byte(`{"id":"b1","text":"build","kind":"bash","done":true,"doneAt":"2026-09-12T00:00:00Z"}`)
	var task WorkTask
	if err := json.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	if !task.Done {
		t.Error("the mark of a finished shell is lost on the way in")
	}
	out, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"done":true`) {
		t.Errorf("the mark is lost on the way out: %s", out)
	}
}
