// Command aacpanel-exec runs panel actions on the host.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/executor"
	"aacpanel/internal/launcher"
	"aacpanel/internal/termlink"
)

const defaultDocker = "unix:///var/run/docker.sock"

const defaultSelf = "aacpanel"

const actionTimeout = 90 * time.Second

func main() {
	list := flag.Bool("list", false, "print the list of allowed actions and exit")
	socket := flag.String("socket", defaultSocket(), "path to the unix socket")
	dockerHost := flag.String("docker", defaultDocker, "docker address: unix:///... or http://host:port")
	self := flag.String("self", defaultSelf, "name of the panel container that must not be shut down from here")
	sessions := flag.Bool("sessions", false,
		"print the open claude sessions and exit")
	launch := flag.Bool("launch", false,
		"start a claude session from the task on stdin and print a JSON report. "+
			"Not a panel action: the executor calls itself here through systemd-run, "+
			"because from its own namespace a session writes no transcript")
	window := flag.Bool("window", false,
		"open a terminal window to a live tmux session from the task on stdin. "+
			"Like -launch, it is called by the executor through systemd-run: otherwise the window "+
			"would die with its restart, and the graphical session environment would not be read")
	flag.Parse()

	if *list {
		kinds := executor.New(executor.NewDocker(*dockerHost), *self).Kinds()
		for _, k := range kinds {
			fmt.Println(k)
		}
		if reason := shortListReason(kinds); reason != "" {
			fmt.Fprintln(os.Stderr, reason)
		}
		return
	}

	if *sessions {
		for _, c := range launcher.Open() {
			fmt.Printf("%s\tagent %d\tkonsole %d\t%s\n", c.Session, c.Agent, c.Konsole, c.Dir)
		}
		return
	}

	if *launch {
		os.Exit(runLaunch())
	}

	if *window {
		os.Exit(runWindow())
	}

	if err := run(*socket, *dockerHost, *self); err != nil {
		log.Fatalf("aacpanel-exec: %v", err)
	}
}

func shortListReason(kinds []action.Kind) string {
	have := make(map[action.Kind]bool, len(kinds))
	for _, k := range kinds {
		have[k] = true
	}
	var sessions, windows int
	for _, k := range action.Kinds {
		switch {
		case have[k]:
		case strings.HasPrefix(string(k), "session."):
			sessions++
		case strings.HasPrefix(string(k), "window."):
			windows++
		}
	}

	switch {
	case sessions > 0:
		return fmt.Sprintf(
			"no tmux — there will be no actions over sessions and windows (%d of %d): "+
				"install tmux, restarting the executor is not needed",
			sessions+windows, len(action.Kinds))
	case windows > 0:
		return fmt.Sprintf(
			"%s is empty — there is nothing to open a window with (%s). Closing a window opened by hand "+
				"still works; the terminal command template lives in the machine description",
			terminalEnv, action.WindowOpen)
	default:
		return ""
	}
}

const terminalEnv = "AACP_TERMINAL"

func defaultSocket() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "aacpanel-exec", "sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("aacpanel-exec-%d", os.Getuid()), "sock")
}

const launchCeiling = 60 * time.Second

func runLaunch() int {
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "aacpanel-exec: starting a session as root is not allowed")
		return 2
	}

	var spec launcher.Spec
	if err := json.NewDecoder(os.Stdin).Decode(&spec); err != nil {
		fmt.Fprintf(os.Stderr, "aacpanel-exec: the task for the launcher was not parsed: %v\n", err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), launchCeiling)
	defer cancel()

	report, err := launcher.Run(ctx, spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aacpanel-exec: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "aacpanel-exec: the launch report was not sent: %v\n", err)
		return 1
	}
	return 0
}

func runWindow() int {
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "aacpanel-exec: opening a window as root is not allowed")
		return 2
	}

	var spec launcher.WindowSpec
	if err := json.NewDecoder(os.Stdin).Decode(&spec); err != nil {
		fmt.Fprintf(os.Stderr, "aacpanel-exec: the window task was not parsed: %v\n", err)
		return 2
	}

	report, err := launcher.Window(spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aacpanel-exec: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "aacpanel-exec: the window report was not sent: %v\n", err)
		return 1
	}
	return 0
}

