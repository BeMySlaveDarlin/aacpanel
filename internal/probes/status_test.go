package probes

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestStatusPageProbe(t *testing.T) {
	client := New(nil).client

	const healthy = `{"page":{"id":"tymt9n04zgry","name":"Claude"},` +
		`"status":{"indicator":"none","description":"All Systems Operational"}}`
	const outage = `{"page":{"id":"01JMDK9XYNY6RXSED6SDWW50WY","name":"OpenAI"},` +
		`"status":{"description":"Partial System Outage","indicator":"major"}}`
	const minor = `{"status":{"indicator":"minor","description":"Elevated error rates"}}`
	const noIndicator = `{"page":{"name":"Nginx"},"status":{}}`

	cases := []struct {
		name    string
		code    int
		body    string
		want    Outcome
		wantErr string
	}{
		{"all is well", 200, healthy, OK, ""},
		{"a major incident", 200, outage, Status, "major incident: Partial System Outage"},
		{"a partial failure", 200, minor, Degraded, "minor incident: Elevated error rates"},
		{"the page answers 503", 503, "", Status, "the status page answered 503"},
		{"the page redirected away", 301, "", Status, "the status page answered 301"},
		{"the address is not a statuspage", 200, "<html>nothing of the sort</html>", Config, "cannot be parsed"},
		{"there is no indicator field", 200, noIndicator, Config, "no status.indicator"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v2/status.json" {
					t.Errorf("the probe knocked on %q instead of the machine answer of statuspage", r.URL.Path)
				}
				w.WriteHeader(c.code)
				w.Write([]byte(c.body))
			}))
			defer srv.Close()

			res := Check(context.Background(), client, Probe{
				Kind:    "status",
				Target:  srv.URL + "/api/v2/status.json",
				Timeout: 3 * time.Second,
			})
			if res.Outcome != c.want {
				t.Fatalf("outcome %q (%s), expected %q", res.Outcome, res.Err, c.want)
			}
			if (res.Outcome == OK) != res.OK {
				t.Errorf("ok=%v with outcome=%q — the fields disagree", res.OK, res.Outcome)
			}
			if c.wantErr == "" && res.Err != "" {
				t.Errorf("a healthy page left the error text %q", res.Err)
			}
			if c.wantErr != "" && !strings.Contains(res.Err, c.wantErr) {
				t.Errorf("the log gets %q — it holds no %q, so the person is told less "+
					"than the probe knows", res.Err, c.wantErr)
			}
		})
	}
}

func TestStatusPageBodyIsCapped(t *testing.T) {
	const timeout = 3 * time.Second

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":{"description":"`))
		chunk := []byte(strings.Repeat("A", 8<<10))
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()

	res := Check(context.Background(), New(nil).client, Probe{
		Kind:    "status",
		Target:  srv.URL,
		Timeout: timeout,
	})
	if res.Outcome != Config {
		t.Fatalf("outcome %q (%s), expected %q: an answer that does not parse is the unknown, not an incident",
			res.Outcome, res.Err, Config)
	}
	if lim := int(timeout/time.Millisecond) / 2; res.LatencyMS > lim {
		t.Errorf("parsing took %d ms against the probe ceiling of %s — the body was read until the timeout, "+
			"so the `statusBodyMax` ceiling did not work", res.LatencyMS, timeout)
	}
	if len(res.Err) > 220 {
		t.Errorf("the database gets an error text of %d bytes — `shorten` never cut it", len(res.Err))
	}
}

func TestStatusPageSeverityKeepsUnknownWord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":{"indicator":"maintenance","description":"Scheduled work"}}`))
	}))
	defer srv.Close()

	res := Check(context.Background(), New(nil).client, Probe{
		Kind: "status", Target: srv.URL, Timeout: 3 * time.Second,
	})
	if res.Outcome != Status {
		t.Fatalf("outcome %q (%s), expected %q", res.Outcome, res.Err, Status)
	}
	if !strings.Contains(res.Err, "maintenance") {
		t.Errorf("the text %q lost the word from the status page", res.Err)
	}
}

