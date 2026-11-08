package observer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAlarmsSurviveSleepAndRebindBeforeRinging(t *testing.T) {
	for _, every := range []string{"", "5m"} {
		t.Run("every="+every, func(t *testing.T) {
			k, owners, clock, dir := alarmed(t)
			ctx := t.Context()
			v, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "check", In: "5m", Every: every})
			if err != nil {
				t.Fatal(err)
			}
			owner, _ := owners.Resolve(ctx, lab)
			owners.resumed = &owner
			if err := k.SetAlarmsSleeping(owner, true); err != nil {
				t.Fatal(err)
			}
			k.lookAtAlarms(ctx)
			if len(k.SleepingAlarms().Alarms) != 1 {
				t.Fatal("the still closing process took its alarm back")
			}
			owners.resumed = nil
			owners.die()
			clock.move(22 * time.Minute)
			for range 100 {
				k.lookAtAlarms(ctx)
			}
			if len(files(dir)) != 1 || len(owners.got()) != 0 {
				t.Fatalf("the sleeping alarm was lost or rang: files %v letters %v", files(dir), owners.got())
			}
			// Loading exercises the disk mark rather than only the keeper's memory.
			k = NewKeeper(dir, newUnits(), owners)
			k.now = clock.now
			k.Load(ctx)
			k.lookAtAlarms(ctx)
			if len(k.SleepingAlarms().Alarms) != 1 {
				t.Fatal("sleep did not survive executor replacement")
			}
			resumed := Owner{Session: "renamed", Conversation: lab.Conversation, PID: 9000, Start: "88", Tmux: true}
			owners.resumed, owners.alive = &resumed, true
			clock.move(sleepStep)
			k.lookAtAlarms(ctx)
			k.lookAtAlarms(ctx)
			if len(owners.got()) != 1 || !owners.woken[0].Same(resumed) || !owners.woken[0].Tmux || !strings.Contains(owners.got()[0], "It rings late") {
				t.Fatalf("the overdue ring was not delivered once to the resumed owner: %+v %v", owners.woken, owners.got())
			}
			if len(k.SleepingAlarms().Alarms) != 0 {
				t.Fatal("a resumed alarm is still sleeping")
			}
			if every == "" {
				if len(files(dir)) != 0 {
					t.Fatal("a one-time alarm remained after delivery")
				}
				return
			}
			rec, err := readAlarm(filepath.Join(alarmDir(dir), files(dir)[0]))
			wantNext := v.Next.Add(20 * time.Minute)
			if err != nil || rec.Sleeping || !rec.Owner.Same(resumed) || !rec.Next.Equal(wantNext) {
				t.Fatalf("the resumed repeat lost its persisted owner or rhythm: %+v %v", rec, err)
			}
			clock.move(wantNext.Sub(clock.now()))
			k.lookAtAlarms(ctx)
			if len(owners.got()) != 2 || strings.Contains(owners.got()[1], "late") {
				t.Fatalf("the repeat missed its next beat: %v", owners.got())
			}
		})
	}
}

func TestSleepingAlarmsShareOneCensusAndWaitBetweenLooks(t *testing.T) {
	k, owners, clock, _ := alarmed(t)
	ctx := t.Context()
	for i := range 5 {
		caller := lab
		caller.Conversation = fmt.Sprintf("%08d-0000-4000-8000-%012d", i, i)
		if _, err := k.SetAlarm(ctx, caller, AlarmAsk{Name: "check", In: "5m"}); err != nil {
			t.Fatal(err)
		}
		owner, _ := owners.Resolve(ctx, caller)
		if err := k.SetAlarmsSleeping(owner, true); err != nil {
			t.Fatal(err)
		}
	}
	k.lookAtAlarms(ctx)
	if owners.searches != 1 {
		t.Fatalf("five sleeping alarms made %d census reads in one look", owners.searches)
	}
	for range 10 {
		k.lookAtAlarms(ctx)
	}
	clock.move(sleepStep - time.Nanosecond)
	k.lookAtAlarms(ctx)
	if owners.searches != 1 {
		t.Fatalf("sleeping alarms read the census between their looks: %d reads", owners.searches)
	}
	clock.move(time.Nanosecond)
	k.lookAtAlarms(ctx)
	if owners.searches != 2 {
		t.Fatalf("the next sleeping look made %d total census reads, want 2", owners.searches)
	}
	owners.resumed = &Owner{Conversation: "00000000-0000-4000-8000-000000000000", PID: 9000, Start: "88", Tmux: true}
	clock.move(sleepStep)
	k.lookAtAlarms(ctx)
	if owners.searches != 3 {
		t.Fatalf("rebinding repeated the shared census: %d total reads, want 3", owners.searches)
	}
}

func TestSleepingAlarmCancellationKeepsLiveAndUnrelatedOwners(t *testing.T) {
	k, owners, _, dir := alarmed(t)
	ctx := t.Context()
	if _, err := k.SetAlarm(ctx, lab, AlarmAsk{Name: "check", In: "5m"}); err != nil {
		t.Fatal(err)
	}
	owner, _ := owners.Resolve(ctx, lab)
	if err := k.SetAlarmsSleeping(owner, true); err != nil {
		t.Fatal(err)
	}
	id := k.SleepingAlarms().Alarms[0].ID
	owners.resumed = &Owner{Conversation: owner.Conversation, PID: 9000, Start: "88"}
	if err := k.CancelSleepingAlarm(ctx, owner.Conversation, id); err != nil || len(files(dir)) != 1 {
		t.Fatalf("a live conversation's alarm was removed: %v", err)
	}
	owners.resumed = nil
	if err := k.CancelSleepingAlarm(ctx, "another-conversation", id); err != nil || len(files(dir)) != 1 {
		t.Fatalf("an unrelated conversation's alarm was removed: %v", err)
	}
	if err := k.CancelSleepingAlarm(ctx, owner.Conversation, id); err != nil || len(files(dir)) != 0 {
		t.Fatalf("the orphaned alarm remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(alarmDir(dir), id+".json")); !os.IsNotExist(err) {
		t.Fatalf("the orphan's file remains: %v", err)
	}
}
