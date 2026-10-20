package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"aacpanel/internal/request"
	"aacpanel/internal/testdb"
)

func requestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRequestOwnershipAndAnswerPersistencePG(t *testing.T) {
	s := requestStore(t)
	blocking := true
	r, err := s.CreateRequest(t.Context(), "request-owner", "work", request.Input{Title: "Restart the router", Steps: "Run reboot", Blocking: &blocking})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Open() || !request.ValidID(r.ID) || r.Created.IsZero() {
		t.Fatalf("created: %+v", r)
	}
	if err := s.WithdrawRequest(t.Context(), "another-session", r.ID); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("another session withdrew: %v", err)
	}
	lease, err := s.LockRequest(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(context.Background())
	if err := lease.Finish(t.Context(), "cant", "The router is unreachable."); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Requests(t.Context(), []string{"request-owner"})
	if err != nil {
		t.Fatal(err)
	}
	var found request.Record
	for _, row := range rows {
		if row.ID == r.ID {
			found = row
		}
	}
	if found.Open() || found.Answer != "cant" || found.Note != "The router is unreachable." {
		t.Fatalf("answer: %+v", found)
	}
	if _, err := s.LockRequest(t.Context(), r.ID); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("second answer: %v", err)
	}
	if err := s.WithdrawRequest(t.Context(), "request-owner", r.ID); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("withdraw answered: %v", err)
	}
}

func TestUnsentRequestRemainsOpenAndWithdrawalWinsPG(t *testing.T) {
	s := requestStore(t)
	blocking := false
	r, err := s.CreateRequest(t.Context(), "request-release", "work", request.Input{Title: "Sign in", Steps: "Open the browser", Blocking: &blocking})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.LockRequest(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A second device cannot reserve the same task while its answer is in flight.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if other, err := s.LockRequest(ctx, r.ID); err == nil {
		other.Close(t.Context())
		t.Fatal("two devices reserved one request")
	}
	lease.Close(t.Context())
	if err := s.WithdrawRequest(t.Context(), "request-release", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LockRequest(t.Context(), r.ID); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("withdrawn request answered: %v", err)
	}
}
