package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The heavy gate: the tests and builds of a session go through the host's
// queue.
//
// A machine that holds many sessions has more of them at their tests at once
// than its memory takes, and the ceilings of memory kill a run but do not put
// runs in order. The executor's queue, aacpanel-exec -heavy, lets a few heavy
// runs go at a time and holds the rest. The gate answers PreToolUse of Bash: a
// line that runs a heavy command, as heavyRules know them, outside the queue is
// refused, and the reason gives the whole line back wrapped in the queue,
// ready to run. A refusal and not a quiet rewrite of the input: a rewrite
// comes with an allow, which would pass the question of permission by, while
// a refusal reads the same to every version of claude and to codex, which
// calls the hook with the same payload and takes the same answer.
//
// The line is read the way the shell splits it, short of a whole parser:
// quotes, escapes, comments, here-documents and substitutions are honoured,
// and each simple command between ; && || | & ( ) and line breaks is read at
// its command word, past the commands that only run the next one — runners —
// and into the script of bash -c one level down and the command docker exec
// or docker compose run or exec runs in a container. What the gate does not
// see — a heavy command inside a script of its own, a build under npm run —
// is left to the ceilings of memory.

// gateTool is the tool whose calls the gate reads, in claude and in codex.
const gateTool = "Bash"

// executable is the binary that answers the hook: the queue the refusal
// names is its own, wherever the install put it, and no PATH is trusted.
var executable = os.Executable

