package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"aacpanel/internal/action"
	"aacpanel/internal/request"
)

func (e *Executor) termPeek(ctx context.Context, id, logPath string) (string, error) {
	if !action.TermID(id) {
		return "", fmt.Errorf("invalid terminal id")
	}
	target := "=" + id + ":"
	status, err := panelTmux.run(ctx, "display-message", "-p", "-t", target, "#{pane_dead} #{pane_dead_status}")
	if err != nil {
		if _, lookup := findPanelTerm(ctx, id); errors.Is(lookup, action.ErrTermNotFound) {
			return "", lookup
		}
		return "", err
	}
	fields := strings.Fields(status)
	if len(fields) == 0 || (fields[0] != "0" && fields[0] != "1") {
		return "", fmt.Errorf("invalid terminal status %q", status)
	}
	peek := action.TermPeekResult{Dead: fields[0] == "1"}
	if peek.Dead {
		if len(fields) != 2 {
			return "", fmt.Errorf("the dead terminal has no exit status")
		}
		peek.Status, err = strconv.Atoi(fields[1])
		if err != nil {
			return "", err
		}
	}
	capture, err := panelTmux.run(ctx, "capture-pane", "-p", "-J", "-S", "-", "-t", target)
	if err != nil {
		return "", err
	}
	capture = strings.TrimRight(capture, "\n")
	peek.Tail, peek.Lines = request.Tail(capture), len(strings.Split(capture, "\n"))
	if logPath != "" {
		if err := saveActLog(logPath, capture+"\n"); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(peek)
	return string(data), err
}

// Only act logs may be written through the capture action. O_NOFOLLOW keeps
// a replaced file from turning the executor into a writer elsewhere on the host.
func saveActLog(path, capture string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := filepath.Join(home, ".local", "state", "aacpanel", "acts")
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 2 || !request.ValidID(parts[0]) || !strings.HasSuffix(parts[1], ".log") {
		return fmt.Errorf("the capture log is not an act log")
	}
	step, err := strconv.Atoi(strings.TrimSuffix(parts[1], ".log"))
	if err != nil || step < 1 || step > 10 || parts[1] != strconv.Itoa(step)+".log" {
		return fmt.Errorf("the capture log needs a step number")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if real != dir {
		return fmt.Errorf("the capture log directory follows a symlink")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := int64(len(capture))
	if info.Size() < size {
		size = info.Size()
	}
	previous := make([]byte, int(size))
	if _, err := f.ReadAt(previous, info.Size()-size); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	// tmux can drop the head of its scrollback. Keep earlier captures and append
	// only the suffix beyond their overlap, rather than truncating the full log.
	prefix := make([]int, len(capture))
	for i, j := 1, 0; i < len(capture); i++ {
		for j > 0 && capture[i] != capture[j] {
			j = prefix[j-1]
		}
		if capture[i] == capture[j] {
			j++
		}
		prefix[i] = j
	}
	overlap := 0
	for _, c := range previous {
		for overlap > 0 && (overlap == len(capture) || c != capture[overlap]) {
			overlap = prefix[overlap-1]
		}
		if overlap < len(capture) && c == capture[overlap] {
			overlap++
		}
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	_, err = f.WriteString(capture[overlap:])
	return err
}
