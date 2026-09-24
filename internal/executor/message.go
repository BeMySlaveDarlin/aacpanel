package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	senderName  = "aacpanel-panel"
	sendTimeout = 5 * time.Second
)

type liveSession struct {
	PID       int
	Name      string
	Socket    string
	Status    string
	CWD       string
	SessionID string
}

func (e *Executor) sessionSend(ctx context.Context, target, text string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	return deliverText(ctx, s, senderName, text)
}

func deliverText(ctx context.Context, s liveSession, from, text string) (string, error) {
	if onStream(s) {
		return streamSend(ctx, s, text)
	}
	where := sessionWhere(s)

	t, kerr := termFor(ctx, s.PID)
	if kerr == nil {
		confirmed, err := pasteAndSend(ctx, t, text, watchTranscript(s))
		if err != nil {
			return "", fmt.Errorf("session %s did not accept the input: %w", s.Name, err)
		}
		detail := fmt.Sprintf("typed into %s (%s), %d characters", s.Name, where, len([]rune(text)))
		if t.attempts() > 1 {
			detail += "; " + t.kind() + " answered on the second attempt"
		}
		if !confirmed {
			detail += "; there is nothing to confirm the send with — the " + t.kind() + " screen cannot be read"
		}
		return detail, nil
	}

	if s.Socket == "" {
		return "", fmt.Errorf(
			"session %s accepts no messages: %v, and its claude publishes no message socket", s.Name, kerr)
	}
	detail, err := deliverLetter(ctx, s, from, "", text)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s — %v, so it was not typed but sent", detail, kerr), nil
}

func deliverLetter(ctx context.Context, s liveSession, from, note, text string) (string, error) {
	if s.Socket == "" {
		return "", fmt.Errorf("session %s accepts no letters: its claude publishes no message socket", s.Name)
	}
	body := text
	if note != "" {
		body = note + "\n" + text
	}
	if err := deliver(ctx, s.Socket, from, body); err != nil {
		return "", fmt.Errorf("session %s did not accept the message: %w", s.Name, err)
	}
	return fmt.Sprintf("delivered as a letter to %s (%s), %d characters",
		s.Name, sessionWhere(s), len([]rune(text))), nil
}

func sessionWhere(s liveSession) string {
	if s.Status == "busy" {
		return "busy — the message queued up"
	}
	return "free — it will read it right away"
}

func findOneLiveSession(target string) (liveSession, error) {
	found, known, err := liveSessionsByName(target)
	if err != nil {
		return liveSession{}, err
	}
	switch {
	case len(found) == 0:
		if len(known) == 0 {
			return liveSession{}, fmt.Errorf("there is no live session %s: there are no live sessions at all right now", target)
		}
		return liveSession{}, fmt.Errorf("there is no live session %s; live ones are: %s", target, strings.Join(known, ", "))
	case len(found) > 1:
		return liveSession{}, fmt.Errorf("there are two sessions named %s right now — it is unclear which one to take", target)
	}
	return found[0], nil
}

func (e *Executor) sessionStop(ctx context.Context, target string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if onStream(s) {
		return streamInterrupt(ctx, s)
	}
	t, kerr := termFor(ctx, s.PID)
	if kerr != nil {
		return "", fmt.Errorf(
			"session %s accepts no stop: %v — Esc is a key, and a letter cannot carry it", target, kerr)
	}
	if err := t.send(ctx, escKey); err != nil {
		return "", fmt.Errorf("session %s did not accept the input: %w", target, err)
	}
	if s.Status == "busy" {
		return fmt.Sprintf("%s stopped: the answer was interrupted, the queue went back to the composer", s.Name), nil
	}
	return fmt.Sprintf("%s: the queue was cleared, there was nothing to interrupt", s.Name), nil
}

