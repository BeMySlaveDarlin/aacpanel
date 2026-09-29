package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
	"aacpanel/internal/install/view"
)

// cloneDir is the clone the installer runs for: the one install.sh names,
// or the working directory under go run.
func cloneDir() (string, error) {
	if c := os.Getenv(cloneEnv); c != "" {
		return c, nil
	}
	return os.Getwd()
}

// lines is the feed of a lifecycle command: colour and questions at a
// terminal, plain lines without one.
func lines(e env, plain bool) view.Lines {
	tty := e.terminal() && !plain
	theme := ui.NewTheme(false)
	if tty {
		theme = ui.ThemeFromEnv()
	}
	return view.Lines{Theme: theme, Out: e.stdout, Tty: tty}
}

// parse reads the flags of a command that takes no argument besides them.
func parse(fs *flag.FlagSet, args []string, e env) (int, bool) {
	fs.SetOutput(e.stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, false
		}
		return 2, false
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(e.stderr, "%s: unexpected %q\n", fs.Name(), fs.Arg(0))
		return 2, false
	}
	return 0, true
}

// journal opens the journal of a command that only reads — only where the
// installer's directory is there already: a look must leave no trace.
func journal(dir, command string) *install.Journal {
	if _, err := os.Stat(filepath.Join(dir, install.ManifestName)); err != nil {
		return nil
	}
	j, err := install.OpenJournal(dir, command, time.Now())
	if err != nil {
		return nil
	}
	return j
}

// kept is the install on this machine as check and enroll read it.
func kept(e env) (*install.Install, *install.Run, error) {
	clone, err := cloneDir()
	if err != nil {
		return nil, nil, err
	}
	in := e.inspect(clone)
	if in.Mode == install.Fresh {
		return nil, nil, errors.New("stop: there is no install of the panel here: ./install.sh puts it on the machine")
	}
	s := install.Kept(e.machine, in, time.Now())
	i := &install.Install{S: s, M: e.machine, Go: goBin(), Cache: os.Getenv(cacheEnv)}
	r := &install.Run{Shell: local(), Place: i.Place(), Journal: journal(in.InstallDir, "check"), Answers: map[string]string{}}
	install.UserBus(r, in.Account.UID)
	return i, r, nil
}

func runCheck(args []string, e env) int {
	fs := flag.NewFlagSet("aacpanel-install check", flag.ContinueOnError)
	plain := fs.Bool("plain", false, "plain lines, no colour and no questions")
	session := fs.Bool("session", false, "open the test session aacpanel-check through the executor, and close it")
	if status, ok := parse(fs, args, e); !ok {
		return status
	}
	return view.Check(view.CheckOptions{Lines: lines(e, *plain), Title: "Check",
		Done: "Every link of the chain holds.",
		Begin: func() (*install.Run, []*install.Step, error) {
			i, r, err := kept(e)
			if err != nil {
				return nil, nil, err
			}
			steps := []*install.Step{i.CheckStep()}
			if *session {
				r.Answers["--check-session"] = "yes"
				steps = append(steps, i.SessionStep())
			}
			return r, steps, nil
		}})
}

func runEnroll(args []string, e env) int {
	fs := flag.NewFlagSet("aacpanel-install enroll", flag.ContinueOnError)
	plain := fs.Bool("plain", false, "print the code in plain lines, without the frame and its countdown")
	if status, ok := parse(fs, args, e); !ok {
		return status
	}
	return view.Check(view.CheckOptions{Lines: lines(e, *plain), Title: "First device",
		Begin: func() (*install.Run, []*install.Step, error) {
			i, r, err := kept(e)
			if err != nil {
				return nil, nil, err
			}
			return r, []*install.Step{{ID: "enroll", Title: "A device", Apply: i.Enroll}}, nil
		}})
}

// moveLocal takes the clone of this machine forward.
func moveLocal(clone string, paradigm int) (install.Moved, error) {
	return install.MoveClone(&install.Run{Shell: local()}, clone, paradigm)
}

// handoverLocal runs the installer of the tree an update moved to: its
// install.sh builds it with the Go the new go.mod names, and runs it.
func handoverLocal(argv, env []string) error {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return err
	}
	return syscall.Exec(bash, append([]string{"bash"}, argv...), env)
}

