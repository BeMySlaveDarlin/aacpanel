package docker

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func frame(stream byte, payload string) []byte {
	hdr := make([]byte, 8)
	hdr[0] = stream
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
	return append(hdr, payload...)
}

func logServer(t *testing.T, body []byte, multiplexed bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if multiplexed {
			w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

type line struct{ stream, text string }

func collect(t *testing.T, c *Client) []line {
	t.Helper()
	var got []line
	err := c.Logs(context.Background(), "abc", 100, func(stream, text string) {
		got = append(got, line{stream, text})
	})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	return got
}

func TestLogsDemultiplexes(t *testing.T) {
	body := append(frame(1, "first\nsecond\n"), frame(2, "an error\n")...)
	got := collect(t, logServer(t, body, true))

	want := []line{{"stdout", "first"}, {"stdout", "second"}, {"stderr", "an error"}}
	if len(got) != len(want) {
		t.Fatalf("%d lines, expected %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: %+v, expected %+v", i, got[i], want[i])
		}
	}
}

func TestLogsReadsTTYStreamAsText(t *testing.T) {
	got := collect(t, logServer(t, []byte("just text\nwith no frames\n"), false))
	if len(got) != 2 || got[0].text != "just text" || got[1].text != "with no frames" {
		t.Fatalf("the TTY stream is parsed wrongly: %+v", got)
	}
}

func TestLogsStaysInSyncAfterHugeFrame(t *testing.T) {
	huge := strings.Repeat("x", 1<<20)
	body := append(frame(1, huge+"\n"), frame(2, "I am next\n")...)
	got := collect(t, logServer(t, body, true))

	if len(got) == 0 {
		t.Fatal("not a single line was read")
	}
	last := got[len(got)-1]
	if last.stream != "stderr" || last.text != "I am next" {
		t.Errorf("the stream parted after a huge frame: the last line is %+v", last)
	}
}

func apiServer(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, "no such endpoint: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestStatsSubtractsPageCache(t *testing.T) {
	c := apiServer(t, map[string]string{"/containers/abc/stats": `{
		"memory_stats": {"usage": 500, "limit": 1000, "stats": {"inactive_file": 300}},
		"cpu_stats":    {"cpu_usage": {"total_usage": 0}, "system_cpu_usage": 0, "online_cpus": 1},
		"precpu_stats": {"cpu_usage": {"total_usage": 0}, "system_cpu_usage": 0}
	}`})
	s, err := c.Stats(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if s.Mem != 200 {
		t.Errorf("memory %d, expected 200 (500 minus the cache of 300)", s.Mem)
	}
}

func TestStatsSurvivesCacheLargerThanUsage(t *testing.T) {
	c := apiServer(t, map[string]string{"/containers/abc/stats": `{
		"memory_stats": {"usage": 100, "limit": 1000, "stats": {"inactive_file": 300}},
		"cpu_stats":    {"cpu_usage": {"total_usage": 0}, "system_cpu_usage": 0, "online_cpus": 1},
		"precpu_stats": {"cpu_usage": {"total_usage": 0}, "system_cpu_usage": 0}
	}`})
	s, err := c.Stats(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if s.Mem > 100 {
		t.Errorf("memory %d — the subtraction went into an overflow", s.Mem)
	}
}

func TestStatsCountsCPUPerCore(t *testing.T) {
	c := apiServer(t, map[string]string{"/containers/abc/stats": `{
		"memory_stats": {"usage": 0, "limit": 0, "stats": {"inactive_file": 0}},
		"cpu_stats":    {"cpu_usage": {"total_usage": 200}, "system_cpu_usage": 1600, "online_cpus": 8},
		"precpu_stats": {"cpu_usage": {"total_usage": 100}, "system_cpu_usage": 800}
	}`})
	s, err := c.Stats(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if s.CPU < 99 || s.CPU > 101 {
		t.Errorf("cpu %.1f%%, expected around 100%%", s.CPU)
	}
}

func TestStatsAssumesOneCPUWhenUnknown(t *testing.T) {
	c := apiServer(t, map[string]string{"/containers/abc/stats": `{
		"memory_stats": {"usage": 0, "limit": 0, "stats": {"inactive_file": 0}},
		"cpu_stats":    {"cpu_usage": {"total_usage": 200}, "system_cpu_usage": 1600},
		"precpu_stats": {"cpu_usage": {"total_usage": 100}, "system_cpu_usage": 800}
	}`})
	s, err := c.Stats(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if s.CPU <= 0 {
		t.Errorf("cpu %.1f%% — the core multiplier zeroed the load", s.CPU)
	}
}

func TestNonOKResponseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not allowed", http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := New(srv.URL).Tree(context.Background(), nil); err == nil {
		t.Fatal("a refusal of the docker api was taken for an empty tree")
	} else if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("the answer of the daemon is not in the error: %v", err)
	}
}

func TestFormatPorts(t *testing.T) {
	cases := []struct {
		name string
		in   []apiPort
		want []string
	}{
		{
			"a published port shows both sides",
			[]apiPort{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
			[]string{"8080→80/tcp"},
		},
		{
			"an unpublished one shows only its own",
			[]apiPort{{PrivatePort: 5432, Type: "tcp"}},
			[]string{"5432/tcp"},
		},
		{
			"the IPv6 duplicate is hidden",
			[]apiPort{
				{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
				{IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			},
			[]string{"8080→80/tcp"},
		},
		{
			"a publication on several addresses is shown once",
			[]apiPort{
				{IP: "127.0.0.1", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
				{IP: "192.168.1.10", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
			},
			[]string{"8080→80/tcp"},
		},
		{"with no ports it is an empty list, not nil", nil, []string{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatPorts(c.in)
			if got == nil {
				t.Fatal("nil instead of an empty list — null travels into the JSON")
			}
			if len(got) != len(c.want) {
				t.Fatalf("%q, expected %q", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("%q, expected %q", got, c.want)
					break
				}
			}
		})
	}
}

func TestHealthFromStatus(t *testing.T) {
	cases := map[string]string{
		"Up 2 hours (healthy)":            "healthy",
		"Up 2 hours (unhealthy)":          "unhealthy",
		"Up 5 seconds (health: starting)": "starting",
		"Up 3 days":                       "",
		"Exited (0) 2 minutes ago":        "",
	}
	for status, want := range cases {
		if got := health(status); got != want {
			t.Errorf("health(%q) = %q, expected %q", status, got, want)
		}
	}
}

const twoStacks = `[
	{"Id":"a1","Names":["/shop-app"],"State":"running","Status":"Up 2 hours (healthy)",
	 "Labels":{"com.docker.compose.project":"shop","com.docker.compose.service":"app"}},
	{"Id":"a2","Names":["/shop-db"],"State":"exited","Status":"Exited (0) 5 minutes ago",
	 "Labels":{"com.docker.compose.project":"shop","com.docker.compose.service":"db"}},
	{"Id":"b1","Names":["/on-its-own"],"State":"running","Status":"Up 3 days","Labels":{}}
]`

func TestTreeGroupsByStack(t *testing.T) {
	c := apiServer(t, map[string]string{"/containers/json": twoStacks})
	tree, err := c.Tree(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Total != 3 || tree.Running != 2 {
		t.Errorf("%d in total, %d running; expected 3 and 2", tree.Total, tree.Running)
	}
	if len(tree.Stacks) != 2 {
		t.Fatalf("%d stacks, expected 2", len(tree.Stacks))
	}
	if tree.Stacks[len(tree.Stacks)-1].Name != noStack {
		t.Errorf("%q is not at the very bottom: %s", noStack, tree.Stacks[len(tree.Stacks)-1].Name)
	}
	shop := tree.Stacks[0]
	if shop.Name != "shop" || shop.Running != 1 || shop.Total != 2 {
		t.Errorf("stack %+v — the name or the counters did not match", shop)
	}
	if shop.Containers[0].Name != "shop-app" {
		t.Errorf("name %q — the leading slash was not trimmed or the order is off", shop.Containers[0].Name)
	}
}

func TestTreeIgnoresStatsOfStoppedContainers(t *testing.T) {
	c := apiServer(t, map[string]string{"/containers/json": twoStacks})
	m := &Metrics{Stats: map[string]Stats{
		"a1": {CPU: 12, Mem: 100, MemLimit: 1000},
		"a2": {CPU: 99, Mem: 900, MemLimit: 1000},
	}}
	tree, err := c.Tree(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	shop := tree.Stacks[0]
	for _, cont := range shop.Containers {
		if cont.State != "running" && cont.HasStats {
			t.Errorf("the stopped %s kept its load", cont.Name)
		}
	}
	if shop.CPU != 12 || shop.Mem != 100 {
		t.Errorf("the sum over the stack is cpu=%.0f mem=%d — the dead one was added in", shop.CPU, shop.Mem)
	}
}

func TestTreeShowsMemPctOnlyWhereLimitIsSet(t *testing.T) {
	c := apiServer(t, map[string]string{"/containers/json": twoStacks})
	const hostMem = 32 << 30
	m := &Metrics{Stats: map[string]Stats{
		"a1": {Mem: 512 << 20, MemLimit: 1 << 30},
		"b1": {Mem: 512 << 20, MemLimit: hostMem},
	}}
	tree, err := c.Tree(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Container{}
	for _, s := range tree.Stacks {
		for _, cont := range s.Containers {
			byName[cont.Name] = cont
		}
	}
	if pct := byName["shop-app"].MemPct; pct < 49 || pct > 51 {
		t.Errorf("the share of the limit is %.1f%%, expected around 50%%", pct)
	}
	if pct := byName["on-its-own"].MemPct; pct != 0 {
		t.Errorf("a container with no limit has the share %.1f%% — that is the share of the machine, not of the limit", pct)
	}
}

func TestFormatPortsKeepsIPv6OnlyPublication(t *testing.T) {
	got := formatPorts([]apiPort{{IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}})
	if len(got) != 1 || got[0] != "8080→80/tcp" {
		t.Errorf("%q — a publication on IPv6 only is lost", got)
	}
}

func TestTreeKeepsStacklessAtTheBottom(t *testing.T) {
	const mixed = `[
		{"Id":"a1","Names":["/sleeping"],"State":"exited","Status":"Exited (0) 5 minutes ago",
		 "Labels":{"com.docker.compose.project":"zzz-slept"}},
		{"Id":"b1","Names":["/on-its-own"],"State":"running","Status":"Up 3 days","Labels":{}}
	]`
	c := apiServer(t, map[string]string{"/containers/json": mixed})
	tree, err := c.Tree(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Stacks[len(tree.Stacks)-1].Name; got != noStack {
		t.Errorf("%q ended up at the bottom: the live heap overtook the dead stack", got)
	}
}
