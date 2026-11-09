package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/observer"
	"aacpanel/internal/stream"
)

var (
	freezeWait   = 3 * time.Second
	writeFreeze  = writeControl
	writeReclaim = writeControl
)

// writeControl writes a cgroup control file with one write(2) of its own.
// Go takes a cgroup file for pollable, and when the kernel answers EAGAIN —
// memory.reclaim that freed only part of the ask — it would wait for the file
// to become writable, which never comes: the executor would hang there.
func writeControl(path string, raw []byte, _ os.FileMode) error {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer syscall.Close(fd)
	for {
		_, err = syscall.Write(fd, raw)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return &os.PathError{Op: "write", Path: path, Err: err}
	}
	return nil
}

// A terminal's unit can hold every session of its tmux server. Only the
// holder of a stream session has a unit that can be frozen as one session.
func streamGroup(s liveSession) (string, error) {
	held, ok := stream.Held(s.SessionID, s.PID)
	if !ok {
		return "", fmt.Errorf("session %s lives in tmux: its shared unit cannot be frozen", s.Name)
	}
	raw, err := os.ReadFile(filepath.Join(procDir(), strconv.Itoa(held.Holder), "cgroup"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if group, ok := strings.CutPrefix(line, "0::"); ok && filepath.IsAbs(group) {
			return filepath.Join(cgroupRoot, group), nil
		}
	}
	return "", fmt.Errorf("the holder of session %s has no cgroup v2 unit", s.Name)
}

func frozenGroup(dir string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "cgroup.events"))
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "frozen" && (fields[1] == "0" || fields[1] == "1") {
			return fields[1] == "1", nil
		}
	}
	return false, fmt.Errorf("%s has no frozen state", dir)
}

func streamFrozen(s liveSession) bool {
	dir, err := streamGroup(s)
	if err != nil {
		return false
	}
	// Cgroup visibility must not become a prerequisite for the stream. Only
	// a confirmed freeze justifies refusing a request before its socket call.
	frozen, err := frozenGroup(dir)
	return err == nil && frozen
}

func setFrozen(ctx context.Context, dir string, frozen bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value := "0"
	if frozen {
		value = "1"
	}
	// The launcher has ended, so systemd can call its unit inactive while
	// the holder and Claude still live in its cgroup. The kernel freezes them.
	return writeFreeze(filepath.Join(dir, "cgroup.freeze"), []byte(value), 0o600)
}

func waitFrozen(ctx context.Context, dir string, want bool) error {
	ctx, cancel := context.WithTimeout(ctx, freezeWait)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		frozen, err := frozenGroup(dir)
		if err != nil {
			return err
		}
		if frozen == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s did not reach frozen %t: %w", filepath.Base(dir), want, ctx.Err())
		case <-tick.C:
		}
	}
}

// EAGAIN means the kernel reclaimed only part of the ask: pinned pages and
// the unit's swap ceiling can keep some of a sleeping session resident.
func reclaimGroup(dir string) error {
	current, err := readCgroupNumber(filepath.Join(dir, "memory.current"))
	if err != nil {
		return err
	}
	err = writeReclaim(filepath.Join(dir, "memory.reclaim"), []byte(strconv.FormatUint(current, 10)), 0o600)
	if errors.Is(err, syscall.EAGAIN) {
		return nil
	}
	return err
}

func (e *Executor) sessionSleep(ctx context.Context, target, conversation string, sleeping bool) (detail string, err error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if conversation == "" || s.SessionID != conversation {
		return "", fmt.Errorf("session %s runs conversation %s, not %s: sleep changes only the conversation it names", target, s.SessionID, conversation)
	}
	dir, err := streamGroup(s)
	if err != nil {
		return "", err
	}
	unit := filepath.Base(dir)
	if filepath.Base(filepath.Dir(dir)) != AgentsSlice || !strings.HasPrefix(unit, "run-") || !strings.HasSuffix(unit, ".service") {
		return "", fmt.Errorf("session %s has no dedicated stream unit in %s", target, AgentsSlice)
	}
	start, ok := procStart(s.PID)
	if !ok {
		return "", fmt.Errorf("the process of session %s is gone", target)
	}
	owner := observer.Owner{Session: target, Conversation: conversation, PID: s.PID, Start: start}
	if !sleeping {
		if err := setFrozen(ctx, dir, false); err != nil {
			return "", err
		}
		if err := waitFrozen(ctx, dir, false); err != nil {
			return "", err
		}
		if err := e.Observers().SetAlarmsSleeping(owner, false); err != nil {
			return "", err
		}
		return fmt.Sprintf("session %s is awake", target), nil
	}
	// A failed mark can have written some alarms already. Roll back even
	// then, and use a fresh deadline so a cancelled caller cannot strand it.
	defer func() {
		if err != nil {
			rollback := context.WithoutCancel(ctx)
			err = errors.Join(err, setFrozen(rollback, dir, false), waitFrozen(rollback, dir, false), e.Observers().SetAlarmsSleeping(owner, false))
		}
	}()
	if err = e.Observers().SetAlarmsSleeping(owner, true); err != nil {
		return "", err
	}
	if err = setFrozen(ctx, dir, true); err != nil {
		return "", err
	}
	// The namespace can refuse reclaim while freezing still works. The
	// process stays visible, and pressure can reclaim its cold pages later.
	reclaimErr := reclaimGroup(dir)
	if err = waitFrozen(ctx, dir, true); err != nil {
		return "", err
	}
	detail = fmt.Sprintf("session %s sleeps in %s", target, unit)
	if reclaimErr != nil {
		var pathErr *os.PathError
		if errors.As(reclaimErr, &pathErr) {
			reclaimErr = pathErr.Err
		}
		detail += fmt.Sprintf("; memory left to the kernel: %v", reclaimErr)
	}
	return detail, nil
}
