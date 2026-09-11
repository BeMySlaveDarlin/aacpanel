package usage

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/store"
)

func TestRewindMakesTheRoundRereadTheFile(t *testing.T) {
	var asked []int64
	client := scriptedAgent(t, func(req map[string]any) []string {
		if req["op"] == "list" {
			return []string{`{"file":{"path":"/p/a.jsonl","contour":"home","inode":7,` +
				`"size":900,"head":"6f6b","first":"2026-09-10T10:00:00.000Z"}}`}
		}
		files, _ := req["files"].([]any)
		first, _ := files[0].(map[string]any)
		offset, _ := first["offset"].(float64)
		asked = append(asked, int64(offset))
		if len(asked) == 1 {
			return []string{`{"result":{"path":"/p/a.jsonl","rewind":true,"offset":800,` +
				`"session":{"sessionId":"s1","contour":"home"},` +
				`"rows":[{"bucket":"2026-09-10T10:00:00Z","model":"opus","answers":1}]}}`}
		}
		return []string{`{"result":{"path":"/p/a.jsonl","offset":900,` +
			`"session":{"sessionId":"s1","contour":"home"},` +
			`"rows":[{"bucket":"2026-09-10T06:00:00Z","model":"opus","answers":40},` +
			`{"bucket":"2026-09-10T10:00:00Z","model":"opus","answers":1}]}}`}
	})

	db := &fakeStore{}
	s := NewScanner(client, db)
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("the round: %v", err)
	}

	if len(db.written) != 1 {
		t.Fatalf("%d writes, expected one: an incomplete batch is not written at all", len(db.written))
	}
	if n := len(db.written[0].Rows); n != 2 {
		t.Fatalf("%d rows written, expected two — the file was reread whole", n)
	}
	if db.written[0].Scan.Offset != 900 {
		t.Errorf("the boundary is %d, expected 900 — from the second pass", db.written[0].Scan.Offset)
	}
	if !db.written[0].Rewrite {
		t.Error("a file reread whole must wipe its previous rows")
	}
	if len(asked) != 2 || asked[1] != 0 {
		t.Errorf("passes %v, expected the second one from zero", asked)
	}
}

func TestProgressCountsWhatReachedTheBase(t *testing.T) {
	client := scriptedAgent(t, func(req map[string]any) []string {
		if req["op"] == "list" {
			return []string{
				`{"file":{"path":"/p/a.jsonl","contour":"home","inode":7,"size":100,"head":"6f6b","first":"2026-09-10T10:00:00.000Z"}}`,
				`{"file":{"path":"/p/b.jsonl","contour":"work","inode":8,"size":200,"head":"6f6b","first":"2026-09-10T11:00:00.000Z"}}`,
			}
		}
		return []string{
			`{"result":{"path":"/p/a.jsonl","offset":100,"session":{"sessionId":"s1","contour":"home"}}}`,
			`{"result":{"path":"/p/b.jsonl","error":"the file was not parsed"}}`,
		}
	})

	s := NewScanner(client, &fakeStore{})
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("the round: %v", err)
	}
	st := s.State()
	if st.Failed != 1 {
		t.Errorf("%d files unparsed, expected one", st.Failed)
	}
	done := map[string]int{}
	for _, c := range st.Contours {
		done[c.Contour] = c.FilesDone
	}
	if done["home"] != 1 {
		t.Errorf("contour home parsed %d files, expected one", done["home"])
	}
	if done["work"] != 0 {
		t.Errorf("contour work parsed %d files, while the file did not parse at all", done["work"])
	}
}

func TestBaseFailureStopsTheRound(t *testing.T) {
	client := scriptedAgent(t, func(req map[string]any) []string {
		if req["op"] == "list" {
			return []string{
				`{"file":{"path":"/p/a.jsonl","contour":"home","inode":7,"size":100,"head":"6f6b","first":"2026-09-10T10:00:00.000Z"}}`,
				`{"file":{"path":"/p/b.jsonl","contour":"home","inode":8,"size":200,"head":"6f6b","first":"2026-09-10T11:00:00.000Z"}}`,
			}
		}
		return []string{
			`{"result":{"path":"/p/a.jsonl","offset":100,"session":{"sessionId":"s1","contour":"home"}}}`,
			`{"result":{"path":"/p/b.jsonl","offset":200,"session":{"sessionId":"s2","contour":"home"}}}`,
		}
	})

	db := &fakeStore{fail: errors.New("the database refuses writes")}
	s := NewScanner(client, db)
	err := s.Run(context.Background())
	if err == nil {
		t.Fatal("a database failure must fail the round")
	}
	if len(db.written) != 0 {
		t.Errorf("%d writes, while the database accepted none", len(db.written))
	}
	if s.State().Error == "" {
		t.Error("the reason did not stay in the state: a silent screen is indistinguishable from success")
	}
	for _, c := range s.State().Contours {
		if c.FilesDone != 0 {
			t.Errorf("contour %s parsed %d files, while nothing reached the database: progress "+
				"counts what was written", c.Contour, c.FilesDone)
		}
	}
}

type fakeStore struct {
	mu      sync.Mutex
	points  map[string]store.UsageScanPoint
	written []store.UsageFile
	missing []string
	fail    error
}

func (f *fakeStore) UsageScanPoints(context.Context) (map[string]store.UsageScanPoint, error) {
	if f.points == nil {
		return map[string]store.UsageScanPoint{}, nil
	}
	return f.points, nil
}

func (f *fakeStore) MarkUsageScanMissing(_ context.Context, paths []string) (int, error) {
	f.missing = append(f.missing, paths...)
	return len(paths), nil
}

func (f *fakeStore) WriteUsageFile(_ context.Context, file store.UsageFile) error {
	if f.fail != nil {
		return f.fail
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, file)
	return nil
}

func scriptedAgent(t *testing.T, reply func(map[string]any) []string) *Client {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "u.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("the socket in %s did not come up: %v", dir, err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				var raw []byte
				buf := make([]byte, 1<<16)
				for {
					n, err := conn.Read(buf)
					raw = append(raw, buf[:n]...)
					if n == 0 || err != nil {
						break
					}
				}
				var req map[string]any
				if err := json.Unmarshal(raw, &req); err != nil {
					return
				}
				for _, line := range reply(req) {
					conn.Write([]byte(line + "\n"))
				}
				conn.Write([]byte(`{"end":true}` + "\n"))
			}()
		}
	}()
	return New(path)
}
