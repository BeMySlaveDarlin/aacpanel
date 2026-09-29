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
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"aacpanel/internal/install"
	"aacpanel/internal/install/demo"
	"aacpanel/internal/install/ui"
	"aacpanel/internal/install/view"
)

const usage = `usage: aacpanel-install install [--plain] [--yes] [--adopt] [--skip-vulncheck] [answers as flags]
       aacpanel-install plan [--plain] [--yes] [answers as flags]
       aacpanel-install update [--to P] [--plain] [--yes] [--adopt] [--skip-vulncheck] [answers as flags]
       aacpanel-install check [--session] [--plain]
       aacpanel-install enroll [--plain]
       aacpanel-install uninstall [--yes] [--purge-db] [--purge-state] [--purge-env] [--purge-files] [--purge-exec] [--purge-data] [--dry-run] [--plain]
       aacpanel-install demo [--speed N] [--fail STEP]

  install looks the machine over, asks the questions, shows the plan and,
          once it is approved, puts the panel on the machine: the host
          description, one sudo for the part as root, the executor built
          and started as a user unit, the .env, the stack, the app role,
          the test database and the tailnet node when the kit has them,
          claude's settings and the panel's tools in every account — the
          terminal goes to claude for a sign-in asked for — the first
          contours of the map, and the collector's restart on a new
          tree; then the check, a test session when asked, the code of
          the first device when there is none, and the report. Each step
          checks the machine first and passes by what is in place, so a
          run that stopped goes on from the step it stopped at. Every
          change is recorded before it is made. An install by hand is
          taken over, each thing of it recorded as found; without a
          terminal that takes --adopt. Ctrl+C stops after the step at
          work; a second stops at once.
  update  moves the clone on — a release to the newest of its paradigm,
          or of the one --to names; a branch pulled without a merge — then
          hands over to the installer of the new tree, which pulls the
          images and goes on as install, keeping the settings unless told
          otherwise. Changes of your own in the clone stop it first.
  check   the chain the panel works through, a line a link, from the
          service's answer to the contours on the map; --session opens the
          test session aacpanel-check through the executor and closes it,
          only it. It changes nothing else.
  enroll  a code for another device, with its countdown; r a new one.
  uninstall takes back what the manifest lists, and nothing else: claude's
          settings first, the units, the executor, the stack, the part as
          root with linger last, the data chosen after typing the host
          name (or named by --purge flags with --yes), the installer's
          cache, and its own directory last. Without a manifest it refuses
          and names what the machine holds of the panel. --dry-run shows
          the plan and what root.sh would run, and changes nothing.

  plan    looks the machine over the way an install would begin — the
          system, docker and compose, claude, the programs the panel needs,
          sudo, the clone, the ports, free room, the network — says what it
          found of an earlier install, asks the questions of the install and
          shows the plan it makes of the answers, with where every value
          came from. It changes nothing. Every question has a flag, and
          --yes takes the suggested answer of the rest.
  demo    the whole install on a made-up machine: the check, the questions,
          the plan, the root command, the steps, the first device and the
          report. Nothing on this machine is changed and nothing is sent;
          the password asked for at the root command is read and dropped.

Without a terminal, or with --plain, the same lines go out plain: no live
part and no colour, and a question that no flag answers stops the run with
the flag's name. Run aacpanel-install <command> -h for its flags.
`

// cloneEnv is where install.sh says which clone it runs from; without it
// the clone is the working directory, as under go run. goEnv and cacheEnv
// are the Go install.sh keeps in its cache and the cache itself: the
// executor is built with the same Go. Under go run the Go on PATH builds it.
const (
	cloneEnv = "AACP_INSTALL_CLONE"
	goEnv    = "AACP_INSTALL_GO"
	cacheEnv = "AACP_INSTALL_CACHE"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, isTerminal))
}

// env carries what a command needs of the process around it, so a test
// runs the command line without a terminal and without the machine.
type env struct {
	stdout, stderr io.Writer
	terminal       func() bool
	inspect        func(clone string) install.Inspection
	survey         func(in install.Inspection, r *install.Run) *install.Survey
	save           func(text string) (string, error)
	// begin opens the run of an approved plan; like is the install the
	// command line shapes, which the steps are made for.
	begin func(s *install.Survey, like install.Install) (*install.Run, []*install.Step, error)
	// move takes the clone forward for an update, and handover runs the
	// installer of the tree it moved to.
	move     func(clone string, paradigm int) (install.Moved, error)
	handover func(argv, env []string) error
	// machine is what check, enroll and uninstall read the machine through.
	machine install.Machine
}

func run(args []string, stdout, stderr io.Writer, terminal func() bool) int {
	return runWith(args, env{stdout: stdout, stderr: stderr, terminal: terminal,
		inspect: inspectLocal, survey: surveyLocal, save: savePlan, begin: beginLocal,
		move: moveLocal, handover: handoverLocal, machine: local()})
}

// goBin is the Go the executor is built with.
func goBin() string {
	if g := os.Getenv(goEnv); g != "" {
		return g
	}
	return "go"
}

// beginLocal opens the run of an approved plan on this machine: the
// manifest and the journal in the installer's directory, and the steps.
func beginLocal(s *install.Survey, like install.Install) (*install.Run, []*install.Step, error) {
	dir := s.Facts.InstallDir
	man, err := install.OpenManifest(dir)
	if err != nil {
		return nil, nil, err
	}
	command := "install"
	if like.Update {
		command = "update"
	}
	j, err := install.OpenJournal(dir, command, time.Now())
	if err != nil {
		return nil, nil, err
	}
	in := like
	in.S, in.M, in.Go, in.Cache = s, local(), goBin(), os.Getenv(cacheEnv)
	r := &install.Run{Manifest: man, Journal: j, Shell: local(), Place: in.Place()}
	return r, in.Steps(), nil
}

