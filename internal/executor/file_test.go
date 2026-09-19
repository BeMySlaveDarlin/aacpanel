package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
)

func TestSessionFileLandsOnDiskAndSendsPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	socket, letters := listenFake(t)
	procFS(t,
		fakeProc{pid: 700, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 701, comm: "claude", args: []string{"claude"}, ppid: 700, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 701, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 701})

	e := &Executor{}
	want := []byte("\x89PNG\r\n\x1a\nthe body")
	detail, err := e.sessionFile(t.Context(), "aacpanel", "here is what is on the screen",
		[]action.File{{Name: "shot.png", Data: want}})
	if err != nil {
		t.Fatalf("the file was not sent: %v", err)
	}

	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Fatalf("%d files in the executor directory, expected one: %v", len(names), names)
	}
	stored := filepath.Join(dir, names[0].Name())
	got, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("the wrong file landed on disk: %q", got)
	}
	if !strings.Contains(names[0].Name(), "shot.png") {
		t.Errorf("the file name got lost in the path: %q", names[0].Name())
	}
	info, err := os.Stat(stored)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fileMode {
		t.Errorf("file mode %v, expected %v", info.Mode().Perm(), os.FileMode(fileMode))
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	sent := string(raw)
	if !strings.Contains(sent, stored) {
		t.Errorf("the path to the file was not typed into the session: %q", sent)
	}
	if !strings.Contains(sent, "here is what is on the screen") {
		t.Errorf("the caption of the file got lost: %q", sent)
	}
	select {
	case line := <-letters:
		t.Errorf("the file also went out as a letter: %q", line)
	case <-time.After(200 * time.Millisecond):
	}
	if !strings.Contains(detail, stored) {
		t.Errorf("the executor did not name the path: %q", detail)
	}
}

func TestSessionFilePackGoesInOneReply(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	socket, letters := listenFake(t)
	procFS(t,
		fakeProc{pid: 800, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 801, comm: "claude", args: []string{"claude"}, ppid: 800, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 801, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 801})

	files := []action.File{
		{Name: "first.png", Data: []byte("one")},
		{Name: "second.log", Data: []byte("two-two")},
		{Name: "third.bin", Data: []byte{0x00, 0xff, 0x1a}},
	}
	e := &Executor{}
	detail, err := e.sessionFile(t.Context(), "aacpanel", "look at three at once", files)
	if err != nil {
		t.Fatalf("the pack was not sent: %v", err)
	}

	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(files) {
		t.Fatalf("%d files out of %d landed on disk", len(names), len(files))
	}
	stored := map[string]string{}
	for _, entry := range names {
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		stored[entry.Name()] = string(raw)
	}
	for _, f := range files {
		found := ""
		for name, body := range stored {
			if strings.Contains(name, f.Name) {
				found = body
			}
		}
		if found == "" {
			t.Errorf("the file %s is not on disk at all", f.Name)
			continue
		}
		if found != string(f.Data) {
			t.Errorf("the file %s landed with the bytes of another: %q", f.Name, found)
		}
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	sent := string(raw)
	// One send, and the caption goes in typed: a message somebody wrote reaches
	// the session as their words, not as quoted matter, and the files ride the
	// same line because the composer takes a path wherever it stands.
	if n := strings.Count(sent, "look at three at once"); n != 1 {
		t.Errorf("the pack went out in %d messages, expected one: %q", n, sent)
	}
	if strings.Contains(sent, pasteStart) {
		t.Errorf("the pack went in as a paste: %q", sent)
	}
	if !strings.Contains(sent, "look at three at once") {
		t.Errorf("the caption of the pack got lost: %q", sent)
	}
	for name := range stored {
		path := filepath.Join(dir, name)
		if !strings.Contains(sent, path) {
			t.Errorf("the path %s was not typed into the session: %q", path, sent)
		}
		if !strings.Contains(sent, path+" ") && !strings.Contains(sent, path+enterKey) {
			t.Errorf("the path %s is not set off from what follows it — the composer will read two paths as one", path)
		}
	}
	select {
	case line := <-letters:
		t.Errorf("the pack also went out as a letter: %q", line)
	case <-time.After(200 * time.Millisecond):
	}
	for _, f := range files {
		if !strings.Contains(detail, f.Name) {
			t.Errorf("the reply %q does not name the file %s", detail, f.Name)
		}
	}
	if !strings.Contains(detail, dir) {
		t.Errorf("the reply %q does not name the directory all of it lies in", detail)
	}
}

func TestSessionFilePackNeverReportsPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 810, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 811, comm: "claude", args: []string{"claude"}, ppid: 810, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 811, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctl(t, map[string]int{"/Sessions/1": 811})

	e := &Executor{}
	_, err := e.sessionFile(t.Context(), "aacpanel", "", []action.File{
		{Name: "first.png", Data: []byte("one")},
		{Name: "no-such-dir/second.png", Data: []byte("two")},
		{Name: "third.png", Data: []byte("three")},
	})
	if err == nil {
		t.Fatal("a pack that only half landed was passed off as sent")
	}
	if !strings.Contains(err.Error(), "first.png") {
		t.Errorf("the refusal does not name what already landed on disk: %v", err)
	}
	if _, err := os.ReadFile(log); err == nil {
		t.Error("a reply went to the session after all — with paths, some of which lead nowhere")
	}
}

func TestSessionFileSweepsOldFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 710, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 711, comm: "claude", args: []string{"claude"}, ppid: 710, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 711, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	fakeBusctl(t, map[string]int{"/Sessions/1": 711})

	old := filepath.Join(dir, "20260101-000000-aaaaaa-old.png")
	fresh := filepath.Join(dir, "20260830-000000-bbbbbb-fresh.png")
	for _, path := range []string{old, fresh} {
		if err := os.WriteFile(path, []byte("x"), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	stale := time.Now().Add(-fileTTL - time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}

	e := &Executor{}
	if _, err := e.sessionFile(t.Context(), "aacpanel", "", []action.File{{Name: "new.png", Data: []byte{1}}}); err != nil {
		t.Fatalf("the file was not sent: %v", err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the old file is still there — the directory will grow without end")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the fresh file was swept away along with the old one: %v", err)
	}
}

func TestSessionFileKeepsFileWhenDeliveryFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	procFS(t,
		fakeProc{pid: 720, comm: "sshd", args: []string{"sshd"}, ppid: 1},
		fakeProc{pid: 721, comm: "claude", args: []string{"claude"}, ppid: 720, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 721, name: "aacpanel", start: "77", status: "idle"})
	fakeBusctl(t, map[string]int{"/Sessions/1": 721})

	e := &Executor{}
	_, err := e.sessionFile(t.Context(), "aacpanel", "", []action.File{{Name: "shot.png", Data: []byte{1}}})
	if err == nil {
		t.Fatal("a delivery refusal was passed off as success")
	}
	names, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(names) != 1 {
		t.Fatalf("%d files in the directory, expected one — the survivor", len(names))
	}
	if !strings.Contains(err.Error(), names[0].Name()) {
		t.Errorf("the refusal does not name the path of the surviving file: %v", err)
	}
}

