package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// collector is a socket of the collector for a test, named to the hook by
// the variable env. It takes what a hook sends up to its end and answers ok,
// unless it is mute: then it holds the connection and says nothing.
type collector struct {
	path string
	got  chan []byte
}

func listenCollector(t *testing.T, env string, mute bool) *collector {
	t.Helper()
	// A path under t.TempDir() is past what a unix socket takes.
	dir, err := os.MkdirTemp("", "col")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	c := &collector{path: filepath.Join(dir, "c.sock"), got: make(chan []byte, 4)}
	l, err := net.Listen("unix", c.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	t.Setenv(env, c.path)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			if mute {
				t.Cleanup(func() { conn.Close() })
				continue
			}
			raw, _ := io.ReadAll(conn)
			c.got <- raw
			conn.Write([]byte(`{"ok": true}`))
			conn.Close()
		}
	}()
	return c
}

// sent is what the collector was handed by the hook that just returned, or
// nil: the collector takes the message before it answers, and the hook waits
// for the answer.
func (c *collector) sent() []byte {
	select {
	case raw := <-c.got:
		return raw
	default:
		return nil
	}
}

// relayCases are the cases the relay shares with the delivery's
// ask-hook.py, which sends the same bytes on the same payload.
type relayCases struct {
	Cases []struct {
		What  string  `json:"what"`
		Input string  `json:"input"`
		Sends *string `json:"sends"`
	} `json:"cases"`
}

func TestTheSharedCasesOfTheQuestionRelay(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "ask-hook.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c relayCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	col := listenCollector(t, "AACP_ASK_SOCKET", false)
	for _, cs := range c.Cases {
		var out, errs bytes.Buffer
		code := Run([]string{"ask-hook"}, strings.NewReader(cs.Input), &out, &errs)
		if code != 0 || out.Len() > 0 || errs.Len() > 0 {
			t.Errorf("%s: exit %d, answer %q, stderr %q", cs.What, code, out.String(), errs.String())
		}
		got := col.sent()
		switch {
		case cs.Sends == nil && got != nil:
			t.Errorf("%s: sent %s", cs.What, got)
		case cs.Sends != nil && string(got) != *cs.Sends:
			t.Errorf("%s: sent\n%s\nwant\n%s", cs.What, got, *cs.Sends)
		}
	}
}

// The relay never holds a question, whatever stands where the collector
// should: the hook says nothing, exits 0 and is done within its steps.
func TestTheRelayNeverHoldsTheQuestion(t *testing.T) {
	was := relayStep
	relayStep = 100 * time.Millisecond
	t.Cleanup(func() { relayStep = was })
	question := `{"session_id":"s","tool_name":"AskUserQuestion","tool_input":{"questions":[]}}`
	stale := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for what, place := range map[string]func(t *testing.T){
		"no collector at all":         func(t *testing.T) { t.Setenv("AACP_ASK_SOCKET", filepath.Join(t.TempDir(), "none.sock")) },
		"a socket nobody listens on":  func(t *testing.T) { t.Setenv("AACP_ASK_SOCKET", stale) },
		"a collector that never says": func(t *testing.T) { listenCollector(t, "AACP_ASK_SOCKET", true) },
	} {
		t.Run(what, func(t *testing.T) {
			place(t)
			began := time.Now()
			var out, errs bytes.Buffer
			if code := Run([]string{"ask-hook"}, strings.NewReader(question), &out, &errs); code != 0 || out.Len() > 0 {
				t.Errorf("exit %d, answer %q", code, out.String())
			}
			if took := time.Since(began); took > 3*relayStep {
				t.Errorf("the relay took %v", took)
			}
		})
	}
}

// The relay waits for the collector's word, so the question is in the book
// before claude puts it on the screen.
func TestTheRelayWaitsForTheCollectorsWord(t *testing.T) {
	dir, err := os.MkdirTemp("", "col")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "c.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	t.Setenv("AACP_ASK_SOCKET", path)
	booked := make(chan time.Time, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.ReadAll(conn)
		time.Sleep(200 * time.Millisecond)
		booked <- time.Now()
		conn.Write([]byte(`{"ok": true}`))
	}()
	Run([]string{"ask-hook"}, strings.NewReader(`{"tool_name":"AskUserQuestion"}`), io.Discard, io.Discard)
	done := time.Now()
	select {
	case at := <-booked:
		if done.Before(at) {
			t.Error("the relay returned before the collector answered")
		}
	default:
		t.Error("the relay returned before the collector answered")
	}
}
