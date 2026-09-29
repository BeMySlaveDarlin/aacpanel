package demo

import (
	"fmt"
	"strings"
	"time"
)

// step is a step of the made-up run: what it prints and how long it takes on
// a machine like the stand's, divided by --speed. A step with a gate does not
// run on a timer: it hands the screen to a frame or a question and goes on
// when that is answered.
type step struct {
	title string        // the heading of its entry in the feed
	verb  string        // what the spinner says while it works
	then  string        // what the spinner says in the last fifth of it
	task  int           // its line in the list of steps
	took  time.Duration // how long it takes before --speed
	out   []string      // what it prints, spread over its time
	done  []string      // the lines of its result, marks first
	// changed names what the step left on the machine, for the summary of
	// an interrupted or failed run.
	changed []string
	gate    gate
	fails   bool // --fail named it: it ends in a diagnosis, not a result
}

type gate int

const (
	noGate      gate = iota
	rootGate         // the frame of the root command and the handover of the terminal
	sessionGate      // the question of a test session
	enrollGate       // the frame of the first device
)

// The lines of the list of steps.
const (
	taskMachine = iota
	taskAnswers
	taskRoot
	taskExecutor
	taskStack
	taskWiring
	taskCheck
)

func taskTitles(a answers) []string {
	wiring := []string{"App role"}
	if a.has("testdb") {
		wiring = append(wiring, "Test database")
	}
	if a.has("tailscale") {
		wiring = append(wiring, "Tailscale")
	}
	wiring = append(wiring, "Claude settings", "Map")
	return []string{
		"Check the machine",
		"Answers",
		"Root part (one sudo)",
		"Executor",
		"Panel stack",
		strings.Join(wiring, " · "),
		"Check · First device",
	}
}

