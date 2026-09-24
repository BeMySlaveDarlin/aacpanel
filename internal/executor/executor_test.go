package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/launcher"
)

type fakeDocker struct {
	mu         sync.Mutex
	containers []Container
	calls      []string
	fail       map[string]string
	after      func(calls int)
	slow       time.Duration
	inFlight   int
	peak       int
}

func (f *fakeDocker) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, r *http.Request) {
		raw := make([]apiContainer, 0, len(f.containers))
		for _, c := range f.containers {
			labels := map[string]string{}
			if c.Project != "" {
				labels[labelProject] = c.Project
			}
			if c.Service != "" {
				labels[labelService] = c.Service
			}
			if len(c.DependsOn) > 0 {
				var parts []string
				for _, d := range c.DependsOn {
					parts = append(parts, d+":service_healthy:false")
				}
				labels[labelDependsOn] = strings.Join(parts, ",")
			}
			raw = append(raw, apiContainer{
				ID:     c.ID,
				Names:  []string{"/" + c.Name},
				State:  c.State,
				Labels: labels,
			})
		}
		json.NewEncoder(w).Encode(raw)
	})

	record := func(verb string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			f.mu.Lock()
			f.calls = append(f.calls, verb+" "+id)
			f.inFlight++
			if f.inFlight > f.peak {
				f.peak = f.inFlight
			}
			calls, after, slow := len(f.calls), f.after, f.slow
			f.mu.Unlock()

			defer func() {
				f.mu.Lock()
				f.inFlight--
				f.mu.Unlock()
			}()

			if after != nil {
				after(calls)
			}
			if slow > 0 {
				select {
				case <-time.After(slow):
				case <-r.Context().Done():
					return
				}
			}
			f.mu.Lock()
			msg, bad := f.fail[id]
			f.mu.Unlock()
			if bad {
				http.Error(w, msg, http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
	mux.HandleFunc("POST /containers/{id}/start", record("start"))
	mux.HandleFunc("POST /containers/{id}/stop", record("stop"))
	mux.HandleFunc("POST /containers/{id}/restart", record("restart"))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTest(t *testing.T, self string, containers ...Container) (*Executor, *fakeDocker) {
	t.Helper()
	fake := &fakeDocker{containers: containers, fail: map[string]string{}}
	srv := fake.server(t)
	e := New(NewDocker(srv.URL), self)

	e.sendSignal = func(pid int, sig syscall.Signal) error {
		if sig == 0 {
			return syscall.ESRCH
		}
		t.Fatalf("the test sends a real signal %v to process %d — replace sending via withSignals", sig, pid)
		return nil
	}
	return e, fake
}

func req(kind action.Kind, target string) action.Request {
	return action.Request{ID: "test-1", Kind: kind, Target: target}
}

func TestContainerActions(t *testing.T) {
	ctx := context.Background()

	t.Run("stop goes by id, not by name", func(t *testing.T) {
		e, fake := newTest(t, "", Container{ID: "abc123", Name: "shopapp-php-1", State: "running"})

		if _, err := e.Execute(ctx, req(action.ContainerStop, "shopapp-php-1")); err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != "stop abc123" {
			t.Errorf("calls %v, expected stop by id", fake.calls)
		}
	})

	t.Run("a container outside the list — refused without a call", func(t *testing.T) {
		e, fake := newTest(t, "", Container{ID: "abc123", Name: "shopapp-php-1", State: "running"})

		_, err := e.Execute(ctx, req(action.ContainerStop, "shopapp-php"))
		if err == nil {
			t.Fatal("a container outside the list was accepted")
		}
		if !strings.Contains(err.Error(), "shopapp-php") {
			t.Errorf("the refusal names no container: %v", err)
		}
		if len(fake.calls) != 0 {
			t.Errorf("docker was called anyway on a refusal: %v", fake.calls)
		}
	})

	t.Run("a partial name match does not count", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "db", Name: "aacpanel-db", State: "running"},
			Container{ID: "svc", Name: "aacpanel", State: "running"},
		)

		if _, err := e.Execute(ctx, req(action.ContainerRestart, "aacpanel-db")); err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != "restart db" {
			t.Errorf("calls %v, expected restart db", fake.calls)
		}
	})

	t.Run("starting an already running container does not go to docker", func(t *testing.T) {
		e, fake := newTest(t, "", Container{ID: "abc", Name: "app", State: "running"})

		detail, err := e.Execute(ctx, req(action.ContainerStart, "app"))
		if err != nil {
			t.Fatal(err)
		}
		if detail != "already running" {
			t.Errorf("reply %q, expected «already running»", detail)
		}
		if len(fake.calls) != 0 {
			t.Errorf("an extra call: %v", fake.calls)
		}
	})

	t.Run("a docker refusal reaches the human", func(t *testing.T) {
		e, fake := newTest(t, "", Container{ID: "abc", Name: "app", State: "exited"})
		fake.fail["abc"] = "no such image"

		_, err := e.Execute(ctx, req(action.ContainerStart, "app"))
		if err == nil {
			t.Fatal("the docker refusal was swallowed")
		}
		if !strings.Contains(err.Error(), "no such image") {
			t.Errorf("the reason is lost: %v", err)
		}
	})
}

func TestSelfIsProtected(t *testing.T) {
	ctx := context.Background()

	for _, kind := range []action.Kind{action.ContainerStop, action.ContainerRestart} {
		t.Run(string(kind), func(t *testing.T) {
			e, fake := newTest(t, "aacpanel", Container{ID: "svc", Name: "aacpanel", State: "running"})

			_, err := e.Execute(ctx, req(kind, "aacpanel"))
			if err == nil {
				t.Fatalf("%s on the panel itself went through", kind)
			}
			if len(fake.calls) != 0 {
				t.Errorf("docker was reached after all: %v", fake.calls)
			}
			if !strings.Contains(err.Error(), "docker "+verb(kind)+" aacpanel") {
				t.Errorf("the refusal has no command for the host: %v", err)
			}
		})
	}

	t.Run("starting itself is allowed", func(t *testing.T) {
		e, fake := newTest(t, "aacpanel", Container{ID: "svc", Name: "aacpanel", State: "exited"})

		if _, err := e.Execute(ctx, req(action.ContainerStart, "aacpanel")); err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != "start svc" {
			t.Errorf("calls %v, expected start svc", fake.calls)
		}
	})
}

