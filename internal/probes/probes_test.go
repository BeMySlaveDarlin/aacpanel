package probes

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	client := New(nil).client

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	dead.Close()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "everything is broken", http.StatusInternalServerError)
	}))
	defer broken.Close()

	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "a key is required", http.StatusUnauthorized)
	}))
	defer unauthorized.Close()

	cases := []struct {
		name  string
		probe Probe
		want  Outcome
	}{
		{"a live port", Probe{Kind: "tcp", Target: ln.Addr().String(), Timeout: 3 * time.Second}, OK},
		{"a closed port is the network", Probe{Kind: "tcp", Target: deadAddr, Timeout: 3 * time.Second}, Network},
		{"a missing host is the network", Probe{Kind: "tcp", Target: "no-such-host.invalid:443", Timeout: 3 * time.Second}, Network},
		{"the service answers", Probe{Kind: "http", Target: ok.URL, Timeout: 3 * time.Second}, OK},
		{"the service answered with an error", Probe{Kind: "http", Target: broken.URL, Timeout: 3 * time.Second}, Status},
		{"an expected 401 is a success", Probe{Kind: "http", Target: unauthorized.URL, Timeout: 3 * time.Second, ExpectStatus: 401}, OK},
		{"an error instead of the expected status", Probe{Kind: "http", Target: broken.URL, Timeout: 3 * time.Second, ExpectStatus: 401}, Status},
		{"200 instead of the expected 401", Probe{Kind: "http", Target: ok.URL, Timeout: 3 * time.Second, ExpectStatus: 401}, Status},
		{"the address does not parse", Probe{Kind: "http", Target: "://broken", Timeout: time.Second}, Config},
		{"an unknown kind", Probe{Kind: "smoke", Target: "x", Timeout: time.Second}, Config},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := Check(context.Background(), client, c.probe)
			if res.Outcome != c.want {
				t.Errorf("outcome %q (error %q), expected %q", res.Outcome, res.Err, c.want)
			}
			if (res.Outcome == OK) != res.OK {
				t.Errorf("ok=%v with outcome=%q — the fields disagree", res.OK, res.Outcome)
			}
		})
	}
}

func TestCheckTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("a real timeout is needed")
	}
	res := Check(context.Background(), New(nil).client,
		Probe{Kind: "tcp", Target: "192.0.2.1:443", Timeout: time.Second})
	if res.Outcome != Timeout {
		t.Errorf("outcome %q (%s), expected %q", res.Outcome, res.Err, Timeout)
	}
	if res.OK {
		t.Error("a probe into a black hole counted as a success")
	}
}

func TestJitter(t *testing.T) {
	const base = time.Minute
	var min, max time.Duration = time.Hour, 0
	for range 200 {
		d := jitter(base)
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}
	lo := base - time.Duration(float64(base)*jitterShare)
	hi := base + time.Duration(float64(base)*jitterShare)
	if min < lo || max > hi {
		t.Errorf("the spread [%s..%s] went outside [%s..%s]", min, max, lo, hi)
	}
	if max-min < time.Duration(float64(base)*jitterShare) {
		t.Errorf("the spread is only %s — the probes would still go in one batch", max-min)
	}
}
