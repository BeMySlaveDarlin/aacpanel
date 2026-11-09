package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/observer"
	"aacpanel/internal/stream"
)

type freezeScene struct {
	t             *testing.T
	e             *Executor
	h             *fakeHolder
	dir           string
	alarm         string
	writes        []string
	refuse, stuck string
}

func newFreezeScene(t *testing.T) *freezeScene {
	t.Helper()
	h := onTheStream(t, false)
	e := New(nil, "")
	k, _, dir := keptBy(t, e)
	if _, err := k.SetAlarm(t.Context(), streamCaller, observer.AlarmAsk{Name: "check", In: "5m"}); err != nil {
		t.Fatal(err)
	}
	alarmDir := filepath.Join(filepath.Dir(dir), "alarms")
	alarm := filepath.Join(alarmDir, onlyEntry(t, alarmDir, ".json")+".json")
	root := t.TempDir()
	before := cgroupRoot
	cgroupRoot = root
	t.Cleanup(func() { cgroupRoot = before })
	group := "/user.slice/user-1001.slice/user@1001.service/aacp.slice/aacp-agents.slice/run-session.service"
	s := &freezeScene{t: t, e: e, h: h, dir: filepath.Join(root, group), alarm: alarm}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	holder := filepath.Join(procDir(), strconv.Itoa(os.Getpid()))
	if err := os.MkdirAll(holder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(holder, "cgroup"), []byte("0::"+group+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.write("cgroup.events", "populated 1\nfrozen 0\n")
	s.write("cgroup.freeze", "0")
	s.write("memory.current", "1048576\n")
	s.write("memory.reclaim", "")
	beforeWrite := writeFreeze
	t.Cleanup(func() { writeFreeze = beforeWrite })
	writeFreeze = func(path string, raw []byte, mode os.FileMode) error {
		value := string(raw)
		if path != filepath.Join(s.dir, "cgroup.freeze") || (value != "0" && value != "1") {
			return fmt.Errorf("unexpected freeze write: %s %q", path, raw)
		}
		s.writes = append(s.writes, value)
		if value == s.refuse {
			return &os.PathError{Op: "write", Path: path, Err: syscall.EPERM}
		}
		if value == "1" && !s.sleeping() {
			return fmt.Errorf("the alarm was not sleeping before its cgroup froze")
		}
		if err := os.WriteFile(path, raw, mode); err != nil {
			return err
		}
		if value != s.stuck {
			s.write("cgroup.events", "populated 1\nfrozen "+value+"\n")
		}
		return nil
	}
	return s
}

func (s *freezeScene) write(name, body string) {
	s.t.Helper()
	if err := os.WriteFile(filepath.Join(s.dir, name), []byte(body), 0o600); err != nil {
		s.t.Fatal(err)
	}
}

func (s *freezeScene) act(kind action.Kind) error {
	s.t.Helper()
	_, err := s.e.Execute(s.t.Context(), action.Request{ID: "sleep", Kind: kind, Target: "demo", Conversation: streamSID})
	return err
}

func (s *freezeScene) sleeping() bool {
	s.t.Helper()
	raw, err := os.ReadFile(s.alarm)
	var alarm observer.Alarm
	if err != nil || json.Unmarshal(raw, &alarm) != nil {
		s.t.Fatalf("the alarm was lost: %s %v", raw, err)
	}
	return alarm.Sleeping
}

func (s *freezeScene) written() string {
	return strings.Join(s.writes, "")
}

func TestCancelledFreezeDoesNotWriteTheCgroup(t *testing.T) {
	s := newFreezeScene(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := setFrozen(ctx, s.dir, true); !errors.Is(err, context.Canceled) || s.written() != "" {
		t.Fatalf("a cancelled caller changed the cgroup: writes %s error %v", s.written(), err)
	}
}

func TestFreezeKeepsTheProcessAndReclaimsBeforeConfirmingSleep(t *testing.T) {
	s := newFreezeScene(t)
	before := writeReclaim
	t.Cleanup(func() { writeReclaim = before })
	wrote := false
	writeReclaim = func(path string, raw []byte, mode os.FileMode) error {
		frozen, err := frozenGroup(s.dir)
		if err != nil || !frozen || !s.sleeping() || string(raw) != "1048576" || path != filepath.Join(s.dir, "memory.reclaim") {
			t.Fatalf("reclaim ran before freeze or asked for the wrong memory: %s %q %v %v", path, raw, frozen, err)
		}
		wrote = true
		return before(path, raw, mode)
	}
	withSignals(t, s.e, map[int]bool{5001: true}, map[int]int{5001: 1})
	s.e.sendSignal = func(_ int, sig syscall.Signal) error {
		if sig != 0 {
			t.Fatalf("sleep signalled its live process: %s", sig)
		}
		return nil
	}
	if err := s.act(action.SessionFreeze); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "cgroup.freeze"))
	if err != nil || string(raw) != "1" || !wrote || !s.sleeping() || s.written() != "1" {
		t.Fatalf("sleep did not freeze and reclaim: value %q reclaimed %t writes %s error %v", raw, wrote, s.written(), err)
	}
	if _, err := findOneLiveSession("demo"); err != nil {
		t.Fatalf("sleep lost its live process: %v", err)
	}
	if err := s.act(action.SessionThaw); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(s.dir, "cgroup.freeze"))
	if err != nil || string(raw) != "0" || s.sleeping() || s.written() != "10" {
		t.Fatalf("wake did not restore the alarm: value %q writes %s error %v", raw, s.written(), err)
	}
	if err := s.act(action.SessionThaw); err != nil {
		t.Fatalf("thawing an awake session failed: %v", err)
	}
	if _, err := streamState(t.Context(), liveSession{Name: "demo", SessionID: streamSID, PID: 5001}); err != nil {
		t.Fatalf("the thawed holder was unreachable: %v", err)
	}
}

