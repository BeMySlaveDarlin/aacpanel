package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/plan"
	"aacpanel/internal/stream"
)

const planExec = "/srv/bin/aacpanel-exec"

// planConfig is the configuration a launch hands claude when the executor
// lives at planExec.
const planConfig = `{"mcpServers":{"aacpanel":{"args":["-plan"],"command":"` + planExec + `","type":"stdio"}}}`

func stubPlanServer(t *testing.T) {
	t.Helper()
	was := planServer
	planServer = func() (string, error) { return planExec, nil }
	t.Cleanup(func() { planServer = was })
}

// planned returns the arguments a launch gives claude with the plan tool set
// the way Run sets it.
func planned(t *testing.T, launch string) Params {
	t.Helper()
	p, warns := parseParams(json.RawMessage(launch))
	if len(warns) != 0 {
		t.Fatalf("%s drew complaints: %v", launch, warns)
	}
	p, warn := withPlan(p)
	if warn != "" {
		t.Fatalf("%s: %s", launch, warn)
	}
	return p
}

// The plan tool is on unless the map turns it off, in a console and on the
// stream alike: the executor in its plan mode as the server, and the tool
// allowed. Both flags take lists, so the name follows them and ends the
// list: an opening message after them would be read as one more server.
func TestTheArgumentsHandTheSessionThePlanTool(t *testing.T) {
	stubPlanServer(t)
	want := []string{"--mcp-config", planConfig, "--allowedTools", "mcp__aacpanel__plan", "-n", "home"}

	for launch, words := range map[string][]string{
		`{}`:                                {"-n", "home"},
		`{"intent":"go"}`:                   {"-n", "home", "go"},
		`{"planTool":true,"model":"haiku"}`: {"-n", "home", "--model", "haiku"},
	} {
		got := claudeArgs("home", "", planned(t, launch))
		if !slices.Equal(got[:len(want)], want) || !slices.Equal(got[len(want)-2:], words) {
			t.Errorf("%s: console arguments %v", launch, got)
		}
		fresh := streamArgs("home", "11111111-1111-4111-8111-111111111111", "", planned(t, launch))
		at := slices.Index(fresh, "--mcp-config")
		if at < 0 || !slices.Equal(fresh[at:at+len(want)], want) {
			t.Errorf("%s: stream arguments %v", launch, fresh)
		}
	}

	off := planned(t, `{"planTool":false,"intent":"go"}`)
	for _, args := range [][]string{claudeArgs("home", "", off), streamArgs("home", "id", "", off)} {
		if slices.Contains(args, "--mcp-config") || slices.Contains(args, "--allowedTools") {
			t.Errorf("the map turned the plan tool off, and the session gets it: %v", args)
		}
	}
}

func TestAPlanToolThatIsNotASwitchIsNamed(t *testing.T) {
	p, warns := parseParams(json.RawMessage(`{"planTool":"no"}`))
	if len(warns) != 1 || !strings.Contains(warns[0], "planTool is not true/false") || !planOn(p) {
		t.Errorf("a broken switch: %+v %v", p, warns)
	}
}

func TestAnExecutorThatDoesNotKnowItsPathStartsTheSessionWithoutThePlan(t *testing.T) {
	was := planServer
	planServer = func() (string, error) { return "", fmt.Errorf("no /proc") }
	t.Cleanup(func() { planServer = was })
	p, warn := withPlan(Params{})
	if !strings.Contains(warn, "without the plan tool") || slices.Contains(claudeArgs("home", "", p), "--mcp-config") {
		t.Errorf("warned %q, arguments %v", warn, claudeArgs("home", "", p))
	}
}

// A session in a console gets the plan tool from Run itself.
func TestRunInAConsoleHandsThePlanTool(t *testing.T) {
	stubPlanServer(t)
	for launch, on := range map[string]bool{`{}`: true, `{"planTool":false}`: false} {
		t.Run(launch, func(t *testing.T) {
			proc := fakeProc(t, fproc{pid: 100, comm: "konsole", args: []string{"konsole"}})
			log := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
			fakeWindow(t)
			machineClaude(t)
			if _, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel", Launch: json.RawMessage(launch)}); err != nil {
				t.Fatal(err)
			}
			said := string(readFile(t, log))
			has := strings.Contains(said, "--mcp-config "+planConfig+" --allowedTools mcp__aacpanel__plan -n aacpanel")
			if has != on || (!on && strings.Contains(said, "--mcp-config")) {
				t.Errorf("plan tool %v, and tmux was called with %s", on, said)
			}
		})
	}
}

