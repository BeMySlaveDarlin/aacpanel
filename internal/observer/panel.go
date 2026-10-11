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
func (k *Keeper) StopFor(ctx context.Context, conversation, id string) (View, error) {
	k.mu.Lock()
	w, ok := k.watch[id]
	held := ok && w.ready && w.rec.Owner.Conversation == conversation
	var view View
	if held {
		view = w.view()
	}
	k.mu.Unlock()
	if !held {
		return View{}, errors.New("the session holds this observer no more: it was stopped, or went with " +
			"the session")
	}
	k.end(ctx, w, "the panel stopped it")
	return view, nil
}

// CancelFor ends the alarm of the conversation by its id, for the panel.
func (k *Keeper) CancelFor(conversation, id string) (AlarmView, error) {
	k.mu.Lock()
	r, ok := k.alarms[id]
	held := ok && r.rec.Owner.Conversation == conversation
	k.mu.Unlock()
	if !held {
		return AlarmView{}, errors.New("the session holds this alarm no more: it rang its last, was " +
			"cancelled, or went with the session")
	}
	r.busy.Lock()
	defer r.busy.Unlock()
	k.mu.Lock()
	view := r.rec.view()
	k.mu.Unlock()
	if !k.dropAlarm(r, "the panel cancelled it") {
		return AlarmView{}, fmt.Errorf("alarm %s rang its last as it was cancelled", view.Name)
	}
	return view, nil
}
