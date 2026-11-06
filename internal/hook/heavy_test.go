package hook

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// gateCases are lines of a Bash call and the heavy command the gate names in
// each, nothing for a line it lets through.
var gateCases = []struct{ line, heavy string }{
	// The commands of the table.
	{"go test ./...", "go test"},
	{"go test -run TestX ./internal/hook/", "go test"},
	{"/usr/local/go/bin/go test ./...", "go test"},
	{"make check", "make check"},
	{"make test", "make test"},
	{"make tests", "make tests"},
	{"make test-go", "make test-go"},
	{"make -C web -j4 check", "make check"},
	{"make GOTEST_PARALLEL=2 check CHECK_JOBS=4", "make check"},
	{"phpunit --filter X", "phpunit"},
	{"vendor/bin/phpunit", "phpunit"},
	{"./vendor/bin/paratest -p4", "paratest"},
	{"vendor/bin/testbench package:test", "testbench"},
	{"php -d memory_limit=-1 vendor/bin/phpunit", "phpunit"},
	{"pytest -x tests/", "pytest"},
	{"python -m pytest", "python -m pytest"},
	{"python3 -u -m pytest -q", "python3 -m pytest"},
	{"python3 -Im pytest", "python3 -m pytest"},
	{"npm test", "npm test"},
	{"npm run test", "npm test"},
	{"pnpm --filter web test", "pnpm test"},
	{"yarn test:unit", "yarn test:unit"},
	{"npx vitest run", "vitest"},
	{"npx -y jest --ci", "jest"},
	{"npx playwright test", "playwright test"},
	{"vitest", "vitest"},
	{"jest --watchAll=false", "jest"},
	{"cargo test --release", "cargo test"},
	{"cargo +nightly build", "cargo build"},
	{"composer test", "composer test"},
	{"composer run-script phpunit", "composer phpunit"},
	{"docker compose run --rm -e A=1 app go test ./...", "docker compose run … go test"},
	{"docker compose -f x.yml exec -T php vendor/bin/phpunit", "docker compose exec … phpunit"},
	{"docker-compose exec app pytest", "docker-compose exec … pytest"},
	{"docker exec -it -w /app php vendor/bin/paratest", "docker exec … paratest"},
	{"docker exec app sh -c 'cd /app && vendor/bin/phpunit'", "docker exec … phpunit"},
	// The words before a command that only run it.
	{"nice -n 10 flock x make check", "make check"},
	{"GOMAXPROCS=4 nice -n 10 flock ~/.cache/claude-tmp/aacpanel-check.lock make check GOTEST_PARALLEL=2", "make check"},
	{"env -i PATH=/bin go test ./...", "go test"},
	{"timeout 600 go test ./...", "go test"},
	{"timeout -k 5 600 make test", "make test"},
	{"ionice -c3 nice make tests", "make tests"},
	{"time make test-go", "make test-go"},
	{"taskset -c 8-11 go test ./...", "go test"},
	{"FOO=1 go test", "go test"},
	{"cd web && npm test", "npm test"},
	{"flock /x/lock -c 'make check'", "make check"},
	// Every simple command of the line, one shell down.
	{"echo start; go test ./...", "go test"},
	{"go test ./... 2>&1 | tail -50", "go test"},
	{"ls && (cd web && npm test)", "npm test"},
	{"if make check; then echo ok; fi", "make check"},
	{"for p in a b; do go test ./$p; done", "go test"},
	{"! go test ./...", "go test"},
	{"git status\ngo test ./...", "go test"},
	{"go test \\\n  ./...", "go test"},
	{"go test ./... &", "go test"},
	{"bash -c 'go test ./...'", "go test"},
	{`bash -lc "make check"`, "make check"},
	{"sh -euc pytest", "pytest"},
	{"out=$(go test ./... 2>&1); echo \"$out\"", "go test"},
	{"echo \"`make check`\"", "make check"},
	// The queue, and what it does not cover.
	{"/home/u/bin/aacpanel-exec -heavy -- bash -c 'go test ./...'", ""},
	{"aacpanel-exec -heavy -mem 8G -name check -- make check", ""},
	{"nice -n 10 aacpanel-exec -heavy -- make check", ""},
	{"aacpanel-exec -heavy -- make check && go test ./...", "go test"},
	{"aacpanel-exec -console x", ""},
	// Lines the gate lets through.
	{"ls", ""},
	{"git status", ""},
	{`git log --grep "go test"`, ""},
	{"echo make check", ""},
	{"grep -rn phpunit .", ""},
	{"cat Makefile", ""},
	{"go build ./...", ""},
	{"go vet ./...", ""},
	{"make front", ""},
	{"make -C test front", ""},
	{"docker ps", ""},
	{"docker exec x ls", ""},
	{"docker run --rm x ls", ""},
	{"python3 script.py -m pytest", ""},
	{"npm run build", ""},
	{"./run.sh", ""},
	{"bash run.sh", ""},
	{"bash -c \"bash -c 'go test ./...'\"", ""},
	// Quotes, comments and here-documents hold no commands.
	{`git log --grep "x; go test ./..."`, ""},
	{"echo 'a && make check now'", ""},
	{"git commit -m \"fix\ngo test ./...\"", ""},
	{"echo ok # then go test ./...", ""},
	{"echo \"${A:-x; go test}\"", ""},
	{"cat > Makefile <<'EOF'\ncheck:\n\tgo test ./...\nEOF\necho done", ""},
	{"cat <<-EOF\n\tmake check\n\tEOF", ""},
	{"git commit -m \"$(cat <<'EOF'\nRun go test\nmake check\nEOF\n)\"", ""},
	{"cat <<EOF\ngo test\nEOF\ngo test ./...", "go test"},
}