func TestFreezeFailureThawsAndRestoresTheAlarms(t *testing.T) {
	for _, step := range []string{"freeze", "confirmation", "alarm mark"} {
		t.Run(step, func(t *testing.T) {
			s := newFreezeScene(t)
			restore := func() {}
			switch step {
			case "freeze":
				s.refuse = "1"
			case "confirmation":
				s.stuck = "1"
			case "alarm mark":
				dir := filepath.Dir(s.alarm)
				if err := os.Rename(dir, dir+"-kept"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dir, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				restore = func() {
					if err := os.Remove(dir); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(dir+"-kept", dir); err != nil {
						t.Fatal(err)
					}
				}
			}
			sleepErr := s.act(action.SessionFreeze)
			if sleepErr == nil {
				t.Fatal("a failed sleep was accepted")
			}
			if step == "freeze" && !errors.Is(sleepErr, syscall.EPERM) {
				t.Fatalf("the freeze write failure was lost: %v", sleepErr)
			}
			restore()
			frozen, err := frozenGroup(s.dir)
			if err != nil || frozen || s.sleeping() || !strings.HasSuffix(s.written(), "0") {
				t.Fatalf("sleep failed without rollback: frozen %t alarm %t writes %s error %v", frozen, s.sleeping(), s.written(), err)
			}
		})
	}
}

func TestPartialReclaimStillSleeps(t *testing.T) {
	s := newFreezeScene(t)
	before := writeReclaim
	writeReclaim = func(string, []byte, os.FileMode) error {
		return &os.PathError{Op: "write", Path: "memory.reclaim", Err: syscall.EAGAIN}
	}
	t.Cleanup(func() { writeReclaim = before })
	detail, err := s.e.Execute(t.Context(), action.Request{ID: "sleep", Kind: action.SessionFreeze, Target: "demo", Conversation: streamSID})
	if err != nil || !s.sleeping() || strings.Contains(detail, "memory left to the kernel") {
		t.Fatalf("a partial reclaim refused sleep or reported a failure: %s %v", detail, err)
	}
}

func TestReclaimFailureLeavesTheSessionFrozenAndSaysWhy(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  syscall.Errno
		why  string
	}{
		{"read-only filesystem", syscall.EROFS, syscall.EROFS.Error()},
		{"access denied", syscall.EACCES, syscall.EACCES.Error()},
		{"operation refused", syscall.EPERM, syscall.EPERM.Error()},
		{"write failed", syscall.EIO, syscall.EIO.Error()},
		{"memory read", 0, "invalid syntax"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			s := newFreezeScene(t)
			if failure.err == 0 {
				s.write("memory.current", "invalid")
			} else {
				before := writeReclaim
				writeReclaim = func(path string, _ []byte, _ os.FileMode) error {
					return &os.PathError{Op: "write", Path: path, Err: failure.err}
				}
				t.Cleanup(func() { writeReclaim = before })
			}
			detail, err := s.e.Execute(t.Context(), action.Request{ID: "sleep", Kind: action.SessionFreeze, Target: "demo", Conversation: streamSID})
			frozen, readErr := frozenGroup(s.dir)
			if err != nil || readErr != nil || !frozen || !s.sleeping() || s.written() != "1" {
				t.Fatalf("a refused reclaim rolled back sleep: %s frozen %t error %v read %v", detail, frozen, err, readErr)
			}
			if !strings.Contains(detail, "memory left to the kernel: ") || !strings.Contains(detail, failure.why) || strings.Contains(detail, s.dir) {
				t.Fatalf("the sleep result does not give a short reason for leaving memory to the kernel: %s", detail)
			}
		})
	}
}