func TestStackUp(t *testing.T) {
	ctx := context.Background()

	t.Run("dependencies come up before the ones that need them", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "app", Name: "shopapp-php-1", Project: "shopapp", Service: "php",
				DependsOn: []string{"mysql"}, State: "exited"},
			Container{ID: "web", Name: "shopapp-nginx-1", Project: "shopapp", Service: "nginx",
				DependsOn: []string{"php"}, State: "exited"},
			Container{ID: "db", Name: "shopapp-mysql-1", Project: "shopapp", Service: "mysql",
				State: "exited"},
		)

		if _, err := e.Execute(ctx, req(action.StackUp, "shopapp")); err != nil {
			t.Fatal(err)
		}
		want := []string{"start db", "start app", "start web"}
		if strings.Join(fake.calls, ",") != strings.Join(want, ",") {
			t.Errorf("order %v, expected %v", fake.calls, want)
		}
	})

	t.Run("a neighbouring stack is left alone", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "a", Name: "shopapp-mysql-1", Project: "shopapp", State: "exited"},
			Container{ID: "b", Name: "demo-reader-1", Project: "demo", State: "exited"},
		)

		if _, err := e.Execute(ctx, req(action.StackUp, "shopapp")); err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != "start a" {
			t.Errorf("calls %v — a neighbouring stack was touched", fake.calls)
		}
	})

	t.Run("running ones are skipped", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "a", Name: "s-1", Project: "s", State: "running"},
			Container{ID: "b", Name: "s-2", Project: "s", State: "exited"},
		)

		detail, err := e.Execute(ctx, req(action.StackUp, "s"))
		if err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != "start b" {
			t.Errorf("calls %v, expected only start b", fake.calls)
		}
		if !strings.Contains(detail, "s-2") {
			t.Errorf("the reply names no started container: %q", detail)
		}
	})

	t.Run("a stack that does not exist — refusal", func(t *testing.T) {
		e, _ := newTest(t, "", Container{ID: "a", Name: "s-1", Project: "s", State: "exited"})

		if _, err := e.Execute(ctx, req(action.StackUp, "no-such-stack")); err == nil {
			t.Fatal("a stack that does not exist was accepted")
		}
	})

	t.Run("a partial start is not passed off as success", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "ok", Name: "s-1", Project: "s", Service: "one", State: "exited"},
			Container{ID: "bad", Name: "s-2", Project: "s", Service: "two", State: "exited"},
		)
		fake.fail["bad"] = "port is already allocated"

		_, err := e.Execute(ctx, req(action.StackUp, "s"))
		if err == nil {
			t.Fatal("a partial start was passed off as success")
		}
		if !strings.Contains(err.Error(), "s-1") || !strings.Contains(err.Error(), "s-2") {
			t.Errorf("the reply does not show what came up and what did not: %v", err)
		}
		if len(fake.calls) != 2 {
			t.Errorf("starting stopped after the first refusal: %v", fake.calls)
		}
	})

	t.Run("a dependency cycle does not hang the start", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "a", Name: "c-1", Project: "c", Service: "one",
				DependsOn: []string{"two"}, State: "exited"},
			Container{ID: "b", Name: "c-2", Project: "c", Service: "two",
				DependsOn: []string{"one"}, State: "exited"},
		)

		if _, err := e.Execute(ctx, req(action.StackUp, "c")); err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 2 {
			t.Errorf("not all of them came up: %v", fake.calls)
		}
	})
}

func TestSessionActionsSayTheyAreNotReady(t *testing.T) {
	t.Setenv(launcherEnv, filepath.Join(t.TempDir(), "no-such-file"))
	t.Setenv(procEnv, t.TempDir())

	e, fake := newTest(t, "")
	for _, kind := range []action.Kind{action.SessionClose, action.SessionKill} {
		if _, err := e.Execute(context.Background(), req(kind, "aacpanel")); err == nil {
			t.Errorf("%s answered with success though it is not implemented", kind)
		}
	}
	if len(fake.calls) != 0 {
		t.Errorf("a session action went to docker: %v", fake.calls)
	}
}

func TestNoForbiddenDockerCalls(t *testing.T) {
	forbidden := map[string]string{
		"/exec":      "docker exec",
		"/images/":   "image operations",
		"/volumes/":  "volume operations",
		"/networks/": "network operations",
		"/build":     "building images",
		"/commit":    "creating images",
	}

	deletes := regexp.MustCompile(`MethodDelete|"DELETE"`)

	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no sources found — the check is useless")
	}

	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		body := withoutImports(string(raw))

		for needle, what := range forbidden {
			if strings.Contains(body, `"`+needle) || strings.Contains(body, needle+`"`) {
				t.Errorf("%s reaches %s: %s — forbidden for the executor (§7.2 of the spec)", path, needle, what)
			}
		}
		if deletes.MatchString(body) {
			t.Errorf("%s uses DELETE — deleting anything is forbidden for the executor (§7.2 of the spec)", path)
		}
	}
}

func TestOnlyKnownEndpoints(t *testing.T) {
	known := []string{
		"/containers/json?all=1",
		"/containers/",
		"/start",
		"/containers/%s/stop?t=%d",
		"/containers/%s/restart?t=%d",
	}

	raw, err := os.ReadFile("docker.go")
	if err != nil {
		t.Fatal(err)
	}

	paths := regexp.MustCompile(`"(/[a-z][a-zA-Z0-9/._?=%-]*)"`).FindAllStringSubmatch(string(raw), -1)
	if len(paths) == 0 {
		t.Fatal("no addresses found — the check is useless, the way they are written has changed")
	}
	for _, m := range paths {
		if !slices.Contains(known, m[1]) {
			t.Errorf("a new docker API address %q — add it to the list deliberately", m[1])
		}
	}
}

