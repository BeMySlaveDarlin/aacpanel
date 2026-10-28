package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/stream"
)

// Message is what a person sends a thread: words, pictures by their paths on
// the host, or both, and the name the panel gave it to take it back by.
type Message struct {
	ID     string   `json:"id,omitempty"`
	Text   string   `json:"text,omitempty"`
	Images []string `json:"images,omitempty"`
}

// input is the message as turn/start and turn/steer take it: the words, then
// each picture as a file of the host codex reads itself.
func (m Message) input() []any {
	out := []any{}
	if m.Text != "" {
		out = append(out, textInput(m.Text)...)
	}
	for _, path := range m.Images {
		out = append(out, map[string]any{"type": "localImage", "path": path})
	}
	return out
}

// Queued is a message that waits in the panel's queue of a thread for the
// first turn that takes it.
type Queued struct {
	Message
	Since time.Time `json:"since"`
}

// Sent is where a message went: into the turn that runs, as a turn of its own,
// or into the panel's queue, at its place there counted from one.
type Sent struct {
	Steered bool
	Place   int
}

// errBusy is a thread that runs a turn a message does not go into: a turn
// that refused it — a review or a compaction takes no message — or one the
// daemon has not listed yet.
var errBusy = errors.New("the thread runs a turn that does not take the message")

// startRefused is a turn/start the daemon refused: the message it carried
// would be refused again.
type startRefused struct{ error }

func (e startRefused) Unwrap() error { return e.error }

// outbox is what the panel holds for a thread until a turn takes it: the
// messages no turn has taken yet, and a model and an effort
// that go with the next turn/start where the daemon would not change them
// sooner. It is kept on the disk by the contour of the home, so an executor
// started again delivers what waited; it goes with the thread when the panel
// closes it or the daemon unloads it. The queue is the person's own words on
// their way to the thread, and the file is the owner's alone.
type outbox struct {
	Queue  []Queued `json:"queue"`
	Model  string   `json:"model,omitempty"`
	Effort string   `json:"effort,omitempty"`
}

func (b *outbox) empty() bool {
	return b == nil || (len(b.Queue) == 0 && b.Model == "" && b.Effort == "")
}

func outboxDir(contour string) string {
	return filepath.Join(stream.Dir(), "codex-outbox", contour)
}

func outboxPath(contour, id string) string { return filepath.Join(outboxDir(contour), id+".json") }

// loadOutboxes reads what an executor that stopped left for the threads of a
// contour.
func loadOutboxes(contour string) map[string]*outbox {
	out := map[string]*outbox{}
	entries, err := os.ReadDir(outboxDir(contour))
	if err != nil {
		return out
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || strings.HasPrefix(id, ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(outboxDir(contour), e.Name()))
		if err != nil {
			continue
		}
		b := &outbox{}
		if json.Unmarshal(raw, b) == nil && !b.empty() {
			out[id] = b
		}
	}
	return out
}

// box is the outbox of a thread, made when it has none. Called with mu held.
func (l *Link) box(id string) *outbox {
	b := l.boxes[id]
	if b == nil {
		b = &outbox{Queue: []Queued{}}
		l.boxes[id] = b
	}
	return b
}

// queued is how many messages wait for a thread. Called with mu held.
func (l *Link) queued(id string) int {
	if b := l.boxes[id]; b != nil {
		return len(b.Queue)
	}
	return 0
}

// keepOutbox writes the outbox of a thread to the disk, and removes it there
// once nothing waits. Called with mu held.
func (l *Link) keepOutbox(id string) {
	path := outboxPath(l.contour, id)
	b := l.boxes[id]
	if b.empty() {
		delete(l.boxes, id)
		_ = os.Remove(path)
		return
	}
	body, err := json.Marshal(b)
	if err == nil {
		err = write(path, body)
	}
	if err != nil {
		log.Printf("codex %s: what waits for thread %s was not kept on the disk, and a restart of the executor "+
			"loses it: %v", l.home, id, err)
	}
}

// later keeps a model and an effort to go with the next turn the panel starts
// in a thread; empty ones clear what was kept.
func (l *Link) later(id, model, effort string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if model == "" && effort == "" && l.boxes[id] == nil {
		return
	}
	b := l.box(id)
	b.Model, b.Effort = model, effort
	l.keepOutbox(id)
}