// local is this machine. sudo asks for its password at the terminal of the
// input, whatever the output goes into, so that is the terminal the check
// asks about.
func local() install.Local {
	_, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	return install.Local{Tty: err == nil}
}

func inspectLocal(clone string) install.Inspection { return install.Inspect(local(), clone) }

func surveyLocal(in install.Inspection, r *install.Run) *install.Survey {
	return install.NewSurvey(local(), in, r, time.Now())
}

// savePlan writes the plan into the installer's own directory, the one
// place a run writes to before the plan is approved, and only when asked.
func savePlan(text string) (string, error) {
	dir := install.StateDir(os.Getenv)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "plan.txt")
	return path, os.WriteFile(path, []byte(text), 0o600)
}

// answers gathers the flags of the questions into the answers of a run.
type answers map[string]string

// flagValue is a flag of a question: a flag given twice gathers its values
// when the question takes several, and a flag about one account keeps the
// account apart.
type flagValue struct {
	f   install.Flag
	out answers
}

func (v flagValue) String() string { return "" }

func (v flagValue) Set(s string) error {
	key := v.f.Name
	if v.f.PerAccount {
		if dir, val, ok := strings.Cut(s, "="); ok {
			home, _ := os.UserHomeDir()
			key, s = key+" "+install.Expand(dir, home), val
		}
	}
	if prev, ok := v.out[key]; ok && v.f.Many {
		s = prev + "," + s
	}
	v.out[key] = s
	return nil
}

// switchValue is a flag without a value that gives another flag its answer.
type switchValue struct {
	sw  install.Switch
	out answers
}

func (v switchValue) String() string   { return "" }
func (v switchValue) IsBoolFlag() bool { return true }

func (v switchValue) Set(s string) error {
	if s != "true" {
		return fmt.Errorf("takes no value")
	}
	v.out[v.sw.For] = v.sw.Value
	return nil
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
		return runPlan("plan", args[1:], e)
	case "install":
		return runPlan("install", args[1:], e)
	case "update":
		return runUpdate(args[1:], e)
	case "check":
		return runCheck(args[1:], e)
	case "enroll":
		return runEnroll(args[1:], e)
	case "uninstall":
		return runUninstall(args[1:], e)
	case "demo":
		return runDemo(args[1:], e.stderr, e.terminal)
	}
	fmt.Fprintf(e.stderr, "aacpanel-install: unknown command %q\n\n%s", args[0], usage)
	return 2
}

// runPlan runs plan, and install, which is plan that goes on past an
// approved plan to the steps, and the install that finishes an update.
func runPlan(command string, args []string, e env) int {
	fs := flag.NewFlagSet("aacpanel-install "+command, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	plain := fs.Bool("plain", false, "the plain view: the same lines with no live part and no colour, as without a terminal")
	yes := fs.Bool("yes", false, "take the suggested answer of every question no flag answers")
	var like install.Install
	var adopt *bool
	if command != "plan" {
		adopt = fs.Bool("adopt", false, "take over an install by hand without a terminal: --yes alone does not")
		fs.BoolVar(&like.SkipVulncheck, "skip-vulncheck", false, "build the image without the check of its dependencies against published vulnerabilities, knowingly")
	}
	if command == "update" {
		fs.Int("to", -1, "the paradigm to move to, P of vP.M.m; an update stays in its own without it")
		like.Update, like.UpdatedFrom = true, os.Getenv(install.UpdatedEnv)
	}
	given := answers{}
	for _, f := range install.Flags {
		fs.Var(flagValue{f, given}, strings.TrimPrefix(f.Name, "--"), f.Help+" ("+f.Takes+")")
	}
	for _, sw := range install.Switches {
		fs.Var(switchValue{sw, given}, strings.TrimPrefix(sw.Name, "--"), sw.Help)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(e.stderr, "aacpanel-install %s: unexpected %q\n", command, fs.Arg(0))
		return 2
	}
	clone := os.Getenv(cloneEnv)
	if clone == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(e.stderr, "aacpanel-install %s: %v\n", command, err)
			return 1
		}
		clone = wd
	}
	tty := e.terminal()
	theme := ui.ThemeFromEnv()
	if *plain || !tty {
		theme = ui.NewTheme(false)
	}
	r := &install.Run{Answers: given, Yes: *yes}
	var survey func(install.Inspection) *install.Survey
	if e.survey != nil {
		survey = func(in install.Inspection) *install.Survey { return e.survey(in, r) }
	}
	o := view.PlanOptions{
		Theme:   theme,
		Plain:   *plain || !tty,
		Out:     e.stdout,
		Inspect: func() install.Inspection { return e.inspect(clone) },
		Survey:  survey,
		Save:    e.save,
		Yes:     *yes,
	}
	if command != "plan" {
		o.Begin = func(s *install.Survey) (*install.Run, []*install.Step, error) {
			run, steps, err := e.begin(s, like)
			if run != nil {
				// A step asks on its way — the test session — and the flags
				// and --yes answer it as they answered the blocks.
				run.Answers, run.Yes = given, *yes
			}
			return run, steps, err
		}
		o.Adopt = *adopt
	}
	if command == "update" {
		o.Command = "update"
	}
	status, err := view.Plan(o)
	if err != nil {
		fmt.Fprintf(e.stderr, "aacpanel-install %s: %v\n", command, err)
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
