package store

import (
	"testing"
	"time"

	"aacpanel/internal/act"
)

func TestClaimStoreKeepsIncomingAndOnlySettlesDeliveredWorkPG(t *testing.T) {
	s := requestStore(t)
	minute := 1
	row, err := s.CreateClaim(t.Context(), "claim-send", "sender", "peer", "claim-peer", "Ship", act.ClaimInput{Expects: "done", Due: &minute})
	if err != nil {
		t.Fatal(err)
	}
	if row.Peer != "peer" || row.Due == nil || row.Rung != 0 || row.Blocking {
		t.Fatalf("created: %+v", row)
	}
	incoming, err := s.ActsOf(t.Context(), "claim-peer")
	if err != nil || len(incoming) != 1 || incoming[0].ID != row.ID || !incoming[0].Incoming {
		t.Fatalf("incoming: %+v %v", incoming, err)
	}
	outgoing, err := s.ActsOf(t.Context(), "claim-send")
	if err != nil || len(outgoing) != 1 || outgoing[0].Incoming {
		t.Fatalf("outgoing: %+v %v", outgoing, err)
	}
	for _, outcome := range []string{"reply", "done"} {
		lease, err := s.LockClaim(t.Context(), row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.FinishClaim(t.Context(), outcome, "peer"); err != nil {
			lease.Close(t.Context())
			t.Fatal(err)
		}
		lease.Close(t.Context())
		open, err := s.OpenClaims(t.Context(), []string{"peer"})
		want := 1
		if outcome == "done" {
			want = 0
		}
		if err != nil || len(open) != want {
			t.Fatalf("%s open: %+v %v", outcome, open, err)
		}
	}
	lease, err := s.LockClaim(t.Context(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(t.Context())
	if lease.Record.State != "answered" || lease.Record.Answer == nil || lease.Record.Answer.Outcome != "done" || lease.Record.AnsweredBy != "peer" {
		t.Fatalf("answer: %+v", lease.Record)
	}
}

func TestClaimStoreSelectsOnlyDueOpenClaimsAndPersistsRungsPG(t *testing.T) {
	s := requestStore(t)
	minute := 1
	create := func(title string, due *int) act.Record {
		t.Helper()
		row, err := s.CreateClaim(t.Context(), "claim-ladder-send", "ladder-sender", "ladder-peer", "claim-ladder-peer", title, act.ClaimInput{Expects: "reply", Due: due})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	noDue := create("No due", nil)
	active := create("Due", &minute)
	closed := create("Closed", &minute)
	lease, err := s.LockClaim(t.Context(), closed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.FinishClaim(t.Context(), "withdraw", "sender"); err != nil {
		t.Fatal(err)
	}
	lease.Close(t.Context())
	now := time.Now().Truncate(time.Microsecond).Add(2 * time.Minute)
	rows, err := s.DueClaims(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.ID == noDue.ID || row.ID == closed.ID {
			t.Fatalf("not an overdue open claim: %+v", row)
		}
		if row.ID == active.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("due claim is missing")
	}
	lease, err = s.LockClaim(t.Context(), active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.AdvanceClaim(t.Context(), 3, now); err != nil {
		t.Fatal(err)
	}
	lease.Close(t.Context())
	lease, err = s.LockClaim(t.Context(), active.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(t.Context())
	if lease.Record.Rung != 3 || lease.Record.RungAt == nil || !lease.Record.RungAt.Equal(now) {
		t.Fatalf("rung not saved: %+v", lease.Record)
	}
	rows, err = s.DueClaims(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == active.ID {
			t.Fatal("completed ladder selected again")
		}
	}
	if err := s.DeleteClaim(t.Context(), noDue.ID); err != nil {
		t.Fatal(err)
	}
	if lease, err := s.LockClaim(t.Context(), noDue.ID); err == nil {
		lease.Close(t.Context())
		t.Fatal("rejected claim survived deletion")
	}
}
