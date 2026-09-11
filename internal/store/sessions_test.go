package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestSessionsHistoryPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := s.HostID(ctx, "SESSIONS-TEST")
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		pool.Exec(ctx, "DELETE FROM sessions_1m WHERE host_id = $1", hostID)
		pool.Exec(ctx, "DELETE FROM sessions_1h WHERE host_id = $1", hostID)
	}
	cleanup()
	defer cleanup()

	partitionsBack(t, ctx, s, "sessions_1m", 50*time.Hour)
	partitionsBack(t, ctx, s, "sessions_1h", 50*time.Hour)

	base := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Minute)
	peakAt := base.Add(24 * time.Hour)
	gapFrom := base.Add(30 * time.Hour)
	gapTo := gapFrom.Add(time.Hour)
	notesLast := base.Add(42 * time.Hour)

	insert := func(table string, at time.Time, name string, avg, peak float64, tokens int64, messages, samples int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO `+table+` (bucket, host_id, name, tokens_max, pct_avg, pct_max, messages_max, samples)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT DO NOTHING`,
			at, hostID, name, tokens, avg, peak, messages, samples); err != nil {
			t.Fatal(err)
		}
	}

	for i := range 48 * 60 {
		at := base.Add(time.Duration(i) * time.Minute)
		if !at.Before(gapFrom) && at.Before(gapTo) {
			continue
		}
		pct, tokens, messages := 12.0, int64(24_000), 40
		if at.Equal(peakAt) {
			pct, tokens, messages = 95.0, 190_000, 320
		}
		insert("sessions_1m", at, "aacpanel", pct, pct, tokens, messages, 6)
		if !at.After(notesLast) {
			insert("sessions_1m", at, "notes", 40.0, 40.0, 80_000, 120, 6)
		}
	}

	for i := range 48 {
		at := base.Add(time.Duration(i) * time.Hour)
		if !at.Before(gapFrom) && at.Before(gapTo) {
			continue
		}
		avg, peak, tokens, messages := 12.0, 12.0, int64(24_000), 40
		if !at.After(peakAt) && peakAt.Before(at.Add(time.Hour)) {
			avg, peak, tokens, messages = 13.4, 95.0, 190_000, 320
		}
		insert("sessions_1h", at, "aacpanel", avg, peak, tokens, messages, 360)
		if !at.After(notesLast) {
			insert("sessions_1h", at, "notes", 40.0, 40.0, 80_000, 120, 360)
		}
	}

	week := func(req SessionsReq) *SessionsHistory {
		t.Helper()
		req.HostID, req.From, req.To = hostID, base, time.Now().UTC()
		got, err := s.SessionsFor(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	t.Run("the filling peak does not dissolve into the mean", func(t *testing.T) {
		got := week(SessionsReq{Resolution: Res1h, MaxPoints: 100})
		if got.StepSec < 3600 {
			t.Fatalf("a step of %d s — hourly buckets smaller than an hour, the check checks nothing", got.StepSec)
		}

		var row *SessionRow
		for i := range got.Rows {
			if got.Rows[i].Name == "aacpanel" {
				row = &got.Rows[i]
			}
		}
		if row == nil {
			t.Fatalf("the aacpanel session is not in the reply: %+v", got.Rows)
		}
		if row.PctMax < 94.9 {
			t.Errorf("peak %.1f%%, expected 95 — the maximum was averaged away", row.PctMax)
		}
		if row.PctAvg > 20 {
			t.Errorf("mean %.1f%% — the peak got into it rather than the other way round", row.PctAvg)
		}
		if row.TokensMax != 190_000 || row.MessagesMax != 320 {
			t.Errorf("at the peak %d tokens and %d messages, expected 190000 and 320", row.TokensMax, row.MessagesMax)
		}

		peak := 0.0
		for _, v := range got.PctMax {
			if v != nil && *v > peak {
				peak = *v
			}
		}
		if peak < 94.9 {
			t.Errorf("the series peak %.1f%%, expected 95", peak)
		}
	})

	t.Run("coarsening the buckets does not eat the peak", func(t *testing.T) {
		got := week(SessionsReq{Resolution: Res1m, MaxPoints: 50})
		if got.StepSec <= 60 {
			t.Fatalf("a step of %d s — the buckets did not coarsen, the check checks nothing", got.StepSec)
		}
		peak := 0.0
		for _, v := range got.PctMax {
			if v != nil && *v > peak {
				peak = *v
			}
		}
		if peak < 94.9 {
			t.Errorf("the series peak %.1f%%, expected 95 — the downsampling took something other than the maximum", peak)
		}
	})

	t.Run("the one worked in last comes first", func(t *testing.T) {
		got := week(SessionsReq{Resolution: Res1h})
		if len(got.Rows) < 2 {
			t.Fatalf("%d sessions in the reply, two were expected", len(got.Rows))
		}
		if got.Rows[0].Name != "aacpanel" {
			t.Errorf("%q comes first, aacpanel was expected — its measurements are fresher", got.Rows[0].Name)
		}
		if got.Rows[0].LastSeen < got.Rows[1].LastSeen {
			t.Errorf("the first row's last measurement (%d) is older than the second's (%d) — the order is reversed",
				got.Rows[0].LastSeen, got.Rows[1].LastSeen)
		}
		if got.Rows[0].PctMax < 94.9 {
			t.Errorf("the first row's peak %.1f%%, expected 95 — the sorting took the number along with it", got.Rows[0].PctMax)
		}
		if got.Total != 2 {
			t.Errorf("%d sessions in all, 2 were expected", got.Total)
		}
	})

	t.Run("a truncated list says that it is truncated", func(t *testing.T) {
		got := week(SessionsReq{Resolution: Res1h, Limit: 1})
		if len(got.Rows) != 1 {
			t.Fatalf("%d rows, one was expected", len(got.Rows))
		}
		if got.Total != 2 {
			t.Errorf("total %d at a limit of 1, 2 was expected — there is nothing to notice the truncation by", got.Total)
		}
	})

	t.Run("a hole in the data stays a hole and does not become zero sessions", func(t *testing.T) {
		got, err := s.SessionsFor(ctx, SessionsReq{
			HostID: hostID, From: gapFrom.Add(-2 * time.Hour), To: gapTo.Add(2 * time.Hour),
			Resolution: Res1m, MaxPoints: maxPointsLimit,
		})
		if err != nil {
			t.Fatal(err)
		}
		holes, zeros := 0, 0
		for _, v := range got.Live {
			switch {
			case v == nil:
				holes++
			case *v == 0:
				zeros++
			}
		}
		if holes == 0 {
			t.Errorf("there are no holes in the series: an hour without collection arrived as steady work (%d points)", len(got.T))
		}
		if zeros > 0 {
			t.Errorf("%d buckets with zero sessions — an absence of data was substituted with a zero", zeros)
		}
	})

	t.Run("the raw resolution is refused", func(t *testing.T) {
		_, err := s.SessionsFor(ctx, SessionsReq{
			HostID: hostID, From: base, To: time.Now().UTC(), Resolution: ResRaw,
		})
		if err == nil {
			t.Fatal("the raw resolution was accepted, although the session history has nowhere to take it from")
		}
		var bad ErrBadRequest
		if !errors.As(err, &bad) {
			t.Errorf("error %v, ErrBadRequest was expected: otherwise HTTP answers 500 instead of 400", err)
		}
	})

	t.Run("a period without data is an empty answer and not an error", func(t *testing.T) {
		from := time.Now().UTC().AddDate(-1, 0, 0)
		got, err := s.SessionsFor(ctx, SessionsReq{
			HostID: hostID, From: from, To: from.Add(time.Hour), Resolution: Res1h,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != 0 || len(got.T) != 0 {
			t.Errorf("%d rows and %d points arrived for last year", len(got.Rows), len(got.T))
		}
		if got.Rows == nil || got.T == nil {
			t.Error("the empty answer was served as null and not as []: the client will have to handle that separately")
		}
	})
}

func TestSessionsHistoryPagingPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := s.HostID(ctx, "SESSIONS-PAGING-TEST")
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { pool.Exec(ctx, "DELETE FROM sessions_1h WHERE host_id = $1", hostID) }
	cleanup()
	defer cleanup()

	const total = 25
	base := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Hour)
	partitionsBack(t, ctx, s, "sessions_1h", 11*24*time.Hour)
	for i := range total {
		if _, err := pool.Exec(ctx, `
			INSERT INTO sessions_1h (bucket, host_id, name, tokens_max, pct_avg, pct_max, messages_max, samples)
			VALUES ($1, $2, $3, 1000, 10, $4, 5, 60) ON CONFLICT DO NOTHING`,
			base.Add(time.Duration(i)*time.Hour), hostID,
			fmt.Sprintf("session-%02d", i), float64(i)+1); err != nil {
			t.Fatal(err)
		}
	}

	page := func(offset, limit int) *SessionsHistory {
		t.Helper()
		got, err := s.SessionsFor(ctx, SessionsReq{
			HostID: hostID, From: base.Add(-time.Hour), To: time.Now().UTC(),
			Resolution: Res1h, Limit: limit, Offset: offset,
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	t.Run("the pages do not overlap and lose nothing", func(t *testing.T) {
		seen := map[string]int{}
		var order []int64
		for offset := 0; offset < total; offset += 10 {
			got := page(offset, 10)
			if got.Total != total {
				t.Errorf("the page at offset=%d speaks of %d sessions, and there are %d in all", offset, got.Total, total)
			}
			if got.Offset != offset || got.Limit != 10 {
				t.Errorf("the page came back with offset=%d limit=%d, %d and 10 were asked for", got.Offset, got.Limit, offset)
			}
			for _, row := range got.Rows {
				seen[row.Name]++
				order = append(order, row.LastSeen)
			}
		}
		if len(seen) != total {
			t.Errorf("%d sessions out of %d were collected across all the pages", len(seen), total)
		}
		for name, times := range seen {
			if times > 1 {
				t.Errorf("session %q occurred on %d pages — the page boundaries drifted apart", name, times)
			}
		}
		for i := 1; i < len(order); i++ {
			if order[i] > order[i-1] {
				t.Errorf("at position %d the last measurement is newer than the previous one (%d against %d) — the order broke between pages",
					i, order[i], order[i-1])
			}
		}
	})

	t.Run("past the end of the list it is empty, but with an honest total", func(t *testing.T) {
		got := page(total+10, 10)
		if len(got.Rows) != 0 {
			t.Errorf("%d rows arrived past the end of the list", len(got.Rows))
		}
		if got.Total != total {
			t.Errorf("past the end of the list total = %d, %d was expected — otherwise the client decides the history has vanished", got.Total, total)
		}
		if got.Rows == nil {
			t.Error("the empty page was served as null and not as []")
		}
	})
}

func TestSessionResumeLookupPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := s.HostID(ctx, "SESSIONS-RESUME-TEST")
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		pool.Exec(ctx, "DELETE FROM sessions_1m WHERE host_id = $1", hostID)
		pool.Exec(ctx, "DELETE FROM sessions_1h WHERE host_id = $1", hostID)
	}
	cleanup()
	defer cleanup()

	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Minute)
	insert := func(table string, at time.Time, name, sessionID, cwd string) {
		t.Helper()
		var sid, dir any = sessionID, cwd
		if sessionID == "" {
			sid, dir = nil, nil
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO `+table+` (bucket, host_id, name, tokens_max, pct_avg, pct_max, messages_max, samples, session_id, cwd)
			VALUES ($1, $2, $3, 1000, 10, 20, 5, 6, $4, $5) ON CONFLICT DO NOTHING`,
			at, hostID, name, sid, dir); err != nil {
			t.Fatal(err)
		}
	}

	insert("sessions_1m", base, "aacpanel", "aaaaaaaa-0000-0000-0000-000000000001", "/srv/proj/aacpanel")
	insert("sessions_1m", base.Add(time.Hour), "aacpanel", "bbbbbbbb-0000-0000-0000-000000000002", "/srv/proj/aacpanel")
	insert("sessions_1m", base, "old", "", "")

	t.Run("the latest conversation is taken and not the first", func(t *testing.T) {
		sessionID, cwd, err := s.SessionResume(ctx, hostID, "aacpanel")
		if err != nil {
			t.Fatal(err)
		}
		if want := "bbbbbbbb-0000-0000-0000-000000000002"; sessionID != want {
			t.Errorf("uuid %q, %q was expected — we would have returned to the day-before-yesterday's conversation", sessionID, want)
		}
		if cwd != "/srv/proj/aacpanel" {
			t.Errorf("directory %q — there is nowhere to launch the continuation", cwd)
		}
	})

	t.Run("a record without an identifier answers with emptiness", func(t *testing.T) {
		sessionID, _, err := s.SessionResume(ctx, hostID, "old")
		if err != nil {
			t.Fatal(err)
		}
		if sessionID != "" {
			t.Errorf("uuid %q came out of nowhere", sessionID)
		}
	})

	t.Run("an unfamiliar name is emptiness too", func(t *testing.T) {
		sessionID, _, err := s.SessionResume(ctx, hostID, "no-such-thing")
		if err != nil {
			t.Fatal(err)
		}
		if sessionID != "" {
			t.Errorf("uuid %q for a session that does not exist", sessionID)
		}
	})

	t.Run("the hourly layer picks up what the minute one does not have", func(t *testing.T) {
		insert("sessions_1h", base.Truncate(time.Hour), "long-ago",
			"cccccccc-0000-0000-0000-000000000003", "/srv/proj/long-ago")
		sessionID, cwd, err := s.SessionResume(ctx, hostID, "long-ago")
		if err != nil {
			t.Fatal(err)
		}
		if want := "cccccccc-0000-0000-0000-000000000003"; sessionID != want {
			t.Errorf("uuid %q, %q was expected — the hourly layer was not asked", sessionID, want)
		}
		if cwd != "/srv/proj/long-ago" {
			t.Errorf("directory %q", cwd)
		}
	})
}
