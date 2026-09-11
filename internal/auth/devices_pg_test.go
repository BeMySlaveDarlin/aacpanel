package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/notify"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestPGDevices(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := connect(t, ctx, dsn)
	again := connect(t, ctx, dsn)
	again.Close()

	devices := NewStore(db)

	credID := randomBytes(t, 32)
	dev := &Device{
		Name:            "test phone",
		CredentialID:    credID,
		PublicKey:       randomBytes(t, 77),
		SignCount:       0,
		AAGUID:          make([]byte, 16),
		Transports:      []string{"internal", "hybrid"},
		AttestationType: "none",
		AttestationFmt:  "none",
		BackupEligible:  true,
		BackupState:     false,
	}
	id, err := devices.Add(ctx, dev)
	if err != nil {
		t.Fatalf("writing a device: %v", err)
	}
	t.Cleanup(func() {
		devices.Revoke(context.Background(), id)
	})

	got, err := devices.ByCredential(ctx, credID)
	if err != nil {
		t.Fatalf("looking a device up: %v", err)
	}
	if got.ID != id || got.Name != dev.Name || !bytes.Equal(got.PublicKey, dev.PublicKey) {
		t.Fatalf("the wrong thing was read back: %+v", got)
	}
	if !got.BackupEligible || got.BackupState {
		t.Fatalf("the backup flags went wrong: BE=%v BS=%v", got.BackupEligible, got.BackupState)
	}
	if len(got.Transports) != 2 || got.Transports[0] != "internal" {
		t.Fatalf("the transports went wrong: %v", got.Transports)
	}
	if got.LastSeen != nil {
		t.Fatalf("the device has never logged in, yet last_seen is filled: %v", got.LastSeen)
	}

	seen := time.Now().Truncate(time.Millisecond)
	if err := devices.Seen(ctx, id, 0xFFFFFFFF, true, seen); err != nil {
		t.Fatalf("marking a login: %v", err)
	}
	got, err = devices.ByCredential(ctx, credID)
	if err != nil {
		t.Fatalf("looking up again: %v", err)
	}
	if got.SignCount != 0xFFFFFFFF || !got.BackupState || got.LastSeen == nil {
		t.Fatalf("the login mark was not written: %+v", got)
	}

	if err := devices.Rename(ctx, id, "work"); err != nil {
		t.Fatalf("renaming: %v", err)
	}
	list, err := devices.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, d := range list {
		if d.ID == id {
			found = d.Name == "work"
		}
	}
	if !found {
		t.Fatalf("the renamed device is missing from the list: %+v", list)
	}

	if ok, err := devices.Exists(ctx, id); err != nil || !ok {
		t.Fatalf("Exists: %v %v", ok, err)
	}
	if err := devices.Revoke(ctx, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if ok, err := devices.Exists(ctx, id); err != nil || ok {
		t.Fatalf("a revoked device is still there: %v %v", ok, err)
	}
	if _, err := devices.ByCredential(ctx, credID); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("after a revocation ErrNoDevice was expected, got %v", err)
	}
	if err := devices.Rename(ctx, id, "nobody"); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("renaming a revoked device: %v", err)
	}
	if err := devices.Seen(ctx, id, 1, false, time.Now()); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("marking a login of a revoked device: %v", err)
	}

	if _, err := devices.Add(ctx, &Device{Name: "", CredentialID: randomBytes(t, 32), PublicKey: []byte{1}}); err == nil {
		t.Fatal("an empty device name went through")
	}
}

func TestPGPasskeyFlow(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := connect(t, ctx, dsn)
	devices := NewStore(db)

	if list, err := devices.List(ctx); err != nil || len(list) != 0 {
		t.Fatalf("this test needs an empty devices table (it holds %d, error %v)", len(list), err)
	}

	s := newStandWith(t, devices)
	key := newSoftKey(t)
	c, id := s.registerFirst(t, key, "production phone")
	t.Cleanup(func() { devices.Revoke(context.Background(), id) })

	dev, err := devices.ByCredential(ctx, key.credID)
	if err != nil {
		t.Fatalf("the device was not written: %v", err)
	}
	if dev.ID != id || dev.Name != "production phone" {
		t.Fatalf("the wrong thing was written: %+v", dev)
	}

	c2, code, body := s.login(t, key, key.count+3)
	if code != http.StatusOK {
		t.Fatalf("passkey login: %d %s", code, body)
	}
	if code, body := s.do(t, c2, "GET", "/protected", nil); code != http.StatusOK {
		t.Fatalf("the session after login does not let through: %d %s", code, body)
	}
	dev, err = devices.ByCredential(ctx, key.credID)
	if err != nil {
		t.Fatalf("reading again: %v", err)
	}
	if dev.SignCount != key.count+3 || dev.LastSeen == nil {
		t.Fatalf("the counter or the login time was not written: %+v", dev)
	}

	if _, code, _ := s.login(t, key, key.count+3); code != http.StatusUnauthorized {
		t.Fatalf("a login whose counter did not grow went through: %d", code)
	}

	if code, body := s.do(t, c, "DELETE", fmt.Sprintf("/api/devices/%d", id), nil); code != http.StatusOK {
		t.Fatalf("revoke: %d %s", code, body)
	}
	if code, _ := s.do(t, c2, "GET", "/protected", nil); code != http.StatusUnauthorized {
		t.Fatalf("a revoked device keeps getting through: %d", code)
	}
	if _, code, _ := s.login(t, key, key.count+9); code != http.StatusUnauthorized {
		t.Fatalf("a revoked key was let through again: %d", code)
	}
	if list, err := devices.List(ctx); err != nil || len(list) != 0 {
		t.Fatalf("after the revocation %d devices are left in the database (%v)", len(list), err)
	}
}

