package observer

import (
	"context"
	"fmt"
	"slices"

	"aacpanel/internal/action"
)

// SetAlarmsSleeping persists the mark before the executor closes the process.
// The same owner clears it if the close fails, so its live alarms keep ringing.
func (k *Keeper) SetAlarmsSleeping(owner Owner, sleeping bool) error {
	if owner.Codex {
		return fmt.Errorf("only Claude alarms can sleep")
	}
	k.mu.Lock()
	all := make([]*ringing, 0, len(k.alarms))
	for _, r := range k.alarms {
		if r.rec.Owner.Same(owner) {
			all = append(all, r)
		}
	}
	k.mu.Unlock()
	for _, r := range all {
		r.busy.Lock()
		k.mu.Lock()
		rec, kept := r.rec, k.alarms[r.rec.ID] == r
		k.mu.Unlock()
		if kept && rec.Owner.Same(owner) && rec.Sleeping != sleeping {
			rec.Sleeping = sleeping
			if err := writeAlarm(k.alarmDir, rec); err != nil {
				r.busy.Unlock()
				return fmt.Errorf("alarm %s: the sleep mark was not written: %w", rec.Name, err)
			}
			k.mu.Lock()
			r.rec = rec
			k.mu.Unlock()
		}
		r.busy.Unlock()
	}
	return nil
}

// SleepingAlarms exposes schedules without giving the service another copy
// to maintain. A single read covers every sleeping conversation on the host.
func (k *Keeper) SleepingAlarms() *action.SleepingAlarms {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := &action.SleepingAlarms{Alarms: []action.SleepingAlarm{}}
	for _, r := range k.alarms {
		if r.rec.Sleeping && !r.rec.Owner.Codex {
			out.Alarms = append(out.Alarms, action.SleepingAlarm{ID: r.rec.ID, Session: r.rec.Owner.Session,
				Conversation: r.rec.Owner.Conversation, Next: r.rec.Next})
		}
	}
	slices.SortFunc(out.Alarms, func(a, b action.SleepingAlarm) int { return a.Next.Compare(b.Next) })
	return out
}

// CancelSleepingAlarm reconciles a closed sleeping conversation. A concurrent
// resume wins: neither an already rebound alarm nor a live owner is removed.
func (k *Keeper) CancelSleepingAlarm(ctx context.Context, conversation, id string) error {
	k.mu.Lock()
	r := k.alarms[id]
	k.mu.Unlock()
	if r == nil {
		return nil
	}
	r.busy.Lock()
	defer r.busy.Unlock()
	k.mu.Lock()
	rec := r.rec
	k.mu.Unlock()
	if !rec.Sleeping || rec.Owner.Codex || rec.Owner.Conversation != conversation {
		return nil
	}
	_, live, err := k.owners.Find(ctx, conversation)
	if err != nil || live {
		return err
	}
	k.dropAlarm(r, "the sleeping conversation was closed")
	return nil
}
