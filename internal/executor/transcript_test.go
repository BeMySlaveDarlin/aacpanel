package executor

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTerm struct {
	sent    []string
	screen_ string
	known   bool
}

func (f *fakeTerm) kind() string  { return "tmux" }
func (f *fakeTerm) attempts() int { return 1 }
func (f *fakeTerm) send(_ context.Context, payload string) error {
	f.sent = append(f.sent, payload)
	return nil
}
func (f *fakeTerm) screen(context.Context) (string, bool) { return f.screen_, f.known }

// extraEnters counts the Enter keys the panel added over the one that sends
// the message: those are the blind presses it makes when it cannot tell
// whether what it typed has left the composer.
func (f *fakeTerm) extraEnters() int {
	if n := f.enters(); n > 0 {
		return n - 1
	}
	return 0
}

func (f *fakeTerm) enters() int {
	n := 0
	for _, s := range f.sent {
		if s == enterKey {
			n++
		}
	}
	return n
}

type fakeSeen struct {
	mu    sync.Mutex
	found bool
	seen  bool
	ended bool
	end   int64
	asks  int
	marks []string
}

func fitsUnixPath(t *testing.T, sock string) {
	t.Helper()
	if len(sock) > 100 {
		t.Skipf("a socket name %d characters long does not fit into a unix path: set a shorter TMPDIR", len(sock))
	}
}

func startFakeSeen(t *testing.T) *fakeSeen {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(seenDirEnv, dir)
	fitsUnixPath(t, filepath.Join(dir, seenSocketName))
	ln, err := net.Listen("unix", filepath.Join(dir, seenSocketName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeSeen{found: true, end: 4210}
	go f.serve(ln)
	return f
}

func (f *fakeSeen) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.answer(conn)
	}
}

func (f *fakeSeen) answer(conn net.Conn) {
	defer conn.Close()
	var req seenReq
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	f.mu.Lock()
	f.asks++
	reply := seenReply{OK: true, Found: f.found, Pos: f.end}
	if req.Ask == seenAskTurn {
		reply = seenReply{OK: true, Found: f.found, Ended: f.ended}
	}
	if req.Pos != nil {
		f.marks = append(f.marks, req.Mark)
		reply.Pos = *req.Pos
		reply.Seen = f.seen
	}
	f.mu.Unlock()
	_ = json.NewEncoder(conn).Encode(reply)
}

func (f *fakeSeen) says(found, seen bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.found, f.seen = found, seen
}

func (f *fakeSeen) asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asks
}

func noSeen(t *testing.T) {
	t.Helper()
	t.Setenv(seenDirEnv, t.TempDir())
}

const emptyScreen = "──────\n❯ \n──────\n"

func TestDeliveryConfirmedByTranscript(t *testing.T) {
	agent := startFakeSeen(t)
	tail := watchTranscript(liveSession{SessionID: "id-1"})
	if !tail.watching() {
		t.Fatal("the conversation is not visible to the collector — there is nothing further to check")
	}
	agent.says(true, true)

	term := &fakeTerm{screen_: emptyScreen, known: true}
	confirmed, err := pasteAndSend(context.Background(), term, "a reply that cannot be seen on the screen", tail)
	if err != nil {
		t.Fatalf("a reply that did arrive was passed off as one that did not: %v", err)
	}
	if !confirmed {
		t.Error("the send is unconfirmed though the collector saw the record of it")
	}
}

