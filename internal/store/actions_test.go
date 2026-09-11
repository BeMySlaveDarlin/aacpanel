package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/testdb"
)

type actionsStand struct {
	store *Store
	pool  *pgxpool.Pool
}

func newActionsStand(t *testing.T) *actionsStand {
	t.Helper()
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	st, err := New(dsn)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	st.Run(ctx)
	if !st.Ready() {
		t.Fatal("the database did not come up")
	}
	t.Cleanup(st.Close)

	pool, err := st.Pool()
	if err != nil {
		t.Fatalf("the pool: %v", err)
	}
	clean := func() {
		ctx := context.Background()
		pool.Exec(ctx, `ALTER TABLE actions DISABLE TRIGGER actions_append_only`)
		pool.Exec(ctx, `DELETE FROM actions`)
		pool.Exec(ctx, `ALTER TABLE actions ENABLE TRIGGER actions_append_only`)
	}
	clean()
	t.Cleanup(clean)

	return &actionsStand{store: st, pool: pool}
}

func testAction() Action {
	device := int64(1)
	return Action{
		DeviceID:   &device,
		DeviceName: "phone",
		Kind:       "container.stop",
		Target:     "shop",
		Params:     map[string]any{"timeout": float64(15)},
	}
}

func TestActionLoggedBeforeAndAfter(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	id, err := s.store.LogAttempt(ctx, testAction())
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}

	list, err := s.store.Actions(ctx, ActionsReq{})
	if err != nil {
		t.Fatalf("the log: %v", err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("the attempt did not reach the log: %+v", list)
	}
	if list[0].Result != "" || list[0].FinishedAt != nil {
		t.Fatalf("an unfinished action has an outcome: %+v", list[0])
	}
	if list[0].Kind != "container.stop" || list[0].Target != "shop" || list[0].DeviceName != "phone" {
		t.Fatalf("the record is distorted: %+v", list[0])
	}
	if list[0].Params["timeout"] != float64(15) {
		t.Fatalf("the parameters are lost: %+v", list[0].Params)
	}

	if err := s.store.LogResult(ctx, id, ActionOutcome{Result: ActionOK, Duration: 1200 * time.Millisecond}); err != nil {
		t.Fatalf("the outcome: %v", err)
	}

	list, _ = s.store.Actions(ctx, ActionsReq{})
	if list[0].Result != ActionOK || list[0].FinishedAt == nil {
		t.Fatalf("the outcome was not written: %+v", list[0])
	}
	if list[0].DurationMS == nil || *list[0].DurationMS != 1200 {
		t.Fatalf("the duration: %+v", list[0].DurationMS)
	}
}

func TestActionFailureIsVisible(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	id, err := s.store.LogAttempt(ctx, testAction())
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	err = s.store.LogResult(ctx, id, ActionOutcome{
		Result:   ActionFailed,
		Error:    "the container does not answer SIGTERM",
		Duration: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("the outcome: %v", err)
	}

	list, _ := s.store.Actions(ctx, ActionsReq{})
	if list[0].Result != ActionFailed || list[0].Error != "the container does not answer SIGTERM" {
		t.Fatalf("the failure was written wrong: %+v", list[0])
	}
}

func TestActionDetailSurvives(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	id, err := s.store.LogAttempt(ctx, testAction())
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	if err := s.store.LogResult(ctx, id, ActionOutcome{
		Result:   ActionOK,
		Detail:   "session aacpanel-2 was raised",
		Duration: 2 * time.Second,
	}); err != nil {
		t.Fatalf("the outcome: %v", err)
	}

	list, _ := s.store.Actions(ctx, ActionsReq{})
	if list[0].Detail != "session aacpanel-2 was raised" {
		t.Fatalf("the detail is lost: %+v", list[0])
	}
}

func TestActionDetailWithoutResultRejected(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	id, err := s.store.LogAttempt(ctx, testAction())
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET detail = 'a session was raised' WHERE id = $1`, id); err == nil {
		t.Fatal("a detail was written into an action with no outcome")
	}
}

func TestActionsAreImmutable(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	id, err := s.store.LogAttempt(ctx, testAction())
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	if err := s.store.LogResult(ctx, id, ActionOutcome{Result: ActionOK, Duration: time.Second}); err != nil {
		t.Fatalf("the outcome: %v", err)
	}

	err = s.store.LogResult(ctx, id, ActionOutcome{Result: ActionFailed, Error: "rewriting history"})
	if !errors.Is(err, ErrActionFinished) {
		t.Fatalf("the outcome was rewritten through the storage layer: %v", err)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE actions SET result = 'failed' WHERE id = $1`, id); err == nil {
		t.Fatal("the outcome was rewritten by a direct UPDATE")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET target = 'another' WHERE id = $1`, id); err == nil {
		t.Fatal("the action's target was rewritten by a direct UPDATE")
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM actions WHERE id = $1`, id); err == nil {
		t.Fatal("a log record was deleted")
	}
	if _, err := s.pool.Exec(ctx, `TRUNCATE actions`); err == nil {
		t.Fatal("the log was wiped entirely through TRUNCATE")
	}

	list, _ := s.store.Actions(ctx, ActionsReq{})
	if len(list) != 1 || list[0].Result != ActionOK || list[0].Target != "shop" {
		t.Fatalf("the log changed after all: %+v", list)
	}
}