func TestStackDown(t *testing.T) {
	ctx := context.Background()

	stack := func() []Container {
		return []Container{
			{ID: "app", Name: "shopapp-php-1", Project: "shopapp", Service: "php",
				DependsOn: []string{"mysql"}, State: "running"},
			{ID: "web", Name: "shopapp-nginx-1", Project: "shopapp", Service: "nginx",
				DependsOn: []string{"php"}, State: "running"},
			{ID: "db", Name: "shopapp-mysql-1", Project: "shopapp", Service: "mysql",
				State: "running"},
		}
	}

	t.Run("the ones that need a dependency go down before it", func(t *testing.T) {
		e, fake := newTest(t, "", stack()...)

		if _, err := e.Execute(ctx, req(action.StackDown, "shopapp")); err != nil {
			t.Fatal(err)
		}
		want := []string{"stop web", "stop app", "stop db"}
		if strings.Join(fake.calls, ",") != strings.Join(want, ",") {
			t.Errorf("order %v, expected %v", fake.calls, want)
		}
	})

	t.Run("already stopped ones are skipped", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "a", Name: "s-1", Project: "s", State: "exited"},
			Container{ID: "b", Name: "s-2", Project: "s", State: "running"},
		)

		detail, err := e.Execute(ctx, req(action.StackDown, "s"))
		if err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != "stop b" {
			t.Errorf("calls %v, expected only stop b", fake.calls)
		}
		if !strings.Contains(detail, "s-2") {
			t.Errorf("the reply %q does not name what was stopped", detail)
		}
	})

	t.Run("a neighbouring stack is left alone", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "a", Name: "shopapp-mysql-1", Project: "shopapp", State: "running"},
			Container{ID: "b", Name: "demo-reader-1", Project: "demo", State: "running"},
		)

		if _, err := e.Execute(ctx, req(action.StackDown, "shopapp")); err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != "stop a" {
			t.Errorf("calls %v — a neighbouring stack was touched", fake.calls)
		}
	})

	t.Run("a fully stopped stack does not fake any activity", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "a", Name: "s-1", Project: "s", State: "exited"},
		)
		detail, err := e.Execute(ctx, req(action.StackDown, "s"))
		if err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 0 {
			t.Errorf("calls %v on an already stopped stack", fake.calls)
		}
		if !strings.Contains(detail, "already stopped") {
			t.Errorf("the reply %q does not say there was nothing to stop", detail)
		}
	})

	t.Run("a partial refusal stays a refusal", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "a", Name: "s-1", Project: "s", State: "running"},
			Container{ID: "b", Name: "s-2", Project: "s", State: "running"},
		)
		fake.fail["b"] = "device or resource busy"

		detail, err := e.Execute(ctx, req(action.StackDown, "s"))
		if err == nil {
			t.Fatalf("a partial stop passed for success: %q", detail)
		}
		if !strings.Contains(err.Error(), "s-1") || !strings.Contains(err.Error(), "s-2") {
			t.Errorf("the error %q lacks both sides: what stopped and what did not", err)
		}
		if !slices.Contains(fake.calls, "stop a") {
			t.Errorf("calls %v — one refusal stopped the walk", fake.calls)
		}
	})

	t.Run("a stack that does not exist", func(t *testing.T) {
		e, _ := newTest(t, "", Container{ID: "a", Name: "s-1", Project: "s", State: "running"})
		if _, err := e.Execute(ctx, req(action.StackDown, "nosuchstack")); err == nil {
			t.Error("stopping a stack that does not exist succeeded")
		}
	})

	t.Run("time ran out — we say who is left", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "a", Name: "s-1", Project: "s", State: "running"},
			Container{ID: "b", Name: "s-2", Project: "s", State: "running"},
			Container{ID: "c", Name: "s-3", Project: "s", State: "running"},
		)
		short, cancel := context.WithCancel(ctx)
		fake.after = func(calls int) {
			if calls == 1 {
				cancel()
			}
		}
		defer cancel()

		_, err := e.Execute(short, req(action.StackDown, "s"))
		if err == nil {
			t.Fatal("a stop cut off halfway passed for success")
		}
		if !strings.Contains(err.Error(), "not enough time") {
			t.Errorf("the error %q does not explain that time ran out", err)
		}
		if !strings.Contains(err.Error(), "s-2") || !strings.Contains(err.Error(), "s-1") {
			t.Errorf("the error %q names neither the stopped ones nor the ones left running", err)
		}
	})

	t.Run("a layer goes down at once, not one by one", func(t *testing.T) {
		var stack []Container
		for i := range 5 {
			stack = append(stack, Container{
				ID: fmt.Sprintf("id-%d", i), Name: fmt.Sprintf("d-%d", i),
				Project: "d", Service: fmt.Sprintf("s-%d", i), State: "running",
			})
		}
		e, fake := newTest(t, "", stack...)
		fake.slow = 200 * time.Millisecond

		short, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
		defer cancel()

		detail, err := e.Execute(short, req(action.StackDown, "d"))
		if err != nil {
			t.Fatalf("the stack did not go down in a single action: %v", err)
		}
		for i := range 5 {
			if !strings.Contains(detail, fmt.Sprintf("d-%d", i)) {
				t.Errorf("the reply %q has no container d-%d", detail, i)
			}
		}
		fake.mu.Lock()
		peak := fake.peak
		fake.mu.Unlock()
		if peak < 2 {
			t.Errorf("%d stop at the peak — the layer went down one by one", peak)
		}
	})

	t.Run("the order between layers is kept", func(t *testing.T) {
		e, fake := newTest(t, "",
			Container{ID: "db", Name: "p-db", Project: "p", Service: "db", State: "running"},
			Container{ID: "app", Name: "p-app", Project: "p", Service: "app",
				DependsOn: []string{"db"}, State: "running"},
			Container{ID: "web", Name: "p-web", Project: "p", Service: "web",
				DependsOn: []string{"app"}, State: "running"},
		)
		fake.slow = 50 * time.Millisecond

		if _, err := e.Execute(ctx, req(action.StackDown, "p")); err != nil {
			t.Fatalf("the stack did not go down: %v", err)
		}
		fake.mu.Lock()
		calls, peak := slices.Clone(fake.calls), fake.peak
		fake.mu.Unlock()

		want := []string{"stop web", "stop app", "stop db"}
		if !slices.Equal(calls, want) {
			t.Errorf("stop order %v, expected %v", calls, want)
		}
		if peak != 1 {
			t.Errorf("%d stops at the peak — linked containers went down at once", peak)
		}
	})

	t.Run("a stack holding the panel is not stopped", func(t *testing.T) {
		e, fake := newTest(t, "aacpanel",
			Container{ID: "svc", Name: "aacpanel", Project: "aacpanel", Service: "aacpanel",
				DependsOn: []string{"db"}, State: "running"},
			Container{ID: "db", Name: "aacpanel-db", Project: "aacpanel", Service: "db", State: "running"},
			Container{ID: "px", Name: "aacpanel-socket-proxy", Project: "aacpanel", Service: "proxy",
				State: "running"},
		)

		_, err := e.Execute(ctx, req(action.StackDown, "aacpanel"))
		if err == nil {
			t.Fatal("the stack holding the panel was stopped — there would be nobody left to report the result")
		}
		if len(fake.calls) != 0 {
			t.Errorf("docker was reached before the refusal: %v", fake.calls)
		}
		if !strings.Contains(err.Error(), "docker compose") {
			t.Errorf("the refusal %q has no hint on how to do this from the host", err)
		}
	})

	t.Run("protecting the panel does not get in the way of another stack", func(t *testing.T) {
		e, fake := newTest(t, "aacpanel",
			Container{ID: "svc", Name: "aacpanel", Project: "aacpanel", State: "running"},
			Container{ID: "a", Name: "demo-reader-1", Project: "demo", State: "running"},
		)
		if _, err := e.Execute(ctx, req(action.StackDown, "demo")); err != nil {
			t.Fatal(err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != "stop a" {
			t.Errorf("calls %v, expected only stop a", fake.calls)
		}
	})
}

func withoutImports(src string) string {
	i := strings.Index(src, "\nimport (")
	if i < 0 {
		return src
	}
	rest := src[i+len("\nimport ("):]
	j := strings.Index(rest, "\n)")
	if j < 0 {
		return src
	}
	return src[:i] + rest[j:]
}

func trustFile(t *testing.T, trusted map[string]bool) {
	t.Helper()
	home := t.TempDir()
	type entry struct {
		Trusted bool `json:"hasTrustDialogAccepted"`
	}
	projects := map[string]entry{}
	for dir, ok := range trusted {
		projects[dir] = entry{Trusted: ok}
	}
	body, err := json.Marshal(map[string]any{"projects": projects})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
}

func fakeLauncher(t *testing.T, rep launcher.Report) string {
	t.Helper()
	body, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	return launcherScript(t, "{ echo \"$@\"; cat; echo; env | grep '^CLAUDE' | wc -l; } > %s\n"+
		"cat <<'END'\n"+string(body)+"\nEND\n")
}

func failLauncher(t *testing.T, reason string) string {
	t.Helper()
	return launcherScript(t, "{ echo \"$@\"; cat; } > %s\n"+
		"echo "+strconv.Quote(reason)+" >&2\nexit 1\n")
}

func launcherScript(t *testing.T, tmpl string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "task")
	script := filepath.Join(dir, "aacpanel-exec")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+fmt.Sprintf(tmpl, log)), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(launcherEnv, script)
	fakeSystemdRun(t)
	return log
}