// nudge asks the link to read the daemon now rather than at the next round.
func (l *Link) nudge() {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// Send gives a thread a message — the person's words, a file, a letter of
// another session — the way claude takes one written during a turn: the turn
// that runs takes it at its next step, and a free thread starts a turn with
// it. A turn that does not take it — a review or a compaction, or one the
// daemon has not listed yet — leaves it in the panel's queue, and so does a
// message that waits there before it, so that none overtakes another: the
// queue gives it to the first turn that takes it, or starts a turn with it
// once the thread is free.
func (l *Link) Send(ctx context.Context, threadID string, m Message) (Sent, error) {
	c, err := l.client()
	if err != nil {
		return Sent{}, err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	l.mu.Lock()
	waiting := l.queued(threadID) > 0
	l.mu.Unlock()
	if !waiting {
		steered, err := l.deliver(ctx, c, threadID, m)
		if !errors.Is(err, errBusy) {
			return Sent{Steered: steered}, err
		}
	}
	return Sent{Place: l.enqueue(threadID, Queued{Message: m, Since: time.Now()})}, nil
}

// Answer gives a thread the answer to a question it asked without waiting. A
// turn that runs takes it at once, past whatever waits in the panel's queue,
// as codex's own terminal hands it — codex asked so as to go on, and an answer
// behind that turn would come after the work it was for — and a free thread
// starts a turn with it. It reports whether the turn that runs took it. An
// answer is never queued: a turn that does not take it is the refusal.
func (l *Link) Answer(ctx context.Context, threadID string, m Message) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	steered, err := l.deliver(ctx, c, threadID, m)
	if errors.Is(err, errBusy) {
		return false, fmt.Errorf("the turn that runs did not take the answer: %w", err)
	}
	return steered, err
}

// deliver gives a message to the turn a thread runs, named by its id so that
// the message goes into that turn or nowhere, and starts a turn with it on a
// free thread. It reports whether the turn that runs took it. A turn that
// ends between the look and the steer leaves no turn to take it, and the
// message starts a turn of its own. A turn that runs on and refuses it, or
// one the daemon has not listed yet, is errBusy. Called with sub held.
func (l *Link) deliver(ctx context.Context, c *conn, id string, m Message) (bool, error) {
	turn, free, err := look(ctx, c, id)
	if err != nil {
		return false, err
	}
	if free {
		return false, l.start(ctx, c, id, m)
	}
	if turn == "" {
		return false, errBusy
	}
	params := map[string]any{"threadId": id, "expectedTurnId": turn, "input": m.input()}
	if m.ID != "" {
		params["clientUserMessageId"] = m.ID
	}
	err = within(ctx, c, "turn/steer", params, nil)
	if err == nil {
		return true, nil
	}
	if !refused(err) {
		return false, err
	}
	refusal := err
	if turn, err = runningTurn(ctx, c, id); err != nil {
		return false, err
	}
	if turn != "" {
		return false, fmt.Errorf("%w: %w", errBusy, refusal)
	}
	return false, l.start(ctx, c, id, m)
}

// enqueue puts a message at the end of the panel's queue of a thread, keeps
// the queue on the disk and has the link look at the thread now. It returns
// the place of the message there.
func (l *Link) enqueue(id string, q Queued) int {
	l.mu.Lock()
	b := l.box(id)
	b.Queue = append(b.Queue, q)
	n := len(b.Queue)
	l.keepOutbox(id)
	l.mu.Unlock()
	l.save(id)
	l.nudge()
	return n
}

// Unqueue takes a message back from the panel's queue of a thread before it
// goes. It reports false when the message is not there: a turn has it.
func (l *Link) Unqueue(threadID, messageID string) bool {
	l.sub.Lock()
	defer l.sub.Unlock()
	l.mu.Lock()
	found := false
	if b := l.boxes[threadID]; b != nil {
		n := len(b.Queue)
		b.Queue = slices.DeleteFunc(b.Queue, func(q Queued) bool { return q.ID == messageID })
		found = len(b.Queue) < n
		l.keepOutbox(threadID)
	}
	l.mu.Unlock()
	if found {
		l.save(threadID)
	}
	return found
}

// look says what a thread runs: the turn in progress, empty when none is
// listed, and whether the thread is free — neither a turn the daemon reports
// active nor one the panel has just started: the daemon answers turn/start
// before the thread turns active, and a second turn/start then would go into
// the first turn.
func look(ctx context.Context, c *conn, id string) (string, bool, error) {
	info, err := read(ctx, c, id)
	if err != nil {
		return "", false, err
	}
	turn, err := runningTurn(ctx, c, id)
	if err != nil {
		return "", false, err
	}
	return turn, turn == "" && info.Status.Type != "active", nil
}

// start starts a turn with a message, and with the model and the effort that
// wait for the next turn. turn/start makes the caller a client of the thread.
// Called with sub held.
func (l *Link) start(ctx context.Context, c *conn, id string, m Message) error {
	params := map[string]any{"threadId": id, "input": m.input()}
	if m.ID != "" {
		params["clientUserMessageId"] = m.ID
	}
	l.mu.Lock()
	if b := l.boxes[id]; b != nil {
		if b.Model != "" {
			params["model"] = b.Model
		}
		if b.Effort != "" {
			params["effort"] = b.Effort
		}
	}
	l.mu.Unlock()
	if err := within(ctx, c, "turn/start", params, nil); err != nil {
		if refused(err) {
			return startRefused{err}
		}
		return err
	}
	l.later(id, "", "")
	l.set(id, func(t *thread) { t.ours, t.subscribed = true, true })
	return nil
}

// drain gives the panel's queue of a thread to the daemon, the oldest message
// first, for as long as the thread takes them: into the turn that runs, if it
// takes one, and as a turn of its own on a free thread, which takes the rest
// behind the first into that turn. So a turn after turn — a goal codex works
// towards starts the next as one ends — leaves no message waiting for a free
// moment that never comes. A message the daemon refuses to start a turn with
// is dropped — it would be refused at every round and hold the rest behind it
// — and the executor's log names the thread; one that did not reach the
// daemon, or that the turn that runs does not take, stays first.
func (l *Link) drain(ctx context.Context, c *conn, id string) {
	l.sub.Lock()
	defer l.sub.Unlock()
	if l.Closing(id) {
		return
	}
	for {
		l.mu.Lock()
		if l.queued(id) == 0 {
			l.mu.Unlock()
			return
		}
		next := l.boxes[id].Queue[0]
		l.mu.Unlock()
		_, err := l.deliver(ctx, c, id, next.Message)
		var bad startRefused
		if err != nil && !errors.As(err, &bad) {
			return
		}
		if err != nil {
			log.Printf("codex %s: thread %s refused a message of the panel's queue, and it is dropped: %v", l.home, id, err)
		}
		l.mu.Lock()
		if b := l.boxes[id]; b != nil && len(b.Queue) > 0 {
			b.Queue = slices.Delete(b.Queue, 0, 1)
			l.keepOutbox(id)
		}
		l.mu.Unlock()
		l.save(id)
	}
}