// A session on the stream gets it through the command its holder starts.
func TestRunOnTheStreamHandsThePlanTool(t *testing.T) {
	stubPlanServer(t)
	for launch, on := range map[string]bool{`{"transport":"stream"}`: true, `{"transport":"stream","planTool":false}`: false} {
		t.Run(launch, func(t *testing.T) {
			shortRuntime(t)
			proc := fakeProc(t)
			specPath := fakeHolder(t, "sleep 5")
			machineClaude(t)
			playHolder(t, proc, specPath)
			if _, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "demo", Launch: json.RawMessage(launch)}); err != nil {
				t.Fatal(err)
			}
			var spec stream.Spec
			if err := json.Unmarshal(readFile(t, specPath), &spec); err != nil {
				t.Fatal(err)
			}
			argv := strings.Join(spec.Argv, " ")
			has := strings.Contains(argv, "--mcp-config "+planConfig+" --allowedTools mcp__aacpanel__plan -n demo")
			if has != on || (!on && strings.Contains(argv, "--mcp-config")) {
				t.Errorf("plan tool %v, and the holder was handed %s", on, argv)
			}
		})
	}
}

// sessionFileAt writes the file claude keeps of a live session.
func sessionFileAt(t *testing.T, config string, pid int, start, id string) {
	t.Helper()
	dir := filepath.Join(config, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":"/srv/proj/lab","procStart":%q,"kind":"interactive","entrypoint":"sdk-cli"}`,
		pid, id, start)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", pid)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The plan server finds where its parent works through the file claude keeps
// of itself — in the config directory the process was started with, and only
// while the file is that process's: the place is that directory and the one
// the file names, and the conversation is the file's too.
func TestWhereIsReadFromTheFileOfTheProcess(t *testing.T) {
	const id = "8d2e9f3a-4b5c-4d6e-8f7a-9b0c1d2e3f4a"
	contour, personal := t.TempDir(), t.TempDir()
	contourDirs(t, personal)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	fakeProc(t,
		fproc{pid: 300, comm: "claude", start: "5555", env: []string{"HOME=/home/u", "CLAUDE_CONFIG_DIR=" + contour}},
		fproc{pid: 301, comm: "claude", start: "6666"},
	)

	sessionFileAt(t, contour, 300, "5555", id)
	want := plan.Binding{Place: plan.Place{ConfigDir: contour, Dir: "/srv/proj/lab"}, SessionID: id, PID: 300}
	if got, err := Where(300); err != nil || got != want {
		t.Errorf("a process in a contour: %+v %v", got, err)
	}

	// The file a dead process with the same pid left is not this one's, and a
	// process without a working directory to read is placed nowhere.
	sessionFileAt(t, contour, 300, "4444", id)
	if got, err := Where(300); err == nil {
		t.Errorf("the file a dead process with the same pid left was taken: %+v", got)
	} else if !strings.Contains(err.Error(), contour) {
		t.Errorf("the refusal does not say where it looked: %v", err)
	}

	// A process that names no directory of its own is looked for in the
	// directories of the contours.
	sessionFileAt(t, personal, 301, "6666", id)
	if got, err := Where(301); err != nil || got.Place.ConfigDir != personal || got.SessionID != id {
		t.Errorf("a process in the personal directory: %+v %v", got, err)
	}

	if _, err := Where(302); err == nil {
		t.Error("a process that is not there is placed")
	}
}

// A session asked before claude has written the file of itself — the
// handshake at its very start — is placed by what the process says of
// itself: the config directory it was started with, or the default under its
// home, and its working directory. It has no conversation yet.
func TestWhereWithoutAFileIsReadFromTheProcess(t *testing.T) {
	contour := t.TempDir()
	contourDirs(t, t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	fakeProc(t,
		fproc{pid: 400, comm: "claude", env: []string{"HOME=/home/u", "CLAUDE_CONFIG_DIR=" + contour}, cwd: "/srv/proj/lab"},
		fproc{pid: 401, comm: "claude", env: []string{"HOME=/home/u"}, cwd: "/srv/proj/home"},
	)
	if got, err := Where(400); err != nil || got != (plan.Binding{Place: plan.Place{ConfigDir: contour, Dir: "/srv/proj/lab"}, PID: 400}) {
		t.Errorf("a process in a contour: %+v %v", got, err)
	}
	if got, err := Where(401); err != nil || got != (plan.Binding{Place: plan.Place{ConfigDir: "/home/u/.claude", Dir: "/srv/proj/home"}, PID: 401}) {
		t.Errorf("a process with the default directory: %+v %v", got, err)
	}

	// Once the file is there, it is what places the process.
	sessionFileAt(t, contour, 400, "1000", "8d2e9f3a-4b5c-4d6e-8f7a-9b0c1d2e3f4a")
	if got, err := Where(400); err != nil || got.SessionID == "" {
		t.Errorf("the file written, the process is still placed without it: %+v %v", got, err)
	}
}
