package heavy

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/hostcfg"
)

// The exits of the client itself, as env and nice have them: 125 is a run
// the queue refused or a call it could not read, 126 a command that was found
// and does not start, 127 one that is not there. Every other exit is the
// command's.
const (
	exitRefused  = 125
	exitNoStart  = 126
	exitNotFound = 127
)

var (
	// dialWait bounds the call to the queue: an executor that does not
	// answer within it is not there.
	dialWait = 2 * time.Second
	// rejoinWait is how long a run whose executor went away tries to join the
	// queue of the next one: the unit is restarted in five seconds.
	rejoinWait = 30 * time.Second
	// silenceWait is how long the client hears nothing from a queue that
	// tells it where it stands every sayEvery before it takes the queue for
	// gone.
	silenceWait = time.Minute
	// cgroupFile tells which cgroup the client lives in.
	cgroupFile = "/proc/self/cgroup"
	// execCommand replaces the client with the command.
	execCommand = syscall.Exec
)

// Run is the client: it waits for the turn of the command at the queue next
// to actionSocket, and becomes the command. It returns only when it does not
// become it.
func Run(args []string, actionSocket string, stderr io.Writer) int {
	fs := flag.NewFlagSet("aacpanel-exec "+Flag, flag.ContinueOnError)
	fs.SetOutput(stderr)
	mem := fs.String("mem", "", "the memory the run asks for, as systemd takes it: 8G, 768M; "+
		"unset, the default of the machine description")
	name := fs.String("name", "", "the name of the run in the queue; unset, the first words of the command")
	socket := fs.String("socket", actionSocket, "the executor's socket; the queue listens next to it")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: aacpanel-exec %s [-mem SIZE] [-name TEXT] -- COMMAND [ARGS...]\n", Flag)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitRefused
	}
	argv := fs.Args()
	if len(argv) == 0 {
		fs.Usage()
		return exitRefused
	}
	say := func(format string, a ...any) { fmt.Fprintf(stderr, Prefix+format+"\n", a...) }
	path, err := exec.LookPath(argv[0])
	if err != nil {
		say("%v", err)
		if errors.Is(err, exec.ErrNotFound) {
			return exitNotFound
		}
		return exitNoStart
	}
	if _, ok := hostcfg.SizeBytes(*mem); *mem != "" && !ok {
		say("-mem %s is not a size systemd takes: a whole number of bytes, or of K, M, G or T, like 8G or 768M", *mem)
		return exitRefused
	}
	if *name == "" {
		*name = firstWords(argv)
	}
	dir, _ := os.Getwd()
	req := Request{Op: OpRun, Name: *name, Mem: *mem, Dir: dir}

	if wait(Socket(*socket), req, say) == StateRefused {
		return exitRefused
	}
	err = execCommand(path, argv, os.Environ())
	say("%s: %v", argv[0], err)
	return exitNoStart
}

// wait holds the run's place in the queue at socket until it is let go or
// refused, and says what the queue says. It ends in a state that runs the
// command unless the queue refused it: an executor that is not there or
// stops answering is a line, not a reason the command does not run.
func wait(socket string, req Request, say func(string, ...any)) string {
	if unit := heldBy(); unit != "" {
		say("running inside the heavy run of %s, which holds its slot and its memory already; not queued", unit)
		return StateAdmitted
	}
	conn, err := net.DialTimeout("unix", socket, dialWait)
	if err != nil {
		say("the queue is not reachable (%v); running now, without the queue and its ceilings", err)
		return StateUnheld
	}
	for {
		state := hold(conn, req, say)
		_ = conn.Close()
		if state != "" {
			return state
		}
		say("the queue went away; joining the queue again at its end")
		if conn = redial(socket); conn == nil {
			say("the queue did not come back within %s; running now, without the queue and its ceilings", rejoinWait)
			return StateUnheld
		}
	}
}

// hold sends the request on conn and reads the queue's lines until a verdict.
// Empty is a queue that went away before it gave one.
func hold(conn net.Conn, req Request, say func(string, ...any)) string {
	_ = conn.SetWriteDeadline(time.Now().Add(dialWait))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return ""
	}
	lines := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(silenceWait))
		raw, err := lines.ReadBytes('\n')
		if err != nil {
			return ""
		}
		var r Reply
		if err := json.Unmarshal(raw, &r); err != nil {
			return ""
		}
		if r.Line != "" {
			say("%s", r.Line)
		}
		if r.Error != "" {
			say("the queue did not take the run: %s; running now, without the queue and its ceilings", r.Error)
			return StateUnheld
		}
		if r.State != StateWaiting {
			return r.State
		}
	}
}

func redial(socket string) net.Conn {
	until := time.Now().Add(rejoinWait)
	for {
		if conn, err := net.DialTimeout("unix", socket, dialWait); err == nil {
			return conn
		}
		if time.Now().After(until) {
			return nil
		}
		time.Sleep(rejoinWait / 30)
	}
}

// heldBy is the scope of a run the client already lives in, or empty. Its
// slot and its memory are counted already, by the run around it: a hook
// wraps a whole line even when a part of it goes through the queue itself,
// and a script or a Makefile may call the queue on its own. Queued again, the
// run inside would wait for a slot its own parent holds, and with every slot
// taken it would wait for ever.
func heldBy() string {
	raw, err := os.ReadFile(cgroupFile)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		for _, part := range strings.Split(line, "/") {
			if strings.HasPrefix(part, UnitPrefix) && strings.HasSuffix(part, ".scope") {
				return part
			}
		}
	}
	return ""
}

// firstWords names a run by its command: the first three words, and those of
// the line a shell is given with -c, which is how a hook hands a command over.
func firstWords(argv []string) string {
	words := argv
	if len(argv) >= 3 && argv[1] == "-c" {
		switch filepath.Base(argv[0]) {
		case "bash", "sh", "dash", "zsh":
			words = strings.Fields(argv[2])
		}
	}
	if len(words) > 3 {
		words = words[:3]
	}
	return cleanName(strings.Join(words, " "))
}
