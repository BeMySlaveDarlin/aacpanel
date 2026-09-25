package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aacpanel/internal/stream"
)

// Remote Control is claude's bridge to claude.ai: while it is up the session
// is reachable from the Claude app and the web. Claude writes the id of the
// bridge into the file of the session and takes it out when the bridge goes,
// so the file is where a switch in a terminal is known to have landed.
//
// On the stream the switch is one request, and its answer is the proof. In a
// terminal it is /remote-control: typed on a session without the bridge it
// brings the bridge up at once; typed on one with it, it opens a dialog, and
// the line that disconnects is aimed at by reading the screen after every
// key, the way a row of the background list is.

const (
	remoteCommand    = "/remote-control"
	remoteTitle      = "Remote Control"
	remoteDisconnect = "Disconnect this session"
	remoteFooter     = "Enter to select"
	remoteSteps      = 6
	remoteURL        = "https://claude.ai/code/"
)

var (
	// How long claude may take to bring the bridge up or down: it registers
	// the session with claude.ai first.
	remoteWait = 20 * time.Second
	// How often the screen and the file of the session are read meanwhile.
	remoteStepWait = 150 * time.Millisecond
)

func (e *Executor) sessionRemote(ctx context.Context, target string, on bool) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if (s.Bridge != "") == on {
		return remoteSaid(s.Name, on, s.Bridge, true), nil
	}
	if onStream(s) {
		reply, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "remote_control",
			Fields: map[string]any{"enabled": on}})
		if err != nil {
			return "", err
		}
		return remoteSaid(s.Name, on, bridgeOfReply(reply.Response), false), nil
	}
	t, err := termFor(ctx, s.PID)
	if err != nil {
		return "", fmt.Errorf("remote control of session %s cannot be switched from the panel: %w", s.Name, err)
	}
	bridge, err := switchRemote(ctx, t, on, func() string { return bridgeNow(s) })
	if err != nil {
		return "", fmt.Errorf("session %s: %w", s.Name, err)
	}
	return remoteSaid(s.Name, on, bridge, false), nil
}

// switchRemote types /remote-control into a terminal and waits for the file
// of the session to say the bridge is up or gone; it returns the bridge.
func switchRemote(ctx context.Context, t term, on bool, bridge func() string) (string, error) {
	screen, known := t.screen(ctx)
	if !known {
		return "", fmt.Errorf("the %s screen cannot be read, and typing into it blindly is not allowed", t.kind())
	}
	if dialogOnScreen(screen) {
		return "", fmt.Errorf("a dialog stands on the screen — answer it first, otherwise %s would land in it", remoteCommand)
	}
	for _, key := range []string{clearLine, pasteStart + remoteCommand + pasteEnd, enterKey} {
		if err := t.send(ctx, key); err != nil {
			return "", err
		}
	}
	if !on {
		if err := disconnectRemote(ctx, t); err != nil {
			return "", err
		}
	}
	deadline := time.Now().Add(remoteWait)
	for {
		now := bridge()
		if (now != "") == on {
			return now, nil
		}
		if time.Now().After(deadline) {
			if on {
				return "", fmt.Errorf("remote control did not come up within %s: claude says why on its own screen", remoteWait)
			}
			return "", fmt.Errorf("remote control did not go within %s", remoteWait)
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("there is nothing left to wait for the bridge with: the request was cancelled")
		case <-time.After(remoteStepWait):
		}
	}
}

// disconnectRemote waits for the dialog /remote-control opens on a session
// with the bridge and picks the line that disconnects it. The cursor stands
// on the line that keeps the bridge, and Esc keeps it too: a dialog the aim
// cannot be taken in is closed with Esc, and the bridge stays.
func disconnectRemote(ctx context.Context, t term) error {
	deadline := time.Now().Add(remoteWait)
	for step := 0; ; {
		select {
		case <-ctx.Done():
			return fmt.Errorf("there is nothing left to wait for the dialog with: the request was cancelled")
		case <-time.After(remoteStepWait):
		}
		screen, known := t.screen(ctx)
		if !known {
			return fmt.Errorf("the %s screen cannot be read, and picking a line blindly is not allowed", t.kind())
		}
		rows, open := remoteRows(screen)
		if !open {
			if time.Now().After(deadline) {
				return fmt.Errorf("the dialog of %s did not appear within %s", remoteCommand, remoteWait)
			}
			continue
		}
		aim, cursor := -1, -1
		for i, row := range rows {
			if row.cursor {
				cursor = i
			}
			if strings.HasPrefix(row.name, remoteDisconnect) {
				aim = i
			}
		}
		switch {
		case aim < 0 || cursor < 0:
			_ = t.send(ctx, escKey)
			return fmt.Errorf("the dialog of %s shows no line to disconnect with — it is closed, the bridge stays",
				remoteCommand)
		case aim == cursor:
			return t.send(ctx, enterKey)
		case step >= remoteSteps:
			_ = t.send(ctx, escKey)
			return fmt.Errorf("the cursor did not reach %q in %d steps — the dialog is closed, the bridge stays",
				remoteDisconnect, remoteSteps)
		}
		key := workDownKey
		if aim < cursor {
			key = workUpKey
		}
		if err := t.send(ctx, key); err != nil {
			return err
		}
		step++
	}
}

// remoteRows returns the lines of the Remote Control dialog, from its title
// down to the line of keys under its choices, and whether it is on the screen.
func remoteRows(screen string) ([]workRow, bool) {
	lines := strings.Split(screen, "\n")
	footer := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], remoteFooter) {
			footer = i
			break
		}
	}
	top := -1
	for i := footer - 1; i >= 0 && footer > 0; i-- {
		if strings.TrimSpace(lines[i]) == remoteTitle {
			top = i
			break
		}
	}
	if top < 0 {
		return nil, false
	}
	var rows []workRow
	for i := top + 1; i < footer; i++ {
		body := strings.TrimSpace(lines[i])
		cursor := false
		for _, r := range workCursors {
			if strings.HasPrefix(body, string(r)) {
				cursor = true
				body = strings.TrimSpace(strings.TrimPrefix(body, string(r)))
				break
			}
		}
		if body != "" {
			rows = append(rows, workRow{at: i, cursor: cursor, name: body})
		}
	}
	return rows, true
}

// bridgeNow reads the bridge of a session from its file as it stands now.
func bridgeNow(s liveSession) string {
	found, _, err := liveSessionsByName(s.Name)
	if err != nil {
		return ""
	}
	for _, f := range found {
		if f.PID == s.PID {
			return f.Bridge
		}
	}
	return ""
}

// bridgeOfReply reads the bridge out of claude's answer to the request that
// brings it up: the address of the session on claude.ai.
func bridgeOfReply(resp json.RawMessage) string {
	var body struct {
		Response struct {
			URL string `json:"session_url"`
		} `json:"response"`
	}
	if json.Unmarshal(resp, &body) != nil {
		return ""
	}
	return strings.TrimPrefix(body.Response.URL, remoteURL)
}

func remoteSaid(name string, on bool, bridge string, already bool) string {
	was := ""
	if already {
		was = "already "
	}
	if !on {
		return fmt.Sprintf("remote control of session %s is %soff", name, was)
	}
	if bridge == "" {
		return fmt.Sprintf("remote control of session %s is %son", name, was)
	}
	return fmt.Sprintf("remote control of session %s is %son: %s%s", name, was, remoteURL, bridge)
}