func gateHeavy(_ string, payload []byte, _ time.Time) Reply {
	var p struct {
		Tool  string `json:"tool_name"`
		Input struct {
			Command string `json:"command"`
		} `json:"tool_input"`
		Turn string `json:"turn_id"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Tool != gateTool {
		return Reply{}
	}
	name := unqueued(p.Input.Command, 0)
	if name == "" {
		return Reply{}
	}
	// A binary that cannot name itself has no command to give, and a
	// refusal without one would only hold the run.
	exe, err := executable()
	if err != nil {
		return Reply{}
	}
	wrapped := shellWord(exe) + " -heavy -- bash -c " + singleQuoted(p.Input.Command)
	why := "`" + name + "` is a heavy run, and heavy runs on this machine wait their turn in the host's queue. " +
		"Run the same line through it; its output, exit code, directory and environment stay its own:\n\n" +
		wrapped + "\n\nThe queue may hold it a while before it starts"
	// turn_id is codex's: codex has no run_in_background to point at.
	if p.Turn == "" {
		why += "; give a long or waiting run run_in_background"
	}
	return Reply{Deny: why + "."}
}

// unqueued is the heavy command a line runs outside the queue, by the name the
// refusal gives it, or nothing. depth is how many shells down the line is.
func unqueued(line string, depth int) string {
	cmds, subs := simpleCommands(line)
	for _, words := range cmds {
		if name := heavyRun(words, depth); name != "" {
			return name
		}
	}
	for _, sub := range subs {
		if name := unqueued(sub, depth); name != "" {
			return name
		}
	}
	return ""
}

// heavyRun is the heavy command a simple command runs outside the queue, or
// nothing: the words before its command word that only set it up are passed
// over, a runner is followed to the command it runs, a shell into its script.
func heavyRun(words []string, depth int) string {
	for len(words) > 0 {
		w, base := words[0], filepath.Base(words[0])
		if reserved[w] || assignment.MatchString(w) {
			words = words[1:]
			continue
		}
		if r, ok := runners[base]; ok {
			cmd, script, queued := r.runs(words[1:])
			switch {
			case queued:
				// The queue runs it: whatever it is, it waits its turn.
				return ""
			case script != "":
				if depth > 0 {
					return ""
				}
				return unqueued(script, depth+1)
			}
			words = cmd
			continue
		}
		if base == "docker" || base == "docker-compose" {
			how, cmd := dockerRuns(words)
			if name := heavyRun(cmd, depth); name != "" {
				return how + " … " + name
			}
			return ""
		}
		for _, h := range heavyRules {
			if !h.cmd.MatchString(w) {
				continue
			}
			if h.with == nil {
				return base
			}
			if sub, ok := h.with(words[1:]); ok {
				return base + " " + sub
			}
		}
		return ""
	}
	return ""
}

// ---- the commands the gate knows ----

// heavyRule is a heavy command: the command word it is known by, a bare name
// or a path, and for a command heavy only at some of its subcommands, what of
// the words after it makes it heavy.
type heavyRule struct {
	cmd *regexp.Regexp
	// with tells the subcommand that makes the command heavy, as the refusal
	// names it; nil for a command heavy by itself.
	with func(args []string) (string, bool)
}

// heavyRules are the heavy commands: test runners and builds that take the
// memory of a machine, unless run in the queue.
var heavyRules = []heavyRule{
	{named("go"), subcommand(`^test$`, nil, "-C")},
	{named("make", "gmake"), target(`^(check|test|tests|test-.+)$`,
		"-C", "--directory", "-f", "--file", "--makefile", "-I", "--include-dir", "-o", "--old-file", "-W", "--what-if")},
	{named("phpunit", "paratest", "pytest", "vitest", "jest"), nil},
	{regexp.MustCompile(`(^|/)vendor/bin/[^/]*test[^/]*$`), nil},
	{regexp.MustCompile(`(^|/)python[0-9.]*$`), module("pytest")},
	{named("npm", "pnpm", "yarn"), subcommand(`^test(:.*)?$`, []string{"run", "run-script"},
		"--prefix", "-w", "--workspace", "-C", "--dir", "--filter", "-F", "--cwd")},
	{named("playwright"), subcommand(`^test$`, nil)},
	{named("cargo"), subcommand(`^(test|build)$`, nil, "--manifest-path", "--config", "-Z", "-C")},
	{named("composer"), subcommand(`^(test|phpunit)$`, []string{"run", "run-script"}, "-d", "--working-dir")},
}

// named is a command word of one of the names, bare or at the end of a path.
func named(names ...string) *regexp.Regexp {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile(`(^|/)(` + strings.Join(quoted, "|") + `)$`)
}

// subcommand is heavy at the first word after the options that matches word,
// past the words of via before it — run of npm run test; args are the options
// that take the next word.
func subcommand(word string, via []string, args ...string) func([]string) (string, bool) {
	re := regexp.MustCompile(word)
	return func(words []string) (string, bool) {
		for i := 0; i < len(words); i++ {
			switch w := words[i]; {
			case option(w, true):
				if slices.Contains(args, w) {
					i++
				}
			case slices.Contains(via, w):
			case re.MatchString(w):
				return w, true
			default:
				return "", false
			}
		}
		return "", false
	}
}

// target is heavy at any word after the options that matches word: make
// takes its targets among its variables, in any order.
func target(word string, args ...string) func([]string) (string, bool) {
	re := regexp.MustCompile(word)
	return func(words []string) (string, bool) {
		for i := 0; i < len(words); i++ {
			switch w := words[i]; {
			case option(w, false):
				if slices.Contains(args, w) {
					i++
				}
			case re.MatchString(w):
				return w, true
			}
		}
		return "", false
	}
}

// module is heavy when python runs the module of the name: -m name among its
// options, or -m closing a cluster of them, before any code or script.
func module(name string) func([]string) (string, bool) {
	return func(words []string) (string, bool) {
		for i := 0; i < len(words); i++ {
			w := words[i]
			switch {
			case w == "-c" || !option(w, false) || strings.HasPrefix(w, "--"):
				return "", false
			case w == "-W" || w == "-X":
				i++
			case strings.HasSuffix(w, "m"):
				ok := i+1 < len(words) && words[i+1] == name
				return "-m " + name, ok
			case strings.HasPrefix(w, "-m"):
				return "-m " + name, w == "-m"+name
			}
		}
		return "", false
	}
}

// runner is a command that runs the command after its options and the words
// it takes before the command: the duration of timeout, the file of flock.
type runner struct {
	args    []string // the options that take the next word
	before  int      // the words after the options and before the command
	scripts []string // the options whose next word is a shell script
	// shell runs a script given with -c, alone or in a cluster of options, as
	// its first word after them; any other word of it is a file to run.
	shell bool
	// queue is the option that puts the command in the queue; a runner with
	// one runs nothing without it.
	queue string
}

var shell = runner{args: []string{"-o", "+o", "-O", "+O", "--rcfile", "--init-file"}, shell: true}

// runners are the commands the gate reads through to the command they run.
var runners = map[string]runner{
	"env":     {args: []string{"-u", "--unset", "-C", "--chdir", "-S", "--split-string"}},
	"nice":    {args: []string{"-n", "--adjustment"}},
	"ionice":  {args: []string{"-c", "--class", "-n", "--classdata", "-p", "--pid", "-P", "--pgid", "-u", "--uid"}},
	"timeout": {args: []string{"-s", "--signal", "-k", "--kill-after"}, before: 1},
	"flock":   {args: []string{"-w", "--wait", "--timeout", "-E", "--conflict-exit-code"}, before: 1, scripts: []string{"-c", "--command"}},
	"time":    {args: []string{"-f", "--format", "-o", "--output"}},
	"taskset": {before: 1},
	"npx":     {args: []string{"-p", "--package"}, scripts: []string{"-c", "--call"}},
	"php":     {args: []string{"-d", "-c", "-z"}},
	"bash":    shell, "sh": shell, "dash": shell, "zsh": shell, "ksh": shell,
	"aacpanel-exec": {args: []string{"-mem", "--mem", "-name", "--name"}, queue: "-heavy"},
}

// runs is what the runner runs, given the words after it: the command, a
// shell script, or nothing the gate reads; and whether it runs in the queue.
func (r runner) runs(words []string) (cmd []string, script string, queued bool) {
	before, wantScript := 0, false
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case r.queue != "" && (w == r.queue || w == "-"+r.queue):
			queued = true
		case w == "--" && !r.shell:
		case slices.Contains(r.scripts, w):
			if i+1 < len(words) {
				return nil, words[i+1], queued
			}
			return nil, "", queued
		case option(w, r.shell):
			if r.shell && w[0] == '-' && w[1] != '-' && strings.ContainsRune(w[1:], 'c') {
				wantScript = true
			}
			if slices.Contains(r.args, w) {
				i++
			}
		case wantScript:
			return nil, w, queued
		case r.shell:
			return nil, "", false
		case before < r.before:
			before++
		case r.queue != "" && !queued:
			return nil, "", false
		default:
			return words[i:], "", queued
		}
	}
	return nil, "", queued
}

// dockerRuns is the command docker exec, docker compose run or exec, or
// docker-compose run or exec runs in a container, with the words that say
// how; nothing for any other command of docker.
func dockerRuns(words []string) (string, []string) {
	compose := filepath.Base(words[0]) == "docker-compose"
	i := 1
	if !compose {
		i = skipOptions(words, i, dockerArgs)
		if i < len(words) && (words[i] == "compose" || words[i] == "container") {
			compose = words[i] == "compose"
			i++
		}
	}
	if compose {
		i = skipOptions(words, i, composeArgs)
	}
	if i >= len(words) || (words[i] != "exec" && !(compose && words[i] == "run")) {
		return "", nil
	}
	how := filepath.Base(words[0]) + " " + words[i]
	if compose && filepath.Base(words[0]) == "docker" {
		how = "docker compose " + words[i]
	}
	// Past the options of the subcommand and the container or service.
	i = skipOptions(words, i+1, containerArgs) + 1
	if i >= len(words) {
		return "", nil
	}
	return how, words[i:]
}

// The options of docker that take the next word: its own, those of compose,
// and those of exec and run.
var (
	dockerArgs  = []string{"-H", "--host", "-c", "--context", "--config", "-l", "--log-level"}
	composeArgs = []string{"-f", "--file", "-p", "--project-name", "--project-directory", "--profile", "--env-file",
		"--ansi", "--progress", "--parallel"}
	containerArgs = []string{"-e", "--env", "--env-file", "-u", "--user", "-w", "--workdir", "--detach-keys", "--index",
		"--name", "--entrypoint", "-p", "--publish", "-v", "--volume", "-l", "--label", "--cap-add", "--cap-drop", "--pull"}
)

// skipOptions is the first word from i on that is not an option, past the
// values of those in args and past a closing --.
func skipOptions(words []string, i int, args []string) int {
	for ; i < len(words) && option(words[i], false); i++ {
		if words[i] == "--" {
			return i + 1
		}
		if slices.Contains(args, words[i]) {
			i++
		}
	}
	return i
}

// option tells an option: a word of a dash and more, or of a plus where plus
// is one of the ways of the command.
func option(w string, plus bool) bool {
	return len(w) > 1 && (w[0] == '-' || (plus && w[0] == '+'))
}

// reserved are the words of the shell that stand before a command and run it
// as it is.
var reserved = map[string]bool{"!": true, "{": true, "}": true, "if": true, "then": true, "else": true, "elif": true,
	"do": true, "while": true, "until": true}

// assignment is a variable given for the command: FOO=x, FOO+=x, a[1]=x.
var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=`)

// plainWord needs no quotes in a command line.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9._/:=@%+-]+$`)

func shellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return singleQuoted(s)
}

