package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/observer"
)

func TestSleepClosePersistsAlarmsBeforeEndingTheProcess(t *testing.T) {
	for _, sleeping := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary close", true: "sleep"}[sleeping], func(t *testing.T) {
			onTheStream(t, false)
			e := New(nil, "")
			k, _, dir := keptBy(t, e)
			if _, err := k.SetAlarm(t.Context(), streamCaller, observer.AlarmAsk{Name: "check", In: "5m"}); err != nil {
				t.Fatal(err)
			}
			signals := withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})
			checked := false
			e.sendSignal = func(pid int, sig syscall.Signal) error {
				if pid == 5001 {
					checked = true
					path := filepath.Join(filepath.Dir(dir), "alarms", onlyEntry(t, filepath.Join(filepath.Dir(dir), "alarms"), ".json")+".json")
					raw, err := os.ReadFile(path)
					var rec observer.Alarm
					if err != nil || json.Unmarshal(raw, &rec) != nil || rec.Sleeping != sleeping {
						t.Fatalf("the process was closed before its alarm's sleep mark: %s %v", raw, err)
					}
				}
				return signals.send(pid, sig)
			}
			req := closeAskedBy(streamSID)
			req.Sleeping = sleeping
			if _, err := e.Execute(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			if !checked {
				t.Fatal("the close never checked its process")
			}
			alarms, err := e.SleepingAlarms(t.Context())
			if err != nil || (len(alarms.Alarms) == 1) != sleeping {
				t.Fatalf("sleeping schedules: %+v %v", alarms, err)
			}
		})
	}
}

func TestFailedSleepCloseRestoresLiveAlarms(t *testing.T) {
	onTheStream(t, false)
	e := New(nil, "")
	k, _, _ := keptBy(t, e)
	if _, err := k.SetAlarm(t.Context(), streamCaller, observer.AlarmAsk{Name: "check", In: "5m"}); err != nil {
		t.Fatal(err)
	}
	e.soft, e.poll = time.Millisecond, time.Millisecond
	e.sendSignal = func(_ int, sig syscall.Signal) error {
		if sig == 0 {
			return nil
		}
		return syscall.EPERM
	}
	req := closeAskedBy(streamSID)
	req.Sleeping = true
	if _, err := e.Execute(t.Context(), req); err == nil {
		t.Fatal("a failed close was accepted")
	}
	if len(k.SleepingAlarms().Alarms) != 0 {
		t.Fatal("the still live session's alarms remained suspended")
	}
	if alarms, err := k.Alarms(t.Context(), streamCaller); err != nil || len(alarms) != 1 {
		t.Fatalf("a failed close lost its alarm: %+v %v", alarms, err)
	}
}

func TestSleepingAlarmFindsTheNewProcessAndReconcilesThroughTheActionGate(t *testing.T) {
	onTheStream(t, false)
	e := New(nil, "")
	k, _, dir := keptBy(t, e)
	owner, err := (observerOwners{e: e}).Resolve(t.Context(), streamCaller)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.SetAlarm(t.Context(), streamCaller, observer.AlarmAsk{Name: "check", In: "5m"}); err != nil {
		t.Fatal(err)
	}
	if err := k.SetAlarmsSleeping(owner, true); err != nil {
		t.Fatal(err)
	}
	id := k.SleepingAlarms().Alarms[0].ID
	// A reused pid with another start is a replacement, even on the same transport.
	procFS(t, fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: "/srv/proj", start: "6666",
		args: []string{"claude", "-p", "--input-format", "stream-json", "-n", "demo", "--session-id", streamSID}})
	sessionFiles(t, fakeSession{pid: 5001, name: "demo", start: "6666", sid: streamSID})
	resumed, live, err := (observerOwners{e: e}).Find(t.Context(), streamSID)
	if err != nil || !live || resumed.Same(owner) || resumed.PID != 5001 || resumed.Start != "6666" || resumed.Tmux {
		t.Fatalf("the replacement owner was not resolved: %+v %v %v", resumed, live, err)
	}
	owners, err := (observerOwners{e: e}).FindAll(t.Context(), []string{streamSID, senderSID})
	if err != nil || len(owners) != 1 || owners[streamSID] != resumed {
		t.Fatalf("the shared census did not resolve the requested live conversation: %+v %v", owners, err)
	}
	req := action.Request{ID: "orphan", Kind: action.AlarmCancel, Target: "demo", Sleeping: true,
		Conversation: streamSID, Work: &action.Work{ID: id}}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute(t.Context(), req); err != nil || len(k.SleepingAlarms().Alarms) != 1 {
		t.Fatalf("reconciliation removed a live conversation's alarm: %v", err)
	}
	procFS(t)
	if _, err := e.Execute(t.Context(), req); err != nil || len(k.SleepingAlarms().Alarms) != 0 {
		t.Fatalf("the orphaned alarm was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "alarms", id+".json")); !os.IsNotExist(err) {
		t.Fatalf("the orphaned alarm's file remains: %v", err)
	}
}
