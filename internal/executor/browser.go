package executor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/browser"
	"aacpanel/internal/browserlink"
)

// BrowserSocket names the typed bridge mounted into the service. Holder
// sockets stay in a separate host-only directory.
func BrowserSocket(actionSocket string) string {
	return filepath.Join(filepath.Dir(actionSocket), "browser.sock")
}

// Browser reports owned profiles only while the host capability is enabled.
func (e *Executor) Browser(ctx context.Context) (browserlink.Status, error) {
	c := e.browserConfig
	out := browserlink.Status{Limits: browserlink.ViewLimits(), Profiles: []browserlink.Profile{}}
	if err := c.Check(); err != nil {
		out.Reason = err.Error()
		return out, nil
	}
	out.Enabled = true
	list, err := browser.Profiles(c)
	if err != nil {
		return out, err
	}
	for i := range list {
		list[i].Open = browserAlive(ctx, browser.Socket(c, list[i].ID))
	}
	out.Profiles = list
	return out, nil
}

func browserAlive(ctx context.Context, path string) bool {
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "unix", path)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (e *Executor) browserAction(ctx context.Context, r action.Request) (string, error) {
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	c := e.browserConfig
	if err := c.Check(); err != nil {
		return "", err
	}
	if !browserlink.ID(r.Target) {
		return "", errors.New("invalid browser profile")
	}
	switch r.Kind {
	case action.BrowserCreate:
		return "Browser profile created", browser.Create(c, r.Target, r.BrowserName)
	case action.BrowserDelete, action.BrowserClose:
		if err := stopBrowser(ctx, r.Target); err != nil {
			return "", err
		}
		if r.Kind == action.BrowserDelete {
			return "Browser profile deleted", browser.Delete(c, r.Target)
		}
		return "Browser closed", nil
	case action.BrowserOpen:
		if _, err := browser.DataDir(c, r.Target); err != nil {
			return "", err
		}
		if browserAlive(ctx, browser.Socket(c, r.Target)) {
			return "Browser is open", nil
		}
		units, err := browserUnits(ctx)
		if err != nil {
			return "", err
		}
		if len(units) >= browserlink.MaxProfiles {
			return "", fmt.Errorf("at most %d browser profiles can be open", browserlink.MaxProfiles)
		}
		bin, err := launcherPath()
		if err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, systemdRunPath(), browserArgs(bin, c, r.Target, e.cores)...)
		// All launch settings belong to the host, never the profile request.
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("the browser holder did not start: %s (%w)", shortenLines(string(out)), err)
		}
		bound, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			if browserAlive(bound, browser.Socket(c, r.Target)) {
				return "Browser opened", nil
			}
			select {
			case <-bound.Done():
				stopBrowser(context.WithoutCancel(ctx), r.Target)
				return "", errors.New("the browser holder did not become ready")
			case <-tick.C:
			}
		}
	}
	return "", errors.New("unknown browser action")
}

// Holder properties contain the whole Chrome process tree independently of
// the executor unit. Only host configuration supplies the executable and paths.
func browserArgs(bin string, c browser.Config, id, cores string) []string {
	args := []string{
		"--user",
		"--quiet",
		"--collect",
		"--unit=" + browser.UnitPrefix + id,
		"--property=MemoryMax=2G",
		"--property=MemorySwapMax=0",
		"--property=CPUQuota=200%",
		"--property=KillMode=control-group",
		"--property=TimeoutStopSec=5s",
	}
	args = append(args, affinity(cores)...)
	for _, key := range []string{"HOME", "PATH", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "AACP_STATE_DIR", "AACP_HOSTCFG"} {
		if v := os.Getenv(key); v != "" {
			args = append(args, "--setenv="+key+"="+v)
		}
	}
	args = append(args, "--setenv="+browser.EnabledEnv+"=1", "--setenv="+browser.ProgramEnv+"="+c.Program)
	return append(args, "--", bin, "-browser-hold", id)
}

