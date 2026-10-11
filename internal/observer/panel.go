package observer

import (
	"context"
	"errors"
	"fmt"
)

// The panel shows a person the observers and the alarms of a session where it
// shows its background work, and ends one at a press. It reads them from the
// directories the keeper writes: the collector reads the files, the service
// gets nothing that starts a command. What it asks of the keeper is the end of
// one, through an action of the executor, by the id the files are named by and
// the conversation of the session the action names: a press on the screen of
// one session ends nothing of another's.

// StopFor ends the observer of the conversation by its id, for the panel.
func (k *Keeper) StopFor(ctx context.Context, conversation, id string) (View, string, error) {
	k.mu.Lock()
	w, ok := k.watch[id]
	held := ok && w.ready && w.rec.Owner.Conversation == conversation
	var view View
	if held {
		view = w.view()
	}
	k.mu.Unlock()
	if !held {
		return View{}, "", errors.New("the session holds this observer no more: it was stopped, or went with " +
			"the session")
	}
	w.busy.Lock()
	defer w.busy.Unlock()
	if !k.kept(w) {
		return View{}, "", fmt.Errorf("observer %s ended as it was stopped", view.Name)
	}
	k.endHeld(ctx, w, "the panel stopped it")
	return view, k.panelNotice(ctx, w.rec.Owner, "observer "+view.Name, "stopped"), nil
}

// CancelFor ends the alarm of the conversation by its id, for the panel.
func (k *Keeper) CancelFor(ctx context.Context, conversation, id string) (AlarmView, string, error) {
	k.mu.Lock()
	r, ok := k.alarms[id]
	held := ok && r.rec.Owner.Conversation == conversation
	k.mu.Unlock()
	if !held {
		return AlarmView{}, "", errors.New("the session holds this alarm no more: it rang its last, was " +
			"cancelled, or went with the session")
	}
	r.busy.Lock()
	defer r.busy.Unlock()
	k.mu.Lock()
	view := r.rec.view()
	k.mu.Unlock()
	if !k.dropAlarm(r, "the panel cancelled it") {
		return AlarmView{}, "", fmt.Errorf("alarm %s rang its last as it was cancelled", view.Name)
	}
	return view, k.panelNotice(ctx, r.rec.Owner, "alarm "+view.Name, "cancelled"), nil
}

// A failed letter does not undo a stop. The action's answer must say that the
// session is still waiting, rather than invite a retry of work already ended.
func (k *Keeper) panelNotice(ctx context.Context, owner Owner, from, verb string) string {
	if err := k.owners.Wake(ctx, owner, from, from+" was "+verb+" from the panel."); err != nil {
		return "the session was not notified: " + err.Error()
	}
	return "the session was notified by letter"
}