func TestUnfinishedActionAllowsOnlyOutcome(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	id, err := s.store.LogAttempt(ctx, testAction())
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET kind = 'container.start' WHERE id = $1`, id); err == nil {
		t.Fatal("the action's kind was rewritten on an unfinished record")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET ts = now() - interval '1 day' WHERE id = $1`, id); err == nil {
		t.Fatal("the action's time was moved backwards")
	}
}

func TestActionOutcomeMustBeComplete(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	id, err := s.store.LogAttempt(ctx, testAction())
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET error = 'an error with no outcome' WHERE id = $1`, id); err == nil {
		t.Fatal("an error with no outcome was written")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET result = 'ok' WHERE id = $1`, id); err == nil {
		t.Fatal("an outcome with no finish time was written")
	}

	if err := s.store.LogResult(ctx, id, ActionOutcome{Result: "something like that"}); err == nil {
		t.Fatal("an unknown outcome was accepted by the storage layer")
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE actions SET result = 'something like that', finished_at = now() WHERE id = $1`, id); err == nil {
		t.Fatal("an unknown outcome was accepted by the schema")
	}
}

func TestActionsSurviveDeviceRevocation(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	var device int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO devices (name, credential_id, public_key, aaguid, transports)
		VALUES ('the log phone', $1, '\x02', '\x', '{internal}') RETURNING id`,
		[]byte("cred-"+t.Name())).Scan(&device)
	if err != nil {
		t.Fatalf("the device: %v", err)
	}

	a := testAction()
	a.DeviceID = &device
	a.DeviceName = "the log phone"
	if _, err := s.store.LogAttempt(ctx, a); err != nil {
		t.Fatalf("the attempt: %v", err)
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM devices WHERE id = $1`, device); err != nil {
		t.Fatalf("revoking the device: %v", err)
	}

	list, err := s.store.Actions(ctx, ActionsReq{})
	if err != nil {
		t.Fatalf("the log: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("the log lost the record when the device was revoked: %+v", list)
	}
	if list[0].DeviceName != "the log phone" {
		t.Fatalf("the device's name was not preserved: %+v", list[0])
	}
}

func TestActionsPaging(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	var ids []int64
	for i := range 5 {
		a := testAction()
		a.Target = string(rune('a' + i))
		id, err := s.store.LogAttempt(ctx, a)
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	first, err := s.store.Actions(ctx, ActionsReq{Limit: 2})
	if err != nil {
		t.Fatalf("the log: %v", err)
	}
	if len(first) != 2 || first[0].ID != ids[4] || first[1].ID != ids[3] {
		t.Fatalf("the first page is not from the end: %+v", first)
	}

	second, err := s.store.Actions(ctx, ActionsReq{Limit: 2, Before: first[1].ID})
	if err != nil {
		t.Fatalf("the log: %v", err)
	}
	if len(second) != 2 || second[0].ID != ids[2] || second[1].ID != ids[1] {
		t.Fatalf("the second page does not continue the first: %+v", second)
	}
}

func TestActedOn(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	if found, err := s.store.ActedOn(ctx, "shop", 10*time.Minute); err != nil || found {
		t.Fatalf("actions were found for an untouched target: %v %v", found, err)
	}

	a := testAction()
	a.Target = "shop"
	if _, err := s.store.LogAttempt(ctx, a); err != nil {
		t.Fatalf("the attempt: %v", err)
	}

	if found, err := s.store.ActedOn(ctx, "shop", 10*time.Minute); err != nil || !found {
		t.Fatalf("a fresh action was not found: %v %v", found, err)
	}
	if found, err := s.store.ActedOn(ctx, "another-stack", 10*time.Minute); err != nil || found {
		t.Fatalf("an action was found for somebody else's target: %v %v", found, err)
	}

	s.pool.Exec(ctx, `ALTER TABLE actions DISABLE TRIGGER actions_append_only`)
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET ts = now() - interval '2 hours' WHERE target = 'shop'`); err != nil {
		t.Fatalf("shifting the time: %v", err)
	}
	s.pool.Exec(ctx, `ALTER TABLE actions ENABLE TRIGGER actions_append_only`)

	if found, err := s.store.ActedOn(ctx, "shop", 10*time.Minute); err != nil || found {
		t.Fatalf("a two-hour-old action fell into a ten-minute window: %v %v", found, err)
	}
	if found, err := s.store.ActedOn(ctx, "shop", 3*time.Hour); err != nil || !found {
		t.Fatalf("the action was not found in a three-hour window: %v %v", found, err)
	}

	if _, err := s.store.ActedOn(ctx, "", time.Minute); err == nil {
		t.Error("an empty target was accepted")
	}
	if _, err := s.store.ActedOn(ctx, "shop", 0); err == nil {
		t.Error("a zero window was accepted")
	}
}

