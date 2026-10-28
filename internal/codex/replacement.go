package codex

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"aacpanel/internal/stream"
)

// RestartSettings carries only what the link knows, with the name the panel
// gave the thread. Empty settings let the executor fall back to the map;
// writable roots are not kept by the link.
func (l *Link) RestartSettings(id string) (Begin, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.threads[id]
	if t == nil || t.info.CWD == "" {
		return Begin{}, fmt.Errorf("thread %s has no live directory", id)
	}
	sandbox := map[string]string{
		"readOnly": "read-only", "workspaceWrite": "workspace-write", "dangerFullAccess": "danger-full-access",
	}[t.settings.sandbox]
	if sandbox == "" {
		sandbox = map[string]string{
			":read-only": "read-only", ":workspace": "workspace-write", ":danger-full-access": "danger-full-access",
		}[t.settings.profile]
	}
	return Begin{CWD: t.info.CWD, Model: t.info.Model, Effort: t.info.Effort, Sandbox: sandbox,
		Parent: t.parent, Name: t.given, Hold: true}, nil
}

// Children includes threads in every home: a visual parent is a session
// name, not the home or the native parentThreadId of a Codex subagent.
func (ls *Links) Children(parent string) []Thread {
	var out []Thread
	for _, l := range ls.list {
		l.mu.Lock()
		for id, t := range l.threads {
			if t.parent == parent {
				out = append(out, Thread{Link: l, ID: id, Name: SessionName(id)})
			}
		}
		l.mu.Unlock()
	}
	return out
}

func (l *Link) closingPath(id string) string {
	return filepath.Join(stream.Dir(), "codex-closing", l.contour, id)
}

// CloseAfterTurn leaves the calling turn alive to receive the tool's reply.
// The mark survives an executor restart; the poll closes only an idle thread.
func (l *Link) CloseAfterTurn(id string) error {
	l.sub.Lock()
	defer l.sub.Unlock()
	if err := write(l.closingPath(id), nil); err != nil {
		return err
	}
	l.nudge()
	return nil
}

func (l *Link) Closing(id string) bool {
	_, err := os.Stat(l.closingPath(id))
	return err == nil
}

func (l *Link) finishClose(ctx context.Context, c *conn, id string) bool {
	if !l.Closing(id) {
		return false
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	info, err := read(ctx, c, id)
	if err != nil || info.Status.Type == "active" {
		return false
	}
	// Status can lag behind turn/start; the turn itself must have ended.
	turn, err := runningTurn(ctx, c, id)
	if err != nil || turn != "" {
		return false
	}
	if err := within(ctx, c, "thread/unsubscribe", map[string]any{"threadId": id}, nil); err != nil {
		log.Printf("codex %s: closing thread %s after its turn: %v", l.home, id, err)
		return false
	}
	l.unhold(id)
	l.mu.Lock()
	l.closed[id] = ""
	l.remove(id)
	delete(l.boxes, id)
	l.keepOutbox(id)
	l.mu.Unlock()
	_ = os.Remove(l.closingPath(id))
	return true
}
