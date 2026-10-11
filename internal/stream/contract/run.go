package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Options struct {
	Claude string
	Model  string
	Keep   bool
}

func command(program, cwd string, env []string, wait time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

// Run starts one stream session on haiku by default, with project hooks of its
// own. It also resumes that conversation once in print mode and once in a TUI.
// It is never called by make check: it spends the account's tokens.
func Run(opts Options, say func(string)) (Report, int) {
	env := cleanEnv(os.Environ())
	config := configDir(env)
	version, err := command(opts.Claude, "", env, requestWait, "--version")
	if err != nil {
		return Report{verdict("binary", false, fmt.Sprintf("%s: %v", opts.Claude, err))}, 2
	}
	help, err := command(opts.Claude, "", env, requestWait, "--help")
	if err != nil {
		return Report{verdict("binary", false, fmt.Sprintf("%s: %v", opts.Claude, err))}, 2
	}
	results := Report{{"version", Info, strings.TrimSpace(string(version)), false}}
	var missing []string
	for _, flag := range streamFlags {
		if !strings.Contains(string(help), flag) {
			missing = append(missing, flag)
		}
	}
	detail := "every stream flag is still there"
	if len(missing) > 0 {
		detail = "gone: " + strings.Join(missing, ", ")
	}
	results = append(results, verdict("flags", len(missing) == 0, detail))
	detail = "no way for a console to attach yet"
	if marks := directConnect(string(help)); len(marks) > 0 {
		detail = "the help names " + strings.Join(marks, ", ") + " — a console may now attach to a running session"
	}
	results = append(results, Result{"direct-connect", Info, detail, false})
	cwd, err := os.MkdirTemp("", "stream-contract-")
	if err != nil {
		return append(results, verdict("session", false, err.Error())), 2
	}
	if !opts.Keep {
		defer os.RemoveAll(cwd)
	}
	if err = writeHooks(cwd); err != nil {
		return append(results, verdict("session", false, err.Error())), 2
	}
	s, err := startSession(opts.Claude, cwd, env, opts.Model)
	if err != nil {
		return append(results, verdict("session", false, err.Error())), 2
	}
	if strings.Contains(string(help), `"manual"`) {
		s.manual = "manual"
	}
	say(fmt.Sprintf("session %s in %s", s.sid, cwd))
	steps := []struct {
		name string
		run  func() Report
	}{
		{"initialize", func() Report { return Report{checkInitialize(s)} }},
		{"turn", func() Report { return checkTurn(s, config) }},
		{"permission", func() Report { return Report{checkPermission(s)} }},
		{"deny", func() Report { return Report{checkDeny(s)} }},
		{"ask", func() Report { return checkAsk(s) }},
		{"plan", func() Report { return Report{checkPlan(s)} }},
		{"interrupt", func() Report { return Report{checkInterrupt(s)} }},
		{"queue", func() Report { return checkQueue(s) }},
		{"model", func() Report { return Report{checkModel(s, opts.Model)} }},
		{"effort", func() Report { return checkEffort(s) }},
		{"data", func() Report { return checkData(s) }},
		{"slash", func() Report { return Report{checkSlash(s)} }},
		{"background", func() Report { return checkBackground(s) }},
	}
	for _, step := range steps {
		if s.gone() || s.err != nil {
			results = append(results, verdict(step.name, false, "the session is gone"))
			continue
		}
		say("… " + step.name)
		results = append(results, step.run()...)
	}
	s.close()
	results = checkLetterTranscripts(results, findTranscript(config, s.sid))
	results = append(results, checkHooks(cwd))
	say("… resume")
	results = append(results, checkResume(opts, cwd, env, s.sid))
	say("… console")
	results = append(results, checkConsole(opts, cwd, env, s.sid))
	transcript := findTranscript(config, s.sid)
	if opts.Keep {
		say(fmt.Sprintf("kept: %s, transcript %s", cwd, transcript))
	} else if transcript != "" {
		// This project directory belongs to the fresh temporary cwd of this run.
		// Wait for both print and console processes to finish before removing it.
		if err := os.RemoveAll(filepath.Dir(transcript)); err != nil {
			results = append(results, verdict("cleanup", false, err.Error()))
		}
	}
	return results, results.ExitCode()
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func writeHooks(cwd string) error {
	dir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	hooks := object{}
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		hooks[name] = []object{{"hooks": []object{{"type": "command", "command": "printf '%s\\n' " + name + " >> " + shellQuote(filepath.Join(cwd, "hooks.log"))}}}}
	}
	body, err := json.Marshal(object{"hooks": hooks})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "settings.json"), body, 0o600)
}
func resumeVerdict(sid string, out object) Result {
	same, heard := out.text("session_id") == sid, strings.Contains(out.text("result"), "RESUMED")
	detail := fmt.Sprintf("id kept %t, answered %t", same, heard)
	if same && heard {
		detail = "the same id, the same history"
	}
	return verdict("resume", same && heard, detail)
}
func checkResume(opts Options, cwd string, env []string, sid string) Result {
	out, err := command(opts.Claude, cwd, env, turnWait, "-p", "--resume", sid, "--model", opts.Model, "--setting-sources", "project", "--output-format", "json", "Reply with the single word: RESUMED")
	if err != nil {
		return verdict("resume", false, err.Error())
	}
	var body object
	if err = json.Unmarshal(out, &body); err != nil {
		return verdict("resume", false, err.Error())
	}
	return resumeVerdict(sid, body)
}

