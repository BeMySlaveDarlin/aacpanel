package executor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

// Executor performs the fixed set of allowed host actions.
type Executor struct {
	docker *Docker
	self   string

	sendSignal func(pid int, sig syscall.Signal) error
	poll       time.Duration
	soft       time.Duration
}

// New creates an Executor for the given Docker client and self container name.
func New(docker *Docker, self string) *Executor {
	return &Executor{docker: docker, self: self}
}

// Kinds reports the action kinds this executor can perform on this host.
func (e *Executor) Kinds() []action.Kind {
	sessions := sessionsPossible()
	canOpen := launcher.WindowPossible()
	if sessions && canOpen {
		return action.Kinds
	}
	out := make([]action.Kind, 0, len(action.Kinds))
	for _, k := range action.Kinds {
		name := string(k)
		switch {
		case !sessions && strings.HasPrefix(name, "session."):
			continue
		case !sessions && strings.HasPrefix(name, "window."):
			continue
		case !sessions && (k == action.TaskStop || k == action.AgentStop):
			continue
		case !canOpen && k == action.WindowOpen:
			continue
		}
		out = append(out, k)
	}
	return out
}

// Execute runs one action request and reports what happened.
func (e *Executor) Execute(ctx context.Context, req action.Request) (string, error) {
	switch req.Kind {
	case action.ContainerStart, action.ContainerStop, action.ContainerRestart:
		return e.container(ctx, req)
	case action.StackUp:
		return e.stackUp(ctx, req.Target)
	case action.StackDown:
		return e.stackDown(ctx, req.Target)
	case action.SessionOpen:
		return e.sessionOpen(ctx, req.Target, req.Project)
	case action.SessionResume:
		return e.sessionResume(ctx, req.Target, req.Resume, req.Project)
	case action.SessionClose:
		return e.sessionClose(ctx, req.Target)
	case action.SessionRestart:
		return e.sessionRestart(ctx, req.Target)
	case action.SessionSend:
		return e.sessionSend(ctx, req.Target, req.Text, req.MessageID)
	case action.SessionUnqueue:
		return e.sessionUnqueue(ctx, req.Target, req.MessageID)
	case action.SessionAnswer:
		return e.sessionAnswer(ctx, req.Target, req.Answer)
	case action.SessionDismiss:
		return e.sessionDismiss(ctx, req.Target, req.Answer)
	case action.SessionKill:
		return e.sessionKill(ctx, req.Target)
	case action.SessionStop:
		return e.sessionStop(ctx, req.Target)
	case action.SessionEscape:
		return e.sessionEscape(ctx, req.Target)
	case action.SessionFile:
		return e.sessionFile(ctx, req.Target, req.Text, req.Files)
	case action.SessionCommand:
		return e.sessionCommand(ctx, req.Target, req.Command)
	case action.SessionPermit:
		return e.sessionPermit(ctx, req.Target, req.Permit)
	case action.SessionSwitch:
		return e.sessionSwitch(ctx, req.Target, req.Switch, req.Project)
	case action.TaskStop:
		return e.taskStop(ctx, req.Target, req.Work)
	case action.AgentStop:
		return e.agentStop(ctx, req.Target, req.Work)
	case action.WindowOpen:
		return e.windowOpen(ctx, req.Target)
	case action.WindowClose:
		return e.windowClose(ctx, req.Target)
	case action.ProjectCreate:
		return projectCreate(req.Target)
	default:
		return "", fmt.Errorf("action %s is not supported by the executor", req.Kind)
	}
}

func (e *Executor) container(ctx context.Context, req action.Request) (string, error) {
	list, err := e.docker.List(ctx)
	if err != nil {
		return "", err
	}

	target, err := find(list, req.Target)
	if err != nil {
		return "", err
	}
	if err := e.allowed(req.Kind, target); err != nil {
		return "", err
	}

	switch req.Kind {
	case action.ContainerStart:
		if target.Running() {
			return "already running", nil
		}
		if err := e.docker.Start(ctx, target.ID); err != nil {
			return "", err
		}
		return "started " + target.Name, nil

	case action.ContainerStop:
		if !target.Running() {
			return "already stopped", nil
		}
		if err := e.docker.Stop(ctx, target.ID); err != nil {
			return "", err
		}
		return "stopped " + target.Name, nil

	default:
		if err := e.docker.Restart(ctx, target.ID); err != nil {
			return "", err
		}
		return "restarted " + target.Name, nil
	}
}

func (e *Executor) allowed(kind action.Kind, target Container) error {
	if e.self == "" || target.Name != e.self {
		return nil
	}
	if kind == action.ContainerStart {
		return nil
	}
	return fmt.Errorf(
		"%s is the panel's own container: %s is not performed from here, otherwise there would be "+
			"nobody left to report the result. From the host: docker %s %s",
		target.Name, kind, verb(kind), target.Name)
}

func verb(kind action.Kind) string {
	switch kind {
	case action.ContainerStop:
		return "stop"
	case action.ContainerRestart:
		return "restart"
	default:
		return "start"
	}
}

func find(list []Container, name string) (Container, error) {
	for _, c := range list {
		if c.Name == name {
			return c, nil
		}
	}
	return Container{}, fmt.Errorf("docker knows no container %q — neither running nor stopped", name)
}