func connect(t *testing.T, ctx context.Context, dsn string) *store.Store {
	t.Helper()
	db, err := store.New(dsn)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	db.Run(ctx)
	if !db.Ready() {
		t.Fatalf("the database did not come up within the time allowed")
	}
	t.Cleanup(db.Close)
	return db
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random bytes: %v", err)
	}
	return b
}

func TestPGEnrollCodes(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := connect(t, ctx, dsn)
	store := NewStore(db)

	handle, err := store.UserHandle(ctx)
	if err != nil {
		t.Fatalf("user handle: %v", err)
	}
	if len(handle) != 32 {
		t.Fatalf("the user handle is %d bytes long", len(handle))
	}
	again, err := store.UserHandle(ctx)
	if err != nil || !bytes.Equal(handle, again) {
		t.Fatalf("the user handle is not stable: %v", err)
	}

	code, expires, err := NewEnrollCode(ctx, store, "console")
	if err != nil {
		t.Fatalf("issuing a code: %v", err)
	}
	if time.Until(expires) > EnrollTTL+time.Minute {
		t.Fatalf("the code lives too long: %s", time.Until(expires))
	}

	id, err := MatchEnrollCode(ctx, store, code)
	if err != nil {
		t.Fatalf("the code was not found: %v", err)
	}
	sloppy, err := MatchEnrollCode(ctx, store, strings.ToLower(strings.ReplaceAll(code, "-", "")))
	if err != nil || sloppy != id {
		t.Fatalf("code normalization does not work: %d %v", sloppy, err)
	}

	used, err := store.UseCode(ctx, id)
	if err != nil || !used {
		t.Fatalf("the code did not burn out: %v %v", used, err)
	}
	if used, err = store.UseCode(ctx, id); err != nil || used {
		t.Fatalf("the code burned out twice: %v %v", used, err)
	}
	if _, err := MatchEnrollCode(ctx, store, code); !errors.Is(err, ErrNoCode) {
		t.Fatalf("a burned-out code still works: %v", err)
	}

	stale, err := generateCode()
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	hash, err := hashCode(stale)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := store.AddCode(ctx, hash, "console", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("writing a code: %v", err)
	}
	if _, err := MatchEnrollCode(ctx, store, stale); !errors.Is(err, ErrNoCode) {
		t.Fatalf("an expired code was accepted: %v", err)
	}

	t.Cleanup(func() {
		pool, err := db.Pool()
		if err != nil {
			return
		}
		pool.Exec(context.Background(), "DELETE FROM enroll_codes")
	})
}

