package collector

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
	"unicode"

	"aacpanel/internal/mcp"
)

const sid = "567f4d24-cd5f-48fa-bdc1-04c89d203494"

// bound is the claude the server serves: a session of /srv/proj.
func bound(id string) mcp.Bind {
	return func() (mcp.Binding, error) {
		return mcp.Binding{Place: mcp.Place{ConfigDir: "/home/u/.claude", Dir: "/srv/proj"}, SessionID: id, PID: 4242}, nil
	}
}

// fake stands in for one socket of the collector: it hands over every request
// as it arrived and answers with what answer returns for it.
type fake struct {
	path string
	got  chan map[string]any
}

// listen opens the fake on a short path: a unix socket's path holds 108 bytes,
// and the directory of a test is named after the test.
func listen(t *testing.T, answer func(map[string]any) any) *fake {
	t.Helper()
	dir, err := os.MkdirTemp("", "b")
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{path: filepath.Join(dir, "c.sock"), got: make(chan map[string]any, 8)}
	ln, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ln.Close()
		os.RemoveAll(dir)
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			raw, _ := io.ReadAll(conn)
			var request map[string]any
			_ = json.Unmarshal(raw, &request)
			f.got <- request
			out, _ := json.Marshal(answer(request))
			_, _ = conn.Write(out)
			conn.Close()
		}
	}()
	return f
}

func answers(reply any) func(map[string]any) any {
	return func(map[string]any) any { return reply }
}

// request is what reached the fake, or a failure when nothing did.
func (f *fake) request(t *testing.T) map[string]any {
	t.Helper()
	select {
	case r := <-f.got:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("nothing reached the collector")
		return nil
	}
}

// quiet fails when anything reached the fake.
func (f *fake) quiet(t *testing.T) {
	t.Helper()
	select {
	case r := <-f.got:
		t.Errorf("the collector was asked anyway: %v", r)
	default:
	}
}

func callTool(t *testing.T, tool mcp.Tool, bind mcp.Bind, args string) (string, bool) {
	t.Helper()
	return tool.Call(context.Background(), bind, json.RawMessage(args))
}

// nowhere is a socket nobody listens on.
func nowhere(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "b")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "gone.sock")
}

// The tools as the server lists them: allowed, since none of them waits on the
// person, each with a line of its own for the server's word to the session.
// The model sees only that line and the name of a deferred tool, so the line
// says when to reach for it, the Russian of a request included; the
// description is read once the tool is looked up, and stays short enough for
// any client to keep whole.
func TestTheToolsAreListedAllowedWithALineOfTheirOwn(t *testing.T) {
	for _, tool := range []mcp.Tool{BriefPublish("/nowhere", ""), BriefDelete("/nowhere"), Notify("/nowhere"),
		SecretAsk("/nowhere")} {
		if !tool.Allowed || tool.Title == "" || tool.InputSchema["type"] != "object" {
			t.Errorf("%s is listed as %+v", tool.Name, tool)
		}
		if len(tool.Description) > 1536 {
			t.Errorf("the description of %s runs to %d bytes", tool.Name, len(tool.Description))
		}
		if tool.Instructions == "" || len(tool.Instructions) > 600 {
			t.Errorf("the line of %s is %d bytes: %q", tool.Name, len(tool.Instructions), tool.Instructions)
		}
	}
	for _, tool := range []mcp.Tool{BriefPublish("/nowhere", ""), Notify("/nowhere"), SecretAsk("/nowhere")} {
		russian := false
		for _, r := range tool.Instructions {
			russian = russian || unicode.Is(unicode.Cyrillic, r)
		}
		if !russian {
			t.Errorf("the line of %s names no request said in Russian: %q", tool.Name, tool.Instructions)
		}
	}
}

// A socket nobody listens on is named, so the model can say where the panel
// is missing rather than retry into nothing.
func TestACollectorThatIsNotListeningIsNamed(t *testing.T) {
	path := nowhere(t)
	for _, c := range []struct {
		tool mcp.Tool
		args string
	}{
		{BriefPublish(path, ""), `{"doc":{"id":"a","title":"A"}}`},
		{BriefDelete(path), `{"id":"a"}`},
		{Notify(path), `{"text":"stuck"}`},
	} {
		said, failed := callTool(t, c.tool, bound(sid), c.args)
		if !failed || !strings.Contains(said, "not listening on "+path) {
			t.Errorf("%s with no collector answered %q (error %v)", c.tool.Name, said, failed)
		}
	}
}

// A conversation claude has not written down yet has no name to go out
// under: the answers of a brief would have nowhere to come back to, and a
// push nothing to open. The call says to come again and reaches nobody.
func TestAConversationNotWrittenDownYetIsToldToComeAgain(t *testing.T) {
	f := listen(t, answers(map[string]any{"ok": true, "id": "a"}))
	for _, c := range []struct {
		tool mcp.Tool
		args string
	}{
		{BriefPublish(f.path, ""), `{"doc":{"id":"a","title":"A"}}`},
		{BriefDelete(f.path), `{"id":"a"}`},
		{Notify(f.path), `{"text":"stuck"}`},
		{SecretAsk(f.path), `{"name":"github-token","title":"GitHub token"}`},
	} {
		said, failed := callTool(t, c.tool, bound(""), c.args)
		if !failed || !strings.Contains(said, "Try again in a moment") {
			t.Errorf("%s without a conversation answered %q (error %v)", c.tool.Name, said, failed)
		}
		broken := func() (mcp.Binding, error) {
			return mcp.Binding{}, errors.New("where claude process 4242 works is not known")
		}
		if said, failed := callTool(t, c.tool, broken, c.args); !failed || !strings.Contains(said, "process 4242") {
			t.Errorf("%s with no place answered %q (error %v)", c.tool.Name, said, failed)
		}
	}
	f.quiet(t)
}
