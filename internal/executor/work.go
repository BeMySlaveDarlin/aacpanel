package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

const (
	workOpen     = "/tasks "
	workTitle    = "Background"
	workSelect   = "to select"
	workClose    = "Esc to close"
	workBack     = "to go back"
	workStopKey  = "x"
	workDownKey  = "\x1b[B"
	workUpKey    = "\x1b[A"
	workBackKey  = "\x1b[D"
	workStepWait = 120 * time.Millisecond
	workSteps    = 60
)

var workStatusRe = regexp.MustCompile(`\s+\([A-Za-z][A-Za-z ]*\)$`)

// A local agent — one sent off to work without a name — is its description
// between a mark of its state and the state with the model:
// "● probe poem   running · Haiku 4.5".
var workAgentRe = regexp.MustCompile(`^[^\p{L}\p{N}\s]\s+(.+?)\s{2,}\p{Ll}[\p{Ll} ]*(?:\s+·\s+.*)?$`)

const workCursors = "❯›>"

type workRow struct {
	at     int
	cursor bool
	name   string
	cut    bool
}

// taskStop stops a background task: a command, a monitor or an agent sent off
// to work. On the stream claude stops it by its id; in a console the panel
// finds its line on the screen of background work and presses the key there.
func (e *Executor) taskStop(ctx context.Context, target string, w *action.Work) (string, error) {
	if w == nil {
		return "", fmt.Errorf("it is not said which task to stop")
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if onStream(s) {
		return streamStopTask(ctx, s, w)
	}
	if w.Line == "" {
		return "", fmt.Errorf("the panel does not know how this task is named on the session screen — " +
			"there would be nothing to check the keypress against")
	}
	if err := e.stopBackgroundWork(ctx, s, w.Line); err != nil {
		return "", err
	}
	return fmt.Sprintf("background task %q (%s) stopped in %s", w.Line, w.ID, target), nil
}

func (e *Executor) agentStop(ctx context.Context, target string, w *action.Work) (string, error) {
	if w == nil {
		return "", fmt.Errorf("it is not said which agent to stop")
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if err := e.stopBackgroundWork(ctx, s, "@"+w.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("subagent %s stopped in %s", w.ID, target), nil
}

// streamStopTask asks claude to stop a task by the id it knows it by. Claude
// answers success for a task that has already ended as well, so the stop
// counts once the task has left the list of the running ones.
func streamStopTask(ctx context.Context, s liveSession, w *action.Work) (string, error) {
	name := w.ID
	if w.Line != "" {
		name = fmt.Sprintf("%q (%s)", w.Line, w.ID)
	}
	st, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	if !runningTask(st, w.ID) {
		return "", fmt.Errorf("background task %s is no longer running in %s — most likely it finished by itself: "+
			"the list in the panel lags behind", name, s.Name)
	}
	stop := stream.Request{Op: stream.OpControl, Subtype: "stop_task", Fields: map[string]any{"task_id": w.ID}}
	if _, err := streamAsk(ctx, s, stop); err != nil {
		return "", err
	}
	deadline := time.Now().Add(workOpenWait)
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("the stop was sent, but there was no time to see background task %s end", name)
		case <-time.After(workStepWait):
		}
		st, err := streamState(ctx, s)
		if err != nil {
			return "", fmt.Errorf("the stop was sent, but whether background task %s ended is unknown: %w", name, err)
		}
		if !runningTask(st, w.ID) {
			return fmt.Sprintf("background task %s stopped in %s", name, s.Name), nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("claude took the stop, but background task %s is still running in %s after %s",
				name, s.Name, workOpenWait)
		}
	}
}

func runningTask(st stream.State, id string) bool {
	for _, t := range st.Tasks {
		if t.ID == id {
			return true
		}
	}
	return false
}

func (e *Executor) stopBackgroundWork(ctx context.Context, s liveSession, want string) error {
	if s.Status == "busy" {
		return fmt.Errorf(
			"session %s is busy right now: the list of background work is opened by a slash command, and "+
				"with a busy session that command queues up and runs only after its turn. Wait until the "+
				"session is free", s.Name)
	}
	t, err := termFor(ctx, s.PID)
	if err != nil {
		return fmt.Errorf("the background work of session %s cannot be stopped from the panel: %w", s.Name, err)
	}

	if screen, known := t.screen(ctx); known && dialogOnScreen(screen) {
		return fmt.Errorf(
			"session %s is holding a dialog on screen — answer it first, otherwise the list of "+
				"background work will not open", s.Name)
	}

	if err := openWorkScreen(ctx, t); err != nil {
		return fmt.Errorf("the list of background work of session %s did not open: %w", s.Name, err)
	}
	defer closeWorkScreen(context.WithoutCancel(ctx), t)

	return aimAndStop(ctx, t, want)
}

func openWorkScreen(ctx context.Context, t term) error {
	if err := t.send(ctx, clearLine); err != nil {
		return err
	}
	if err := t.send(ctx, pasteStart+workOpen+pasteEnd); err != nil {
		return err
	}
	if err := t.send(ctx, enterKey); err != nil {
		return err
	}
	deadline := time.Now().Add(workOpenWait)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("there is nothing left to wait for the screen with: the request was cancelled")
		case <-time.After(workStepWait):
		}
		screen, known := t.screen(ctx)
		if !known {
			return fmt.Errorf("the %s screen cannot be read, and stopping work blindly is not allowed", t.kind())
		}
		if workScreenOpen(screen) {
			return nil
		}
		// With one piece of work running the list opens straight on its
		// card; the way back from the card is the list.
		if workCardOpen(screen) {
			if err := t.send(ctx, workBackKey); err != nil {
				return err
			}
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"it did not appear within %s: the session took a turn of its own and the command queued up. "+
					"The list will open there by itself once the session is free — close it with Esc", workOpenWait)
		}
	}
}