func fakeSystemdRun(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := filepath.Join(dir, "systemd-run")
	body := `#!/bin/sh
printf '%s\n' "$@" > ` + log + `
while [ $# -gt 0 ]; do
	case "$1" in
		--setenv=*) export "${1#--setenv=}" ;;
		--) shift; break ;;
	esac
	shift
done
exec "$@"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(systemdRunEnv, script)
	return log
}

func TestSessionOpenHome(t *testing.T) {
	ctx := context.Background()

	const homeSession = "stand-home"

	ok := launcher.Report{Session: homeSession, Konsole: 1234, Agent: 1235}

	trustHome := func(t *testing.T) string {
		t.Helper()
		t.Setenv(hostcfg.HomeSessionEnv, homeSession)
		home := t.TempDir()
		body, err := json.Marshal(map[string]any{
			"projects": map[string]any{home: map[string]any{"hasTrustDialogAccepted": true}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude.json"), body, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)
		return home
	}

	open := func(t *testing.T, target func(home string) string) (string, string, string) {
		t.Helper()
		home := trustHome(t)
		log := fakeLauncher(t, ok)
		e, _ := newTest(t, "")
		detail, err := e.Execute(ctx, req(action.SessionOpen, target(home)))
		if err != nil {
			t.Fatalf("target %q: %v", target(home), err)
		}
		args, readErr := os.ReadFile(log)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return detail, string(args), home
	}

	t.Run("by the name of the home session", func(t *testing.T) {
		_, args, home := open(t, func(string) string { return homeSession })
		if !strings.Contains(args, home) {
			t.Errorf("the launcher was called with another directory, not home: %s", args)
		}
		if !strings.Contains(args, `"session":"`+homeSession+`"`) {
			t.Errorf("the session name was not passed to the launcher: %s", args)
		}
	})

	t.Run("by the directory name", func(t *testing.T) {
		_, args, home := open(t, func(home string) string { return filepath.Base(home) })
		if !strings.Contains(args, home) {
			t.Errorf("home was not found by the directory name: %s", args)
		}
	})

	t.Run("with a counting suffix", func(t *testing.T) {
		_, args, home := open(t, func(string) string { return homeSession + "-2" })
		if !strings.Contains(args, home) {
			t.Errorf("the suffix was not trimmed and home was not found: %s", args)
		}
	})
}

func TestFindProjectKeepsNamesWithDashes(t *testing.T) {
	list := []project{{Session: "chatbot", Path: "/opt/x"}, {Session: "aacpanel", Path: "/opt/y"}}

	if _, err := findProject(list, "chatbot"); err != nil {
		t.Errorf("a name with dashes was not found: %v", err)
	}
	if p, err := findProject(list, "aacpanel-3"); err != nil || p.Path != "/opt/y" {
		t.Errorf("the third session of a project did not find its project: %v", err)
	}
	if _, err := findProject(list, "tg-ai"); err == nil {
		t.Error("trimming at the dash found a project nobody asked for")
	}
}

func TestSessionOpen(t *testing.T) {
	ctx := context.Background()

	ok := launcher.Report{Session: "aacpanel", Konsole: 1234, Agent: 1235}

	_, dir := launchDir(t, "aacpanel")
	open := func() action.Request {
		return openWith(action.SessionOpen, &action.Project{Path: dir, Session: "aacpanel"}, "")
	}

	t.Run("brings the project up and names the session", func(t *testing.T) {
		trustFile(t, map[string]bool{dir: true})
		log := fakeLauncher(t, ok)
		e, _ := newTest(t, "")

		detail, err := e.Execute(ctx, open())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "aacpanel") {
			t.Errorf("the reply %q has no session name", detail)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), dir) {
			t.Errorf("the launcher was called with %q — the path must come from the map", string(raw))
		}
	})

	t.Run("the session comes up in a clean namespace", func(t *testing.T) {
		trustFile(t, map[string]bool{dir: true})
		t.Setenv(hostcfg.DisplayEnv, ":9")
		fakeLauncher(t, ok)
		log := fakeSystemdRun(t)
		e, _ := newTest(t, "")

		if _, err := e.Execute(ctx, open()); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("the wrapper was not called at all — the launcher went as a direct start: %v", err)
		}
		all := strings.Split(strings.TrimSpace(string(raw)), "\n")
		var args, envs []string
		for _, a := range all {
			if name, ok := strings.CutPrefix(a, "--setenv="); ok {
				key, _, _ := strings.Cut(name, "=")
				envs = append(envs, key)
				continue
			}
			args = append(args, a)
		}

		has := func(want string) bool {
			for _, a := range args {
				if a == want {
					return true
				}
			}
			return false
		}

		for _, key := range envs {
			if key == "GITLAB_TOKEN" || strings.Contains(key, "TOKEN") ||
				strings.Contains(key, "SECRET") || strings.Contains(key, "PASSWORD") {
				t.Errorf("the variable %s went into the systemd-run command line — anyone sees it in ps", key)
			}
		}
		if has("--scope") {
			t.Error("--scope does not break the namespace — a transient service is needed")
		}
		if !has("--user") {
			t.Errorf("no --user: the unit goes to the system manager, which has neither DISPLAY nor a session: %v", args)
		}
		if !has("--property=KillMode=process") {
			t.Errorf("no KillMode=process: konsole dies together with the launcher: %v", args)
		}
		if !has("--pipe") {
			t.Errorf("no --pipe: the task will not reach the launcher: %v", args)
		}
		var display bool
		for _, key := range envs {
			display = display || key == "DISPLAY"
		}
		if !display {
			t.Errorf("DISPLAY did not arrive: konsole will not find the screen, only %v was passed", envs)
		}
	})

	t.Run("the CLAUDE* environment does not reach the launcher", func(t *testing.T) {
		trustFile(t, map[string]bool{dir: true})
		log := fakeLauncher(t, ok)
		t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
		t.Setenv("CLAUDECODE", "1")
		e, _ := newTest(t, "")

		if _, err := e.Execute(ctx, open()); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Fields(strings.TrimSpace(string(raw)))
		if got := lines[len(lines)-1]; got != "0" {
			t.Errorf("%s CLAUDE* variables reached the launcher — the transcript will not be written", got)
		}
	})

	t.Run("the suffix for a taken name comes from the launcher", func(t *testing.T) {
		trustFile(t, map[string]bool{dir: true})
		fakeLauncher(t, launcher.Report{Session: "aacpanel-2", Konsole: 999, Agent: 1000})
		e, _ := newTest(t, "")

		detail, err := e.Execute(ctx, open())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "aacpanel-2") {
			t.Errorf("the reply %q lost the suffixed name, the app will show the wrong session", detail)
		}
	})

	t.Run("an environment leak makes it into the reply", func(t *testing.T) {
		trustFile(t, map[string]bool{dir: true})
		fakeLauncher(t, launcher.Report{
			Session: "aacpanel", Konsole: 1234, Agent: 1235,
			Leaks: []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID"},
		})
		e, _ := newTest(t, "")

		detail, err := e.Execute(ctx, open())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "leaked") || !strings.Contains(detail, "transcript") {
			t.Errorf("the reply %q says nothing about the leak — the session came up but will write no history", detail)
		}
	})

	t.Run("the launcher refused — the refusal arrives whole", func(t *testing.T) {
		trustFile(t, map[string]bool{dir: true})
		failLauncher(t, "konsole did not come up (DISPLAY=)")
		e, _ := newTest(t, "")

		_, err := e.Execute(ctx, open())
		if err == nil {
			t.Fatal("a refusal from the launcher passed for success")
		}
		if !strings.Contains(err.Error(), "DISPLAY") {
			t.Errorf("the error %q carries no reason from the launcher", err)
		}
	})
}

type fakeProc struct {
	pid   int
	comm  string
	args  []string
	ppid  int
	cwd   string
	start string
}

func procFS(t *testing.T, procs ...fakeProc) {
	t.Helper()
	root := t.TempDir()
	for _, p := range procs {
		dir := filepath.Join(root, strconv.Itoa(p.pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write := func(name, body string) {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("comm", p.comm+"\n")
		write("cmdline", strings.Join(p.args, "\x00")+"\x00")
		start := p.start
		if start == "" {
			start = "1000"
		}
		fields := make([]string, 0, 20)
		fields = append(fields, "S", strconv.Itoa(p.ppid))
		for len(fields) < 19 {
			fields = append(fields, "0")
		}
		fields = append(fields, start)
		write("stat", fmt.Sprintf("%d (%s) %s", p.pid, p.comm, strings.Join(fields, " ")))
		if p.cwd != "" {
			if err := os.Symlink(p.cwd, filepath.Join(dir, "cwd")); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv(procEnv, root)
}

type fakeSession struct {
	pid    int
	name   string
	start  string
	socket string
	status string
	cwd    string
	kind   string
	sid    string
	// startedAt is the start of the session in milliseconds, as claude writes it.
	startedAt int64
}

func sessionFiles(t *testing.T, files ...fakeSession) {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		status := f.status
		if status == "" {
			status = "idle"
		}
		cwd := f.cwd
		if cwd == "" {
			cwd = "/opt/x"
		}
		kind := f.kind
		if kind == "" {
			kind = "interactive"
		}
		sid := f.sid
		if sid == "" {
			sid = fmt.Sprintf("s-%d", f.pid)
		}
		body := fmt.Sprintf(
			`{"pid":%d,"sessionId":%q,"cwd":%q,"name":%q,"procStart":%q,"kind":%q,`+
				`"messagingSocketPath":%q,"status":%q}`,
			f.pid, sid, cwd, f.name, f.start, kind, f.socket, status)
		if f.startedAt != 0 {
			body = strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"startedAt":%d}`, f.startedAt)
		}
		if err := os.WriteFile(filepath.Join(root, strconv.Itoa(f.pid)+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(sessionsEnv, root)
}

type signalLog struct {
	sent     []string
	alive    map[int]bool
	dieAfter map[int]int
}

func (l *signalLog) send(pid int, sig syscall.Signal) error {
	if sig == 0 {
		if n, ok := l.dieAfter[pid]; ok {
			if n <= 0 {
				return syscall.ESRCH
			}
			l.dieAfter[pid] = n - 1
		}
		if l.alive[pid] {
			return nil
		}
		return syscall.ESRCH
	}
	l.sent = append(l.sent, fmt.Sprintf("%d:%v", pid, sig))
	return nil
}

func withSignals(t *testing.T, e *Executor, alive map[int]bool, dieAfter map[int]int) *signalLog {
	t.Helper()
	log := &signalLog{alive: alive, dieAfter: dieAfter}
	e.sendSignal = log.send
	e.poll = time.Millisecond
	e.soft = 20 * time.Millisecond
	return log
}

func TestSessionNameFromSessionFile(t *testing.T) {
	ctx := context.Background()

	t.Run("the name comes from the file, not from the command line", func(t *testing.T) {
		procFS(t,
			fakeProc{pid: 2001, comm: "claude", args: []string{"claude", "-n", "old-name"},
				ppid: 1, cwd: "/srv/proj/Beta/rnd/probe", start: "7788"},
		)
		sessionFiles(t, fakeSession{pid: 2001, name: "new-name", start: "7788"})
		e, _ := newTest(t, "")
		withSignals(t, e, map[int]bool{2001: true}, map[int]int{2001: 1})

		if _, err := e.Execute(ctx, req(action.SessionClose, "new-name")); err != nil {
			t.Fatalf("the session was not found under the name from the file: %v", err)
		}
	})

	t.Run("a file left by a dead namesake does not count", func(t *testing.T) {
		procFS(t,
			fakeProc{pid: 2002, comm: "claude", args: []string{"claude", "-n", "alive"},
				ppid: 1, cwd: "/srv/proj/Beta/rnd/probe", start: "9999"},
		)
		sessionFiles(t, fakeSession{pid: 2002, name: "deceased", start: "1111"})
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{2002: true}, map[int]int{2002: 1})

		_, err := e.Execute(ctx, req(action.SessionClose, "deceased"))
		if err == nil {
			t.Fatal("a session was closed by a name taken from a foreign file")
		}
		if len(log.sent) > 0 {
			t.Errorf("signals %v went out — to a process that reused the number", log.sent)
		}
		if _, err := e.Execute(ctx, req(action.SessionClose, "alive")); err != nil {
			t.Fatalf("the fallback through -n did not work: %v", err)
		}
	})

	t.Run("a one-shot run does not count as a session", func(t *testing.T) {
		procFS(t,
			fakeProc{pid: 2003, comm: "claude", args: []string{"claude", "-p", "--output-format", "json"},
				ppid: 1, cwd: "/srv/proj/Beta/rnd/probe", start: "4242"},
			fakeProc{pid: 2004, comm: "claude", args: []string{"claude", "-n", "real-one"},
				ppid: 1, cwd: "/srv/proj/Beta/rnd/probe", start: "4343"},
		)
		sessionFiles(t, fakeSession{pid: 2003, name: "tmp-8b", start: "4242"})
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{2003: true, 2004: true}, map[int]int{2004: 1})

		_, err := e.Execute(ctx, req(action.SessionClose, "tmp-8b"))
		if err == nil {
			t.Fatal("a one-shot run was closed as a session")
		}
		if strings.Contains(err.Error(), "tmp-8b,") || strings.Contains(err.Error(), ", tmp-8b") {
			t.Errorf("a one-shot run got into the list of running sessions: %v", err)
		}
		if len(log.sent) > 0 {
			t.Errorf("signals %v went out — to a one-shot run", log.sent)
		}
	})
}

