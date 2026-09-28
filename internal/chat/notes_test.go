package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// collectorSocket is a stand-in for the chat socket of the collector: it
// answers every request with reply and hands back what it was asked.
func collectorSocket(t *testing.T, reply any) (string, <-chan map[string]any) {
	t.Helper()
	dir, err := os.MkdirTemp("", "notes")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	path := filepath.Join(dir, "chat.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("the stand-in collector did not come up: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	asked := make(chan map[string]any, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				body, err := io.ReadAll(conn)
				if err != nil {
					return
				}
				var req map[string]any
				if json.Unmarshal(body, &req) == nil {
					asked <- req
				}
				_ = json.NewEncoder(conn).Encode(reply)
			}()
		}
	}()
	return path, asked
}

// The request and the answer travel under the names the collector reads and
// writes: a name out of step is a call that never reaches a phone, with both
// sides green in their own tests.
func TestTheCallsTravelUnderTheNamesOfTheCollector(t *testing.T) {
	root := filepath.Join("..", "..")
	server, err := os.ReadFile(filepath.Join(root, "agent", "chat", "server.py"))
	if err != nil {
		t.Fatalf("the collector source is out of reach: %v", err)
	}
	board, err := os.ReadFile(filepath.Join(root, "agent", "notes.py"))
	if err != nil {
		t.Fatalf("the collector source is out of reach: %v", err)
	}
	for src, names := range map[string][]string{
		string(server): {`request.get("notes")`, `want_notes.get("after")`, `want_notes.get("wait")`,
			`"notes": standing, "seq": seq`},
		string(board): {`"sessionId": session`, `"text": text`, `"at": time.strftime`},
	} {
		for _, name := range names {
			if !strings.Contains(src, name) {
				t.Errorf("the collector no longer says %s — the Go side of the calls speaks to nobody", name)
			}
		}
	}

	path, asked := collectorSocket(t, map[string]any{"ok": true, "seq": 4, "notes": []map[string]string{
		{"sessionId": "4d89ed41", "text": "stuck on the migration", "at": "2026-09-08T03:00:00Z"},
	}})
	after := int64(3)
	got, err := New(path).Notes(context.Background(), &after, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := <-asked
	notes, _ := req["notes"].(map[string]any)
	if notes["after"] != float64(3) {
		t.Errorf("the count the panel has seen went as %v", notes["after"])
	}
	if wait, _ := notes["wait"].(float64); wait <= 0 || wait > MaxNoteWait.Seconds() {
		t.Errorf("the wait went as %v s — past %s the socket gives up on the answer first", notes["wait"], MaxNoteWait)
	}
	if got.Seq != 4 || len(got.Notes) != 1 || got.Notes[0] != (SessionNote{
		SessionID: "4d89ed41", Text: "stuck on the migration", At: "2026-09-08T03:00:00Z",
	}) {
		t.Errorf("the board came back as %+v", got)
	}
}

// A collector that predates the calls takes the request for a feed of no
// session: it says so, rather than passing for an empty board, since an empty
// board sends nobody to update anything.
func TestACollectorWithoutTheCallsSaysSo(t *testing.T) {
	for name, reply := range map[string]any{
		"refused":     map[string]any{"ok": false, "error": "there is no transcript with this identifier on disk"},
		"no count":    map[string]any{"ok": true, "items": []any{}},
		"empty board": map[string]any{"ok": true, "seq": 0, "notes": []any{}},
	} {
		t.Run(name, func(t *testing.T) {
			path, _ := collectorSocket(t, reply)
			_, err := New(path).Notes(context.Background(), nil, time.Second)
			if name == "empty board" {
				if err != nil {
					t.Errorf("an empty board was taken for a collector without the calls: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrNoNotes) {
				t.Errorf("the answer of an older collector came back as %v", err)
			}
		})
	}
}