func steps(mc machine, a answers, fail string) []*step {
	uidDir := fmt.Sprintf("/run/user/%d/aacpanel-exec", mc.uid)
	list := []*step{
		{
			title: "Host description", verb: "Writing the host description", task: taskRoot, took: time.Second,
			done: []string{"✓ " + stagedHostEnv + " · 14 keys, for the root step to put in place"},
		},
		{title: "Root part", task: taskRoot, gate: rootGate,
			changed: []string{strings.Join(mc.missing, " ") + " (apt)", a.state, "aacpanel-agent@" + mc.user, "linger"}},
		{
			title: "User manager", verb: "Waiting for the user manager", task: taskExecutor, took: 2 * time.Second,
			done: []string{"✓ systemctl --user answers: running · linger keeps it up after you log out"},
		},
		{
			title: "Build the executor", verb: "Building the executor", task: taskExecutor, took: 40 * time.Second,
			out: executorBuild(), changed: []string{"~/bin/aacpanel-exec"},
			done: []string{"✓ ~/bin/aacpanel-exec · 14.1 MB · -list names 41 actions"},
		},
		{
			title: "Executor unit", verb: "Starting the executor", task: taskExecutor, took: 3 * time.Second,
			out: []string{
				"Created symlink ~/.config/systemd/user/default.target.wants/aacpanel-exec.service → ~/.config/systemd/user/aacpanel-exec.service.",
				"aacpanel-exec[48213]: listening on " + uidDir + "/sock",
			},
			changed: []string{"aacpanel-exec.service"},
			done:    []string{"✓ aacpanel-exec.service active · " + uidDir + "/sock, owner " + mc.user + ", 0700"},
		},
		{
			title: "Panel settings", verb: "Writing .env", task: taskStack, took: time.Second,
			changed: []string{"~/aacpanel/.env"},
			done:    []string{"✓ ~/aacpanel/.env from .env.example · 0600 · 11 keys set, secrets not shown"},
		},
		{
			title: "Panel stack", verb: "Building the panel image", then: "Starting the stack", task: taskStack, took: 100 * time.Second,
			out:     imageBuild(a, fail == "compose"),
			changed: []string{"compose: " + a.services()},
			done:    []string{"✓ aacpanel-db healthy in 9 s · /healthz ok in 4 s · store: connected"},
			fails:   fail == "compose",
		},
		{
			title: "App role", verb: "Creating the app role", task: taskWiring, took: 6 * time.Second,
			out: []string{
				"deploy/create-app-role.sh: CREATE ROLE monitor_app LOGIN",
				"deploy/create-app-role.sh: GRANT CONNECT ON DATABASE aacpanel TO monitor_app",
				" Container aacpanel  Recreate",
				" Container aacpanel  Started",
			},
			changed: []string{"role monitor_app"},
			done:    []string{"✓ role monitor_app · the panel reconnected under it · authentication failed: 0"},
		},
	}
	if a.has("testdb") {
		list = append(list, &step{
			title: "Test database", verb: "Starting the test database", task: taskWiring, took: 5 * time.Second,
			out:     []string{"aacpanel-test-db: listening on 127.0.0.1:55432", "AACP_TEST_DSN written to ~/aacpanel/.env"},
			changed: []string{"aacpanel-test-db"},
			done:    []string{"✓ aacpanel-test-db on 127.0.0.1:55432 · make check finds it"},
		})
	}
	if a.has("tailscale") {
		node := a.tsnode + "." + mc.tailnet
		list = append(list, &step{
			title: "Tailscale", verb: "Joining the tailnet", task: taskWiring, took: 20 * time.Second,
			out: []string{
				" Container aacpanel-tailscale  Creating",
				" Container aacpanel-tailscale  Started",
				"tailscaled: logged in as " + mc.user + "@",
				"tailscaled: BackendState Starting → Running",
				"tailscale serve: https://" + node + " → http://aacpanel:8776",
				"tailscaled: certificate for " + node + " issued",
				"TS_AUTHKEY wiped from ~/aacpanel/.env",
				" Container aacpanel  Recreate",
				" Container aacpanel  Started",
			},
			changed: []string{"tailscale node " + node},
			done:    []string{"✓ " + node + " · Running · passkeys belong to it · the auth key is wiped"},
		})
	}
	var wired []string
	for _, dir := range a.accounts {
		wired = append(wired, fmt.Sprintf("✓ %s/settings.json · question hook · status line chained · %d hooks · %d allow rules · MCP server aacpanel",
			dir, a.hooks(), a.allows()))
	}
	var mcp []string
	for _, dir := range a.accounts {
		mcp = append(mcp,
			"CLAUDE_CONFIG_DIR="+dir+" claude mcp add --scope user aacpanel -- ~/bin/aacpanel-exec -mcp",
			"Added stdio MCP server aacpanel with command: ~/bin/aacpanel-exec -mcp to user config",
		)
	}
	contours := []string{}
	for _, dir := range a.accounts {
		contours = append(contours, fmt.Sprintf("✓ contour %s (%s) · %s", a.contours[dir], dir, a.transportLine()))
	}
	if a.group != "-" && a.group != "" {
		contours = append(contours, fmt.Sprintf("✓ group %s · %s", a.group, count(len(a.projects), "project")))
	}
	list = append(list,
		&step{
			title: "Claude settings", verb: "Wiring claude", task: taskWiring, took: 3 * time.Second,
			out: mcp, changed: []string{strings.Join(a.accounts, ", ") + " settings"}, done: wired,
		},
		&step{
			title: "Map", verb: "Filling the map", task: taskWiring, took: 2 * time.Second,
			changed: []string{"map: " + a.mapLine()}, done: contours,
		},
		&step{
			title: "Check", verb: "Checking", task: taskCheck, took: 5 * time.Second,
			done: check(mc, a),
		},
		&step{title: "Test session", task: taskCheck, gate: sessionGate},
		&step{title: "First device", task: taskCheck, gate: enrollGate},
	)
	return list
}

// sessionStep is the test session, run when the person says yes to it.
func sessionStep() *step {
	return &step{
		title: "Test session", verb: "Opening aacpanel-check", task: taskCheck, took: 8 * time.Second,
		out: []string{
			`{"ask":"session.open","name":"aacpanel-check","transport":"stream"} → ok`,
			"state.json: aacpanel-check live after 3 s",
			`{"ask":"session.close","name":"aacpanel-check"} → ok`,
		},
		done: []string{
			"✓ aacpanel-check opened through the executor",
			"✓ the panel saw it in its snapshot after 3 s",
			"✓ aacpanel-check closed — only it",
		},
	}
}

func check(mc machine, a answers) []string {
	containers := "aacpanel, aacpanel-db, socket-proxy"
	if a.has("tailscale") {
		containers += ", tailscale"
	}
	last := "✓ the journal holds no error since the stack came up"
	if a.has("tailscale") {
		last = "✓ tailscale: Running, serve points at the panel"
	}
	return []string{
		"✓ /healthz ok on 127.0.0.1:8776 and on :8777",
		"✓ state.json is 3 s old",
		"✓ aacpanel-exec -list names 41 actions",
		"✓ executor socket: owner " + mc.user + " · /api/exec says available",
		"✓ database: /api/profiles 200 · authentication failed: 0",
		"✓ containers healthy: " + containers,
		"✓ aacpanel-agent@" + mc.user + " active and enabled",
		"✓ aacpanel-exec.service active and enabled · linger on",
		"✓ ask.sock in place",
		"✓ claude wiring in " + strings.Join(a.accounts, ", ") + ": hook once, status line chained, MCP server listed",
		"✓ the map has " + count(len(a.accounts), "contour"),
		last,
	}
}