// singleQuoted quotes any text for the shell: a quote inside closes the
// string, puts an escaped quote, and opens it again.
func singleQuoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ---- the line as the shell splits it ----

// simpleCommands splits a command line into its simple commands, each the
// words of it with the quotes taken off, and the command lines its
// substitutions run — $(…), `…`, <(…) — which stay a part of their words as
// well. A redirection and its target are no words of the command, nor are a
// comment and the body of a here-document.
func simpleCommands(line string) (cmds [][]string, subs []string) {
	l := lexer{s: line}
	for l.i < len(l.s) {
		l.step()
	}
	l.split()
	return l.out, l.subs
}

type lexer struct {
	s     string
	i     int
	out   [][]string
	words []string
	word  strings.Builder
	in    bool // a word is being read, an empty one among them: ''
	redir bool // the word being read is the target of a redirection
	doc   int  // the target is the end of a here-document: 1 for <<, 2 for <<-
	docs  []heredoc
	subs  []string
}

// heredoc is a here-document whose body follows the next line break.
type heredoc struct {
	end  string
	tabs bool // <<-: the tabs that open a line of it, its last among them, go
}

func (l *lexer) step() {
	c := l.s[l.i]
	switch {
	case c == '\\':
		if l.i+1 < len(l.s) && l.s[l.i+1] != '\n' {
			l.add(l.s[l.i+1 : l.i+2])
		}
		l.i += 2
	case c == '\'':
		l.single()
	case c == '"':
		l.double()
	case c == '`' || (c == '$' || c == '<' || c == '>') && (l.at(1) == '(' || c == '$' && l.at(1) == '{'):
		l.substitute()
	case c == '#' && !l.in:
		if n := strings.IndexByte(l.s[l.i:], '\n'); n >= 0 {
			l.i += n
		} else {
			l.i = len(l.s)
		}
	case c == ' ' || c == '\t' || c == '\r':
		l.end()
		l.i++
	case c == '\n':
		l.split()
		l.i++
		l.bodies()
	case c == ';' || c == '(' || c == ')':
		l.split()
		l.i++
	case c == '|' || c == '&' && l.at(1) != '>':
		l.split()
		l.i++
		if l.at(0) == '|' || l.at(0) == '&' {
			l.i++
		}
	case c == '<' || c == '>' || c == '&':
		l.redirect()
	default:
		l.add(l.s[l.i : l.i+1])
		l.i++
	}
}