func closeWorkScreen(ctx context.Context, t term) {
	screen, known := t.screen(ctx)
	if !known || !(workScreenOpen(screen) || workCardOpen(screen)) {
		return
	}
	_ = t.send(ctx, escKey)
}

// workCardOpen reports the card of one piece of work, which the screen of
// background work opens instead of the list when there is one to show.
func workCardOpen(screen string) bool {
	for _, line := range strings.Split(screen, "\n") {
		if trimmed := strings.TrimSpace(line); strings.Contains(trimmed, workBack) && strings.Contains(trimmed, "to close") {
			return true
		}
	}
	return false
}

func workScreenOpen(screen string) bool {
	title, footer := false, false
	for _, line := range strings.Split(screen, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == workTitle {
			title = true
		}
		if strings.Contains(trimmed, workSelect) && strings.Contains(trimmed, workClose) {
			footer = true
		}
	}
	return title && footer
}

func workRows(screen string) []workRow {
	lines := strings.Split(screen, "\n")
	top, bottom := -1, -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == workTitle {
			top = i
		}
		if top >= 0 && i > top &&
			strings.Contains(trimmed, workSelect) && strings.Contains(trimmed, workClose) {
			bottom = i
			break
		}
	}
	if top < 0 || bottom < 0 {
		return nil
	}

	var rows []workRow
	for i := top + 1; i < bottom; i++ {
		row, ok := parseWorkRow(lines[i])
		if !ok {
			continue
		}
		row.at = i
		rows = append(rows, row)
	}
	return rows
}

func parseWorkRow(line string) (workRow, bool) {
	body := strings.TrimRight(line, " \t")
	cursor := false
	for _, r := range workCursors {
		at := strings.IndexRune(body, r)
		if at < 0 || strings.TrimSpace(body[:at]) != "" {
			continue
		}
		cursor = true
		body = body[at+len(string(r)):]
		break
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return workRow{}, false
	}

	if strings.HasPrefix(body, "@") {
		name := body
		if at := strings.IndexByte(name, ':'); at > 0 {
			name = name[:at]
		}
		return workRow{cursor: cursor, name: strings.TrimSpace(name)}, true
	}

	var name string
	if found := workStatusRe.FindStringIndex(body); found != nil {
		name = strings.TrimSpace(body[:found[0]])
	} else if agent := workAgentRe.FindStringSubmatch(body); agent != nil {
		name = strings.TrimSpace(agent[1])
	} else {
		return workRow{}, false
	}
	cut := false
	if trimmed := strings.TrimRight(name, "…"); trimmed != name {
		name = strings.TrimSpace(trimmed)
		cut = true
	}
	if name == "" {
		return workRow{}, false
	}
	return workRow{cursor: cursor, name: name, cut: cut}, true
}

func workRowIs(row workRow, want string) bool {
	got, need := squeeze(row.name), squeeze(want)
	if got == "" || need == "" {
		return false
	}
	if row.cut {
		return strings.HasPrefix(need, got)
	}
	return got == need
}

func aimAndStop(ctx context.Context, t term, want string) error {
	for step := 0; ; step++ {
		screen, known := t.screen(ctx)
		if !known {
			return fmt.Errorf("the %s screen cannot be read, and stopping work blindly is not allowed", t.kind())
		}
		rows := workRows(screen)
		aim, cursor := -1, -1
		hits := 0
		for i, row := range rows {
			if row.cursor {
				cursor = i
			}
			if workRowIs(row, want) {
				hits++
				aim = i
			}
		}
		switch {
		case hits == 0:
			return fmt.Errorf(
				"this work is no longer on the session screen — the list now holds %d %s. Most likely it "+
					"finished by itself: the list in the panel lags behind", len(rows), workPlural(len(rows)))
		case hits > 1:
			return fmt.Errorf(
				"the session screen shows %d identical lines — which one to stop cannot be told "+
					"by the name", hits)
		case cursor < 0:
			return fmt.Errorf("no cursor is visible on the session screen — it is unclear what the keypress would stop")
		case cursor == aim:
			return stopAimed(ctx, t, want)
		case step >= workSteps:
			return fmt.Errorf("the cursor did not reach the wanted line in %d steps", workSteps)
		}

		key := workDownKey
		if rows[aim].at < rows[cursor].at {
			key = workUpKey
		}
		if err := t.send(ctx, key); err != nil {
			return fmt.Errorf("the cursor step did not go through: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopping was interrupted: the cursor stayed on the list, nothing was pressed")
		case <-time.After(workStepWait):
		}
	}
}

func stopAimed(ctx context.Context, t term, want string) error {
	if err := t.send(ctx, workStopKey); err != nil {
		return fmt.Errorf("the keypress did not go through: %w", err)
	}
	deadline := time.Now().Add(workOpenWait)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("the keypress was sent, but there was no time to confirm the stop — look at the list in the session")
		case <-time.After(workStepWait):
		}
		screen, known := t.screen(ctx)
		if !known {
			return fmt.Errorf("the keypress was sent, but the %s screen stopped being readable — whether the work stopped is unknown", t.kind())
		}
		gone := true
		for _, row := range workRows(screen) {
			if workRowIs(row, want) {
				gone = false
				break
			}
		}
		if gone {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the keypress was sent, but the line stayed on screen for %s — the work was not stopped", workOpenWait)
		}
	}
}

func workPlural(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}