// failure is how the stack step ends under --fail compose.
func failure() (diagnosis string, tail []string, more int) {
	return "/healthz did not answer in 120 s", []string{
		"aacpanel-db  | FATAL:  could not map anonymous shared memory: Cannot allocate memory",
		"aacpanel-db  | LOG:  database system is shut down",
		"aacpanel     | store: dial tcp aacpanel-db:5432: connection refused",
		"aacpanel     | store: retrying in 5s",
		"aacpanel     | store: dial tcp aacpanel-db:5432: connection refused",
	}, 20
}

func executorBuild() []string {
	mods := []string{
		"charm.land/bubbletea/v2 v2.0.10",
		"charm.land/lipgloss/v2 v2.0.6",
		"github.com/jackc/pgx/v5 v5.10.0",
		"github.com/go-webauthn/webauthn v0.17.4",
		"golang.org/x/sys v0.47.0",
		"gopkg.in/yaml.v3 v3.0.1",
		"github.com/alecthomas/chroma/v2 v2.27.0",
		"github.com/bluekeyes/go-gitdiff v0.9.0",
		"github.com/fxamacker/cbor/v2 v2.9.2",
		"github.com/golang-jwt/jwt/v5 v5.3.1",
		"github.com/google/uuid v1.6.0",
		"github.com/jackc/pgpassfile v1.0.0",
		"github.com/jackc/puddle/v2 v2.2.2",
		"golang.org/x/crypto v0.52.0",
		"golang.org/x/text v0.39.0",
		"golang.org/x/sync v0.22.0",
	}
	var out []string
	for _, m := range mods {
		out = append(out, "go: downloading "+m)
	}
	pkgs := []string{"internal/action", "internal/hostcfg", "internal/launcher", "internal/stream", "internal/executor", "internal/mcp", "cmd/aacpanel-exec"}
	for _, p := range pkgs {
		out = append(out, "aacpanel/"+p)
	}
	return append(out,
		"CGO_ENABLED=0 go build -trimpath -buildvcs=false -o ~/bin/aacpanel-exec.new ./cmd/aacpanel-exec",
		"~/bin/aacpanel-exec was not there: mv -f ~/bin/aacpanel-exec.new ~/bin/aacpanel-exec",
		"~/bin/aacpanel-exec -list: 41 actions",
	)
}

// imageBuild is the output of the image build and compose up; a failing run
// ends it where the database gives up.
func imageBuild(a answers, failing bool) []string {
	out := []string{
		"#0 building with \"default\" instance using docker driver",
		"#1 [internal] load build definition from Dockerfile",
		"#2 [internal] load metadata for docker.io/library/golang:1.26-alpine",
		"#3 [internal] load metadata for docker.io/library/alpine:3.22",
		"#4 [build 1/9] FROM docker.io/library/golang:1.26-alpine",
		"#4 resolve docker.io/library/golang:1.26-alpine done",
		"#4 sha256:7a1f… 64.2MB / 64.2MB 4.1s done",
		"#5 [build 2/9] WORKDIR /src",
		"#6 [build 3/9] COPY go.mod go.sum ./",
		"#7 [build 4/9] RUN go mod download",
		"#7 12.4s done",
		"#8 [build 5/9] COPY . .",
		"#9 [build 6/9] RUN go run ./cmd/webbuild",
		"#9 3.1s bundle web/dist/bundle.js 612.4kb",
		"#10 [build 7/9] RUN go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...",
		"#10 21.8s No vulnerabilities found.",
		"#11 [build 8/9] RUN CGO_ENABLED=0 go build -trimpath -o /aacpanel ./cmd/aacpanel",
		"#11 38.5s done",
		"#12 [stage-1 2/3] COPY --from=build /aacpanel /aacpanel",
		"#13 exporting to image",
		"#13 naming to docker.io/library/aacpanel-aacpanel done",
		" Network aacpanel_default  Created",
		" Volume aacpanel_aacpanel-db  Created",
		" Container aacpanel-socket-proxy  Created",
		" Container aacpanel-db  Created",
		" Container aacpanel  Created",
		" Container aacpanel-socket-proxy  Started",
		" Container aacpanel-db  Started",
		" Container aacpanel-db  Waiting",
		" Container aacpanel-db  Healthy",
		" Container aacpanel  Started",
		"aacpanel  | store: connected",
		"aacpanel  | listening on :8776 and on :8777",
	}
	if failing {
		for i, line := range out {
			if strings.HasSuffix(line, "aacpanel-db  Waiting") {
				_, tail, _ := failure()
				return append(out[:i+1], tail...)
			}
		}
	}
	if a.has("testdb") {
		out = append(out, " Container aacpanel-test-db  Started")
	}
	return out
}