func TestSessionClose(t *testing.T) {
	ctx := context.Background()

	stand := func(t *testing.T) {
		t.Helper()
		procFS(t,
			fakeProc{pid: 1000, comm: "konsole", args: []string{"konsole"}, ppid: 1},
			fakeProc{pid: 1001, comm: "claude", args: []string{"claude", "-n", "probe-2", "--remote-control", "probe-2"},
				ppid: 1000, cwd: "/srv/proj/Beta/rnd/probe"},
			fakeProc{pid: 1003, comm: "konsole", args: []string{"konsole", "--workdir", "/srv/proj/Beta/rnd/probe"}, ppid: 1},
			fakeProc{pid: 1002, comm: "bash", args: []string{"bash"}, ppid: 1003},
			fakeProc{pid: 1004, comm: "claude", args: []string{"claude", "-n", "probe", "--remote-control", "probe"},
				ppid: 1002, cwd: "/srv/proj/Beta/rnd/probe"},
		)
	}

	t.Run("softly stops konsole and the agent", func(t *testing.T) {
		stand(t)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{1004: true, 1003: true}, map[int]int{1004: 1})

		detail, err := e.Execute(ctx, req(action.SessionClose, "probe"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "probe") || !strings.Contains(detail, "transcript") {
			t.Errorf("the reply %q does not say the transcript was flushed", detail)
		}
		want := []string{"1003:terminated", "1004:terminated"}
		if strings.Join(log.sent, ",") != strings.Join(want, ",") {
			t.Errorf("signals %v, expected %v", log.sent, want)
		}
	})

	t.Run("stops the tmux session as well", func(t *testing.T) {
		stand(t)
		e, _ := newTest(t, "")
		withSignals(t, e, map[int]bool{1004: true, 1003: true}, map[int]int{1004: 1})
		tmuxLog := fakeTmux(t, []string{"1004 probe:0.0"}, "")

		if _, err := e.Execute(ctx, req(action.SessionClose, "probe")); err != nil {
			t.Fatal(err)
		}

		called := strings.Join(tmuxArgv(t, tmuxLog), " ")
		if !strings.Contains(called, "kill-session -t probe") {
			t.Errorf("the tmux session was not stopped (calls: %s) — the terminal would hang there empty", called)
		}
	})

	t.Run("it does not finish off what it did not wait for", func(t *testing.T) {
		stand(t)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{1004: true, 1003: true}, nil)

		_, err := e.Execute(ctx, req(action.SessionClose, "probe"))
		if err == nil {
			t.Fatal("the session did not close, yet the action passed for success")
		}
		if !strings.Contains(err.Error(), "kill -9") {
			t.Errorf("the error %q does not hint at what to do next", err)
		}
		for _, s := range log.sent {
			if strings.HasSuffix(s, ":killed") {
				t.Errorf("the soft close moved to KILL on its own: %v", log.sent)
			}
		}
	})

	t.Run("a neighbouring session of the same project is untouched", func(t *testing.T) {
		stand(t)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{1004: true, 1003: true}, map[int]int{1004: 1})

		if _, err := e.Execute(ctx, req(action.SessionClose, "probe")); err != nil {
			t.Fatal(err)
		}
		for _, s := range log.sent {
			if strings.HasPrefix(s, "1001:") || strings.HasPrefix(s, "1000:") {
				t.Errorf("a neighbouring session was touched: %v", log.sent)
			}
		}
	})

	t.Run("the name is not confused with a similar one", func(t *testing.T) {
		stand(t)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{1001: true, 1000: true}, map[int]int{1001: 1})

		if _, err := e.Execute(ctx, req(action.SessionClose, "probe-2")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(log.sent, ","), "1001:") {
			t.Errorf("signals %v — the wrong session was closed", log.sent)
		}
		for _, s := range log.sent {
			if strings.HasPrefix(s, "1004:") {
				t.Errorf("a session with a similar name was caught in the sweep: %v", log.sent)
			}
		}
	})

	t.Run("an unknown session — a refusal with the list", func(t *testing.T) {
		stand(t)
		e, _ := newTest(t, "")
		withSignals(t, e, map[int]bool{}, nil)

		_, err := e.Execute(ctx, req(action.SessionClose, "nosuchsession"))
		if err == nil {
			t.Fatal("a session that does not exist was closed")
		}
		if !strings.Contains(err.Error(), "probe") {
			t.Errorf("the refusal %q has no list of running sessions", err)
		}
	})

	t.Run("the main host session is not closed", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		procFS(t,
			fakeProc{pid: 300, comm: "konsole", args: []string{"konsole"}, ppid: 1},
			fakeProc{pid: 302, comm: "claude", args: []string{"claude", "-n", "host"}, ppid: 300, cwd: home},
		)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{302: true}, nil)

		_, err := e.Execute(ctx, req(action.SessionClose, "host"))
		if err == nil {
			t.Fatal("the main host session was closed")
		}
		if len(log.sent) != 0 {
			t.Errorf("signals were sent before the refusal: %v", log.sent)
		}
	})

	t.Run("a nameless session is not found", func(t *testing.T) {
		procFS(t,
			fakeProc{pid: 400, comm: "claude", args: []string{"claude"}, ppid: 1, cwd: "/srv/proj/x"},
		)
		e, _ := newTest(t, "")
		withSignals(t, e, map[int]bool{400: true}, nil)

		if _, err := e.Execute(ctx, req(action.SessionClose, "claude")); err == nil {
			t.Error("a session without a name was found")
		}
	})
}

