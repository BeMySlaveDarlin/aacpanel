package executor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/observer"
)

// heldUnits stands in for the user's manager: a unit starts as nothing and is
// stopped by name.
type heldUnits struct {
	mu      sync.Mutex
	stopped []string
}

func (*heldUnits) Start(context.Context, string, string, observer.Spec) error { return nil }

func (u *heldUnits) Stop(_ context.Context, unit string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stopped = append(u.stopped, unit)
	return nil
}

func (*heldUnits) Units(context.Context) ([]string, error) { return nil, nil }

func (u *heldUnits) wasStopped(unit string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Contains(u.stopped, unit)
}

// keptBy gives the executor a keeper of its own in a directory of the test,
// the one its actions reach.
func keptBy(t *testing.T, e *Executor) (*observer.Keeper, *heldUnits, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "observers")
	units := &heldUnits{}
	e.keeperOnce.Do(func() { e.keeper = observer.NewKeeper(dir, units, observerOwners{e: e}) })
	return e.keeper, units, dir
}

func onlyEntry(t *testing.T, dir, suffix string) string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%s holds %v", dir, entries)
	}
	return strings.TrimSuffix(entries[0].Name(), suffix)
}

// A press in the panel stops an observer of the session the action names and
// cancels its alarm: the executor finds the conversation the session runs and
// asks the keeper that holds them, by the id the collector read them under.
func TestThePanelStopsTheObserverAndCancelsTheAlarmOfASessionOnTheStream(t *testing.T) {
	onTheStream(t, false)
	e := New(nil, "")
	k, units, dir := keptBy(t, e)
	ctx := context.Background()
	if _, err := k.Start(ctx, streamCaller, observer.Spec{Name: "ci", Command: "sleep 30", Dir: t.TempDir(),
		Wake: observer.AtExit}); err != nil {
		t.Fatal(err)
	}
	id := onlyEntry(t, dir, "")

	if _, err := e.Execute(ctx, action.Request{Kind: action.ObserverStop, Target: "demo",
		Work: &action.Work{ID: "00000000000000ff"}}); err == nil {
		t.Error("an observer the session does not have was stopped")
	}
	detail, err := e.Execute(ctx, action.Request{Kind: action.ObserverStop, Target: "demo", Work: &action.Work{ID: id}})
	if err != nil {
		t.Fatal(err)
	}
	if detail != "observer ci of demo stopped, with its command" || !units.wasStopped(observer.Unit(id)) {
		t.Errorf("the stop said %q, the unit stopped %v", detail, units.wasStopped(observer.Unit(id)))
	}

	if _, err := k.SetAlarm(ctx, streamCaller, observer.AlarmAsk{Name: "standup", Every: "10m"}); err != nil {
		t.Fatal(err)
	}
	alarm := onlyEntry(t, filepath.Join(filepath.Dir(dir), "alarms"), ".json")
	detail, err = e.Execute(ctx, action.Request{Kind: action.AlarmCancel, Target: "demo", Work: &action.Work{ID: alarm}})
	if err != nil || detail != "alarm standup of demo cancelled" {
		t.Errorf("the cancel said %q, %v", detail, err)
	}
	if _, err := e.Execute(ctx, action.Request{Kind: action.AlarmCancel, Target: "demo",
		Work: &action.Work{ID: alarm}}); err == nil {
		t.Error("an alarm cancelled once was cancelled again")
	}
}

// A thread of codex is named by its name as a claude session is, and its
// observer is stopped by its thread.
func TestThePanelStopsTheObserverOfAThreadOfCodex(t *testing.T) {
	srv, e := onCodex(t, nil)
	k, units, dir := keptBy(t, e)
	ctx := context.Background()
	if _, err := k.Start(ctx, observer.Caller{Conversation: codexThread, Codex: true, Home: srv.Home},
		observer.Spec{Name: "deploy", Command: "sleep 30", Dir: t.TempDir(), Wake: observer.EveryLine}); err != nil {
		t.Fatal(err)
	}
	id := onlyEntry(t, dir, "")
	detail, err := e.Execute(ctx, action.Request{Kind: action.ObserverStop, Target: codexName,
		Work: &action.Work{ID: id}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(detail, "observer deploy of ") || !units.wasStopped(observer.Unit(id)) {
		t.Errorf("the stop said %q, the unit stopped %v", detail, units.wasStopped(observer.Unit(id)))
	}
}