// runUpdate moves the clone and hands over to the installer of the new
// tree, which goes on as install; a clone that did not move goes on here.
func runUpdate(args []string, e env) int {
	if os.Getenv(install.UpdatedEnv) != "" || slices.ContainsFunc(args, func(a string) bool {
		return a == "-h" || a == "-help" || a == "--help"
	}) {
		return runPlan("update", args, e)
	}
	to := -1
	for i, a := range args {
		v, ok := strings.CutPrefix(a, "--to=")
		if !ok && (a == "--to" || a == "-to") && i+1 < len(args) {
			v, ok = args[i+1], true
		}
		if ok {
			if _, err := fmt.Sscanf(v, "%d", &to); err != nil || to < 0 {
				fmt.Fprintf(e.stderr, "aacpanel-install update: --to takes the paradigm, a number, not %q\n", v)
				return 2
			}
		}
	}
	clone, err := cloneDir()
	if err != nil {
		fmt.Fprintln(e.stderr, "aacpanel-install update:", err)
		return 1
	}
	o := lines(e, slices.Contains(args, "--plain") || slices.Contains(args, "-plain"))
	p := &view.Plain{W: e.stdout, T: o.Theme, Width: view.PlainWidth, Color: o.Tty}
	p.Print("\n" + o.Theme.Step(ui.Plain, "Update the clone", view.PlainWidth))
	m, err := e.move(clone, to)
	if err != nil {
		var f *install.Failed
		out := []string{o.Theme.Result(ui.Fail, err.Error())}
		if errors.As(err, &f) {
			out = append(out, f.Fix...)
			out = append(out, f.Tail...)
		}
		p.Print(o.Theme.Output(out, view.PlainWidth))
		return 1
	}
	said := []string{o.Theme.Result(ui.Pass, fmt.Sprintf("%s · %s → %s", m.Says, short(m.From), short(m.To)))}
	if m.Ahead != "" {
		said = append(said, fmt.Sprintf("%s of a later paradigm is out: ./install.sh update --to %s takes it, knowingly", m.Ahead, strings.SplitN(strings.TrimPrefix(m.Ahead, "v"), ".", 2)[0]))
	}
	p.Print(o.Theme.Output(said, view.PlainWidth))
	if m.To == m.From {
		return runPlan("update", args, e)
	}
	// The installer of the new tree carries on: install.sh builds it with
	// the Go its go.mod names.
	envs := append(os.Environ(), install.UpdatedEnv+"="+m.From)
	if err := e.handover(append([]string{filepath.Join(clone, "install.sh"), "update"}, args...), envs); err != nil {
		fmt.Fprintln(e.stderr, "aacpanel-install update: the installer of the new tree did not start:", err)
		return 1
	}
	return 0
}

func short(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

func runUninstall(args []string, e env) int {
	fs := flag.NewFlagSet("aacpanel-install uninstall", flag.ContinueOnError)
	plain := fs.Bool("plain", false, "plain lines, no colour and no questions")
	yes := fs.Bool("yes", false, "remove without the question; the data the --purge flags name goes without the host name typed")
	dry := fs.Bool("dry-run", false, "show the plan and what root.sh would run, and change nothing")
	chosen := map[string]*bool{}
	for _, d := range install.Data {
		chosen[d.ID] = fs.Bool(strings.TrimPrefix(d.Flag, "--"), false, "delete "+strings.ToLower(d.Label[:1])+d.Label[1:]+": "+d.Detail)
	}
	all := fs.Bool(strings.TrimPrefix(install.PurgeAll, "--"), false, "delete every kind of data above")
	if status, ok := parse(fs, args, e); !ok {
		return status
	}
	var purge []string
	for _, d := range install.Data {
		if *all || *chosen[d.ID] {
			purge = append(purge, d.ID)
		}
	}
	clone, err := cloneDir()
	if err != nil {
		fmt.Fprintln(e.stderr, "aacpanel-install uninstall:", err)
		return 1
	}
	in := e.inspect(clone)
	o := view.UninstallOptions{Lines: lines(e, *plain), Yes: *yes, DryRun: *dry, Purge: purge}
	entries, err := install.ReadManifest(filepath.Join(in.InstallDir, install.ManifestName))
	if err != nil {
		o.Refusal, o.Traces = install.NoManifest(in.Facts), install.Traces(in)
		return view.Uninstall(o)
	}
	o.Removal = install.NewRemoval(e.machine, in.Facts, entries, install.CacheDir(os.Getenv))
	o.Begin = func(rm *install.Removal) (*install.Run, error) {
		j, err := install.OpenJournal(in.InstallDir, "uninstall", time.Now())
		if err != nil {
			return nil, err
		}
		r := &install.Run{Journal: j, Shell: local(), Place: rm.Place, Again: "./install.sh uninstall"}
		install.UserBus(r, in.Account.UID)
		return r, nil
	}
	o.DryRoot = func(args []string) (string, error) {
		out, err := exec.Command("bash", append(append([]string{o.Removal.Place.RootScript()}, args...), "--dry-run")...).CombinedOutput()
		return string(out), err
	}
	return view.Uninstall(o)
}