func (e *Executor) stackUp(ctx context.Context, project string) (string, error) {
	list, err := e.docker.List(ctx)
	if err != nil {
		return "", err
	}

	var stack []Container
	for _, c := range list {
		if c.Project == project {
			stack = append(stack, c)
		}
	}
	if len(stack) == 0 {
		return "", fmt.Errorf("there is no stack %q: not a single container with the compose label %s=%s", project, labelProject, project)
	}

	var started, failed []string
	for _, c := range order(stack) {
		if c.Running() {
			continue
		}
		if err := e.docker.Start(ctx, c.ID); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", c.Name, err))
			continue
		}
		started = append(started, c.Name)
	}

	switch {
	case len(failed) > 0 && len(started) == 0:
		return "", fmt.Errorf("not a single container of stack %s started: %s", project, strings.Join(failed, "; "))
	case len(failed) > 0:
		return "", fmt.Errorf("started %s, failed to start %s", strings.Join(started, ", "), strings.Join(failed, "; "))
	case len(started) == 0:
		return "the whole stack is already running", nil
	default:
		return "started: " + strings.Join(started, ", "), nil
	}
}

func (e *Executor) stackDown(ctx context.Context, project string) (string, error) {
	list, err := e.docker.List(ctx)
	if err != nil {
		return "", err
	}

	var stack []Container
	for _, c := range list {
		if c.Project == project {
			stack = append(stack, c)
		}
	}
	if len(stack) == 0 {
		return "", fmt.Errorf("there is no stack %q: not a single container with the compose label %s=%s", project, labelProject, project)
	}

	if e.self != "" {
		for _, c := range stack {
			if c.Name == e.self {
				return "", fmt.Errorf(
					"stack %s holds %s itself: shutting it down would leave the panel unable to write this to "+
						"the log or to bring the stack back — the database and the access to docker go down "+
						"with it. From the host: docker compose -p %s stop",
					project, c.Name, project)
			}
		}
	}

	down := layers(stack)
	slices.Reverse(down)

	var stopped, failed []string
	var ranOut []string
	for i, layer := range down {
		if ctx.Err() != nil {
			for _, rest := range down[i:] {
				for _, c := range rest {
					if c.Running() {
						ranOut = append(ranOut, c.Name)
					}
				}
			}
			break
		}

		ok, bad, late := e.stopLayer(ctx, layer)
		stopped = append(stopped, ok...)
		failed = append(failed, bad...)
		ranOut = append(ranOut, late...)
		if len(late) > 0 {
			for _, rest := range down[i+1:] {
				for _, c := range rest {
					if c.Running() {
						ranOut = append(ranOut, c.Name)
					}
				}
			}
			break
		}
	}

	if len(ranOut) > 0 {
		return "", fmt.Errorf("there was not enough time for the whole stack %s: stopped %s, still up %s — repeat the action",
			project, listOr(stopped, "nothing"), strings.Join(ranOut, ", "))
	}
	switch {
	case len(failed) > 0 && len(stopped) == 0:
		return "", fmt.Errorf("not a single container of stack %s stopped: %s", project, strings.Join(failed, "; "))
	case len(failed) > 0:
		return "", fmt.Errorf("stopped %s, still running %s", strings.Join(stopped, ", "), strings.Join(failed, "; "))
	case len(stopped) == 0:
		return "the whole stack is already stopped", nil
	default:
		return "stopped: " + strings.Join(stopped, ", "), nil
	}
}

const atOnce = 8

func (e *Executor) stopLayer(ctx context.Context, layer []Container) (stopped, failed, ranOut []string) {
	type outcome struct {
		name string
		err  error
	}
	results := make([]*outcome, len(layer))

	var wg sync.WaitGroup
	slots := make(chan struct{}, atOnce)
	for i, c := range layer {
		if !c.Running() {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i] = &outcome{name: c.Name, err: e.docker.Stop(ctx, c.ID)}
		}()
	}
	wg.Wait()

	for _, r := range results {
		if r == nil {
			continue
		}
		switch {
		case r.err == nil:
			stopped = append(stopped, r.name)
		case errors.Is(r.err, context.Canceled), errors.Is(r.err, context.DeadlineExceeded):
			ranOut = append(ranOut, r.name)
		default:
			failed = append(failed, fmt.Sprintf("%s (%v)", r.name, r.err))
		}
	}
	return stopped, failed, ranOut
}

func listOr(names []string, empty string) string {
	if len(names) == 0 {
		return empty
	}
	return strings.Join(names, ", ")
}

func order(stack []Container) []Container {
	var out []Container
	for _, layer := range layers(stack) {
		out = append(out, layer...)
	}
	return out
}

func layers(stack []Container) [][]Container {
	byService := map[string]Container{}
	for _, c := range stack {
		if c.Service != "" {
			byService[c.Service] = c
		}
	}

	placed := map[string]bool{}
	var out [][]Container
	done := 0

	rest := append([]Container(nil), stack...)
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })

	for done < len(rest) {
		var layer []Container
		for _, c := range rest {
			if !placed[c.Name] && ready(c, byService, placed) {
				layer = append(layer, c)
			}
		}
		if len(layer) == 0 {
			for _, c := range rest {
				if !placed[c.Name] {
					layer = append(layer, c)
				}
			}
		}
		for _, c := range layer {
			placed[c.Name] = true
		}
		done += len(layer)
		out = append(out, layer)
	}
	return out
}

func ready(c Container, byService map[string]Container, placed map[string]bool) bool {
	for _, dep := range c.DependsOn {
		other, ok := byService[dep]
		if !ok {
			continue
		}
		if !placed[other.Name] {
			return false
		}
	}
	return true
}
