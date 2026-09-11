package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestParsePeriod(t *testing.T) {
	now := time.Now()

	cases := []struct {
		query string
		want  time.Duration
	}{
		{"period=1h", time.Hour},
		{"period=30m", 30 * time.Minute},
		{"period=7d", 7 * 24 * time.Hour},
		{"", time.Hour},
		{"period=2x", 0},
		{"period=-1h", 0},
		{"period=0h", 0},
		{"from=1787000000&to=1787003600", time.Hour},
		{"from=tomorrow", 0},
		{"from=1787003600&to=1787000000", 0},
		{"period=500d", 0},
		{"from=1787000000&to=1787003600&period=7d", time.Hour},
	}

	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/history?"+c.query, nil)
			from, to, err := parsePeriod(r)
			if c.want == 0 {
				if err == nil {
					t.Fatalf("there was no error, the period is %s..%s", from, to)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := to.Sub(from); got != c.want {
				t.Errorf("the period is %s, expected %s", got, c.want)
			}
			if to.After(now.Add(time.Minute)) {
				t.Errorf("the end of the period is in the future: %s", to)
			}
		})
	}
}

func TestHistoryWithoutDB(t *testing.T) {
	srv := &Server{hostName: "STAND-01"}
	w := httptest.NewRecorder()
	srv.apiHistory(w, httptest.NewRequest(http.MethodGet, "/api/history?subject=host&metric=cpu", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("without a database the status is %d, expected 503", w.Code)
	}
}

func TestHistoryUnreachableDB(t *testing.T) {
	db, err := store.New("postgres://postgres:test@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	srv := &Server{db: db, hostName: "STAND-01"}
	w := httptest.NewRecorder()
	srv.apiHistory(w, httptest.NewRequest(http.MethodGet, "/api/history?subject=host&metric=cpu&period=1h", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, expected 503. body: %s", w.Code, w.Body.String())
	}
}

func TestHistoryHandlerPG(t *testing.T) {
	dsn := testdb.DSN(t)

	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	srv := &Server{db: db, hostName: "HANDLER-TEST"}

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		switch {
		case strings.HasPrefix(path, "/api/history/top"):
			srv.apiHistoryTop(w, r)
		case strings.HasPrefix(path, "/api/history/sessions"):
			srv.apiHistorySessions(w, r)
		case strings.HasPrefix(path, "/api/history/net"):
			srv.apiHistoryNet(w, r)
		default:
			srv.apiHistory(w, r)
		}
		return w
	}

	t.Run("a series comes with its resolution", func(t *testing.T) {
		w := get("/api/history?subject=host&metric=cpu&period=1h")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		var s store.Series
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		if s.Resolution == "" || s.StepSec <= 0 {
			t.Errorf("the response has no resolution and no step: %+v", s)
		}
		if s.T == nil || s.Avg == nil {
			t.Error("empty series came as null, not as []: the client would have to handle that separately")
		}
	})

	t.Run("sessions come as peaks and as a list", func(t *testing.T) {
		w := get("/api/history/sessions?period=7d")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		var h store.SessionsHistory
		if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
			t.Fatal(err)
		}
		if h.Resolution == "" || h.StepSec <= 0 {
			t.Errorf("the response has no resolution and no step: %+v", h)
		}
		if h.T == nil || h.Live == nil || h.PctMax == nil || h.Rows == nil {
			t.Errorf("the series came as null: %+v", h)
		}
	})

	t.Run("traffic comes as series per interface", func(t *testing.T) {
		w := get("/api/history/net?period=1h")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		var n store.NetHistory
		if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil {
			t.Fatal(err)
		}
		if n.StepSec <= 0 {
			t.Errorf("the response has no step: %+v", n)
		}
		if n.T == nil || n.Ifaces == nil {
			t.Errorf("the series came as null: %+v", n)
		}
	})

	t.Run("a month of traffic gives a series, not emptiness", func(t *testing.T) {
		pool, err := db.Pool()
		if err != nil {
			t.Fatal(err)
		}
		hostID, err := db.HostID(t.Context(), "HANDLER-TEST")
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(t.Context(), "DELETE FROM metrics_net_1h WHERE host_id = $1", hostID)

		base := time.Now().UTC().Add(-20 * 24 * time.Hour).Truncate(time.Hour)
		testdb.PartitionsBack(t, t.Context(), pool, "metrics_net_1h", 21*24*time.Hour)
		for i := range 24 {
			if _, err := pool.Exec(t.Context(),
				`INSERT INTO metrics_net_1h
				   (bucket, host_id, iface, rx_avg, rx_max, tx_avg, tx_max, rx_bytes, tx_bytes, samples)
				 VALUES ($1, $2, 'eth', 1000, 2000, 500, 900, $3, $4, 60)
				 ON CONFLICT DO NOTHING`,
				base.Add(time.Duration(i)*time.Hour), hostID, 1000*3600, 500*3600); err != nil {
				t.Fatal(err)
			}
		}

		w := get("/api/history/net?period=30d")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		var n store.NetHistory
		if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil {
			t.Fatal(err)
		}
		if n.Resolution != "1h" {
			t.Errorf("a month is read from the %q layer, expected 1h", n.Resolution)
		}
		if len(n.Ifaces) == 0 || len(n.T) == 0 {
			t.Fatalf("the month came back empty: %d interfaces, %d points", len(n.Ifaces), len(n.T))
		}
		if want := float64(24 * 1000 * 3600); n.Ifaces[0].RxTotal != want {
			t.Errorf("the volume for the month is %.0f, the buckets hold %.0f", n.Ifaces[0].RxTotal, want)
		}
	})

	t.Run("a raw resolution for sessions is rejected", func(t *testing.T) {
		w := get("/api/history/sessions?period=7d&resolution=raw")
		if w.Code != http.StatusBadRequest {
			t.Errorf("status %d, expected 400: the session history does not read the raw layer", w.Code)
		}
	})

	t.Run("the top comes back", func(t *testing.T) {
		w := get("/api/history/top?metric=mem&period=24h&limit=5")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		var top store.Top
		if err := json.Unmarshal(w.Body.Bytes(), &top); err != nil {
			t.Fatal(err)
		}
		if top.Rows == nil {
			t.Error("an empty top came as null, not as []")
		}
	})

	t.Run("a malformed metric — 400", func(t *testing.T) {
		if w := get("/api/history?subject=host&metric=nosuch&period=1h"); w.Code != http.StatusBadRequest {
			t.Errorf("status %d, expected 400", w.Code)
		}
	})

	t.Run("a malformed period — 400", func(t *testing.T) {
		if w := get("/api/history?subject=host&metric=cpu&period=tomorrow"); w.Code != http.StatusBadRequest {
			t.Errorf("status %d, expected 400", w.Code)
		}
	})

	t.Run("the container never existed — 200 and empty", func(t *testing.T) {
		w := get("/api/history?subject=container:no-such-one&metric=cpu&period=1h")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, expected 200: missing data is not a client error. body %s", w.Code, w.Body.String())
		}
		var s store.Series
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		if len(s.T) != 0 {
			t.Errorf("%d points came for a container that does not exist", len(s.T))
		}
	})

	t.Run("a year ago — 200 and empty", func(t *testing.T) {
		from := time.Now().AddDate(-1, 0, 0).Unix()
		w := get("/api/history?subject=host&metric=cpu&from=" + strconv.FormatInt(from, 10) +
			"&to=" + strconv.FormatInt(from+3600, 10))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, expected 200: an empty period is not an error. body %s", w.Code, w.Body.String())
		}
		var s store.Series
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		if len(s.T) != 0 {
			t.Errorf("%d points came for last year", len(s.T))
		}
	})
}
