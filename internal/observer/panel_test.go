package observer

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The panel ends an observer by the id its directory is named by, and only for
// the conversation of the session the press was made on: a press on the
// screen of one session ends nothing of another's. Its command stops with it
// and its directory goes, as when the session stops it.
func TestThePanelStopsAnObserverOfTheSessionItNames(t *testing.T) {
	quick(t)
	units, owners := newUnits(), &fakeOwners{alive: true}
	dir := filepath.Join(t.TempDir(), "observers")
	k := NewKeeper(dir, units, owners)
	keeping(t, k)
	if _, err := k.Start(context.Background(), lab, Spec{Name: "ci", Dir: t.TempDir(), Wake: AtExit,
		Command: "sleep 30"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("the observers kept: %v", entries)
	}
	id := entries[0].Name()

	if _, _, err := k.StopFor(context.Background(), "0f1e2d3c-0000-4000-8000-000000000000", id); err == nil {
		t.Fatal("the observer of one session was stopped by a press on the screen of another")
	}
	if units.wasStopped(Unit(id)) {
		t.Fatal("a refused stop stopped the unit")
	}
	if _, _, err := k.StopFor(context.Background(), lab.Conversation, "00000000000000ff"); err == nil {
		t.Error("an observer the keeper does not hold was stopped")
	}
	v, notice, err := k.StopFor(context.Background(), lab.Conversation, id)
	if err != nil {
		t.Fatal(err)
	}
	if notice != "the session was notified by letter" || v.Name != "ci" || !units.wasStopped(Unit(id)) {
		t.Errorf("the stop said %+v, the unit stopped %v", v, units.wasStopped(Unit(id)))
	}
	if _, err := os.Stat(filepath.Join(dir, id)); !os.IsNotExist(err) {
		t.Errorf("the directory of a stopped observer is still there: %v", err)
	}
	if list, _ := k.List(context.Background(), lab); len(list) != 0 {
		t.Errorf("the session still lists %+v", list)
	}
	if _, _, err := k.StopFor(context.Background(), lab.Conversation, id); err == nil {
		t.Error("a second press stopped the observer once more")
	}
}

// The panel cancels an alarm the same way: by its id, for the session it
// names, and the alarm is gone from the disk and rings no more.
func TestThePanelCancelsAnAlarmOfTheSessionItNames(t *testing.T) {
	k, owners, clock, dir := alarmed(t)
	if _, err := k.SetAlarm(context.Background(), lab, AlarmAsk{Name: "standup", Every: "10m"}); err != nil {
		t.Fatal(err)
	}
	kept := files(dir)
	if len(kept) != 1 {
		t.Fatalf("the alarms kept: %v", kept)
	}
	id := strings.TrimSuffix(kept[0], ".json")

	if _, _, err := k.CancelFor(context.Background(), "0f1e2d3c-0000-4000-8000-000000000000", id); err == nil {
		t.Fatal("the alarm of one session was cancelled by a press on the screen of another")
	}
	v, notice, err := k.CancelFor(context.Background(), lab.Conversation, id)
	if err != nil {
		t.Fatal(err)
	}
	if notice != "the session was notified by letter" || v.Name != "standup" || len(files(dir)) != 0 {
		t.Errorf("the cancel said %+v and left %v", v, files(dir))
	}
	clock.move(11 * time.Minute)
	k.look(context.Background())
	if n := len(owners.got()); n != 1 {
		t.Errorf("a cancelled alarm rang %d times", n)
	}
	if _, _, err := k.CancelFor(context.Background(), lab.Conversation, id); err == nil {
		t.Error("a second press cancelled the alarm once more")
	}
}

// The collector reads the observers and the alarms from the files the keeper
// writes, and reads nothing the keeper does not write: a key it looks for
// under a name the records do not carry reads as absent on every observer,
// and the screen says the observer never woke its session, or never ends.
func TestTheCollectorReadsTheRecordsAsTheKeeperWritesThem(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "agent", "observers.py"))
	if err != nil {
		t.Fatalf("the collector source is out of reach: %v", err)
	}
	src := string(raw)

	written := map[string]bool{}
	for _, typ := range []reflect.Type{reflect.TypeOf(Record{}), reflect.TypeOf(Spec{}), reflect.TypeOf(Owner{}),
		reflect.TypeOf(Ending{}), reflect.TypeOf(Alarm{})} {
		for i := 0; i < typ.NumField(); i++ {
			if name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]; name != "" && name != "-" {
				written[name] = true
			}
		}
	}
	read := regexp.MustCompile(`\.get\("([a-zA-Z]+)"\)`).FindAllStringSubmatch(src, -1)
	if len(read) < 10 {
		t.Fatalf("only %d keys read in the collector: the test looks in the wrong place", len(read))
	}
	for _, m := range read {
		if !written[m[1]] {
			t.Errorf("the collector reads %q, which no record of the keeper carries", m[1])
		}
	}

	for name, file := range map[string]string{"RECORD": recordFile, "LOG": logFile, "OLD_LOG": oldLogFile,
		"END": endFile} {
		if !strings.Contains(src, "\n"+name+` = "`+file+`"`) {
			t.Errorf("the collector does not name %s %q, as the keeper does", name, file)
		}
	}
	if filepath.Base(alarmDir("/state/aacpanel/observers")) != "alarms" || !strings.Contains(src, `"alarms")`) {
		t.Error("the collector looks for the alarms elsewhere than the keeper keeps them")
	}
}