func TestSessionKill(t *testing.T) {
	ctx := context.Background()

	t.Run("it finishes off and says honestly what happens to the transcript", func(t *testing.T) {
		procFS(t,
			fakeProc{pid: 100, comm: "konsole", args: []string{"konsole"}, ppid: 1},
			fakeProc{pid: 102, comm: "claude", args: []string{"claude", "-n", "probe"}, ppid: 100,
				cwd: "/srv/proj/Beta/rnd/probe"},
		)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{1004: true, 1003: true}, map[int]int{1004: 1})

		detail, err := e.Execute(ctx, req(action.SessionKill, "probe"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "transcript") {
			t.Errorf("the reply %q says nothing about the history possibly being lost", detail)
		}
		want := []string{"100:killed", "102:killed"}
		if strings.Join(log.sent, ",") != strings.Join(want, ",") {
			t.Errorf("signals %v, expected %v", log.sent, want)
		}
	})

	t.Run("the main host session is not killed", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		procFS(t,
			fakeProc{pid: 302, comm: "claude", args: []string{"claude", "-n", "host"}, ppid: 1, cwd: home},
		)
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{302: true}, nil)

		if _, err := e.Execute(ctx, req(action.SessionKill, "host")); err == nil {
			t.Fatal("the main host session was killed")
		}
		if len(log.sent) != 0 {
			t.Errorf("signals were sent before the refusal: %v", log.sent)
		}
	})
}

func TestSessionRefusesOwnBranch(t *testing.T) {
	self := os.Getpid()
	parent, ok := procParentReal(self)
	if !ok {
		t.Skip("the parent of our own process could not be read")
	}

	procFS(t,
		fakeProc{pid: parent, comm: "claude", args: []string{"claude", "-n", "our-own"}, ppid: 1,
			cwd: "/srv/proj/Beta/service/aacpanel"},
		fakeProc{pid: self, comm: "aacpanel-exec", args: []string{"aacpanel-exec"}, ppid: parent},
	)
	e, _ := newTest(t, "")
	log := withSignals(t, e, map[int]bool{parent: true}, nil)

	for _, kind := range []action.Kind{action.SessionClose, action.SessionKill} {
		if _, err := e.Execute(context.Background(), req(kind, "our-own")); err == nil {
			t.Errorf("%s closed the session the executor itself runs in", kind)
		}
	}
	if len(log.sent) != 0 {
		t.Errorf("signals were sent before the refusal: %v", log.sent)
	}
}

