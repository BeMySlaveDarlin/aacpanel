package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aacpanel/internal/auth"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestHostPollMarksTheSessionOnScreenPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}

	var device int64
	err = pool.QueryRow(ctx, `
		INSERT INTO devices (name, credential_id, public_key)
		VALUES ('probe phone', $1, $2) RETURNING id`,
		[]byte("cred-poll"), []byte("key-poll")).Scan(&device)
	if err != nil {
		t.Fatalf("the device for the probe: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM devices WHERE id = $1`, device)

	guard, err := auth.New("0123456789abcdef0123456789abcdef", auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	if err != nil {
		t.Fatalf("the session service: %v", err)
	}
	guard.UseDevices(aliveDevice{})
	srv := &Server{auth: guard, db: db, host: hostWith(t, liveSnapshot)}

	poll := func(path string) {
		t.Helper()
		w := httptest.NewRecorder()
		srv.auth.IssueDevice(w, false, device)
		r := httptest.NewRequest(http.MethodGet, path, nil)
		for _, c := range w.Result().Cookies() {
			r.AddCookie(c)
		}
		got := httptest.NewRecorder()
		srv.apiHost(got, r)
		if got.Code != http.StatusOK {
			t.Fatalf("the poll %s answered %d: %s", path, got.Code, got.Body.String())
		}
	}

	poll("/api/host?viewing=sentinel")
	seen, err := db.SessionViews().Watching(ctx, "sentinel", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != device {
		t.Fatalf("the poll did not say what the device is looking at: %v", seen)
	}

	poll("/api/host")
	seen, err = db.SessionViews().Watching(ctx, "sentinel", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("a poll without a session on the screen did not clear the mark: %v", seen)
	}
}
