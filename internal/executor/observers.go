package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/observer"
	"aacpanel/internal/stream"
)

// The observers of the panel are kept by the executor: it starts their
// units, reads their logs and wakes their sessions; it rings the alarms of
// the sessions the same way, with the same owners. A letter reaches the
// stream through its holder and codex through its daemon. In tmux it is
// typed into the composer, with its origin written out: a dialog gets no
// key, even when it opens while the letter is being typed.

// Observers is the keeper of the observers and the alarms of this host, one
// for the life of the executor.
func (e *Executor) Observers() *observer.Keeper {
	e.keeperOnce.Do(func() {
		e.keeper = observer.NewKeeper(observer.Dir(), observerUnits{cores: e.cores}, observerOwners{e: e})
	})
	return e.keeper
}

type observerOwners struct{ e *Executor }

// Resolve finds the live session a call names: a claude by its process and
// the conversation it runs, a codex by the thread a daemon holds.
func (o observerOwners) Resolve(ctx context.Context, c observer.Caller) (observer.Owner, error) {
	if c.Codex {
		th, ok := o.e.codex.Thread(c.Conversation)
		if !ok {
			return observer.Owner{}, fmt.Errorf("no daemon of codex the panel is linked to holds thread %s: "+
				"the panel wakes a thread it reaches", c.Conversation)
		}
		return observer.Owner{Session: th.Name, Conversation: th.ID, Codex: true}, nil
	}
	all, err := allLiveSessions()
	if err != nil {
		return observer.Owner{}, err
	}
	for _, s := range all {
		if s.PID != c.PID || s.SessionID != c.Conversation {
			continue
		}
		tmux := !onStream(s)
		if tmux {
			if _, err := tmuxPaneFor(ctx, s.PID); err != nil {
				return observer.Owner{}, err
			}
		} else {
			st, err := streamState(ctx, s)
			if err != nil {
				return observer.Owner{}, err
			}
			if st.Protocol < stream.LettersSince {
				return observer.Owner{}, fmt.Errorf("session %s was started before the panel could wake it, and it "+
					"keeps the program it started with until it ends: restart the session (the conversation is kept) "+
					"and call again then", s.Name)
			}
		}
		start, ok := procStart(s.PID)
		if !ok {
			return observer.Owner{}, fmt.Errorf("the process of session %s is gone", s.Name)
		}
		return observer.Owner{Session: s.Name, Conversation: s.SessionID, PID: s.PID, Start: start, Tmux: tmux}, nil
	}
	return observer.Owner{}, fmt.Errorf("no live session runs conversation %s in process %d", c.Conversation, c.PID)
}

// Alive says whether a session still runs as it did when its observer
// started. A claude is the same process, with its holder still there on the
// stream: a close, a restart or a move between transports ends it. A thread
// of codex is held while a daemon holds it and the panel has not closed it.
func (o observerOwners) Alive(owner observer.Owner) (alive, known bool) {
	if owner.Codex {
		return o.e.codex.Holds(owner.Conversation)
	}
	start, ok := procStart(owner.PID)
	if !ok || start != owner.Start {
		return false, true
	}
	if owner.Tmux {
		_, live := liveSessionOfPID(owner.PID, owner.Conversation)
		return live, true
	}
	_, held := stream.Held(owner.Conversation, owner.PID)
	return held, true
}

// Wake gives the session a letter of its observer: on the stream in the
// envelope of a letter between sessions, signed by the observer and with no
// address to answer; to codex in the same envelope with the words claude
// would put around it, as the daemon takes a letter of another session.
// In tmux the same words are typed only while the composer owns the keys.
func (o observerOwners) Wake(ctx context.Context, owner observer.Owner, from, text string) error {
	if owner.Codex {
		th, ok := o.e.codex.Thread(owner.Conversation)
		if !ok {
			return fmt.Errorf("no daemon holds thread %s", owner.Conversation)
		}
		_, err := th.Link.Send(ctx, th.ID, codex.Message{Text: wakeLetter(from, text)})
		return err
	}
	s, ok := liveSessionOfPID(owner.PID, owner.Conversation)
	if !ok {
		return fmt.Errorf("session %s is not live", owner.Session)
	}
	if start, ok := procStart(owner.PID); !ok || start != owner.Start {
		return fmt.Errorf("the process of session %s changed", owner.Session)
	}
	if !onStream(s) {
		pane, err := tmuxPaneFor(ctx, s.PID)
		if err != nil {
			return err
		}
		confirmed, err := pasteAndSend(ctx, wakePane{pane}, tmuxWakeLetter(from, text), nil)
		if err == nil && !confirmed {
			return fmt.Errorf("the wake of session %s was not confirmed on its tmux screen", owner.Session)
		}
		return err
	}
	st, err := streamState(ctx, s)
	if err != nil {
		return err
	}
	if st.Protocol < stream.LettersSince {
		return fmt.Errorf("the holder of session %s cannot take letters; restart the session", owner.Session)
	}
	_, err = streamAsk(ctx, s, stream.Request{Op: stream.OpLetter, Text: letterEnvelope("", from, text)})
	return err
}