func TestPGPushSubscriptions(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := connect(t, ctx, dsn)
	store := NewStore(db)

	id, err := store.Add(ctx, &Device{
		Name:            "phone with push",
		CredentialID:    randomBytes(t, 32),
		PublicKey:       randomBytes(t, 77),
		AAGUID:          make([]byte, 16),
		Transports:      []string{"internal"},
		AttestationType: "none",
		AttestationFmt:  "none",
	})
	if err != nil {
		t.Fatalf("device: %v", err)
	}
	t.Cleanup(func() { store.Revoke(context.Background(), id) })

	sub := notify.Subscription{
		Device:   id,
		Endpoint: "https://push.example.net/" + t.Name(),
		P256dh:   randomBytes(t, 65),
		Auth:     randomBytes(t, 16),
	}
	if err := store.SaveSubscription(ctx, sub); err != nil {
		t.Fatalf("writing a subscription: %v", err)
	}

	subs, err := store.Subscriptions(ctx)
	if err != nil {
		t.Fatalf("list of subscriptions: %v", err)
	}
	var got *notify.Subscription
	for i := range subs {
		if subs[i].Device == id {
			got = &subs[i]
		}
	}
	if got == nil {
		t.Fatalf("the subscription was not found among %d", len(subs))
	}
	if got.Endpoint != sub.Endpoint || !bytes.Equal(got.P256dh, sub.P256dh) || !bytes.Equal(got.Auth, sub.Auth) {
		t.Fatalf("the subscription was read back wrong: %+v", got)
	}

	if err := store.SubscriptionSent(ctx, id, time.Now()); err != nil {
		t.Fatalf("marking a delivery: %v", err)
	}
	if err := store.SubscriptionFailed(ctx, id, "answered 429"); err != nil {
		t.Fatalf("marking an error: %v", err)
	}

	first, err := store.PushKeys(ctx)
	if err != nil {
		t.Fatalf("reading the keys: %v", err)
	}
	if first == nil {
		keys, err := notify.NewKeys()
		if err != nil {
			t.Fatalf("generating: %v", err)
		}
		if err := store.SavePushKeys(ctx, keys.PrivateDER, keys.Public); err != nil {
			t.Fatalf("writing the keys: %v", err)
		}
		first, err = store.PushKeys(ctx)
		if err != nil || first == nil {
			t.Fatalf("the keys were not saved: %v", err)
		}
	}
	other, err := notify.NewKeys()
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	if err := store.SavePushKeys(ctx, other.PrivateDER, other.Public); err != nil {
		t.Fatalf("writing again: %v", err)
	}
	again, err := store.PushKeys(ctx)
	if err != nil {
		t.Fatalf("reading the keys: %v", err)
	}
	if !bytes.Equal(first, again) {
		t.Fatal("the VAPID pair was overwritten — every subscription would turn into garbage")
	}

	if err := store.Revoke(ctx, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	subs, err = store.Subscriptions(ctx)
	if err != nil {
		t.Fatalf("list of subscriptions: %v", err)
	}
	for _, s := range subs {
		if s.Device == id {
			t.Fatal("a subscription survived the revocation of its device — a phone that was thrown away keeps getting push")
		}
	}
}

func TestPGRevokeLeavesTombstone(t *testing.T) {
	dsn := testdb.DSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := connect(t, ctx, dsn)
	devices := NewStore(db)
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}

	credID := randomBytes(t, 32)
	id, err := devices.Add(ctx, &Device{
		Name: "lost phone", CredentialID: credID, PublicKey: randomBytes(t, 77),
		AAGUID: make([]byte, 16), Transports: []string{"internal"},
		AttestationType: "none", AttestationFmt: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM devices WHERE id = $1", id) })

	if _, err := pool.Exec(ctx, `INSERT INTO push_subscriptions (device_id, endpoint, p256dh, auth)
		VALUES ($1, 'https://push.example/ab', $2, $3)`,
		id, randomBytes(t, 65), randomBytes(t, 16)); err != nil {
		t.Fatal(err)
	}

	if err := devices.Revoke(ctx, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	var name string
	var revoked *time.Time
	var cred, key []byte
	if err := pool.QueryRow(ctx,
		`SELECT name, revoked_at, credential_id, public_key FROM devices WHERE id = $1`, id,
	).Scan(&name, &revoked, &cred, &key); err != nil {
		t.Fatalf("there is no tombstone, the row was deleted: %v", err)
	}
	if revoked == nil {
		t.Error("there is no revocation mark")
	}
	if cred != nil || key != nil {
		t.Error("the key of a revoked device stayed in the database")
	}
	if name != "lost phone" {
		t.Errorf("the name was not saved: %q", name)
	}

	var subs int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM push_subscriptions WHERE device_id = $1", id).Scan(&subs); err != nil {
		t.Fatal(err)
	}
	if subs != 0 {
		t.Error("the push subscription survived the revocation — push will go to a lost phone")
	}

	live, err := devices.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range live {
		if d.ID == id {
			t.Error("a revoked device stayed among the ones that let through")
		}
	}
	gone, err := devices.Revoked(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(gone, func(d Device) bool { return d.ID == id }) {
		t.Error("the revoked one is missing from the list of revoked ones — again no trace is left")
	}

	if err := devices.Revoke(ctx, id); !errors.Is(err, ErrNoDevice) {
		t.Errorf("revoking again: %v, want ErrNoDevice", err)
	}

	again, err := devices.Add(ctx, &Device{
		Name: "the same one, registered again", CredentialID: credID, PublicKey: randomBytes(t, 77),
		AAGUID: make([]byte, 16), Transports: []string{"internal"},
		AttestationType: "none", AttestationFmt: "none",
	})
	if err != nil {
		t.Fatalf("registering the same key again: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM devices WHERE id = $1", again) })
}

func TestPGRevokeIsAllOrNothing(t *testing.T) {
	dsn := testdb.DSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := connect(t, ctx, dsn)
	devices := NewStore(db)
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}

	id, err := devices.Add(ctx, &Device{
		Name: "half-revoked", CredentialID: randomBytes(t, 32), PublicKey: randomBytes(t, 77),
		AAGUID: make([]byte, 16), Transports: []string{"internal"},
		AttestationType: "none", AttestationFmt: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM devices WHERE id = $1", id) })

	if _, err := pool.Exec(ctx,
		"UPDATE devices SET revoked_at = now() WHERE id = $1", id); err == nil {
		t.Error("the revocation mark went through while the key stayed — the device is revoked and still lets through")
	}
	if _, err := pool.Exec(ctx,
		"UPDATE devices SET credential_id = NULL, public_key = NULL WHERE id = $1", id); err == nil {
		t.Error("the key was wiped without a mark — the device does not let through and is not revoked either")
	}
}
