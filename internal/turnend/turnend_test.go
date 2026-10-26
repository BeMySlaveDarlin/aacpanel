package turnend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
)

func TestTheDeclarationBelongsToTheBoundConversation(t *testing.T) {
	now := time.UnixMilli(1700000000000)
	for _, codex := range []bool{false, true} {
		dir := t.TempDir()
		b := mcp.Binding{Place: mcp.Place{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab"},
			Name: "lab", SessionID: "019a1f00-0000-7000-8000-00000000abcd", PID: 42, Codex: codex}
		tool := Tool(dir, func() time.Time { return now })
		for _, next := range []string{"owner", "self", "done"} {
			reply, bad := tool.Call(context.Background(), func() (mcp.Binding, error) { return b, nil },
				json.RawMessage(`{"next":"`+next+`","what":" Choose a release "}`))
			if bad || reply != "Noted: "+next+". End the turn now." {
				t.Fatalf("%q, error %v", reply, bad)
			}
			name := File(b.Place, b.Name)
			if codex {
				name = ThreadFile(b.SessionID)
			}
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			var got Turn
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			want := Turn{ConfigDir: b.Place.ConfigDir, Dir: b.Place.Dir, Name: b.Name, SessionID: b.SessionID,
				PID: b.PID, At: now.UnixMilli(), Next: next, What: "Choose a release"}
			if got != want {
				t.Fatalf("kept %+v, want %+v", got, want)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Fatalf("a replacement left %v", entries)
			}
		}
	}
	if File(mcp.Place{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab"}, "lab") != "f7138acc90830a36ac4a7481c26dfb18.json" {
		t.Fatal("the collector and the tool name different files")
	}
}

func TestTheToolRefusesInvalidDeclarationsWithoutWriting(t *testing.T) {
	b := mcp.Binding{Place: mcp.Place{ConfigDir: "/srv/claude", Dir: "/srv/proj"}, SessionID: "conversation"}
	dir := t.TempDir()
	tool := Tool(dir, time.Now)
	for _, key := range []string{"if", "then"} {
		if _, found := tool.InputSchema[key]; found {
			t.Errorf("the tool exposes %q at the schema root; the owner requirement belongs in the call", key)
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"next":"later"}`, `{"next":"owner"}`, `{"next":"owner","what":" "}`,
		`{"next":"done","extra":true}`, `{"next":true}`, `{"next":"done"} {}`, `{"next":"owner","what":"` + strings.Repeat("界", 301) + `"}`} {
		if reply, bad := tool.Call(context.Background(), func() (mcp.Binding, error) { return b, nil }, json.RawMessage(raw)); !bad {
			t.Errorf("%s was accepted: %s", raw, reply)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("invalid arguments left %v", entries)
	}
	if _, err := Keep(dir, b, "owner", strings.Repeat("界", 300), time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{"self", "done"} {
		if _, err := Keep(dir, b, next, "", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	b.Codex = true
	if _, err := Keep(dir, b, "done", "", time.Now()); err == nil {
		t.Fatal("an invalid thread id was accepted")
	}
}

func TestSweepKeepsFreshDeclarationsAndRemovesAbandonedWrites(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for _, name := range []string{"old.json", ".turn-abandoned", "fresh.json", "unrelated"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if name != "fresh.json" {
			old := now.Add(-checklist.MaxAge - time.Second)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := Sweep(dir, now); got != 2 {
		t.Fatalf("removed %d files, want 2", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("left %v", entries)
	}
}
