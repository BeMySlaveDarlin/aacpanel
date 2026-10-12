package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/codex/contract"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/install"
	"aacpanel/internal/stream"
)

var codexSource = struct{ channel, installer string }{
	"https://releases.openai.com/codex/channels/latest",
	"https://chatgpt.com/codex/install.sh",
}

var releaseNumber = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`)

type programState struct {
	Version        string            `json:"version"`
	At             int64             `json:"at"`
	CheckedVersion string            `json:"checkedVersion,omitempty"`
	Verdict        string            `json:"verdict,omitempty"`
	Detail         string            `json:"detail,omitempty"`
	UpdateError    string            `json:"updateError,omitempty"`
	RestartPending map[string]string `json:"restartPending,omitempty"`
}

func readProgram(name string) programState {
	var state programState
	raw, err := os.ReadFile(stream.ProgramPath(name))
	if err == nil {
		_ = json.Unmarshal(raw, &state)
	}
	return state
}

func saveProgram(name string, state programState) error {
	path := stream.ProgramPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return stream.WriteSnapshot(path, raw)
}

func nativeClaude() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "claude")
}

func standaloneCodex() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "packages", "standalone")
}

func programVersion(ctx context.Context, program string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, program, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", program, err)
	}
	words := strings.Fields(string(out))
	for _, word := range words {
		if releaseNumber.MatchString(word) {
			return word, nil
		}
	}
	return "", fmt.Errorf("%s did not report a version", program)
}

// KeepPrograms observes Claude's own updates too. The collector reads the files,
// so the service needs neither host binaries nor another right on the host.
func (e *Executor) KeepPrograms(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if e.updates.TryLock() {
			for _, name := range []string{"codex", "claude"} {
				if err := refreshProgram(ctx, name); err != nil {
					log.Printf("agent version: %v", err)
				}
			}
			e.updates.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func refreshProgram(ctx context.Context, name string) error {
	program := nativeClaude()
	if name == "codex" {
		var err error
		program, err = codexBin()
		if err != nil {
			return nil
		}
	}
	if _, err := os.Stat(program); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	version, err := programVersion(ctx, program)
	if err != nil {
		return err
	}
	state := readProgram(name)
	state.Version, state.At = version, time.Now().Unix()
	return saveProgram(name, state)
}

func (e *Executor) updateAgent(ctx context.Context, kind action.Kind) (detail string, err error) {
	if !e.updates.TryLock() {
		return "", errors.New("an agent update is already running")
	}
	defer e.updates.Unlock()
	name := "codex"
	if kind == action.ClaudeUpdate {
		name = "claude"
	}
	defer func() {
		state := readProgram(name)
		state.UpdateError = ""
		if err != nil {
			state.UpdateError = err.Error()
		}
		state.At = time.Now().Unix()
		if saveErr := saveProgram(name, state); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
		if refreshErr := refreshProgram(ctx, name); refreshErr != nil {
			err = errors.Join(err, refreshErr)
		}
	}()
	if kind == action.ClaudeUpdate {
		// PATH can hold a contour wrapper; updates belong to the native installation.
		cmd := exec.CommandContext(ctx, nativeClaude(), "update")
		cmd.Env = cleanEnv(os.Environ())
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("claude update: %w: %s", err, shortenLines(string(out)))
		}
		return e.checkClaude(ctx)
	}
	return e.updateCodex(ctx)
}

func fetchUpdate(ctx context.Context, url string, ceiling int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("package source answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, ceiling+1))
	if err == nil && int64(len(data)) > ceiling {
		err = errors.New("package source exceeded its size limit")
	}
	return data, err
}

func (e *Executor) updateCodex(ctx context.Context) (string, error) {
	root := standaloneCodex()
	current := filepath.Join(root, "current")
	program := filepath.Join(current, "bin", "codex")
	selected, err := codexBin()
	if err != nil {
		return "", err
	}
	if filepath.Clean(selected) != program {
		return "", fmt.Errorf("panel updates require AACP_CODEX=%s; the configured program is %s", program, selected)
	}
	// A new session must not start a daemon from the switched link before the
	// contract has accepted it or restored the previous installation.
	e.daemons.Lock()
	defer e.daemons.Unlock()
	// Share the host updater's lock during the handover. Its presence check is
	// not enough when it already started before the panel's timer was installed.
	lock, err := os.OpenFile(filepath.Join(root, ".atlas-update.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return "", errors.New("another codex updater holds the installation lock")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	old, err := os.Readlink(current)
	if err != nil {
		return "", fmt.Errorf("the standalone current path must be a symlink: %w", err)
	}
	previous, err := selectedRelease(root)
	if err != nil {
		return "", err
	}
	data, err := fetchUpdate(ctx, codexSource.channel, 1<<20)
	if err != nil {
		return "", err
	}
	var channel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &channel); err != nil {
		return "", err
	}
	latest := strings.TrimPrefix(channel.Tag, "rust-v")
	if !releaseNumber.MatchString(latest) {
		return "", errors.New("the codex channel did not name a release")
	}
	version, err := programVersion(ctx, program)
	if err != nil {
		return "", err
	}
	state := readProgram("codex")
	if version == latest && state.CheckedVersion == latest && state.Verdict == "passed" && state.UpdateError == "" && len(state.RestartPending) == 0 && daemonPackagesCurrent(ctx, latest) {
		return "Codex " + latest + " is current; its protocol passed the contract.", pruneReleases(root)
	}

	data, err = fetchUpdate(ctx, codexSource.installer, 2<<20)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "codex-update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, data, 0o600); err != nil {
		return "", err
	}
	if version != latest {
		if err := installCodex(ctx, script, latest, ""); err != nil {
			return "", errors.Join(err, restoreCurrent(current, old))
		}
	}
	installed, err := programVersion(ctx, program)
	if err != nil || installed != latest {
		return "", errors.Join(fmt.Errorf("codex installed %q, expected %s: %v", installed, latest, err), restoreCurrent(current, old))
	}
	report, err := contract.Probe(ctx, program)
	state.CheckedVersion, state.Version = latest, latest
	if err != nil {
		state.Verdict, state.Detail = "failed", err.Error()
	} else {
		state.Verdict, state.Detail = "passed", report.Text()
		if report.Red() {
			state.Verdict = "failed"
		}
	}
	if state.Verdict == "failed" {
		if err := saveProgram("codex", state); err != nil {
			return "", errors.Join(err, restoreCurrent(current, old))
		}
		return "", errors.Join(fmt.Errorf("Codex %s failed the protocol contract; the previous link is restored. %s", latest, state.Detail), restoreCurrent(current, old))
	}
	if err := rememberRelease(root, previous); err != nil {
		return "", err
	}
	// A daemon outlives the executor. Restart it through the same transient unit
	// used for its first start, or it would inherit the executor's restrictions.
	var failures []error
	var stopped []string
	state.RestartPending = map[string]string{}
	for _, home := range registry.CodexHomes() {
		packageRoot := filepath.Dir(filepath.Dir(filepath.Dir(daemonProgram(home.Dir))))
		previous, err := selectedRelease(packageRoot)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		// Lifecycle commands use the selected package of CODEX_HOME, regardless
		// of the invoking CLI. Installing only the shared CLI leaves these old.
		if err := installCodex(ctx, script, latest, home.Dir); err != nil {
			failures = append(failures, fmt.Errorf("codex package of %s: %w", home.Contour, err))
			continue
		}
		installed, err := programVersion(ctx, daemonProgram(home.Dir))
		if err != nil || installed != latest {
			failures = append(failures, fmt.Errorf("codex package of %s reports %q, expected %s: %v", home.Contour, installed, latest, err))
			continue
		}
		if err := rememberRelease(packageRoot, previous); err != nil {
			failures = append(failures, err)
			continue
		}
		busy, running, err := e.restartCodex(ctx, program, home.Dir)
		if err != nil || busy > 0 {
			detail := fmt.Sprintf("restart pending: %d turns running", busy)
			if err != nil {
				detail = err.Error()
				failures = append(failures, fmt.Errorf("codex daemon %s: %w", home.Contour, err))
			}
			state.RestartPending[home.Contour] = detail
			continue
		}
		if !running {
			stopped = append(stopped, home.Contour)
		} else if link := e.codex.Home(home.Dir); link != nil {
			wait, cancel := context.WithTimeout(ctx, daemonWait)
			err := link.ReadyVersion(wait, latest)
			cancel()
			if err != nil {
				failures = append(failures, err)
				continue
			}
		}
		hooks, err := os.ReadFile(filepath.Join(home.Dir, "hooks.json"))
		if err == nil {
			_, err = install.CheckCodexHooks(ctx, program, home.Dir, hooks)
		}
		finding := contract.Finding{Place: "hooks/list " + home.Contour, Detail: "the installed panel hooks are present, trusted and enabled"}
		if err != nil {
			finding.Red, finding.Detail = true, err.Error()
			failures = append(failures, fmt.Errorf("codex hooks of %s: %w", home.Contour, err))
		}
		report.Hooks = append(report.Hooks, finding)
		if err == nil {
			failures = append(failures, pruneReleases(packageRoot))
		}
	}
	state.Detail = report.Text()
	for _, contour := range stopped {
		state.Detail += "\n" + contour + ": daemon not running; no restart needed"
	}
	if report.Red() {
		state.Verdict = "failed"
	}
	if len(state.RestartPending) == 0 && errors.Join(failures...) == nil {
		failures = append(failures, pruneReleases(root))
	}
	failures = append(failures, saveProgram("codex", state))
	if err := errors.Join(failures...); err != nil {
		return "", err
	}
	detail := "Codex " + latest + " updated; idle daemons restarted.\n" + state.Detail
	for contour, pending := range state.RestartPending {
		detail += "\n" + contour + ": " + pending
	}
	return detail, nil
}

func (e *Executor) restartCodex(ctx context.Context, program, home string) (busy int, running bool, err error) {
	// The launcher also uses the home's control socket to decide whether a
	// daemon exists. With no socket the next start takes the selected package;
	// a link that has not polled a live daemon is still unsafe to restart.
	if _, err := os.Stat(codex.SocketPath(home)); errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	} else if err != nil {
		return 0, true, fmt.Errorf("restart pending: daemon control socket: %w", err)
	}
	restart := func() error {
		args := daemonArgs(cleanEnv(os.Environ()), program, home, home, e.cores)
		args[len(args)-1] = "restart"
		cmd := exec.CommandContext(ctx, systemdRunPath(), args...)
		cmd.Env = cleanEnv(os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("daemon restart: %w: %s", err, shortenLines(string(out)))
		}
		return nil
	}
	if link := e.codex.Home(home); link != nil {
		busy, err := link.RestartIdle(restart)
		return busy, true, err
	}
	return 0, true, errors.New("restart pending: daemon activity is not known")
}

// The native daemon owns a package in each home. Explicit releases clear its
// latest-channel selection, leaving automatic updates to the delivery timer.
func daemonProgram(home string) string {
	return filepath.Join(home, "packages", "app-server-daemon", "current", "bin", "codex")
}

func daemonPackagesCurrent(ctx context.Context, version string) bool {
	for _, home := range registry.CodexHomes() {
		got, err := programVersion(ctx, daemonProgram(home.Dir))
		if err != nil || got != version {
			return false
		}
	}
	return true
}

func installCodex(ctx context.Context, script, version, home string) error {
	shim := filepath.Join(standaloneCodex(), "bin")
	if home == "" {
		if err := os.MkdirAll(shim, 0o755); err != nil {
			return err
		}
	}
	cmd := exec.CommandContext(ctx, "sh", script, "--release", version)
	cmd.Env = updateEnvironment(shim, home)
	// A cancelled shell must not leave its downloader switching current after
	// the caller has restored the previous link.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("codex installation: %w: %s", err, shortenLines(string(out)))
	}
	return nil
}

func updateEnvironment(shim, home string) []string {
	var env []string
	for _, item := range cleanEnv(os.Environ()) {
		key, _, _ := strings.Cut(item, "=")
		if key == "CODEX_HOME" || key == "CODEX_RELEASE" || key == "CODEX_NON_INTERACTIVE" || key == "PATH" || strings.HasPrefix(key, "CODEX_INSTALL") {
			continue
		}
		env = append(env, item)
	}
	env = append(env, "CODEX_NON_INTERACTIVE=1", "CODEX_INSTALL_DIR="+shim, "PATH="+os.Getenv("PATH")+string(os.PathListSeparator)+shim)
	if home != "" {
		env = append(env, "CODEX_HOME="+home, "CODEX_INSTALL_DAEMON_ONLY=1")
	}
	return env
}

func restoreCurrent(path, old string) error {
	tmp := path + ".aacpanel"
	if err := os.Symlink(old, tmp); err != nil {
		return err
	}
	defer os.Remove(tmp)
	return os.Rename(tmp, path)
}