func TestTheGateKnowsHeavyCommandsAtTheirPlace(t *testing.T) {
	for _, c := range gateCases {
		if got := unqueued(c.line, 0); got != c.heavy {
			t.Errorf("%q: %q, want %q", c.line, got, c.heavy)
		}
	}
}

// heavyCall is the payload of a Bash call as claude passes it to PreToolUse.
func heavyCall(t *testing.T, command string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"session_id": "c0ffee", "transcript_path": "/home/u/.claude/projects/x/c0ffee.jsonl", "cwd": "/srv/repo",
		"permission_mode": "default", "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_use_id": "toolu_1",
		"tool_input": map[string]any{"command": command, "description": "Run the tests", "timeout": 120000},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// denial is the reason of a refused call, or nothing for an answer that lets
// it through.
func denial(t *testing.T, out string) string {
	t.Helper()
	if out == "" {
		return ""
	}
	var reply struct {
		Specific struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("the answer %q: %v", out, err)
	}
	if reply.Specific.Event != "PreToolUse" || reply.Specific.Decision != "deny" || reply.Specific.Reason == "" {
		t.Fatalf("the answer %q is no refusal", out)
	}
	return reply.Specific.Reason
}

func withExecutable(t *testing.T, path string) {
	t.Helper()
	was := executable
	executable = func() (string, error) { return path, nil }
	t.Cleanup(func() { executable = was })
}

// A heavy line is refused with the line itself in the queue, ready to run:
// the shell gives bash -c the line back word for word, quotes and line breaks
// in it, and the gate lets the command it gave through. The shell that reads
// the command is bash itself, with a function in the executor's place that
// prints the words it is given.
func TestTheGateRefusesWithTheLineInTheQueue(t *testing.T) {
	withExecutable(t, "aacpanel-exec")
	for _, line := range []string{
		"go test ./...",
		"cd web && npm test -- --grep 'it'\"'\"'s slow'",
		"make check 2>&1 | tail -n 40\necho \"done: $?\" `date` $(id -u) \\\n  >/dev/null",
	} {
		code, out, errs := run(t, []string{"heavy-gate"}, heavyCall(t, line))
		why := denial(t, out)
		if code != 0 || errs != "" {
			t.Errorf("%q: exit %d, stderr %q", line, code, errs)
		}
		_, wrapped, _ := strings.Cut(why, "\n\n")
		wrapped, _, _ = strings.Cut(wrapped, "\n\n")
		words, err := exec.Command("bash", "-c", `aacpanel-exec() { printf '%s\0' "$@"; }; `+wrapped).Output()
		if want := "-heavy\x00--\x00bash\x00-c\x00" + line + "\x00"; err != nil || string(words) != want {
			t.Errorf("%q: the command of the refusal %q gives %q (%v)", line, wrapped, words, err)
		}
		if !strings.Contains(why, "run_in_background") {
			t.Errorf("%q: the refusal says nothing of run_in_background: %q", line, why)
		}
		if _, again, _ := run(t, []string{"heavy-gate"}, heavyCall(t, wrapped)); again != "" {
			t.Errorf("%q: the command the gate gave is refused: %q", line, again)
		}
	}
}

// An executor whose path the shell would split is quoted in the command.
func TestTheQueueOfAnExecutorAtAnOddPathIsQuoted(t *testing.T) {
	withExecutable(t, "/home/u/my bin/aacpanel-exec")
	_, out, _ := run(t, []string{"heavy-gate"}, heavyCall(t, "go test ./..."))
	if why := denial(t, out); !strings.Contains(why, "\n'/home/u/my bin/aacpanel-exec' -heavy -- bash -c 'go test ./...'\n") {
		t.Errorf("the refusal %q", why)
	}
}

// codex calls the hook before a command of its shell with the payload of
// claude's form, tool_name Bash and the command a string, and its own
// turn_id; it takes the same refusal. It has no run_in_background, and the
// refusal does not point at it.
func TestCodexIsRefusedTheSameWithoutRunInBackground(t *testing.T) {
	withExecutable(t, "/home/u/bin/aacpanel-exec")
	payload := `{"session_id": "019a2f", "turn_id": "019a30", "transcript_path": null, "cwd": "/srv/repo",
		"hook_event_name": "PreToolUse", "model": "gpt-6-luna", "permission_mode": "default",
		"tool_name": "Bash", "tool_input": {"command": "cargo test"}, "tool_use_id": "call_7"}`
	code, out, _ := run(t, []string{"heavy-gate"}, payload)
	why := denial(t, out)
	if code != 0 || !strings.Contains(why, "/home/u/bin/aacpanel-exec -heavy -- bash -c 'cargo test'") ||
		strings.Contains(why, "run_in_background") {
		t.Errorf("exit %d, the refusal %q", code, why)
	}
}

// What is not a Bash call with a command line is let through.
func TestTheGateLetsThroughWhatItDoesNotRead(t *testing.T) {
	withExecutable(t, "/home/u/bin/aacpanel-exec")
	for _, payload := range []string{
		``, `{}`, `not json`, `[]`,
		`{"tool_name": "Read", "tool_input": {"command": "go test ./..."}}`,
		`{"tool_name": "Bash", "tool_input": {"command": ["bash", "-lc", "go test ./..."]}}`,
		`{"tool_name": "Bash", "tool_input": {}}`,
		`{"tool_name": "Bash", "tool_input": {"command": "git status"}}`,
	} {
		if code, out, errs := run(t, []string{"heavy-gate"}, payload); code != 0 || out != "" || errs != "" {
			t.Errorf("%q: exit %d, answer %q, stderr %q", payload, code, out, errs)
		}
	}
}
