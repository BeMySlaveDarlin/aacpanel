// Command aacpanel-install is the installer of the panel. install.sh in the
// clone builds it with a Go of its own and runs it; a developer runs it with
// go run ./cmd/aacpanel-install from the clone.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"aacpanel/internal/install"
	"aacpanel/internal/install/demo"
	"aacpanel/internal/install/ui"
	"aacpanel/internal/install/view"
)

const usage = `usage: aacpanel-install plan [--plain]
       aacpanel-install demo [--speed N] [--fail STEP]

  plan    looks the machine over the way an install would begin — the
          system, docker and compose, claude, the programs the panel needs,
          sudo, the clone, the ports, free room, the network — and says
          what it found of an earlier install. It changes nothing.
  demo    the whole install on a made-up machine: the check, the questions,
          the plan, the root command, the steps, the first device and the
          report. Nothing on this machine is changed and nothing is sent;
          the password asked for at the root command is read and dropped.

Without a terminal, or with --plain, the same lines go out plain: no live
part and no colour. Run aacpanel-install <command> -h for its flags.
`

// cloneEnv is where install.sh says which clone it runs from; without it
// the clone is the working directory, as under go run.
const cloneEnv = "AACP_INSTALL_CLONE"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, isTerminal))
}

// env carries what a command needs of the process around it, so a test
// runs the command line without a terminal and without the machine.
type env struct {
	stdout, stderr io.Writer
	terminal       func() bool
	inspect        func(clone string) install.Inspection
}

func run(args []string, stdout, stderr io.Writer, terminal func() bool) int {
	return runWith(args, env{stdout: stdout, stderr: stderr, terminal: terminal, inspect: inspectLocal})
}

// inspectLocal looks this machine over. sudo asks for its password at the
// terminal of the input, whatever the output goes into, so that is the
// terminal the check asks about.
func inspectLocal(clone string) install.Inspection {
	_, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	return install.Inspect(install.Local{Tty: err == nil}, clone)
}

func runWith(args []string, e env) int {
	if len(args) == 0 {
		fmt.Fprint(e.stderr, usage)
		return 2
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(e.stdout, usage)
		return 0
	case "plan":
		return runPlan(args[1:], e)
	case "demo":
		return runDemo(args[1:], e.stderr, e.terminal)
	}
	fmt.Fprintf(e.stderr, "aacpanel-install: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func runPlan(args []string, e env) int {
	fs := flag.NewFlagSet("aacpanel-install plan", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	plain := fs.Bool("plain", false, "the plain view: the same lines with no live part and no colour, as without a terminal")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(e.stderr, "aacpanel-install plan: unexpected %q\n", fs.Arg(0))
		return 2
	}
	clone := os.Getenv(cloneEnv)
	if clone == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(e.stderr, "aacpanel-install plan:", err)
			return 1
		}
		clone = wd
	}
	tty := e.terminal()
	theme := ui.ThemeFromEnv()
	if *plain || !tty {
		theme = ui.NewTheme(false)
	}
	status, err := view.Plan(view.PlanOptions{
		Theme:   theme,
		Plain:   *plain || !tty,
		Out:     e.stdout,
		Inspect: func() install.Inspection { return e.inspect(clone) },
	})
	if err != nil {
		fmt.Fprintln(e.stderr, "aacpanel-install plan:", err)
		return 1
	}
	return status
}

func runDemo(args []string, stderr io.Writer, terminal func() bool) int {
	fs := flag.NewFlagSet("aacpanel-install demo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	speed := fs.Float64("speed", 1, "divide every wait of the made-up steps by N (3 turns a build of a minute and a half into thirty seconds)")
	fail := fs.String("fail", "", "end the run with a failure at STEP: "+strings.Join(demo.Failures, ", "))
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	switch {
	case fs.NArg() > 0:
		fmt.Fprintf(stderr, "aacpanel-install demo: unexpected %q\n", fs.Arg(0))
		return 2
	case *speed <= 0:
		fmt.Fprintln(stderr, "aacpanel-install demo: --speed takes a number above zero")
		return 2
	case *fail != "" && !slices.Contains(demo.Failures, *fail):
		fmt.Fprintf(stderr, "aacpanel-install demo: --fail takes %s, not %q\n", strings.Join(demo.Failures, ", "), *fail)
		return 2
	case !terminal():
		fmt.Fprintln(stderr, "aacpanel-install demo: needs a terminal: it draws the installer's screen and reads keys")
		return 1
	}
	status, err := demo.Run(demo.Options{Speed: *speed, Fail: *fail, Theme: ui.ThemeFromEnv()})
	if err != nil {
		fmt.Fprintln(stderr, "aacpanel-install demo:", err)
		return 1
	}
	return status
}

// isTerminal tells whether both ends are a terminal: the screen draws on one
// and reads keys from one.
func isTerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		if _, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS); err != nil {
			return false
		}
	}
	return true
}
