package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// github stands for the API of GitHub: it answers the latest release as it
// is told and keeps every request it got.
type github struct {
	mu     sync.Mutex
	status int
	body   string
	asked  []*http.Request
}

func newGitHub(t *testing.T, status int, body string) (*github, *httptest.Server) {
	t.Helper()
	g := &github{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.asked = append(g.asked, r)
		status, body := g.status, g.body
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return g, srv
}

func (g *github) answer(status int, body string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status, g.body = status, body
}

func (g *github) requests() []*http.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]*http.Request(nil), g.asked...)
}

func checkOf(build, url string) *Check {
	c := NewCheck(build, false)
	c.URL, c.First, c.Every = url+"/repos/BeMySlaveDarlin/aacpanel/releases/latest", time.Millisecond, 10*time.Millisecond
	return c
}

func latest(tag string) string {
	return `{"tag_name": "` + tag + `", "html_url": "https://github.com/BeMySlaveDarlin/aacpanel/releases/tag/` + tag +
		`", "draft": false, "prerelease": false, "body": "notes"}`
}

// Only a latest release strictly newer than the build is news: the build
// on it has none, a build ahead of it has none, and an answer that names no
// release of the panel is not taken for one.
func TestOnlyALatestReleaseNewerThanTheBuildIsNews(t *testing.T) {
	for _, c := range []struct {
		name, build string
		status      int
		body        string
		failed      bool
		answered    bool
		newer       string
	}{
		{"a minor out", "v1.2.0", 200, latest("v1.2.1"), false, true, "v1.2.1"},
		{"a major out", "v1.2.0", 200, latest("v1.3.0"), false, true, "v1.3.0"},
		{"a paradigm out", "v1.9.9", 200, latest("v2.0.0"), false, true, "v2.0.0"},
		{"numbers compare as numbers", "v1.9.0", 200, latest("v1.10.0"), false, true, "v1.10.0"},
		{"the build is on it", "v1.2.0", 200, latest("v1.2.0"), false, true, ""},
		{"the build is ahead of it", "v1.3.0", 200, latest("v1.2.0"), false, true, ""},
		{"no release yet", "v1.2.0", 404, `{"message": "Not Found"}`, false, true, ""},
		{"a malformed tag", "v1.2.0", 200, latest("v1.3"), true, false, ""},
		{"a tag of something else", "v1.2.0", 200, latest("v1.3.0-rc1"), true, false, ""},
		{"an answer that does not parse", "v1.2.0", 200, `<html>`, true, false, ""},
		{"GitHub fails", "v1.2.0", 502, `{}`, true, false, ""},
		{"the build names no release", "", 200, latest("v1.3.0"), false, true, ""},
		{"the build names something else", "1.2.0", 200, latest("v1.3.0"), false, true, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, srv := newGitHub(t, c.status, c.body)
			check := checkOf(c.build, srv.URL)
			err := check.Once(t.Context())
			if (err != nil) != c.failed {
				t.Errorf("the question ended with %v, a failure meant: %v", err, c.failed)
			}
			if check.Answered() != c.answered {
				t.Errorf("answered: %v, want %v", check.Answered(), c.answered)
			}
			newer, ok := check.Newer()
			if got := map[bool]string{true: newer.String(), false: ""}[ok]; got != c.newer {
				t.Errorf("the news is %q, want %q", got, c.newer)
			}
			asked := g.requests()
			if len(asked) != 1 {
				t.Fatalf("GitHub was asked %d times, want once", len(asked))
			}
			r := asked[0]
			if r.Method != http.MethodGet || r.URL.Path != "/repos/BeMySlaveDarlin/aacpanel/releases/latest" {
				t.Errorf("the question was %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("Authorization") != "" || r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Errorf("the question carried Authorization %q and Accept %q: no token, the JSON of the API",
					r.Header.Get("Authorization"), r.Header.Get("Accept"))
			}
		})
	}
}

// A failure keeps what was known: a machine that lost the network still
// knows the release it was told of, and a later answer moves it on.
func TestAFailureKeepsTheLatestReleaseKnown(t *testing.T) {
	g, srv := newGitHub(t, 200, latest("v1.3.0"))
	check := checkOf("v1.2.0", srv.URL)
	if err := check.Once(t.Context()); err != nil {
		t.Fatal(err)
	}
	g.answer(503, `{}`)
	if err := check.Once(t.Context()); err == nil {
		t.Error("a 503 was taken for an answer")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := check.Once(ctx); err == nil {
		t.Error("a question that never went out was taken for an answer")
	}
	if newer, ok := check.Newer(); !ok || newer.String() != "v1.3.0" {
		t.Errorf("after the failures the news is %v (%v), want v1.3.0 still", newer, ok)
	}
	g.answer(200, latest("v1.4.0"))
	if err := check.Once(t.Context()); err != nil {
		t.Fatal(err)
	}
	if newer, ok := check.Newer(); !ok || newer.String() != "v1.4.0" {
		t.Errorf("the next answer gave %v (%v), want v1.4.0", newer, ok)
	}
}

// The check runs on its own schedule; switched off, or in a build that names
// no release, it sends nothing at all.
func TestACheckThatIsOffAsksNothing(t *testing.T) {
	g, srv := newGitHub(t, 200, latest("v1.3.0"))
	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan struct{})
	go func() {
		checkOf("v1.2.0", srv.URL).Run(ctx)
		close(ran)
	}()
	for deadline := time.Now().Add(5 * time.Second); len(g.requests()) < 2 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-ran
	if n := len(g.requests()); n < 2 {
		t.Fatalf("a check that is on asked %d times in its run, want the first question and the next: "+
			"the cases below prove nothing", n)
	}

	// A check that asks nothing returns at once; the deadline is for one
	// that would ask after all, which its first pause lets do.
	run := func(c *Check) {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		c.Run(ctx)
	}

	for _, c := range []struct {
		name  string
		check func(url string) *Check
	}{
		{"switched off", func(url string) *Check { c := checkOf("v1.2.0", url); c.Off = true; return c }},
		{"a build of no release", func(url string) *Check { return checkOf("", url) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, srv := newGitHub(t, 200, latest("v1.3.0"))
			check := c.check(srv.URL)
			if check.Asks() {
				t.Error("the check says it asks")
			}
			run(check)
			if n := len(g.requests()); n != 0 {
				t.Errorf("GitHub was asked %d times", n)
			}
			if _, ok := check.Newer(); ok || check.Answered() {
				t.Error("a check that asked nothing has news")
			}
		})
	}
}
