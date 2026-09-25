package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"aacpanel/internal/action"
)

const (
	workOpen     = "/tasks "
	workTitle    = "Background"
	workSelect   = "to select"
	workClose    = "Esc to close"
	workStopKey  = "x"
	workDownKey  = "\x1b[B"
	workUpKey    = "\x1b[A"
	workStepWait = 120 * time.Millisecond
	workSteps    = 60
)

var workStatusRe = regexp.MustCompile(`\s+\([A-Za-z][A-Za-z ]*\)$`)

const workCursors = "❯›>"

type workRow struct {
	at     int
	cursor bool
	name   string
	cut    bool
}

func (e *Executor) taskStop(ctx context.Context, target string, w *action.Work) (string, error) {
	if w == nil {
		return "", fmt.Errorf("it is not said which task to stop")
	}
	if err := e.stopBackgroundWork(ctx, target, w.Line); err != nil {
		return "", err
	}
	return fmt.Sprintf("background task %q (%s) stopped in %s", w.Line, w.ID, target), nil
}

func (e *Executor) agentStop(ctx context.Context, target string, w *action.Work) (string, error) {
	if w == nil {
		return "", fmt.Errorf("it is not said which agent to stop")
	}
	if err := e.stopBackgroundWork(ctx, target, "@"+w.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("subagent %s stopped in %s", w.ID, target), nil
}

func (e *Executor) stopBackgroundWork(ctx context.Context, target, want string) error {
	s, err := findOneLiveSession(target)
	if err != nil {
		return err
	}
	if s.Status == "busy" {
		return fmt.Errorf(
			"session %s is busy right now: the list of background work is opened by a slash command, and "+
				"with a busy session that command queues up and runs only after its turn. Wait until the "+
				"session is free", target)
	}
	t, err := termFor(ctx, s.PID)
	if err != nil {
		return fmt.Errorf("the background work of session %s cannot be stopped from the panel: %w", target, err)
	}

	if screen, known := t.screen(ctx); known && dialogOnScreen(screen) {
		return fmt.Errorf(
			"session %s is holding a dialog on screen — answer it first, otherwise the list of "+
				"background work will not open", target)
	}

	if err := openWorkScreen(ctx, t); err != nil {
		return fmt.Errorf("the list of background work of session %s did not open: %w", target, err)
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
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"it did not appear within %s: the session took a turn of its own and the command queued up. "+
					"The list will open there by itself once the session is free — close it with Esc", workOpenWait)
		}
	}
}

func closeWorkScreen(ctx context.Context, t term) {
	screen, known := t.screen(ctx)
	if !known || !workScreenOpen(screen) {
		return
	}
	_ = t.send(ctx, escKey)
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

	found := workStatusRe.FindStringIndex(body)
	if found == nil {
		return workRow{}, false
	}
	name := strings.TrimSpace(body[:found[0]])
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
