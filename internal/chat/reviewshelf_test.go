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

// shelfSocket is a stand-in for the agent: it answers one reading the way the
// shelf does and hands back what it was given.
func shelfSocket(t *testing.T, reply func(put ReviewPut) any) string {
	t.Helper()

	// A socket of its own in a short directory: the name of a test travels into
	// the path of a temporary one, and the limit of a unix path is 108 bytes.
	dir, err := os.MkdirTemp("", "shelf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	path := filepath.Join(dir, "review.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("the stand-in shelf did not come up: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				// The agent reads to the end of the stream, so the stand-in
				// does too: a decoder that stops at the first object would
				// take a request whose sender never closed its half and call
				// the exchange a success.
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				body, err := io.ReadAll(conn)
				if err != nil || len(body) == 0 {
					return
				}
				var put ReviewPut
				if err := json.Unmarshal(body, &put); err != nil {
					return
				}
				_ = json.NewEncoder(conn).Encode(reply(put))
			}()
		}
	}()
	return path
}

// The reading goes over in one piece and comes back with the path of its file.
func TestAReadingReachesTheShelfAndAnswersWithItsPath(t *testing.T) {
	var got ReviewPut
	path := shelfSocket(t, func(put ReviewPut) any {
		got = put
		return map[string]any{"ok": true, "id": put.ID, "path": "/var/lib/aacpanel/reviews/" + put.ID + ".json"}
	})

	shelf := &ReviewShelf{Socket: path}
	where, err := shelf.Put(context.Background(), ReviewPut{
		ID: "lab-20260919-081500", Session: "lab", Cwd: "/srv/proj", Base: "main",
		Notes: []ReviewNote{{ID: "n1", Path: "pkg/env.go", Line: 74, Quote: "\tif warn != \"\" {", Text: "reads twice"}},
	})
	if err != nil {
		t.Fatalf("the reading did not reach the shelf: %v", err)
	}
	if where != "/var/lib/aacpanel/reviews/lab-20260919-081500.json" {
		t.Errorf("the shelf answered with %q", where)
	}
	if len(got.Notes) != 1 || got.Notes[0].Quote != "\tif warn != \"\" {" {
		t.Errorf("the note arrived as %+v — the quote is what finds it again later", got.Notes)
	}
}

// A reading larger than one read still arrives whole: the write half is closed
// so the agent can take the request to the end of the stream.
func TestALongReadingArrivesWhole(t *testing.T) {
	var notes int
	path := shelfSocket(t, func(put ReviewPut) any {
		notes = len(put.Notes)
		return map[string]any{"ok": true, "path": "/shelf/long.json"}
	})

	long := ReviewPut{ID: "lab-long", Session: "lab"}
	for i := 0; i < 200; i++ {
		long.Notes = append(long.Notes, ReviewNote{
			ID:   "n" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + strings.Repeat("0", i%5),
			Path: "pkg/env.go", Line: i + 1, Quote: strings.Repeat("q", 1500), Text: strings.Repeat("t", 3000),
		})
	}
	if _, err := (&ReviewShelf{Socket: path}).Put(context.Background(), long); err != nil {
		t.Fatalf("a long reading did not get through: %v", err)
	}
	if notes != 200 {
		t.Errorf("the shelf saw %d notes of 200 — the request was cut", notes)
	}
}

// A refusal from the shelf is said out loud rather than taken for success: the
// panel is about to tell a person their notes have gone.
func TestARefusalFromTheShelfIsNotTakenForSuccess(t *testing.T) {
	path := shelfSocket(t, func(ReviewPut) any {
		return map[string]any{"ok": false, "error": "that is not the name of a reading"}
	})

	if _, err := (&ReviewShelf{Socket: path}).Put(context.Background(), ReviewPut{ID: "x", Session: "lab"}); err == nil {
		t.Error("a refused reading came back as written")
	} else if !strings.Contains(err.Error(), "not the name of a reading") {
		t.Errorf("the refusal lost its reason: %v", err)
	}
}

// An answer that says yes and names no file is a reading nobody can quote.
func TestAShelfThatNamesNoFileIsARefusal(t *testing.T) {
	path := shelfSocket(t, func(ReviewPut) any { return map[string]any{"ok": true} })

	if _, err := (&ReviewShelf{Socket: path}).Put(context.Background(), ReviewPut{ID: "x", Session: "lab"}); err == nil {
		t.Error("a reading with no path came back as written")
	}
}

// With no shelf at all the panel says so plainly instead of hanging on a
// socket that is not there.
func TestWithNoShelfThePanelSaysSo(t *testing.T) {
	if _, err := (&ReviewShelf{}).Put(context.Background(), ReviewPut{ID: "x", Session: "lab"}); !errors.Is(err, ErrNoShelf) {
		t.Errorf("an unconfigured shelf answered %v", err)
	}
	if _, err := (&ReviewShelf{Socket: "/nowhere/review.sock"}).Put(context.Background(), ReviewPut{ID: "x", Session: "lab"}); !errors.Is(err, ErrNoShelf) {
		t.Errorf("a missing socket answered %v", err)
	}
}