func TestActionsFilterByResultPG(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	var okID, failedID, pendingID int64
	for i, outcome := range []string{ActionOK, ActionFailed, ""} {
		id, err := s.store.LogAttempt(ctx, testAction())
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		switch outcome {
		case ActionOK:
			okID = id
		case ActionFailed:
			failedID = id
		default:
			pendingID = id
			continue
		}
		if err := s.store.LogResult(ctx, id, ActionOutcome{
			Result: outcome, Error: "it did not work out", Duration: time.Second,
		}); err != nil {
			t.Fatalf("outcome %d: %v", i, err)
		}
	}

	ids := func(req ActionsReq) []int64 {
		t.Helper()
		list, err := s.store.Actions(ctx, req)
		if err != nil {
			t.Fatalf("the log %+v: %v", req, err)
		}
		out := make([]int64, 0, len(list))
		for _, a := range list {
			out = append(out, a.ID)
		}
		return out
	}

	if got := ids(ActionsReq{}); len(got) != 3 {
		t.Errorf("%d records without a filter, wanted 3: %v", len(got), got)
	}
	if got := ids(ActionsReq{Result: ActionOK}); len(got) != 1 || got[0] != okID {
		t.Errorf("the \"done\" filter gave %v, wanted [%d]", got, okID)
	}
	if got := ids(ActionsReq{Result: ActionFailed}); len(got) != 1 || got[0] != failedID {
		t.Errorf("the \"failures\" filter gave %v, wanted [%d]", got, failedID)
	}
	if got := ids(ActionsReq{Result: "pending"}); len(got) != 1 || got[0] != pendingID {
		t.Errorf("the \"in progress\" filter gave %v, wanted [%d]", got, pendingID)
	}

	if got := ids(ActionsReq{Result: "pending", Before: pendingID}); len(got) != 0 {
		t.Errorf("with before=%d the filter served %v, wanted nothing", pendingID, got)
	}

	if _, err := s.store.Actions(ctx, ActionsReq{Result: "made up"}); err == nil {
		t.Error("an unknown outcome was accepted: the value goes into SQL, the list has to be closed")
	}
}

func TestStaleAttachmentsCloseButOthersKeepRunning(t *testing.T) {
	s := newActionsStand(t)
	ctx := context.Background()

	attach := testAction()
	attach.Kind = "term.attach"
	attach.Target = "aacpanel"
	stale, err := s.store.LogAttempt(ctx, attach)
	if err != nil {
		t.Fatalf("the attachment: %v", err)
	}
	running, err := s.store.LogAttempt(ctx, testAction())
	if err != nil {
		t.Fatalf("the action in flight: %v", err)
	}

	n, err := s.store.CloseStaleAttachments(ctx)
	if err != nil {
		t.Fatalf("the cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("%d attachments closed, one was expected", n)
	}

	list, err := s.store.Actions(ctx, ActionsReq{})
	if err != nil {
		t.Fatalf("the log: %v", err)
	}
	byID := map[int64]Action{}
	for _, a := range list {
		byID[a.ID] = a
	}
	if got := byID[stale]; got.Result != ActionFailed || got.FinishedAt == nil {
		t.Errorf("the attachment was left without an outcome: result=%q finished=%v", got.Result, got.FinishedAt)
	}
	if got := byID[stale]; got.Detail == "" {
		t.Error("an outcome without a reason: a day later nobody will remember why the attachment was cut off")
	}
	if got := byID[running]; got.Result != "" || got.FinishedAt != nil {
		t.Errorf("somebody else's action in flight was closed for company: result=%q", got.Result)
	}

	again, err := s.store.CloseStaleAttachments(ctx)
	if err != nil {
		t.Fatalf("the repeat: %v", err)
	}
	if again != 0 {
		t.Errorf("the repeated cleanup touched %d rows, and there was nothing to close", again)
	}
}