// at is the byte n past the one being read, or 0 past the end.
func (l *lexer) at(n int) byte {
	if l.i+n < len(l.s) {
		return l.s[l.i+n]
	}
	return 0
}

func (l *lexer) add(s string) {
	l.word.WriteString(s)
	l.in = true
}

// end closes the word being read: a word of the command, or the target of a
// redirection, which goes — the end of a here-document kept for its body.
func (l *lexer) end() {
	if !l.in {
		return
	}
	w := l.word.String()
	l.word.Reset()
	l.in = false
	if !l.redir {
		l.words = append(l.words, w)
		return
	}
	l.redir = false
	if l.doc > 0 {
		l.docs = append(l.docs, heredoc{end: w, tabs: l.doc == 2})
		l.doc = 0
	}
}

// split closes the simple command being read.
func (l *lexer) split() {
	l.end()
	l.redir, l.doc = false, 0
	if len(l.words) > 0 {
		l.out = append(l.out, l.words)
	}
	l.words = nil
}

func (l *lexer) single() {
	l.in = true
	n := strings.IndexByte(l.s[l.i+1:], '\'')
	if n < 0 {
		l.add(l.s[l.i+1:])
		l.i = len(l.s)
		return
	}
	l.add(l.s[l.i+1 : l.i+1+n])
	l.i += n + 2
}