func liveSessionOfPID(pid int, conversation string) (liveSession, bool) {
	all, err := allLiveSessions()
	if err != nil {
		return liveSession{}, false
	}
	for _, s := range all {
		if s.PID == pid && s.SessionID == conversation {
			return s, true
		}
	}
	return liveSession{}, false
}

// wakePane uses the composer's typing path, but never its blind fallback.
// Re-read before every write, including clearing the line and pressing
// Enter: a dialog can take the keyboard between two chunks of the letter.
type wakePane struct{ tmuxPane }

func (p wakePane) send(ctx context.Context, payload string) error {
	screen, seen := p.screen(ctx)
	if !seen {
		return fmt.Errorf("wake typing stopped: the tmux screen could not be read")
	}
	if busy, ready := composerReady(screen); !ready {
		return fmt.Errorf("wake typing stopped: %s", busy)
	}
	return p.tmuxPane.send(ctx, payload)
}

// Command output can contain terminal controls. Show those as text: an Esc
// or Tab in a log must not close the composer or choose a row on its screen.
func tmuxWakeLetter(from, text string) string {
	var out strings.Builder
	for _, r := range wakeLetter(from, text) {
		if unicode.IsControl(r) && r != '\n' {
			quoted := strconv.QuoteRune(r)
			out.WriteString(quoted[1 : len(quoted)-1])
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Neither codex nor a tmux terminal frames a letter for its model, so the
// panel says before the envelope where it comes from.
func wakeLetter(from, text string) string {
	return "A letter of the panel's " + from + ", which this session asked the panel for. Your person " +
		"did not type it.\n\n" + letterEnvelope("", from, text)
}

// observerUnits runs observers in transient units of the user's manager:
// started from the executor, a command would live in the executor's unit and
// its sandbox and die with its restart. They run on the cores every unit of
// the executor is held to.
type observerUnits struct{ cores string }

// observerStopWait is how long a command asked to stop gets before its unit
// kills it.
const observerStopWait = "5s"

func (u observerUnits) Start(ctx context.Context, unit, dir string, spec observer.Spec) error {
	bin, err := launcherPath()
	if err != nil {
		return err
	}
	env := cleanEnv(os.Environ())
	cmd := exec.CommandContext(ctx, systemdRunPath(), observerArgs(env, bin, unit, dir, spec, u.cores)...)
	cmd.Env = env
	var errs bytes.Buffer
	cmd.Stderr = &errs
	if err := cmd.Run(); err != nil {
		if text := strings.TrimSpace(errs.String()); text != "" {
			return errors.New(shortenLines(text))
		}
		return err
	}
	return nil
}

// observerArgs start the observer of dir in a unit of its own, held to the
// ceilings of an observer, and return once it has started. The unit is
// collected when it ends, failed or not: how the command ended is in the
// directory of the observer, not in the unit.
func observerArgs(env []string, bin, unit, dir string, spec observer.Spec, cores string) []string {
	args := []string{
		"--user", "--quiet", "--collect",
		"--unit=" + unit,
		"--description=aacpanel observer " + spec.Name,
		"--nice=" + fmt.Sprint(observer.Nice),
		"--property=MemoryMax=" + observer.MemoryMax,
		"--property=MemorySwapMax=0",
		"--property=CPUQuota=" + observer.CPUQuota,
		"--property=TimeoutStopSec=" + observerStopWait,
	}
	args = append(args, affinity(cores)...)
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); passEnv(name) {
			args = append(args, "--setenv="+kv)
		}
	}
	return append(args, "--", bin, observer.Flag, dir)
}

func (observerUnits) Stop(ctx context.Context, unit string) error {
	out, err := exec.CommandContext(ctx, systemctlPath(), "--user", "stop", unit).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "not loaded") {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (observerUnits) Units(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlPath(), "--user", "list-units", "--all", "--plain",
		"--no-legend", "--type=service", observer.UnitPrefix+"*").Output()
	if err != nil {
		return nil, err
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 && strings.HasPrefix(f[0], observer.UnitPrefix) {
			units = append(units, f[0])
		}
	}
	return units, nil
}

const systemctlEnv = "AACP_SYSTEMCTL"

func systemctlPath() string {
	if p := os.Getenv(systemctlEnv); p != "" {
		return p
	}
	return "systemctl"
}

// observerStop ends an observer of a session at a press in the panel: the
// one the session runs under conversation, by the id its files are named by.
// The keeper stops its unit and takes its log away, as observer_stop does.
func (e *Executor) observerStop(ctx context.Context, session, conversation string, work *action.Work) (string, error) {
	if work == nil {
		return "", fmt.Errorf("it is not said which observer of %s to stop", session)
	}
	v, notice, err := e.Observers().StopFor(ctx, conversation, work.ID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("observer %s of %s stopped, with its command; %s", v.Name, session, notice), nil
}

// alarmCancel cancels an alarm of a session at a press in the panel, as
// alarm_cancel does.
func (e *Executor) alarmCancel(ctx context.Context, session, conversation string, work *action.Work) (string, error) {
	if work == nil {
		return "", fmt.Errorf("it is not said which alarm of %s to cancel", session)
	}
	v, notice, err := e.Observers().CancelFor(ctx, conversation, work.ID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("alarm %s of %s cancelled; %s", v.Name, session, notice), nil
}
