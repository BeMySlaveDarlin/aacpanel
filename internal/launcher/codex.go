package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
	"aacpanel/internal/schema"
)

// The keys of the map a codex session starts with.
const (
	keyCodexTransport = "codexTransport"
	keyCodexModel     = "codexModel"
	keyCodexEffort    = "codexEffort"
	keyCodexApproval  = "codexApproval"
	keyCodexSandbox   = "codexSandbox"
)

// CodexInDaemon is where a codex thread lives unless the map says tmux: in the
// daemon of its contour, with the panel as its client.
const CodexInDaemon = "daemon"

// CodexSpec names the thread codex in tmux resumes: the executor started it
// in the daemon of Home, and Bin is the codex program.
type CodexSpec struct {
	Thread string `json:"thread"`
	Home   string `json:"home"`
	Bin    string `json:"bin"`
}

// CodexParams is what a codex session of a project starts with: the codex keys
// of the map and the first message.
type CodexParams struct {
	Agent     string
	Transport string
	Model     string
	Effort    string
	Approval  string
	Sandbox   string
	Intent    string
}

// ParseCodex reads the launch of a project for codex. A value the schema does
// not take is dropped with a word: the map is checked when it is saved, and
// what goes on to codex has to be a word codex knows.
func ParseCodex(raw json.RawMessage) (CodexParams, []string) {
	var obj map[string]any
	if len(raw) == 0 {
		return CodexParams{Transport: CodexInDaemon}, nil
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return CodexParams{Transport: CodexInDaemon}, []string{"launch parameters were not parsed: " + err.Error()}
	}
	var warns []string
	word := func(key string) string {
		value, ok := obj[key]
		if !ok {
			return ""
		}
		if problems := schema.Check(schema.LevelProject, map[string]any{key: value}); len(problems) > 0 {
			warns = append(warns, fmt.Sprintf("parameter %s is skipped: %s", key, problems[0].Why))
			return ""
		}
		s, _ := value.(string)
		return s
	}
	p := CodexParams{
		Agent: word(keyAgent), Transport: word(keyCodexTransport), Model: word(keyCodexModel),
		Effort: word(keyCodexEffort), Approval: word(keyCodexApproval), Sandbox: word(keyCodexSandbox),
		Intent: word(keyIntent),
	}
	if p.Transport == "" {
		p.Transport = CodexInDaemon
	}
	return p, warns
}

// CodexArgs are the words that start codex in tmux on a thread the panel
// started. The thread comes first, resumed by its id, so that the command
// tmux keeps for the pane names it; then the directory, and the first message
// last, after "--", so that a message starting with a dash is not read as a
// flag. The choices of the map are not flags here: the thread has them from
// its start, the effort among them, and codex given a configuration override
// — the only way a terminal takes an effort — refuses a thread the daemon
// holds loaded.
func CodexArgs(dir, thread, intent string) []string {
	args := []string{"resume", thread, "-C", dir}
	if intent != "" {
		args = append(args, "--", intent)
	}
	return args
}

// LaunchedCodex tells the pane of codex the launcher started on a thread, by
// the command tmux keeps for it: the launcher's own start, then codex resuming
// that thread.
func LaunchedCodex(startCommand, thread string) bool {
	if thread == "" || !Launched(startCommand) {
		return false
	}
	words := strings.Fields(startCommand)
	return len(words) >= 7 && words[5] == "resume" && words[6] == thread
}

// codexGrace is how long codex is watched after it took its environment: a
// codex that cannot resume the thread ends at once, and its tmux session with
// it.
var codexGrace = time.Second

// runCodex starts codex in tmux on the thread the spec names, under the name
// of the project's session or the first free one after it. The environment
// is the session's, with the home of the contour as CODEX_HOME.
func runCodex(ctx context.Context, spec Spec) (Report, error) {
	c := spec.Codex
	if c.Thread == "" || c.Home == "" || c.Bin == "" {
		return Report{}, fmt.Errorf("the codex task names no thread, home or program: %+v", *c)
	}
	// What the map holds that codex does not take, the executor has named.
	p, _ := ParseCodex(spec.Launch)
	var warns []string
	taken := takenNames()
	for _, name := range tmuxSessions() {
		taken[name] = true
	}
	name, err := freeName(spec.Session, taken)
	if err != nil {
		return Report{}, err
	}

	host := hostcfg.Load()
	env, envWarns := childEnv(os.Environ(), Params{}, host.Display, host.Lang, "")
	warns = append(warns, envWarns...)
	drop, dropWarns, err := writeSessionEnv(codexEnv(env, c.Home))
	if err != nil {
		return Report{}, err
	}
	defer drop.remove()
	warns = append(warns, dropWarns...)

	args := CodexArgs(spec.Dir, c.Thread, p.Intent)
	windowPID, startWarns, err := start(spec.Dir, name, c.Bin, args, drop)
	if err != nil {
		return Report{}, err
	}
	warns = append(warns, startWarns...)
	if err := waitCodex(ctx, name, drop.path); err != nil {
		return Report{}, fmt.Errorf("%w — the command was: %s %s", err, c.Bin, strings.Join(args, " "))
	}
	return Report{
		Session: name, Dir: spec.Dir, Konsole: windowPID, Warnings: warns, Intent: p.Intent,
		Transport: TransportTmux,
	}, nil
}

// codexEnv is the environment of codex in tmux: the session's, without what
// claude reads, and with the home of the contour. A router in front of codex
// may choose the home by the directory instead; the panel names it all the
// same, and the executor names the program.
func codexEnv(env []string, home string) []string {
	out := slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return strings.HasPrefix(name, "CLAUDE") || name == "CODEX_HOME"
	})
	out = append(out, "CODEX_HOME="+home)
	slices.Sort(out)
	return out
}

// waitCodex waits until the pane has read its environment — the file removes
// itself once read — and codex is still there a moment later.
func waitCodex(ctx context.Context, name, envFile string) error {
	deadline := time.Now().Add(startWait)
	for {
		if _, err := os.Stat(envFile); os.IsNotExist(err) {
			break
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("codex did not start in tmux session %s within %s", name, startWait)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("codex did not start in tmux session %s: the action ran out of time", name)
		case <-time.After(startPoll):
		}
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("codex did not start in tmux session %s: the action ran out of time", name)
	case <-time.After(codexGrace):
	}
	if err := exec.Command(tool(tmuxEnv, "tmux"), "has-session", "-t", "="+name).Run(); err != nil {
		return fmt.Errorf("codex ended at once in tmux session %s, and the session with it", name)
	}
	return nil
}

// tmuxSessions are the names of the sessions of the user's tmux server: a
// new one cannot take any of them.
func tmuxSessions() []string {
	out, err := exec.Command(tool(tmuxEnv, "tmux"), "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
}