func procParentReal(pid int) (int, bool) {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	line := string(raw)
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(line[i+1:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}

func TestNoLeftoverMutations(t *testing.T) {
	root := filepath.Join("..", "..")
	bad := regexp.MustCompile(`\bif (false|true) \{`)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "node_modules" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if bad.MatchString(line) {
				t.Errorf("%s:%d — a disabled branch %q: looks like a leftover from a mutation check",
					path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the sources: %v", err)
	}
}

func TestSignalRefusesSelfAndAncestors(t *testing.T) {
	e, _ := newTest(t, "")

	self := os.Getpid()
	parent := os.Getppid()

	for _, c := range []struct {
		what string
		pid  int
	}{
		{"our own process", self},
		{"the parent", parent},
	} {
		t.Run(c.what, func(t *testing.T) {
			if c.pid <= 1 {
				t.Skip("nothing to check: the process is an orphan")
			}
			err := e.signal(c.pid, syscall.SIGTERM)
			if err == nil {
				t.Fatalf("%s (%d) would have got TERM", c.what, c.pid)
			}
			if !strings.Contains(err.Error(), "ancestor") {
				t.Errorf("the refusal %q does not explain the reason", err)
			}
		})
	}

	t.Run("the liveness check is not forbidden", func(t *testing.T) {
		if err := e.signal(self, 0); err != nil && !strings.Contains(err.Error(), "no such process") {
			if strings.Contains(err.Error(), "ancestor") {
				t.Errorf("the liveness check was rejected by the invariant: %v", err)
			}
		}
	})
}

func TestSessionResume(t *testing.T) {
	ctx := context.Background()

	ok := launcher.Report{Session: "aacpanel-2", Konsole: 1234, Agent: 1235}

	const uuid = "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"
	_, dir := launchDir(t, "aacpanel")
	project := func() *action.Project { return &action.Project{Path: dir, Session: "aacpanel"} }

	t.Run("the launcher is called with the conversation id", func(t *testing.T) {
		trustFile(t, map[string]bool{dir: true})
		log := fakeLauncher(t, ok)
		e, _ := newTest(t, "")

		detail, err := e.Execute(ctx, openWith(action.SessionResume, project(), uuid))
		if err != nil {
			t.Fatal(err)
		}

		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		args := string(raw)
		if !strings.Contains(args, `"resume":"`+uuid+`"`) {
			t.Errorf("the launcher was called with %q — without the uuid an empty session comes up", args)
		}
		if !strings.Contains(args, dir) {
			t.Errorf("the launcher was called with %q — the path must come from the map", args)
		}

		if !strings.Contains(detail, uuid) {
			t.Errorf("the reply %q has no conversation id — in the action log it is no different from a new session", detail)
		}
	})

	t.Run("an ordinary open goes without the flag", func(t *testing.T) {
		trustFile(t, map[string]bool{dir: true})
		log := fakeLauncher(t, ok)
		e, _ := newTest(t, "")

		if _, err := e.Execute(ctx, openWith(action.SessionOpen, project(), "")); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"resume"`) {
			t.Errorf("opening a console called the launcher with %q — a new session must resume nothing", string(raw))
		}
	})
}

func listenFake(t *testing.T) (path string, got chan string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "sock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	path = filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	got = make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadString('\n')
			got <- line
			conn.Close()
		}
	}()
	return path, got
}

func TestSessionSendDelivers(t *testing.T) {
	socket, got := listenFake(t)
	procFS(t, fakeProc{pid: 300, comm: "claude", args: []string{"claude"}, start: "77"})
	sessionFiles(t, fakeSession{pid: 300, name: "aacpanel", start: "77", socket: socket, status: "busy"})

	e := &Executor{}
	detail, err := e.sessionSend(t.Context(), "aacpanel", "check the stack logs")
	if err != nil {
		t.Fatalf("sending failed: %v", err)
	}

	var line string
	select {
	case line = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("nothing reached the session socket")
	}

	var msg struct {
		Type    string `json:"type"`
		From    string `json:"from"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		t.Fatalf("the protocol line was not parsed: %v (%q)", err, line)
	}
	if msg.Type != "user" || msg.Message.Role != "user" {
		t.Errorf("type %q, role %q — claude will not accept such a message", msg.Type, msg.Message.Role)
	}
	if msg.From == "" {
		t.Error("there is no sender signature: the receiving log will show «unknown»")
	}
	if msg.Message.Content != "check the stack logs" {
		t.Errorf("the text did not arrive verbatim: %q — the model reads service lines as an address", msg.Message.Content)
	}
	if !strings.Contains(detail, "busy") {
		t.Errorf("detail %q does not say the session is busy", detail)
	}
	if !strings.Contains(detail, "20") {
		t.Errorf("detail %q does not name the length — that is exactly what goes into the action log", detail)
	}
}

func TestSessionStopSendsEsc(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 700, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 701, comm: "claude", args: []string{"claude"}, ppid: 700, start: "88"},
	)
	sessionFiles(t, fakeSession{pid: 701, name: "aacpanel", start: "88", socket: socket, status: "busy"})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 701})

	e := &Executor{}
	detail, err := e.sessionStop(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("stopping failed: %v", err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	sent := string(raw)
	if sent != escKey {
		t.Errorf("%q went out instead of a bare Esc — inside a paste wrapper it turns into a character in the field", sent)
	}
	if !strings.Contains(detail, "interrupted") || !strings.Contains(detail, "queue") {
		t.Errorf("detail %q does not name what exactly was cut off", detail)
	}
}

func TestSessionStopTellsWhenNothingRuns(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 710, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 711, comm: "claude", args: []string{"claude"}, ppid: 710, start: "89"},
	)
	sessionFiles(t, fakeSession{pid: 711, name: "aacpanel", start: "89", socket: socket, status: "idle"})
	fakeBusctl(t, map[string]int{"/Sessions/1": 711})

	e := &Executor{}
	detail, err := e.sessionStop(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("stopping failed: %v", err)
	}
	if !strings.Contains(detail, "nothing to interrupt") {
		t.Errorf("detail %q lies about work being interrupted", detail)
	}
}

func TestSessionStopRefusesWithoutKonsole(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t, fakeProc{pid: 720, comm: "claude", args: []string{"claude"}, start: "90"})
	sessionFiles(t, fakeSession{pid: 720, name: "aacpanel", start: "90", socket: socket, status: "busy"})

	e := &Executor{}
	if _, err := e.sessionStop(t.Context(), "aacpanel"); err == nil {
		t.Fatal("stopping succeeded where there is no konsole")
	} else if !strings.Contains(err.Error(), "Esc is a key") {
		t.Errorf("the error %q does not explain why the fallback channel does not work here", err)
	}
}

func TestSessionSendRefusesUnknown(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t, fakeProc{pid: 301, comm: "claude", args: []string{"claude"}, start: "78"})
	sessionFiles(t, fakeSession{pid: 301, name: "aacpanel-2", start: "78", socket: socket})

	e := &Executor{}
	if _, err := e.sessionSend(t.Context(), "aacpanel", "hello"); err == nil {
		t.Fatal("sending to a session that does not exist succeeded")
	} else if !strings.Contains(err.Error(), "aacpanel-2") {
		t.Errorf("the error %q does not list the live sessions", err)
	}
}

func TestSessionSendRefusesNamesakes(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 302, comm: "claude", args: []string{"claude"}, start: "79"},
		fakeProc{pid: 303, comm: "claude", args: []string{"claude"}, start: "80"})
	sessionFiles(t,
		fakeSession{pid: 302, name: "aacpanel", start: "79", socket: socket},
		fakeSession{pid: 303, name: "aacpanel", start: "80", socket: socket})

	e := &Executor{}
	if _, err := e.sessionSend(t.Context(), "aacpanel", "hello"); err == nil {
		t.Fatal("sending succeeded with two namesakes around — the reply went at random")
	}
}

func TestSessionSendNeedsSocket(t *testing.T) {
	procFS(t, fakeProc{pid: 304, comm: "claude", args: []string{"claude"}, start: "81"})
	sessionFiles(t, fakeSession{pid: 304, name: "aacpanel", start: "81"})

	e := &Executor{}
	_, err := e.sessionSend(t.Context(), "aacpanel", "hello")
	if err == nil {
		t.Fatal("sending succeeded without a socket")
	}
	if !strings.Contains(err.Error(), "message socket") {
		t.Errorf("the error %q does not explain that there is no channel", err)
	}
}

func TestSessionSendChecksProcessIdentity(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t, fakeProc{pid: 305, comm: "claude", args: []string{"claude"}, start: "99"})
	sessionFiles(t, fakeSession{pid: 305, name: "aacpanel", start: "82", socket: socket})

	e := &Executor{}
	if _, err := e.sessionSend(t.Context(), "aacpanel", "hello"); err == nil {
		t.Fatal("the reply went to a process that merely took over the number of the old one")
	}
}