func (s *freezeScene) unreadableState(part string) {
	s.t.Helper()
	path := filepath.Join(s.dir, "cgroup.events")
	if part == "holder cgroup" {
		path = filepath.Join(procDir(), strconv.Itoa(os.Getpid()), "cgroup")
	}
	if part == "unknown frozen state" {
		s.write("cgroup.events", "populated 1\nfrozen unknown\n")
		return
	}
	if err := os.Remove(path); err != nil {
		s.t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		s.t.Fatal(err)
	}
}

func TestUnreadableFreezeStateStillAsksTheHolder(t *testing.T) {
	for _, part := range []string{"holder cgroup", "cgroup.events", "unknown frozen state"} {
		t.Run(part, func(t *testing.T) {
			s := newFreezeScene(t)
			s.unreadableState(part)
			request := stream.Request{Op: stream.OpSend, Text: "Read this message"}
			reply, err := streamAsk(t.Context(), liveSession{Name: "demo", SessionID: streamSID, PID: 5001}, request)
			if err != nil || !reply.OK {
				t.Fatalf("an unreadable freeze state blocked the holder: %+v %v", reply, err)
			}
			got := s.h.asked()
			if len(got) != 1 || got[0].Op != request.Op || got[0].Text != request.Text {
				t.Fatalf("the request did not reach its holder: %+v", got)
			}
		})
	}
}

func TestUnreadableFreezeStateStillClosesTheSession(t *testing.T) {
	for _, part := range []string{"holder cgroup", "cgroup.events", "unknown frozen state"} {
		t.Run(part, func(t *testing.T) {
			s := newFreezeScene(t)
			s.unreadableState(part)
			withSignals(t, s.e, map[int]bool{5001: true}, map[int]int{5001: 1})
			if _, err := s.e.Execute(t.Context(), closeAskedBy(streamSID)); err != nil {
				t.Fatalf("an unreadable freeze state blocked close: %v", err)
			}
			got := s.h.asked()
			if len(got) != 1 || got[0].Op != stream.OpClose || s.written() != "" {
				t.Fatalf("close did not follow its ordinary path: requests %+v cgroup writes %s", got, s.written())
			}
		})
	}
}

func TestAFrozenHolderRefusesEveryRequestWithoutASocketCall(t *testing.T) {
	s := newFreezeScene(t)
	s.write("cgroup.events", "frozen 1\n")
	s.h.mu.Lock()
	before := s.h.contacts
	s.h.mu.Unlock()
	for _, op := range []string{stream.OpState, stream.OpSend, stream.OpLetter, stream.OpControl, stream.OpRespond} {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		_, err := streamAsk(ctx, liveSession{Name: "demo", SessionID: streamSID, PID: 5001}, stream.Request{Op: op})
		cancel()
		if err == nil || !strings.Contains(err.Error(), "session demo sleeps: wake it before") {
			t.Fatalf("a frozen holder was asked %s: %v", op, err)
		}
	}
	s.h.mu.Lock()
	after := s.h.contacts
	s.h.mu.Unlock()
	if after != before {
		t.Fatalf("%d requests reached the frozen socket", after-before)
	}
}