func TestServiceProbesFromMigrationsAreRunnablePG(t *testing.T) {
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

	rows, err := pool.Query(ctx, `SELECT name, kind, target, components FROM probes WHERE runner = 'service' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	dead.Close()

	seen := map[string]int{}
	watched := 0
	for rows.Next() {
		var name, kind, target string
		var components *string
		if err := rows.Scan(&name, &kind, &target, &components); err != nil {
			t.Fatal(err)
		}
		seen[kind]++

		if kind == "status" && !strings.HasSuffix(target, "/api/v2/status.json") &&
			!strings.HasSuffix(target, "/api/v2/summary.json") {
			t.Errorf("probe %q looks at %s — statuspage keeps the state in "+
				"/api/v2/status.json or /api/v2/summary.json, while the page for people serves html "+
				"and would go into a permanent config", name, target)
		}
		if components != nil {
			watched++
			if !strings.HasSuffix(target, "/api/v2/summary.json") {
				t.Errorf("probe %q watches components %q while looking at %s — components "+
					"live only in /api/v2/summary.json", name, *components, target)
			}
		}

		probe := Probe{Kind: kind, Target: "http://" + deadAddr + "/", Timeout: 2 * time.Second}
		if kind == "tcp" {
			probe.Target = deadAddr
		}
		res := Check(ctx, New(nil).client, probe)
		if strings.Contains(res.Err, "unknown probe kind") {
			t.Errorf("probe %q is set up with kind %q, which the service cannot do: it would answer "+
				"config forever while the service is healthy", name, kind)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if seen["status"] < 2 {
		t.Errorf("%d status pages in the database, while two were set up (Anthropic and OpenAI)", seen["status"])
	}
	if watched < 2 {
		t.Errorf("components are set on %d status probes out of %d — the rest judge by the aggregate "+
			"of the whole page, Console and Sora included", watched, seen["status"])
	}
}

func summaryPage(indicator string, comps ...[2]string) string {
	out := `{"page":{"id":"tymt9n04zgry","name":"Claude"},"components":[`
	for i, c := range comps {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf(`{"id":"c%d","name":%q,"status":%q}`, i, c[0], c[1])
	}
	return out + `],"status":{"indicator":"` + indicator + `","description":"from the page itself"}}`
}

func checkStatusPage(t *testing.T, body string, watch ...string) Result {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()
	return Check(context.Background(), New(nil).client, Probe{
		Kind:       "status",
		Target:     srv.URL + "/api/v2/summary.json",
		Timeout:    3 * time.Second,
		Components: watch,
	})
}

func TestStatusPageWatchesComponents(t *testing.T) {
	client := New(nil).client

	const (
		api  = "Claude API (api.anthropic.com)"
		code = "Claude Code"
		web  = "claude.ai"
		cons = "Claude Console (platform.claude.com)"
	)
	watch := []string{api, code}

	cases := []struct {
		name    string
		watch   []string
		body    string
		want    Outcome
		wantErr string
	}{
		{
			"someone else's component does not fail our probe", watch,
			summaryPage("major", [2]string{web, "major_outage"}, [2]string{cons, "degraded_performance"},
				[2]string{api, "operational"}, [2]string{code, "operational"}),
			OK, "",
		},
		{
			"our component is down while the indicator is minor", watch,
			summaryPage("minor", [2]string{web, "operational"}, [2]string{api, "major_outage"},
				[2]string{code, "operational"}),
			Status, "Claude API (api.anthropic.com): outage",
		},
		{
			"degraded performance is a degradation", watch,
			summaryPage("none", [2]string{api, "degraded_performance"}, [2]string{code, "operational"}),
			Degraded, "degraded performance",
		},
		{
			"maintenance is a degradation too", watch,
			summaryPage("none", [2]string{api, "operational"}, [2]string{code, "under_maintenance"}),
			Degraded, "Claude Code: maintenance",
		},
		{
			"a partial outage is an outage", watch,
			summaryPage("none", [2]string{api, "partial_outage"}, [2]string{code, "operational"}),
			Status, "partial outage",
		},
		{
			"the worse of the two states wins", watch,
			summaryPage("none", [2]string{api, "degraded_performance"}, [2]string{code, "major_outage"}),
			Status, "Claude Code: outage",
		},
		{
			"the case in the setting does not matter", []string{"CLAUDE CODE"},
			summaryPage("none", [2]string{code, "degraded_performance"}),
			Degraded, "degraded performance",
		},
		{
			"the component is not on the page", []string{"Claude Cowork"},
			summaryPage("none", [2]string{api, "operational"}, [2]string{code, "operational"}),
			Config, `component "Claude Cowork" is not on the page`,
		},
		{
			"a piece of the name is not a match", []string{"api.anthropic.com"},
			summaryPage("none", [2]string{api, "operational"}, [2]string{code, "operational"}),
			Config, `component "api.anthropic.com" is not on the page`,
		},
		{
			"spaces around the name on the page do not count", []string{"Claude Code"},
			summaryPage("none", [2]string{"  Claude Code  ", "major_outage"}),
			Status, "outage",
		},
		{
			"the answer holds no components", watch,
			`{"status":{"indicator":"none","description":"All Systems Operational"}}`,
			Config, "no components",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(c.body))
			}))
			defer srv.Close()

			res := Check(context.Background(), client, Probe{
				Kind:       "status",
				Target:     srv.URL + "/api/v2/summary.json",
				Timeout:    3 * time.Second,
				Components: c.watch,
			})
			if res.Outcome != c.want {
				t.Fatalf("outcome %q (%s), expected %q", res.Outcome, res.Err, c.want)
			}
			if (res.Outcome == OK) != res.OK {
				t.Errorf("ok=%v with outcome=%q — the fields disagree", res.OK, res.Outcome)
			}
			if c.wantErr == "" && res.Err != "" {
				t.Errorf("a healthy page left the error text %q", res.Err)
			}
			if c.wantErr != "" && !strings.Contains(res.Err, c.wantErr) {
				t.Errorf("the log gets %q — it holds no %q, so the person is told less "+
					"than the probe knows", res.Err, c.wantErr)
			}
		})
	}
}

func TestStatusPageMissingComponentNamesThePage(t *testing.T) {
	const (
		api  = "Claude API (api.anthropic.com)"
		code = "Claude Code"
		web  = "claude.ai"
	)
	res := checkStatusPage(t, summaryPage("none",
		[2]string{api, "operational"}, [2]string{code, "operational"},
		[2]string{web, "operational"}), "Claude Cowork")

	if res.Outcome != Config {
		t.Fatalf("outcome %q (%s), expected %q", res.Outcome, res.Err, Config)
	}
	for _, name := range []string{api, code, web} {
		if !strings.Contains(res.Err, `"`+name+`"`) {
			t.Errorf("the refusal %q holds no name %q — the person has nothing to replace the missing one with", res.Err, name)
		}
	}
	if strings.Contains(res.Err, "more") {
		t.Errorf("the refusal %q counts a remainder while three names fit whole", res.Err)
	}
}

func TestStatusPageListSurvivesTheCut(t *testing.T) {
	comps := make([][2]string, 0, 40)
	for i := range cap(comps) {
		comps = append(comps, [2]string{fmt.Sprintf("Component %02d", i), "operational"})
	}
	res := checkStatusPage(t, summaryPage("none", comps...), "Claude Cowork")

	if res.Outcome != Config {
		t.Fatalf("outcome %q (%s), expected %q", res.Outcome, res.Err, Config)
	}
	if len(res.Err) > errMax {
		t.Errorf("the database and the push get %d bytes against a ceiling of %d: %q", len(res.Err), errMax, res.Err)
	}

	known := map[string]bool{}
	for _, c := range comps {
		known[c[0]] = true
	}
	quoted := regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(res.Err, -1)
	if len(quoted) < 2 {
		t.Fatalf("the refusal %q holds not a single name from the page", res.Err)
	}
	shown := quoted[1:]
	for _, m := range shown {
		if !known[m[1]] {
			t.Errorf("the refusal %q has name %q cut in the middle — there is no such component on the page",
				res.Err, m[1])
		}
	}
	if want := fmt.Sprintf(" and %d more", len(comps)-len(shown)); !strings.Contains(res.Err, want) {
		t.Errorf("the refusal %q holds no %q — the list breaks off silently and how many names are "+
			"left the person cannot tell", res.Err, want)
	}
}

func TestStatusPageListPutsLookalikesFirst(t *testing.T) {
	const renamed = "Example API (api.example.com)"

	comps := make([][2]string, 0, 40)
	for i := range 39 {
		comps = append(comps, [2]string{fmt.Sprintf("Component number %02d of the page", i), "operational"})
	}
	comps = append(comps, [2]string{renamed, "operational"})

	res := checkStatusPage(t, summaryPage("none", comps...), "api.example.com")
	if res.Outcome != Config {
		t.Fatalf("outcome %q (%s), expected %q", res.Outcome, res.Err, Config)
	}
	if !strings.Contains(res.Err, `"`+renamed+`"`) {
		t.Errorf("the refusal %q holds no %q — the only lookalike name went into the remainder", res.Err, renamed)
	}
}

func TestShortenCutsOnCharacterBoundary(t *testing.T) {
	for pad := range 4 {
		got := shorten(strings.Repeat("x", pad) + strings.Repeat("é", errMax))
		if len(got) > errMax {
			t.Errorf("offset %d: %d bytes against a ceiling of %d", pad, len(got), errMax)
		}
		if !utf8.ValidString(got) {
			t.Errorf("offset %d: the text is cut in the middle of a character — the database would not take it at all", pad)
		}
	}
}

func TestProbeComponentsSplit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"api.anthropic.com, Claude Code", []string{"api.anthropic.com", "Claude Code"}},
		{" Chat Completions ,, Responses,", []string{"Chat Completions", "Responses"}},
		{"   ", nil},
	}
	for _, c := range cases {
		got := splitComponents(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("%q parsed into %q, expected %q", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: piece %d is %q, expected %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestEveryOutcomeIsNamedOnScreen(t *testing.T) {
	src, err := os.ReadFile("probes.go")
	if err != nil {
		t.Fatal(err)
	}
	outcomes := regexp.MustCompile(`Outcome\s*=\s*"([a-z_]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(outcomes) < 5 {
		t.Fatalf("%d outcomes found in probes.go — the regexp stopped seeing them "+
			"and the test silently checks nothing", len(outcomes))
	}

	screen, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "alerts.js"))
	if err != nil {
		t.Fatal(err)
	}
	dict := string(screen)
	if i := strings.Index(dict, "export const OUTCOME"); i >= 0 {
		if j := strings.Index(dict[i:], "};"); j >= 0 {
			dict = dict[i : i+j]
		}
	} else {
		t.Fatal("the OUTCOME dictionary in web/src/alerts.js was not found — there is nothing to name the outcomes")
	}

	for _, m := range outcomes {
		if !strings.Contains(dict, m[1]+":") {
			t.Errorf("outcome %q is not named in OUTCOME (web/src/alerts.js) — on screen it would show as "+
				"the bare machine word exactly when it first happens", m[1])
		}
	}
}