func run(socket, dockerHost, self string) error {
	if os.Geteuid() == 0 {
		return errors.New("running as root is not allowed: the executor gets by with the rights of the session owner")
	}

	exec := executor.New(executor.NewDocker(dockerHost), self)
	srv := action.NewServer(socket, audited{next: exec}, actionTimeout)
	ln, err := srv.Listen()
	if err != nil {
		return err
	}
	defer os.Remove(socket)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	termSocket := executor.TermSocket(socket)
	if termLn, err := startTerminals(ctx, termSocket); err != nil {
		log.Printf("the session terminal is unavailable (%s): %v", termSocket, err)
	} else {
		defer os.Remove(termSocket)
		defer termLn.Close()
		log.Printf("session terminal: %s", termSocket)
	}

	kinds := exec.Kinds()
	log.Printf("listening on %s, actions allowed: %d of %d", socket, len(kinds), len(action.Kinds))
	if len(kinds) < len(action.Kinds) {
		log.Printf("no tmux — actions over panel sessions are not announced: "+
			"install tmux, the list is recounted on every question, restarting is not needed (%d actions hidden)",
			len(action.Kinds)-len(kinds))
	}
	log.Printf("docker %s, the panel container %q is not shut down from here", dockerHost, self)
	log.Print("sessions are started by the launcher in tmux; the main host session and one-off runs are not closed")

	if err := srv.Run(ctx, ln); err != nil {
		return err
	}
	log.Print("stopped")
	return nil
}

type audited struct{ next action.Executor }

// Kinds reports which action kinds the wrapped executor can run.
func (a audited) Kinds() []action.Kind {
	able, ok := a.next.(action.Capable)
	if !ok {
		return action.Kinds
	}
	return able.Kinds()
}

// Permission asks the wrapped executor about a session dialog and audits the call.
func (a audited) Permission(ctx context.Context, target string) (*action.Permission, error) {
	asker, ok := a.next.(action.Asker)
	if !ok {
		return nil, fmt.Errorf("this executor cannot read session dialogs")
	}
	perm, err := asker.Permission(ctx, target)
	switch {
	case err != nil:
		log.Printf("audit phase=done action=ask.permission target=%s result=failed error=%q", target, err.Error())
	case perm == nil:
		log.Printf("audit phase=done action=ask.permission target=%s result=none", target)
	default:
		log.Printf("audit phase=done action=ask.permission target=%s result=ok options=%d partial=%v",
			target, len(perm.Options), perm.Partial)
	}
	return perm, err
}

// Window asks the wrapped executor about the session window.
func (a audited) Window(ctx context.Context, target string) (*action.Window, error) {
	asker, ok := a.next.(action.WindowAsker)
	if !ok {
		return nil, fmt.Errorf("this executor does not know about session windows")
	}
	return asker.Window(ctx, target)
}

func (a audited) Execute(ctx context.Context, req action.Request) (string, error) {
	log.Printf("audit phase=start action=%s target=%s device=%q request=%s",
		req.Kind, req.Target, req.Device, req.ID)

	started := time.Now()
	detail, err := a.next.Execute(ctx, req)
	took := time.Since(started).Milliseconds()

	if err != nil {
		log.Printf("audit phase=done action=%s target=%s device=%q request=%s result=failed duration_ms=%d error=%q",
			req.Kind, req.Target, req.Device, req.ID, took, err.Error())
		return detail, err
	}
	log.Printf("audit phase=done action=%s target=%s device=%q request=%s result=ok duration_ms=%d detail=%q",
		req.Kind, req.Target, req.Device, req.ID, took, detail)
	return detail, nil
}

func startTerminals(ctx context.Context, socket string) (net.Listener, error) {
	srv := termlink.NewServer(socket, executor.NewTermOpener())
	ln, err := srv.Listen()
	if err != nil {
		return nil, err
	}
	go func() {
		if err := srv.Serve(ctx, ln); err != nil {
			log.Printf("session terminal: %v", err)
		}
	}()
	return ln, nil
}