func TestClosingAFrozenSessionThawsBeforeAskingOrSignalling(t *testing.T) {
	s := newFreezeScene(t)
	if err := s.act(action.SessionFreeze); err != nil {
		t.Fatal(err)
	}
	signals := withSignals(t, s.e, map[int]bool{5001: true}, map[int]int{5001: 1})
	s.e.sendSignal = func(pid int, sig syscall.Signal) error {
		frozen, err := frozenGroup(s.dir)
		if err != nil || frozen || s.sleeping() {
			t.Fatalf("close signalled before thaw: %t %v", frozen, err)
		}
		return signals.send(pid, sig)
	}
	if _, err := s.e.Execute(t.Context(), closeAskedBy(streamSID)); err != nil {
		t.Fatal(err)
	}
	got := s.h.asked()
	if len(got) != 1 || got[0].Op != stream.OpClose {
		t.Fatalf("the thawed holder was not closed: %+v", got)
	}
}

func TestFreezeAndThawRefuseOtherConversationsAndSharedUnits(t *testing.T) {
	for _, mode := range []string{"another conversation", "tmux", "shared unit"} {
		t.Run(mode, func(t *testing.T) {
			s := newFreezeScene(t)
			conversation := streamSID
			switch mode {
			case "another conversation":
				conversation = senderSID
			case "tmux":
				if err := os.Remove(stream.StatePath(streamSID)); err != nil {
					t.Fatal(err)
				}
			case "shared unit":
				if err := os.Rename(s.dir, filepath.Join(filepath.Dir(s.dir), "tmux.service")); err != nil {
					t.Fatal(err)
				}
				holder := filepath.Join(procDir(), strconv.Itoa(os.Getpid()), "cgroup")
				raw, _ := os.ReadFile(holder)
				if err := os.WriteFile(holder, []byte(strings.ReplaceAll(string(raw), "run-session.service", "tmux.service")), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, kind := range []action.Kind{action.SessionFreeze, action.SessionThaw} {
				_, err := s.e.Execute(t.Context(), action.Request{ID: "sleep", Kind: kind, Target: "demo", Conversation: conversation})
				if err == nil || mode == "tmux" && !strings.Contains(err.Error(), "tmux") {
					t.Fatalf("%s accepted %s: %v", kind, mode, err)
				}
			}
			if s.sleeping() || s.written() != "" {
				t.Fatal("a refused request touched the unit or its alarm")
			}
		})
	}
}

func TestThawWaitsForTheKernelBeforeRestoringAlarms(t *testing.T) {
	for _, failure := range []string{"write", "confirmation"} {
		t.Run(failure, func(t *testing.T) {
			s := newFreezeScene(t)
			if err := s.act(action.SessionFreeze); err != nil {
				t.Fatal(err)
			}
			if failure == "write" {
				s.refuse = "0"
			} else {
				s.stuck = "0"
			}
			started := time.Now()
			thawErr := s.act(action.SessionThaw)
			if thawErr == nil {
				t.Fatal("a session the kernel kept frozen was accepted as awake")
			}
			if failure == "write" && !errors.Is(thawErr, syscall.EPERM) {
				t.Fatalf("the thaw write failure was lost: %v", thawErr)
			}
			if !s.sleeping() || time.Since(started) > time.Second {
				t.Fatal("a failed thaw restored alarms or waited beyond its bound")
			}
		})
	}
}

func TestExecutorUnitPermitsReclaimOnlyInItsUserManager(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/systemd/aacpanel-exec.service")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ProtectControlGroups=yes\n") || !strings.Contains(string(raw), "ReadWritePaths=/sys/fs/cgroup/user.slice/user-%U.slice/user@%U.service\n") {
		t.Fatal("the executor cannot reclaim its session memory while keeping other users' cgroups read-only")
	}
}