func TestSessionFileWaitsUntilComposerLetsGo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	socket, letters := listenFake(t)
	procFS(t,
		fakeProc{pid: 730, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 731, comm: "claude", args: []string{"claude"}, ppid: 730, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 731, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctlAs(t, fakeBus{
		tabs: map[string]int{"/Sessions/1": 731}, swallow: 2, attach: true, renderAfter: 2,
	})

	e := &Executor{}
	detail, err := e.sessionFile(t.Context(), "aacpanel", "here is what is on the screen",
		[]action.File{{Name: "shot.png", Data: []byte{1}}})
	if err != nil {
		t.Fatalf("the file was not sent: %v", err)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	sent := string(raw)
	if n := strings.Count(sent, enterKey); n <= 2 {
		t.Errorf("Enter went out %d times, and the composer ate the first two: the reply stayed in it — %q", n, sent)
	}
	if !strings.HasPrefix(detail, "typed into") {
		t.Errorf("the executor did not say the reply was typed into the terminal: %q", detail)
	}
	if strings.Contains(detail, "nothing to confirm") {
		t.Errorf("the send is unconfirmed though the konsole screen can be read: %q", detail)
	}
	select {
	case line := <-letters:
		t.Errorf("the file also went out as a letter: %q", line)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSessionFileFailsWhenComposerKeepsIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 740, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 741, comm: "claude", args: []string{"claude"}, ppid: 740, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 741, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	fakeBusctlAs(t, fakeBus{
		tabs: map[string]int{"/Sessions/1": 741}, swallow: 99, attach: true,
	})

	e := &Executor{}
	detail, err := e.sessionFile(t.Context(), "aacpanel", "",
		[]action.File{{Name: "shot.png", Data: []byte{1}}})
	if err == nil {
		t.Fatalf("a reply stuck in the composer was passed off as sent: %q", detail)
	}
	if !strings.Contains(err.Error(), "composer") {
		t.Errorf("the refusal does not say where the reply stayed: %v", err)
	}
	names, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(names) != 1 {
		t.Fatalf("%d files in the directory, expected one — the survivor", len(names))
	}
	if !strings.Contains(err.Error(), names[0].Name()) {
		t.Errorf("the refusal does not name the path of the surviving file: %v", err)
	}
}

func TestSessionSendSaysWhenDeliveryIsUnconfirmed(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 750, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 751, comm: "claude", args: []string{"claude"}, ppid: 750, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 751, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctlAs(t, fakeBus{tabs: map[string]int{"/Sessions/1": 751}, blind: true})

	e := &Executor{}
	detail, err := e.sessionSend(t.Context(), "aacpanel", "check the stack logs")
	if err != nil {
		t.Fatalf("sending failed: %v", err)
	}
	if !strings.Contains(detail, "nothing to confirm") {
		t.Errorf("the executor said nothing about not having checked the send: %q", detail)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	if n := strings.Count(string(raw), enterKey); n != 2 {
		t.Errorf("Enter went out %d times, expected two: without a screen capture the behaviour stays as before", n)
	}
}

func TestScreenNeverLeavesExecutor(t *testing.T) {
	const secret = "the bank key hunter2 and a private conversation"

	for _, c := range []struct {
		name    string
		swallow int
		wantErr bool
	}{
		{name: "the reply went out", swallow: 1},
		{name: "the reply stuck in the composer", swallow: 99, wantErr: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(filesEnv, dir)
			socket, _ := listenFake(t)
			procFS(t,
				fakeProc{pid: 760, comm: "konsole", args: []string{"konsole"}, ppid: 1},
				fakeProc{pid: 761, comm: "claude", args: []string{"claude"}, ppid: 760, start: "77"},
			)
			sessionFiles(t, fakeSession{pid: 761, name: "aacpanel", start: "77", socket: socket, status: "idle"})
			fakeBusctlAs(t, fakeBus{
				tabs: map[string]int{"/Sessions/1": 761}, swallow: c.swallow, attach: true, secret: secret,
			})

			e := &Executor{}
			detail, err := e.sessionFile(t.Context(), "aacpanel", "here is what is on the screen",
				[]action.File{{Name: "shot.png", Data: []byte{1}}})
			if c.wantErr && err == nil {
				t.Fatalf("a stuck reply was passed off as sent: %q", detail)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("the file was not sent: %v", err)
			}
			said := detail
			if err != nil {
				said = err.Error()
			}
			if strings.Contains(said, secret) {
				t.Errorf("the session screen leaked into the reply of the executor: %q", said)
			}
			if strings.Contains(said, "do the same thing") {
				t.Errorf("a hint from the screen leaked into the reply of the executor: %q", said)
			}
		})
	}
}

func TestSessionFileFailsWhenScreenNeverShowsReply(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 770, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 771, comm: "claude", args: []string{"claude"}, ppid: 770, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 771, name: "aacpanel", start: "77", socket: socket, status: "busy"})
	fakeBusctlAs(t, fakeBus{
		tabs: map[string]int{"/Sessions/1": 771}, swallow: 1, attach: true, renderAfter: 10000,
	})

	e := &Executor{}
	detail, err := e.sessionFile(t.Context(), "aacpanel", "A test of the ordinary file send that used to fail",
		[]action.File{{Name: "shot.png", Data: []byte{1}}})
	if err == nil {
		t.Fatalf("a reply that was never seen on the screen was passed off as sent: %q", detail)
	}
	if !strings.Contains(err.Error(), "never showed") {
		t.Errorf("the refusal does not say the reply was never seen on the screen: %v", err)
	}
}

func TestSessionSendConfirmsFromIdleComposer(t *testing.T) {
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 780, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 781, comm: "claude", args: []string{"claude"}, ppid: 780, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 781, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	log := fakeBusctlAs(t, fakeBus{tabs: map[string]int{"/Sessions/1": 781}, swallow: 0})

	e := &Executor{}
	detail, err := e.sessionSend(t.Context(), "aacpanel", "check the stack logs and say what crashed")
	if err != nil {
		t.Fatalf("sending failed: %v", err)
	}
	if strings.Contains(detail, "nothing to confirm") {
		t.Errorf("the send is unconfirmed though the reply is visible in the conversation: %q", detail)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	if n := strings.Count(string(raw), enterKey); n != 1 {
		t.Errorf("Enter went out %d times, expected one: the reply was already sent by the one that came with the paste", n)
	}
}

func TestSessionFileWithoutCaptionIsConfirmedByChip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	socket, _ := listenFake(t)
	procFS(t,
		fakeProc{pid: 790, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 791, comm: "claude", args: []string{"claude"}, ppid: 790, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 791, name: "aacpanel", start: "77", socket: socket, status: "busy"})
	log := fakeBusctlAs(t, fakeBus{
		tabs: map[string]int{"/Sessions/1": 791}, swallow: 1, attach: true, renderAfter: 1,
	})

	e := &Executor{}
	detail, err := e.sessionFile(t.Context(), "aacpanel", "", []action.File{{Name: "shot.png", Data: []byte{1}}})
	if err != nil {
		t.Fatalf("a file without a caption was not sent: %v", err)
	}
	if strings.Contains(detail, "nothing to confirm") {
		t.Errorf("the send is unconfirmed though the screen can be read: %q", detail)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing went out to konsole: %v", err)
	}
	if n := strings.Count(string(raw), enterKey); n < 2 {
		t.Errorf("Enter went out %d times: the first is eaten by the paste parsing, one more is needed", n)
	}
}

// A message carrying files is a message somebody wrote, and it has to reach the
// session as their words. The composer takes a path for an attachment wherever
// in the line it stands, so caption and paths share one line — and one line is
// typed instead of pasted, which is what keeps the caption out of quotes.
func TestACaptionAndItsFilesShareOneLine(t *testing.T) {
	cases := []struct {
		name    string
		caption string
		paths   []string
		want    string
	}{
		{
			name:    "one file with a caption",
			caption: "the header is cut on the right",
			paths:   []string{"/files/20260919-shot.png"},
			want:    "the header is cut on the right /files/20260919-shot.png",
		},
		{
			name:  "files with no caption",
			paths: []string{"/files/one.png", "/files/two.png"},
			want:  "/files/one.png /files/two.png",
		},
		{
			// Read by spaces, a path with a space in it is two paths and
			// neither of them exists.
			name:    "a path with a space keeps its own line",
			caption: "look",
			paths:   []string{"/files/a shot.png"},
			want:    "look\n/files/a shot.png",
		},
		{
			// Nothing is gained by joining a caption that already has breaks.
			name:    "a caption of several lines stays as written",
			caption: "first\nsecond",
			paths:   []string{"/files/one.png"},
			want:    "first\nsecond\n/files/one.png",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fileMessage(c.caption, c.paths); got != c.want {
				t.Errorf("the message is %q, expected %q", got, c.want)
			}
		})
	}
}

// The single-line form is the one that gets typed: a message with a file should
// not arrive quoted just because it carries one.
func TestAMessageWithAFileIsTypedNotPasted(t *testing.T) {
	text := fileMessage("the header is cut on the right", []string{"/files/shot.png"})
	if sent := composerInput(text); strings.Contains(sent, pasteStart) {
		t.Errorf("a caption with one file went in as a paste: %q", sent)
	}
}
