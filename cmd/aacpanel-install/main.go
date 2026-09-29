// Command aacpanel-install is the installer of the panel. It has one command
// so far, demo: the whole run on a made-up machine, with the screen the
// installer will have, changing nothing on this one.
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

	"aacpanel/internal/install/demo"
	"aacpanel/internal/install/ui"
)

const usage = `usage: aacpanel-install demo [--speed N] [--fail STEP]

  demo    the whole install on a made-up machine: the check, the questions,
          the plan, the root command, the steps, the first device and the
          report. Nothing on this machine is changed and nothing is sent;
          the password asked for at the root command is read and dropped.

Run aacpanel-install demo -h for its flags.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, isTerminal))
}

func run(args []string, stdout, stderr io.Writer, terminal func() bool) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "demo":
		return runDemo(args[1:], stderr, terminal)
	}
	fmt.Fprintf(stderr, "aacpanel-install: unknown command %q\n\n%s", args[0], usage)
	return 2
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

// isTerminal tells whether both ends are a terminal: the demo draws on one
// and reads keys from one.
func isTerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		if _, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS); err != nil {
			return false
		}
	}
	return true
}