func TestTranscriptCarriesTheMark(t *testing.T) {
	agent := startFakeSeen(t)
	const text = "check the stack logs and say what crashed"
	tail := watchTranscript(liveSession{SessionID: "id-2"})
	agent.says(true, true)

	term := &fakeTerm{screen_: emptyScreen, known: true}
	if _, err := pasteAndSend(context.Background(), term, text, tail); err != nil {
		t.Fatalf("sending refused: %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.marks) == 0 {
		t.Fatal("the collector was never once asked about the reply")
	}
	if agent.marks[0] != composerMark(text) {
		t.Errorf("the mark %q went to the collector, while the reply begins with %q", agent.marks[0], composerMark(text))
	}
}

func TestGhostTextStillGoesThroughTheScreen(t *testing.T) {
	const text = "a reply that landed in the composer and waits for a second Enter"
	startFakeSeen(t)
	tail := watchTranscript(liveSession{SessionID: "id-3"})

	term := &fakeTerm{screen_: "──────\n❯ " + composerMark(text) + "\n──────\n", known: true}
	confirmed, err := pasteAndSend(context.Background(), term, text, tail)
	if err == nil {
		t.Fatal("a reply left in the composer was passed off as sent")
	}
	if confirmed {
		t.Error("something that never happened was confirmed")
	}
	if term.enters() < 2 {
		t.Errorf("the composer was not pressed through with Enter: %d sent", term.enters())
	}
}

func TestTranscriptAnswersWhenTheScreenCannotBeRead(t *testing.T) {
	agent := startFakeSeen(t)
	tail := watchTranscript(liveSession{SessionID: "id-4"})
	agent.says(true, true)

	term := &fakeTerm{known: false}
	confirmed, err := pasteAndSend(context.Background(), term, "the screen cannot be read, and the reply went out", tail)
	if err != nil {
		t.Fatalf("sending refused: %v", err)
	}
	if !confirmed {
		t.Error("the collector saw the record, yet the send is unconfirmed")
	}
	if term.extraEnters() != 0 {
		t.Errorf("%d extra Enter sent: the reply had already gone out", term.extraEnters())
	}
}

func TestUnreadableScreenWithoutTranscriptAnswersAtOnce(t *testing.T) {
	noSeen(t)
	term := &fakeTerm{known: false}
	start := time.Now()
	confirmed, err := pasteAndSend(context.Background(), term, "a reply into a blind session", nil)
	if err != nil {
		t.Fatalf("sending refused: %v", err)
	}
	if confirmed {
		t.Error("something nobody saw was confirmed")
	}
	if term.extraEnters() != 1 {
		t.Errorf("%d Enter sent blind, while there should be one", term.extraEnters())
	}
	if waited := time.Since(start); waited > arriveWait/2 {
		t.Errorf("waited %s where there is nothing to wait for", waited.Round(time.Millisecond))
	}
}

func TestSilentAgentLeavesEverythingToTheScreen(t *testing.T) {
	noSeen(t)
	tail := watchTranscript(liveSession{SessionID: "id-5"})
	if tail.watching() {
		t.Fatal("watching a conversation nobody ever told us about")
	}

	term := &fakeTerm{screen_: emptyScreen, known: true}
	_, err := pasteAndSend(context.Background(), term, "the screen will confirm it: the collector is silent", tail)
	if err == nil {
		t.Fatal("the send was confirmed without a single sign of it")
	}
	if !strings.Contains(err.Error(), "screen") || strings.Contains(err.Error(), "conversation") {
		t.Errorf("the refusal names the wrong place for where we looked: %q", err)
	}
}

func TestInvisibleTalkIsAskedAboutRarely(t *testing.T) {
	agent := startFakeSeen(t)
	agent.says(false, false)
	tail := watchTranscript(liveSession{SessionID: "id-6"})
	for i := 0; i < 5; i++ {
		tail.saw("the mark")
	}
	if got := agent.asked(); got != 1 {
		t.Errorf("the collector was asked %d times instead of once: polling a silent conversation is not slowed down", got)
	}
}

func TestPositionSurvivesASilentAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(seenDirEnv, dir)
	sock := filepath.Join(dir, seenSocketName)
	fitsUnixPath(t, sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	agent := &fakeSeen{found: true, end: 4210}
	go agent.serve(ln)

	tail := watchTranscript(liveSession{SessionID: "id-7"})
	if !tail.watching() || tail.pos < 0 {
		t.Fatalf("watching did not start: visible=%v position=%d", tail.watching(), tail.pos)
	}
	was := tail.pos

	ln.Close()
	os.Remove(sock)
	if tail.saw("the mark") {
		t.Error("silence from the collector was counted as delivery")
	}
	if tail.watching() {
		t.Error("the conversation counts as visible though there is nobody to ask about it")
	}
	if tail.pos != was {
		t.Errorf("the position moved from %d to %d: whatever was read between the questions gets lost", was, tail.pos)
	}
}

func agentSeen(t *testing.T, lines ...string) (string, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 was not found: the end-to-end check of the collector is skipped")
	}

	const talk = "6b0a0f2c-3d1e-4a5b-8c7d-9e0f1a2b3c4d"
	dir := t.TempDir()
	projects := filepath.Join(dir, "projects", "-srv-proj-panel")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projects, talk+".jsonl")
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	sockDir := filepath.Join(dir, "run")
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(seenDirEnv, sockDir)
	fitsUnixPath(t, filepath.Join(sockDir, seenSocketName))

	cmd := exec.Command(python, filepath.Join("..", "..", "agent", "seen.py"))
	cmd.Env = append(os.Environ(),
		"AACP_SEEN_DIR="+sockDir,
		"AACP_CLAUDE_PROJECTS="+filepath.Join(dir, "projects"),
		"AACP_STATE_DIR="+filepath.Join(dir, "state"),
		"HOME="+dir,
		"PYTHONDONTWRITEBYTECODE=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("the collector did not start: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	sock := filepath.Join(sockDir, seenSocketName)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if _, err := os.Stat(sock); err == nil {
			return talk, path
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the collector never brought the socket up in ten seconds")
	return "", ""
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

func userLine(text string) string {
	return `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}`
}

func queueLine(text string) string {
	return `{"type":"queue-operation","operation":"enqueue","content":"` + text + `"}`
}

func TestLiveAgentConfirmsDelivery(t *testing.T) {
	const text = "a delivery check with no screen, a reply somewhat longer than the mark"
	talk, path := agentSeen(t)
	tail := watchTranscript(liveSession{SessionID: talk})
	if !tail.watching() {
		t.Fatal("the conversation is not visible to the collector — there is nothing further to check")
	}
	appendLine(t, path, userLine(text))

	term := &fakeTerm{screen_: emptyScreen, known: true}
	confirmed, err := pasteAndSend(context.Background(), term, text, tail)
	if err != nil {
		t.Fatalf("a reply that did arrive was passed off as one that did not: %v", err)
	}
	if !confirmed {
		t.Error("the send is unconfirmed though the record of it lies in the conversation")
	}
}

func TestLiveAgentSeesAttachmentChip(t *testing.T) {
	const text = "/srv/files/20260908-035522-848e57-shot.png"
	talk, path := agentSeen(t)
	tail := watchTranscript(liveSession{SessionID: talk})
	appendLine(t, path, queueLine("[Image #1]"))

	term := &fakeTerm{screen_: emptyScreen, known: true}
	confirmed, err := pasteAndSend(context.Background(), term, text, tail)
	if err != nil || !confirmed {
		t.Fatalf("a reply with a file was not counted: confirmed=%v error=%v", confirmed, err)
	}
}

func TestTranscriptNeverLeavesExecutor(t *testing.T) {
	const secret = "the bank key hunter2 and a private conversation"
	const text = "our own reply, the one that never turns up in the conversation"
	talk, path := agentSeen(t, userLine(text))
	tail := watchTranscript(liveSession{SessionID: talk})
	appendLine(t, path, userLine(secret))

	term := &fakeTerm{screen_: emptyScreen, known: true}
	confirmed, err := pasteAndSend(context.Background(), term, text, tail)
	if confirmed || err == nil {
		t.Fatalf("a reply of another passed for ours: confirmed=%v error=%v", confirmed, err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), path) {
		t.Errorf("the session conversation leaked into the refusal of the executor: %q", err)
	}
}

func TestLiveDeliveryConfirmedByTranscript(t *testing.T) {
	name := os.Getenv("AACP_LIVE_SESSION")
	if name == "" {
		t.Skip("no live session is named: AACP_LIVE_SESSION=<name of the claude session>")
	}
	text := os.Getenv("AACP_LIVE_TEXT")
	if text == "" {
		t.Fatal("AACP_LIVE_TEXT is empty: there is nothing to send")
	}
	s, err := findOneLiveSession(name)
	if err != nil {
		t.Fatalf("the session was not found: %v", err)
	}
	tail := watchTranscript(s)
	if !tail.watching() {
		t.Fatalf("the conversation of session %s is not visible to the collector — there is nothing to confirm with", name)
	}
	tm, err := termFor(t.Context(), s.PID)
	if err != nil {
		t.Fatalf("the terminal of the session was not found: %v", err)
	}

	var sender term = blindTerm{tm}
	where := "with no screen"
	if os.Getenv("AACP_LIVE_SCREEN") == "on" {
		sender, where = tm, "with a screen"
	}

	start := time.Now()
	confirmed, err := pasteAndSend(t.Context(), sender, text, tail)
	t.Logf("%s: confirmed=%v error=%v in %s",
		where, confirmed, err, time.Since(start).Round(time.Millisecond))
	if err != nil {
		t.Fatalf("the reply was not delivered: %v", err)
	}
	if !confirmed {
		t.Error("the collector did not confirm delivery though the reply went out")
	}
}

type blindTerm struct{ term }

func (blindTerm) screen(context.Context) (string, bool) { return "", false }

func TestAttachChipsMatchTheAgent(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "agent", "seen.py"))
	if err != nil {
		t.Fatal(err)
	}
	line := ""
	for _, s := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(s, "CHIPS = ") {
			line = s
			break
		}
	}
	if line == "" {
		t.Fatal("no CHIPS found in agent/seen.py — the attachment chips were renamed there")
	}
	for _, chip := range attachChips {
		if !strings.Contains(line, `"`+chip+`"`) {
			t.Errorf("the collector has no chip %q: %s", chip, line)
		}
	}
	if got := strings.Count(line, `"`); got != 2*len(attachChips) {
		t.Errorf("the collector has more or fewer chips than the screen (%d against %d): %s",
			got/2, len(attachChips), line)
	}
}