// sessionEscape gives the composer back. A session showing a screen of its own
// — a dialog it drew, a list it opened — holds the keyboard, and a message
// typed into that answers a question the person at the panel never saw.
//
// The screen is read before the key and again after it. Esc means a different
// thing to every screen, so a composer that did not come back is said so
// plainly, together with what stands on the screen now. Nothing is pressed
// while the composer is free: the same key in an ordinary conversation
// interrupts the answer being written, and a button that quietly does that is
// worse than one that does nothing.
func (e *Executor) sessionEscape(ctx context.Context, target string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if onStream(s) {
		return streamEscape(ctx, s)
	}
	t, kerr := termFor(ctx, s.PID)
	if kerr != nil {
		return "", fmt.Errorf(
			"session %s takes no Esc: %v — Esc is a key, and a letter cannot carry it", target, kerr)
	}
	screen, seen := t.screen(ctx)
	if !seen {
		return "", fmt.Errorf(
			"the screen of session %s cannot be read, and Esc would go in blind: in an ordinary "+
				"conversation it interrupts the answer instead of closing a dialog", s.Name)
	}
	if _, ready := composerReady(screen); ready {
		return fmt.Sprintf("%s: nothing was pressed, its composer is free as it is", s.Name), nil
	}
	if err := t.send(ctx, escKey); err != nil {
		return "", fmt.Errorf("session %s did not accept the input: %w", target, err)
	}
	busy, back := composerFree(ctx, t)
	switch {
	case !back:
		return "", fmt.Errorf(
			"Esc went into session %s, and its screen can no longer be read: whether the composer "+
				"came back is unknown", s.Name)
	case busy != "":
		return "", fmt.Errorf("Esc went into session %s and its composer did not come back: %s", s.Name, busy)
	}
	return fmt.Sprintf("Esc pressed in %s: its composer is free", s.Name), nil
}

func deliver(ctx context.Context, socket, from, text string) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()

	line, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": text},
		"from":    from,
	})
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	_, err = conn.Write(append(line, '\n'))
	return err
}

func liveSessionsByName(target string) (found []liveSession, known []string, err error) {
	all, err := allLiveSessions()
	if err != nil {
		return nil, nil, err
	}
	for _, s := range all {
		known = append(known, s.Name)
		if s.Name == target {
			found = append(found, s)
		}
	}
	return found, known, nil
}

func allLiveSessions() ([]liveSession, error) {
	var out []liveSession
	var lastErr error
	seen := 0
	for _, dir := range sessionsDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			lastErr = err
			continue
		}
		seen++
		out = append(out, sessionsFromDir(dir, entries)...)
	}
	if seen == 0 && lastErr != nil {
		return nil, fmt.Errorf("the list of live sessions was not read: %w", lastErr)
	}
	return out, nil
}

func sessionsFromDir(dir string, entries []os.DirEntry) []liveSession {
	var out []liveSession
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue
		}
		if s, ok := readSessionFile(dir, pid); ok {
			out = append(out, s)
		}
	}
	return out
}

func readSessionFile(dir string, pid int) (liveSession, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(pid)+".json"))
	if err != nil {
		return liveSession{}, false
	}
	var file struct {
		PID       int    `json:"pid"`
		Name      string `json:"name"`
		SessionID string `json:"sessionId"`
		CWD       string `json:"cwd"`
		Socket    string `json:"messagingSocketPath"`
		Status    string `json:"status"`
		Kind      string `json:"kind"`
		ProcStart string `json:"procStart"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return liveSession{}, false
	}
	if file.PID != pid || file.Name == "" {
		return liveSession{}, false
	}
	if !consoleKind(file.Kind) {
		return liveSession{}, false
	}
	start, ok := procStart(pid)
	if !ok || file.ProcStart == "" || file.ProcStart != start {
		return liveSession{}, false
	}
	return liveSession{
		PID: pid, Name: file.Name, Socket: file.Socket,
		Status: file.Status, CWD: file.CWD, SessionID: file.SessionID,
	}, true
}