func (l *lexer) double() {
	l.in = true
	for l.i++; l.i < len(l.s); {
		c := l.s[l.i]
		switch {
		case c == '"':
			l.i++
			return
		case c == '\\' && strings.IndexByte("\"\\$`\n", l.at(1)) >= 0:
			if l.at(1) != '\n' {
				l.add(l.s[l.i+1 : l.i+2])
			}
			l.i += 2
		case c == '`' || c == '$' && (l.at(1) == '(' || l.at(1) == '{'):
			l.substitute()
		default:
			l.add(l.s[l.i : l.i+1])
			l.i++
		}
	}
}

// substitute reads the substitution that opens here into the word, and the
// command line inside it as a line of its own, unless it is a parameter's:
// ${…}.
func (l *lexer) substitute() {
	start, end := l.i, l.substitution(l.i)
	l.add(l.s[start:end])
	l.i = end
	open := 2
	switch {
	case l.s[start] == '`':
		open = 1
	case l.s[start+1] == '{':
		return
	}
	shut := end
	if shut > start+open && (l.s[shut-1] == ')' || l.s[shut-1] == '`') {
		shut--
	}
	l.subs = append(l.subs, l.s[start+open:shut])
}

// substitution is where the substitution that opens at i ends: a backquote
// at the next one, a bracket where it closes, past the quotes inside.
func (l *lexer) substitution(i int) int {
	if l.s[i] == '`' {
		for j := i + 1; j < len(l.s); j++ {
			switch l.s[j] {
			case '\\':
				j++
			case '`':
				return j + 1
			}
		}
		return len(l.s)
	}
	open := l.s[i+1]
	shut := byte(')')
	if open == '{' {
		shut = '}'
	}
	depth := 0
	for j := i + 1; j < len(l.s); j++ {
		switch l.s[j] {
		case '\\':
			j++
		case '\'':
			n := strings.IndexByte(l.s[j+1:], '\'')
			if n < 0 {
				return len(l.s)
			}
			j += n + 1
		case '"':
			for j++; j < len(l.s) && l.s[j] != '"'; j++ {
				if l.s[j] == '\\' {
					j++
				}
			}
		case open:
			depth++
		case shut:
			if depth--; depth == 0 {
				return j + 1
			}
		}
	}
	return len(l.s)
}

// redirect reads a redirection: the number of the descriptor just before it,
// its operator and, as the next word, its target, none of them a word of the
// command.
func (l *lexer) redirect() {
	if w := l.word.String(); l.in && w != "" && strings.Trim(w, "0123456789") == "" {
		l.word.Reset()
		l.in = false
	} else {
		l.end()
	}
	if l.s[l.i] == '&' {
		l.i++
	}
	op := l.s[l.i]
	l.i++
	switch {
	case op == '<' && l.at(0) == '<' && l.at(1) == '<':
		l.i += 2
	case op == '<' && l.at(0) == '<':
		l.i++
		l.doc = 1
		if l.at(0) == '-' {
			l.i++
			l.doc = 2
		}
	case l.at(0) == '>' || l.at(0) == '&' || l.at(0) == '|':
		l.i++
	}
	l.redir = true
}

// bodies passes over the bodies of the here-documents of the line just ended.
func (l *lexer) bodies() {
	for _, d := range l.docs {
		for l.i < len(l.s) {
			line := l.s[l.i:]
			if n := strings.IndexByte(line, '\n'); n >= 0 {
				line = line[:n]
				l.i += n + 1
			} else {
				l.i = len(l.s)
			}
			if d.tabs {
				line = strings.TrimLeft(line, "\t")
			}
			if line == d.end {
				break
			}
		}
	}
	l.docs = nil
}
