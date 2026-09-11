package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"aacpanel/internal/notify"
	"aacpanel/internal/store"
)

// PushStore is what push delivery keeps in the database.
type PushStore interface {
	notify.Store
	SaveSubscription(ctx context.Context, sub notify.Subscription) error
}

func (p *pgDevices) Subscriptions(ctx context.Context) ([]notify.Subscription, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		SELECT s.device_id, s.endpoint, s.p256dh, s.auth
		  FROM push_subscriptions s
		  JOIN devices dev ON dev.id = s.device_id AND dev.revoked_at IS NULL
		 ORDER BY s.device_id`)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: %w", store.Unavailable(err))
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (notify.Subscription, error) {
		var s notify.Subscription
		err := row.Scan(&s.Device, &s.Endpoint, &s.P256dh, &s.Auth)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("subscriptions: %w", store.Unavailable(err))
	}
	return list, nil
}

func (p *pgDevices) SaveSubscription(ctx context.Context, sub notify.Subscription) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO push_subscriptions (device_id, endpoint, p256dh, auth)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (device_id) DO UPDATE
		SET endpoint = excluded.endpoint, p256dh = excluded.p256dh, auth = excluded.auth,
		    created_at = now(), last_ok = NULL, last_error = NULL, fails = 0`,
		sub.Device, sub.Endpoint, sub.P256dh, sub.Auth)
	if err != nil {
		return fmt.Errorf("saving the subscription: %w", store.Unavailable(err))
	}
	return nil
}

func (p *pgDevices) DropSubscription(ctx context.Context, device int64) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE device_id = $1`, device); err != nil {
		return fmt.Errorf("deleting the subscription: %w", store.Unavailable(err))
	}
	return nil
}

func (p *pgDevices) SubscriptionSent(ctx context.Context, device int64, at time.Time) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`UPDATE push_subscriptions SET last_ok = $2, last_error = NULL, fails = 0 WHERE device_id = $1`, device, at)
	if err != nil {
		return fmt.Errorf("marking the delivery: %w", store.Unavailable(err))
	}
	return nil
}

func (p *pgDevices) SubscriptionFailed(ctx context.Context, device int64, reason string) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`UPDATE push_subscriptions SET last_error = $2, fails = fails + 1 WHERE device_id = $1`, device, reason)
	if err != nil {
		return fmt.Errorf("marking the delivery failure: %w", store.Unavailable(err))
	}
	return nil
}

func (p *pgDevices) PushKeys(ctx context.Context) ([]byte, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT private_key FROM push_keys WHERE id = 1`)
	if err != nil {
		return nil, fmt.Errorf("VAPID keys: %w", store.Unavailable(err))
	}
	key, err := pgx.CollectExactlyOneRow(rows, pgx.RowTo[[]byte])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("VAPID keys: %w", store.Unavailable(err))
	}
	return key, nil
}

func (p *pgDevices) SavePushKeys(ctx context.Context, private, public []byte) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO push_keys (private_key, public_key) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`,
		private, public)
	if err != nil {
		return fmt.Errorf("saving the VAPID keys: %w", store.Unavailable(err))
	}
	return nil
}

type subscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (p *Passkey) pushDevice(w http.ResponseWriter, r *http.Request) (int64, bool) {
	device := p.session.CurrentDevice(r)
	switch {
	case device == 0:
		fail(w, http.StatusUnauthorized, "not authorized")
		return 0, false
	case device < 0:
		fail(w, http.StatusForbidden, "this session was opened with a token, and pushes hang on a device — sign in with a passkey")
		return 0, false
	}
	return device, true
}

// Subscribe binds a push subscription to the device of the current session.
func (p *Passkey) Subscribe(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	device, ok := p.pushDevice(w, r)
	if !ok {
		return
	}

	var body subscribeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "the request body is malformed")
		return
	}

	p256dh, err1 := decodeKey(body.Keys.P256dh)
	auth, err2 := decodeKey(body.Keys.Auth)
	if err1 != nil || err2 != nil {
		fail(w, http.StatusBadRequest, "the subscription keys cannot be decoded")
		return
	}

	sub := notify.Subscription{Device: device, Endpoint: body.Endpoint, P256dh: p256dh, Auth: auth}
	if err := sub.Validate(); err != nil {
		log.Printf("push: the subscription of device %d was rejected: %v", device, err)
		fail(w, http.StatusBadRequest, "the subscription is not good: "+err.Error())
		return
	}

	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	if err := p.store.SaveSubscription(ctx, sub); err != nil {
		dbFail(w, err, "the database could not be reached")
		return
	}
	log.Printf("%s: device %d subscribed to pushes", ClientIP(r), device)
	w.WriteHeader(http.StatusNoContent)
}

// Unsubscribe drops the push subscription of the current device.
func (p *Passkey) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	device, ok := p.pushDevice(w, r)
	if !ok {
		return
	}

	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	if err := p.store.DropSubscription(ctx, device); err != nil {
		dbFail(w, err, "the database could not be reached")
		return
	}
	log.Printf("%s: device %d unsubscribed from pushes", ClientIP(r), device)
	w.WriteHeader(http.StatusNoContent)
}

func decodeKey(s string) ([]byte, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	return base64.URLEncoding.DecodeString(s)
}