func stopBrowser(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlPath(), "--user", "stop", browser.UnitPrefix+id+".service").CombinedOutput()
	if err != nil && !strings.Contains(string(out), "not loaded") {
		return fmt.Errorf("the browser holder did not stop: %s", shortenLines(string(out)))
	}
	return nil
}

func browserUnits(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(
		ctx,
		systemctlPath(),
		"--user",
		"list-units",
		"--all",
		"--plain",
		"--no-legend",
		"--type=service",
		browser.UnitPrefix+"*",
	).Output()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(f[0], browser.UnitPrefix), ".service")
		if browserlink.ID(id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// StartBrowsers removes the public socket while disabled and stops surviving
// units before accepting actions. A service cannot revive a private holder.
func (e *Executor) StartBrowsers(ctx context.Context, actionSocket string) error {
	path := BrowserSocket(actionSocket)
	if e.browserConfig.Enabled && e.browserConfig.PrivateFrom(filepath.Dir(actionSocket)) != nil {
		e.browserConfig.Enabled = false
	}
	if err := e.browserConfig.Check(); err != nil {
		os.Remove(path)
		ids, listErr := browserUnits(ctx)
		if listErr != nil {
			return listErr
		}
		for _, id := range ids {
			if err := stopBrowser(ctx, id); err != nil {
				return err
			}
		}
		return nil
	}
	ln, err := browserlink.Listen(path)
	if err != nil {
		return err
	}
	context.AfterFunc(ctx, func() { ln.Close(); os.Remove(path) })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go e.bridgeBrowser(ctx, conn)
		}
	}()
	return nil
}

// The public socket carries a closed input vocabulary, not opaque CDP. Both
// ends are closed together so the holder observes the last viewer leaving.
func (e *Executor) bridgeBrowser(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	l := browserlink.New(conn)
	l.ReadDeadline(time.Now().Add(5 * time.Second))
	f, err := l.Read()
	if err != nil {
		return
	}
	refuse := func(err error) { l.Write(browserlink.Frame{Type: "end", Error: err.Error()}) }
	if err = e.browserConfig.Check(); err != nil {
		refuse(err)
		return
	}
	if f.Type != "attach" || f.Version != browserlink.Protocol || !browserlink.ID(f.Profile) || f.Size == nil {
		refuse(errors.New("invalid browser attachment"))
		return
	}
	if err = f.Size.Check(); err != nil {
		refuse(err)
		return
	}
	if _, err = browser.DataDir(e.browserConfig, f.Profile); err != nil {
		refuse(err)
		return
	}
	holder, err := browserlink.Attach(ctx, browser.Socket(e.browserConfig, f.Profile), f.Profile, *f.Size)
	if err != nil {
		refuse(err)
		return
	}
	defer holder.Close()
	if err = l.Write(browserlink.Frame{Type: "ready", Version: browserlink.Protocol}); err != nil {
		return
	}
	stop := context.AfterFunc(ctx, func() { l.Close(); holder.Close() })
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			f, err := holder.Read()
			if err != nil {
				l.Close()
				return
			}
			if err = l.Write(f); err != nil {
				return
			}
		}
	}()
	for {
		l.ReadDeadline(time.Now().Add(45 * time.Second))
		f, err = l.Read()
		if err != nil {
			break
		}
		switch f.Type {
		case "input":
			if f.Input == nil {
				err = errors.New("missing browser input")
			} else {
				err = f.Input.Check()
			}
		case "size":
			if f.Size == nil {
				err = errors.New("missing browser size")
			} else {
				err = f.Size.Check()
			}
		case "ack", "ping", "detach":
		default:
			err = errors.New("unknown browser frame")
		}
		if err != nil {
			refuse(err)
			break
		}
		if err = holder.Write(f); err != nil {
			break
		}
	}
	holder.Close()
	l.Close()
	<-done
}
