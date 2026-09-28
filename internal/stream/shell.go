package stream

import (
	"errors"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

// A command typed after "!" in the composer of a terminal runs in a shell of
// the session with no turn of the model, and what it printed goes into the
// conversation as two prompts of the person — the command, then its streams —
// which the model then answers. On the stream claude takes such a command as a
// line of its own kind and runs it the same way, in the session's directory
// and with its environment, but it keeps the output out of the conversation:
// the output comes back to the host and nothing reaches the transcript. So the
// holder hands the output to claude as a message in the shape the terminal
// writes, once the command has ended, and the model reads it as it would there.

// shellKeep is how much of each stream of a command goes to the model: the
// terminal gives the model no more of a command's output than this.
const shellKeep = 30000

// shellOut is the output claude hands back for a command it ran: both streams
// escaped for markup, and the exit code.
var shellOut = regexp.MustCompile(`(?s)\A<bash-stdout>(.*)</bash-stdout><bash-stderr>(.*)</bash-stderr>((?:<bash-exit-code>-?\d+</bash-exit-code>)?)\z`)

// shell has claude run a command. It answers once claude has the command; the
// output goes into the conversation when the command ends.
func (h *Holder) shell(command, id string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", errors.New("the command is empty")
	}
	if id == "" {
		id = newUUID()
	} else if !uuidLike(id) {
		return "", fmt.Errorf("the command id %q is not a uuid", id)
	}
	h.mu.Lock()
	h.state.Shells = append(h.state.Shells, Shell{UUID: id, Command: command, Since: time.Now()})
	h.mu.Unlock()
	err := h.write(map[string]any{
		"type": "bash_command", "command": command, "uuid": id, "session_id": h.spec.SessionID,
	})
	if err != nil {
		h.dropShell(id)
		return "", err
	}
	return id, nil
}

// shellPrinted notes the output of a command claude has just ended. Claude
// writes it as a user message of its own and says which command it was only
// in the word that comes right after — the end of the command.
func (h *Holder) shellPrinted(text string) {
	if !strings.HasPrefix(text, "<bash-stdout>") && !strings.HasPrefix(text, "<bash-stderr>") {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.state.Shells) > 0 {
		h.printed = text
	}
}

// shellEnded takes a command whose end claude reports with the output noted
// before it, and hands that output on to the conversation. It says whether the
// id was a command of the panel's.
func (h *Holder) shellEnded(id string) bool {
	h.mu.Lock()
	var ran Shell
	found := false
	for _, s := range h.state.Shells {
		if s.UUID == id {
			ran, found = s, true
			break
		}
	}
	out := h.printed
	if found {
		h.printed = ""
	}
	h.mu.Unlock()
	if !found {
		return false
	}
	// Not from the reading of claude's lines: the message is written to
	// claude, and a write that waits for claude to read must not stop the
	// holder from reading what claude writes meanwhile.
	go h.passOutput(ran, out)
	return true
}

// passOutput puts what a command printed into the conversation. The command
// stays among the running ones until the message is on its way, so a switch
// in between is refused rather than losing it.
func (h *Holder) passOutput(ran Shell, out string) {
	defer func() {
		if p := recover(); p != nil {
			h.logf("the output of a shell command was not passed on: %v\n%s", p, debug.Stack())
		}
		h.dropShell(ran.UUID)
	}()
	if out == "" {
		h.logf("claude ended a shell command without its output")
		out = "<bash-stdout></bash-stdout><bash-stderr>claude did not hand back the output of the command</bash-stderr>"
	}
	// Composed by the host: claude delivers it as written, with no files
	// looked up by an @ in the output and nothing attached to the turn, as a
	// terminal does with a command's output.
	if _, err := h.say(shellMessage(ran.Command, out), "", map[string]any{"client_composed": true}); err != nil {
		h.logf("the output of a shell command did not reach claude: %v", err)
	}
}

// shellMessage is the command and its output as the terminal puts them into
// the conversation, with each stream cut to what the model is given.
func shellMessage(command, out string) string {
	var b strings.Builder
	b.WriteString("<bash-input>" + command + "</bash-input>")
	found := shellOut.FindStringSubmatch(out)
	if found == nil {
		b.WriteString(keepEnds(out, 2*shellKeep))
		return b.String()
	}
	b.WriteString("<bash-stdout>" + keepEnds(found[1], shellKeep) + "</bash-stdout>")
	b.WriteString("<bash-stderr>" + keepEnds(found[2], shellKeep) + "</bash-stderr>")
	b.WriteString(found[3])
	return b.String()
}

// keepEnds leaves of a long stream its start and its end, and says how much of
// the middle is gone: the first lines of a command say what it did, the last
// ones how it ended.
func keepEnds(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	half := limit / 2
	return string(r[:half]) + fmt.Sprintf("\n… %d characters cut …\n", len(r)-2*half) + string(r[len(r)-half:])
}

func (h *Holder) dropShell(id string) {
	h.mu.Lock()
	for i, s := range h.state.Shells {
		if s.UUID == id {
			h.state.Shells = append(h.state.Shells[:i], h.state.Shells[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
}