func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type fakeBus struct {
	tabs        map[string]int
	bus         string
	swallow     int
	renderAfter int
	attach      bool
	blind       bool
	secret      string
	stallTree   int
	stallSend   bool
	tabError    string
}

func fakeBusctl(t *testing.T, tabs map[string]int) string {
	t.Helper()
	return fakeBusctlAs(t, fakeBus{tabs: tabs, swallow: 1})
}

func fakeBusctlOnBus(t *testing.T, wantBus string, tabs map[string]int) string {
	t.Helper()
	return fakeBusctlAs(t, fakeBus{tabs: tabs, bus: wantBus, swallow: 1})
}

func fakeBusctlAs(t *testing.T, bus fakeBus) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "sent")

	var paths []string
	for path := range bus.tabs {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	script := fmt.Sprintf(`#!/usr/bin/env python3
import json, os, sys, time

LOG = %q
TABS = json.loads(%q)
PATHS = json.loads(%q)
SWALLOW = %d
RENDER_AFTER = %d
ATTACH = %s
BLIND = %s
WANT = %q
SECRET = %q
STALL_TREE = %d
STALL_SEND = %s
TAB_ERROR = %q

if WANT and os.environ.get("DBUS_SESSION_BUS_ADDRESS", "") != WANT:
    sys.exit(1)

def counted(key):
    n = 0
    try:
        n = int(open(LOG + "." + key).read())
    except Exception:
        pass
    open(LOG + "." + key, "w").write(str(n + 1))
    return n

def stall():
    time.sleep(30)
    sys.exit(1)

a = sys.argv[1:]
if a[2] == "tree":
    if counted("trees") < STALL_TREE:
        stall()
    print("/")
    print("/Sessions")
    for p in PATHS:
        print(p)
    sys.exit(0)

path, method = a[4], a[6]
if method == "processId":
    if TAB_ERROR:
        sys.stderr.write(TAB_ERROR + "\n")
        sys.exit(1)
    if path not in TABS:
        sys.exit(1)
    print("i %%d" %% TABS[path])
    sys.exit(0)
if method == "sendText" and STALL_SEND:
    stall()

def pasted():
    # What stands in the composer: everything written since the line was last
    # cleared, whether it was handed over as a paste or typed in a run of small
    # writes. The markers are dropped, so both ways read the same here.
    try:
        raw = open(LOG, "rb").read().decode("utf-8", "replace")
    except FileNotFoundError:
        return "", 0
    # A dialog of the session is answered without clearing a line first, so
    # what is in the field is everything written since the last clear, if there
    # was one, and the whole log otherwise.
    i = raw.rfind("\x15")
    rest = raw[i + 1:].replace("\x1b[200~", "").replace("\x1b[201~", "")
    j = rest.find("\r")
    text = rest if j < 0 else rest[:j]
    enters = 0 if j < 0 else rest[j:].count("\r")
    if ATTACH:
        lines = text.split("\n")
        if lines and lines[-1].startswith("/"):
            text = "[Image #1]" + "\n".join(lines[:-1])
    return text, enters

def looked():
    n = 0
    try:
        n = int(open(LOG + ".looks").read())
    except Exception:
        pass
    open(LOG + ".looks", "w").write(str(n + 1))
    return n

if method == "getAllDisplayedText":
    if BLIND:
        sys.exit(1)
    text, enters = pasted()
    sent = enters > SWALLOW
    visible = looked() >= RENDER_AFTER
    held = "" if (sent or not visible) else text
    body = held.split("\n") if held else ["do the same thing once more"]
    lines = [SECRET or "  session conversation", "─" * 46 + " aacpanel ─"]
    if sent:
        lines = ["❯ " + l for l in text.split("\n")] + lines
    lines += ["❯ " + body[0]] + ["  " + l for l in body[1:]]
    lines += ["─" * 56, "  " + "─" * 40, "   Opus 5"]
    out = "\\n".join(l.replace("\\", "\\\\").replace('"', '\\"') for l in lines)
    sys.stdout.write('s "%%s"\n' %% out)
    sys.exit(0)

with open(LOG, "ab") as f:
    f.write(sys.argv[-1].encode("utf-8", "surrogateescape"))
`, log, mustJSON(t, bus.tabs), mustJSON(t, paths), bus.swallow, bus.renderAfter, pyBool(bus.attach), pyBool(bus.blind), bus.bus, bus.secret,
		bus.stallTree, pyBool(bus.stallSend), bus.tabError)

	bin := filepath.Join(dir, "busctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(busctlEnv, bin)
	stubTmux(t)
	return log
}

func stubTmux(t *testing.T) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tmux")
	script := "#!/bin/sh\necho 'no server running on /tmp/tmux-0/default' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
}

func TestSessionSendTypesIntoKonsole(t *testing.T) {
	socket, got := listenFake(t)
	procFS(t,
		fakeProc{pid: 500, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 501, comm: "claude", args: []string{"claude"}, ppid: 500, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 501, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 999, "/Sessions/2": 501})

	e := &Executor{}
	detail, err := e.sessionSend(t.Context(), "aacpanel", "check the stack logs")
	if err != nil {
		t.Fatalf("sending failed: %v", err)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	sent := string(raw)
	if !strings.Contains(sent, "check the stack logs") {
		t.Fatalf("the reply text is missing from what went out to konsole: %q", sent)
	}
	if strings.Contains(sent, pasteStart) || strings.Contains(sent, pasteEnd) {
		t.Errorf("the text went out as a paste: a session reads what wears that mark as quoted data "+
			"rather than as the words of the person at the panel — %q", sent)
	}
	if !strings.HasPrefix(sent, clearLine) {
		t.Errorf("the composer was not cleared before the text: %q", sent)
	}
	if !strings.HasSuffix(sent, enterKey) {
		t.Errorf("the reply was not sent — no Enter went out: %q", sent)
	}
	if strings.Contains(sent, "pane aacpanel") {
		t.Errorf("a service mark stayed inside the typed reply: %q", sent)
	}
	select {
	case line := <-got:
		t.Fatalf("the reply also went as a letter — the session gets it twice: %q", line)
	case <-time.After(200 * time.Millisecond):
	}
	if !strings.HasPrefix(detail, "typed into") {
		t.Errorf("the executor did not say the reply was typed into the terminal: %q", detail)
	}
}

func TestSessionSendFallsBackToLetter(t *testing.T) {
	socket, got := listenFake(t)
	procFS(t,
		fakeProc{pid: 600, comm: "sshd", args: []string{"sshd"}, ppid: 1},
		fakeProc{pid: 601, comm: "claude", args: []string{"claude"}, ppid: 600, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 601, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	fakeBusctl(t, map[string]int{"/Sessions/1": 601})

	e := &Executor{}
	detail, err := e.sessionSend(t.Context(), "aacpanel", "check the stack logs")
	if err != nil {
		t.Fatalf("sending failed: %v", err)
	}
	select {
	case line := <-got:
		var msg struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("the protocol line was not parsed: %v (%q)", err, line)
		}
		if msg.Message.Content != "check the stack logs" {
			t.Errorf("the letter did not go out verbatim: %q", msg.Message.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing reached the session socket")
	}
	if !strings.Contains(detail, "as a letter") {
		t.Errorf("the executor did not say the reply went out as a letter: %q", detail)
	}
}
