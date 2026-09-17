package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	tmuxEnv     = "AACP_TMUX"
	defaultTmux = "tmux"
	paneFormat  = "#{pane_pid} #{session_name}:#{window_index}.#{pane_index}"
)

var tmuxTimeout = 5 * time.Second

const (
	// What goes into one send-keys. The ceiling tmux itself keeps is several
	// times this; the room left over is for the target, the flags and the
	// quoting around them.
	tmuxMaxInput = 2048
	escByte      = 0x1b
	// How far back from a cut an escape sequence is looked for. The longest one
	// the panel types is the bracketed paste marker, six bytes.
	escReach = 16
)

type tmuxPane struct {
	Target string
}

func (p tmuxPane) kind() string { return "tmux" }

func (p tmuxPane) attempts() int { return 1 }

func (p tmuxPane) send(ctx context.Context, payload string) error {
	if strings.ContainsRune(payload, 0) {
		return fmt.Errorf("there is a NUL byte in the text — terminals are not written to like that")
	}
	for _, piece := range tmuxPieces(payload) {
		if _, err := tmuxRun(ctx, "send-keys", "-t", p.Target, "-l", "--", piece); err != nil {
			return fmt.Errorf("tmux did not accept the input: %w", err)
		}
	}
	return nil
}

// tmuxPieces cuts the text into what tmux will take in one command.
//
// tmux carries a command to its server through a socket with a ceiling on the
// length, and answers "command too long" to anything over it: the answers to
// seventy questions are well over, and nothing is typed at all. The pieces go
// in one after another, and the terminal sees one paste however many pieces it
// arrived in — the brackets around it are part of the text.
//
// A cut lands neither inside a rune nor inside an escape sequence: half a
// character is not a character, and half of an escape is a terminal reading
// the rest of the paste as commands.
func tmuxPieces(text string) []string {
	if len(text) <= tmuxMaxInput {
		return []string{text}
	}
	var out []string
	for len(text) > tmuxMaxInput {
		cut := tmuxMaxInput
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		// An escape sequence is short; anything that begins one within reach of
		// the cut goes over whole with the next piece.
		if esc := strings.LastIndexByte(text[:cut], escByte); esc >= 0 && cut-esc <= escReach {
			cut = esc
		}
		if cut == 0 {
			break
		}
		out = append(out, text[:cut])
		text = text[cut:]
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}

func (p tmuxPane) screen(ctx context.Context) (string, bool) {
	out, err := tmuxRun(ctx, "capture-pane", "-p", "-t", p.Target)
	if err != nil {
		return "", false
	}
	return out, true
}

func tmuxPaneFor(ctx context.Context, pid int) (tmuxPane, error) {
	out, err := tmuxRun(ctx, "list-panes", "-a", "-F", paneFormat)
	if err != nil {
		return tmuxPane{}, fmt.Errorf("tmux did not tell about its panes: %w", err)
	}

	panes := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		owner, target, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		num, err := strconv.Atoi(owner)
		if err != nil {
			continue
		}
		panes[num] = target
	}
	if len(panes) == 0 {
		return tmuxPane{}, fmt.Errorf("there are no tmux panes right now")
	}

	probe := pid
	for range maxParentHops {
		if target, ok := panes[probe]; ok {
			return tmuxPane{Target: target}, nil
		}
		parent, ok := procParent(probe)
		if !ok || parent <= 1 {
			break
		}
		probe = parent
	}
	return tmuxPane{}, fmt.Errorf("process %d is in no tmux pane", pid)
}

func tmuxRun(ctx context.Context, args ...string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, tmuxTimeout)
	defer cancel()

	out, err := exec.CommandContext(callCtx, tmuxBin(), args...).Output()
	if err != nil {
		return "", errWithStderr(err)
	}
	return string(out), nil
}

func tmuxBin() string {
	if bin := os.Getenv(tmuxEnv); bin != "" {
		return bin
	}
	return defaultTmux
}

func tmuxSessionOf(target string) string {
	name, _, _ := strings.Cut(target, ":")
	return name
}

func tmuxWindowOf(target string) string {
	colon := strings.LastIndex(target, ":")
	if colon < 0 {
		return target
	}
	if dot := strings.Index(target[colon:], "."); dot >= 0 {
		return target[:colon+dot]
	}
	return target
}

func tmuxWindowSize(ctx context.Context, window string) string {
	out, err := tmuxRun(ctx, "display-message", "-p", "-t", window, "#{window_width}x#{window_height}")
	if err != nil {
		return ""
	}
	size := strings.TrimSpace(out)
	if _, _, ok := strings.Cut(size, "x"); !ok {
		return ""
	}
	return size
}

func tmuxWindowSizeOption(ctx context.Context, window string) string {
	out, err := tmuxRun(ctx, "show-options", "-t", window, "-w", "window-size")
	if err != nil {
		return ""
	}
	_, value, ok := strings.Cut(strings.TrimSpace(out), " ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func tmuxWindowSizeRule(ctx context.Context, window string) string {
	out, err := tmuxRun(ctx, "show-options", "-t", window, "-w", "-A", "-v", "window-size")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func tmuxSessionHasClients(ctx context.Context, session string) bool {
	if session == "" {
		return true
	}
	out, err := tmuxRun(ctx, "list-clients", "-t", session, "-F", "#{client_tty}")
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) != ""
}

func killTmuxSession(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	_, err := tmuxRun(ctx, "kill-session", "-t", name)
	return err
}
