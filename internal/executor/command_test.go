package executor

import (
	"os"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
)

func TestSessionCommandTypesSlashLine(t *testing.T) {
	socket, letters := listenFake(t)
	procFS(t,
		fakeProc{pid: 800, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 801, comm: "claude", args: []string{"claude"}, ppid: 800, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 801, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctlAs(t, fakeBus{tabs: map[string]int{"/Sessions/1": 801}, swallow: 1})

	e := &Executor{}
	detail, err := e.sessionCommand(t.Context(), "aacpanel", &action.Command{Name: "model", Arg: "opus"})
	if err != nil {
		t.Fatalf("the command was not sent: %v", err)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	sent := string(raw)
	if !strings.Contains(sent, "/model opus") {
		t.Fatalf("the command is missing from what went out to konsole: %q", sent)
	}
	if strings.Contains(sent, pasteStart) || strings.Contains(sent, pasteEnd) {
		t.Errorf("the command went out between the paste markers: what carries that mark is no longer "+
			"a command, it is quoted text — %q", sent)
	}
	if n := strings.Count(sent, enterKey); n != 2 {
		t.Errorf("Enter went out %d times, expected two: the command menu catches the first one — %q", n, sent)
	}
	select {
	case line := <-letters:
		t.Errorf("the command also went out as a letter, where it will not work anyway: %q", line)
	case <-time.After(200 * time.Millisecond):
	}
	if !strings.Contains(detail, "/model opus") {
		t.Errorf("the executor did not name what exactly it typed: %q", detail)
	}
}

func TestSessionCommandClosesSuggestMenu(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 810, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 811, comm: "claude", args: []string{"claude"}, ppid: 810, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 811, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 811})

	e := &Executor{}
	if _, err := e.sessionCommand(t.Context(), "aacpanel", &action.Command{Name: "compact"}); err != nil {
		t.Fatalf("the command was not sent: %v", err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "/compact "+enterKey) {
		t.Errorf("the command was typed without a trailing space — the hint menu stayed open: %q", raw)
	}
}

func TestSessionCommandRefusesWithoutKonsole(t *testing.T) {
	socket, letters := listenFake(t)
	procFS(t,
		fakeProc{pid: 820, comm: "sshd", args: []string{"sshd"}, ppid: 1},
		fakeProc{pid: 821, comm: "claude", args: []string{"claude"}, ppid: 820, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 821, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	fakeBusctl(t, map[string]int{"/Sessions/1": 821})

	e := &Executor{}
	if _, err := e.sessionCommand(t.Context(), "aacpanel", &action.Command{Name: "clear"}); err == nil {
		t.Fatal("a command for a session outside konsole was passed off as sent")
	}
	select {
	case line := <-letters:
		t.Errorf("the command went out as a letter after all, where it will not work: %q", line)
	case <-time.After(200 * time.Millisecond):
	}
}
