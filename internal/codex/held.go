package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/mcp"
	"aacpanel/internal/stream"
)

const resumeAttempts = 3

// unloaded takes held threads back even when the daemon has let them go.
// The subscription lock keeps a close from crossing a resume; queues stay
// until recovery succeeds or its bounded attempts are exhausted.
func (l *Link) unloaded(ctx context.Context, c *conn, ids []string) []string {
	l.mu.Lock()
	var missing []string
	for id := range l.held {
		if _, closed := l.closed[id]; closed {
			delete(l.held, id)
			delete(l.recovering, id)
			_ = os.Remove(heldPath(l.contour, id))
		} else if !slices.Contains(ids, id) {
			missing = append(missing, id)
		} else {
			delete(l.recovering, id)
		}
	}
	for id := range l.closed {
		if !slices.Contains(ids, id) {
			delete(l.closed, id)
		}
	}
	l.mu.Unlock()
	for _, id := range missing {
		err := l.recover(ctx, c, id)
		if ctx.Err() != nil || c.down() {
			return ids
		}
		l.mu.Lock()
		if err == nil {
			delete(l.recovering, id)
			ids = append(ids, id)
		} else {
			l.recovering[id]++
			if l.recovering[id] >= resumeAttempts {
				delete(l.held, id)
				delete(l.recovering, id)
				_ = os.Remove(heldPath(l.contour, id))
				log.Printf("codex %s: held thread %s could not be resumed after %d attempts and is released: %v",
					l.home, id, resumeAttempts, err)
			}
		}
		l.mu.Unlock()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, b := range l.boxes {
		if slices.Contains(ids, id) || l.held[id] {
			continue
		}
		if len(b.Queue) > 0 {
			log.Printf("codex %s: thread %s is no longer loaded, and %d messages of the panel's queue go with it",
				l.home, id, len(b.Queue))
		}
		delete(l.boxes, id)
		l.keepOutbox(id)
	}
	return ids
}

func (l *Link) recover(ctx context.Context, c *conn, id string) error {
	raw, err := os.ReadFile(heldPath(l.contour, id))
	if err != nil {
		return fmt.Errorf("the held settings were not read: %w", err)
	}
	params := map[string]any{}
	// Empty marks carry no retained launch overrides.
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return fmt.Errorf("the held settings do not parse: %w", err)
		}
	}
	if params == nil {
		params = map[string]any{}
	}
	params["threadId"], params["excludeTurns"] = id, true
	var out struct {
		Thread threadInfo `json:"thread"`
		settingsWire
	}
	if err := within(ctx, c, "thread/resume", params, &out); err != nil {
		return err
	}
	if out.Thread.ID != id || out.Thread.Parent != "" || out.Thread.Status.Type == "notLoaded" || out.Thread.Status.Type == "" {
		return fmt.Errorf("the daemon did not load held thread %s as a session", id)
	}
	l.seen(out.Thread, time.Now())
	l.set(id, func(t *thread) { t.subscribed, t.settings = true, out.read() })
	l.save(id)
	return nil
}

// heldDir keeps a mark per thread the panel holds, by the contour of its
// home: the threads outlive an executor, and the next executor needs their
// launch settings because the daemon does not persist sandbox overrides.
func heldDir(contour string) string {
	return filepath.Join(stream.Dir(), "codex-held", contour)
}

func heldPath(contour, id string) string { return filepath.Join(heldDir(contour), id) }

func heldMarks(contour string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(heldDir(contour))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasSuffix(e.Name(), ".tmp") {
			out[e.Name()] = true
		}
	}
	return out
}

// hold keeps only launch configuration in the mark: no message, name or
// opening record. Those have their own lifetime and never go into a resume.
func (l *Link) hold(id string, params map[string]any) {
	l.mu.Lock()
	l.held[id] = true
	delete(l.recovering, id)
	l.mu.Unlock()
	var raw []byte
	var err error
	if params != nil {
		raw, err = json.Marshal(params)
	}
	if err == nil {
		err = write(heldPath(l.contour, id), raw)
	}
	if err != nil {
		log.Printf("codex %s: thread %s is held, and its mark was not written: %v", l.home, id, err)
	}
}

// keepHeldTools replaces the panel server's overrides after a reconnect, so
// a daemon restart cannot restore tools that an update removed or omit new
// approvals. A mark without launch settings stays without them.
func (l *Link) keepHeldTools(id, tools string, allowed []string) error {
	path := heldPath(l.contour, id)
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return err
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	if len(saved) == 0 {
		return nil
	}
	config := map[string]json.RawMessage{}
	if raw := saved["config"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return err
		}
	}
	if config == nil {
		config = map[string]json.RawMessage{}
	}
	for key := range config {
		if strings.HasPrefix(key, "mcp_servers."+mcp.ServerName+".") {
			delete(config, key)
		}
	}
	if tools != "" {
		for key, value := range toolsConfig(tools, allowed) {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			config[key] = raw
		}
	}
	if saved["config"], err = json.Marshal(config); err != nil {
		return err
	}
	raw, err = json.Marshal(saved)
	if err != nil {
		return err
	}
	return write(path, raw)
}

func (l *Link) unhold(id string) {
	l.mu.Lock()
	delete(l.held, id)
	delete(l.recovering, id)
	l.mu.Unlock()
	_ = os.Remove(heldPath(l.contour, id))
}