func trustKey(screen string) string {
	if !strings.Contains(screen, "Yes, I trust this folder") {
		return ""
	}
	for _, line := range strings.Split(screen, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "❯") && strings.Contains(line, "Yes, I trust this folder") {
			return "Enter"
		}
	}
	return "Down"
}
func waitGone(pid string) {
	deadline := time.Now().Add(15 * time.Second)
	for pid != "" && time.Now().Before(deadline) && exists(filepath.Join("/proc", pid)) {
		time.Sleep(200 * time.Millisecond)
	}
}
func checkConsole(opts Options, cwd string, env []string, sid string) Result {
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		return Result{"console", Info, "no tmux on this machine: the console side of a switch was not checked", false}
	}
	name := "stream-contract-" + sid[:8]
	// A private server inherits only the cleaned environment. Putting env -i
	// and its values into the pane command exposes account tokens in argv.
	// The server and every pane it owns belong to this probe alone.
	run := func(wait time.Duration, args ...string) ([]byte, error) {
		return command(tmux, cwd, append(env, "TERM=screen-256color"), wait, append([]string{"-L", name, "-f", "/dev/null"}, args...)...)
	}
	pid := ""
	defer func() { run(requestWait, "kill-server"); waitGone(pid) }()
	if _, err = run(requestWait, "new-session", "-d", "-s", name, "-x", "160", "-y", "48", "-c", cwd, "--", opts.Claude, "--resume", sid, "--model", opts.Model, "--setting-sources", "project"); err != nil {
		return verdict("console", false, "the terminal did not start: "+err.Error())
	}
	listed, err := run(10*time.Second, "list-panes", "-t", name, "-F", "#{pane_pid}")
	if err != nil {
		return verdict("console", false, "the terminal did not start: "+err.Error())
	}
	if fields := strings.Fields(string(listed)); len(fields) > 0 {
		pid = fields[0]
	}
	screen := ""
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		captured, err := run(10*time.Second, "capture-pane", "-p", "-t", name)
		if err != nil {
			return verdict("console", false, "the terminal did not start: "+err.Error())
		}
		screen = string(captured)
		if strings.Contains(screen, "RESUMED") {
			return verdict("console", true, "a terminal resumes what the stream wrote")
		}
		if key := trustKey(screen); key != "" {
			if _, err = run(10*time.Second, "send-keys", "-t", name, key); err != nil {
				return verdict("console", false, "the terminal did not start: "+err.Error())
			}
		}
		time.Sleep(time.Second)
	}
	var lines []string
	for _, line := range strings.Split(screen, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	last := []rune(strings.Join(lines, " | "))
	return verdict("console", false, "the terminal did not show the history: "+string(last[max(0, len(last)-300):]))
}

func checkLetterTranscripts(results Report, path string) Report {
	var transcript []object
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		readEvents(f, func(e eventRead) bool {
			if e.err != nil {
				err = e.err
				return false
			}
			transcript = append(transcript, e.event)
			return true
		})
	}
	for i, r := range results {
		if r.Check != "letter.prompting" && r.Check != "letter.bypass" {
			continue
		}
		word := "LETTERWORD"
		if r.Check == "letter.bypass" {
			word = "STILLLETTER"
		}
		found, framing := transcriptLetter(transcript, letterText(word))
		if err != nil || !found {
			results[i].Status = Fail
			results[i].Detail += "; no letter text in the transcript"
		} else {
			results[i].Detail += "; letter text in the transcript"
		}
		if framing != "" {
			results = append(results, Result{r.Check + ".framing", Info, framing, false})
		}
	}
	return results
}
