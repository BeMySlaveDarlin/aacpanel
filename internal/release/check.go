package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// LatestURL is where GitHub answers which release of the repository is the
// latest: the one make release marks so, never a draft or a pre-release.
const LatestURL = "https://api.github.com/repos/BeMySlaveDarlin/aacpanel/releases/latest"

const (
	// checkFirst is the pause before the first question: a service that
	// fails on start and is started again by docker does not ask on every
	// start.
	checkFirst   = time.Minute
	checkEvery   = 24 * time.Hour
	checkTimeout = 15 * time.Second
	// answerLimit is more than the answer about a release with its notes
	// ever takes.
	answerLimit = 1 << 20
)

// Check asks GitHub, once shortly after the start and then once a day,
// which release of the panel is the latest, and says whether it is newer
// than the one the service was built from. The question carries no token
// and nothing of the machine: GitHub learns the address it came from and
// the release in the user agent. A failure is passed over: a machine off
// the network is a fine place for the panel, and the latest release known
// stays known until GitHub answers otherwise.
type Check struct {
	// Build is the release the service was built from, as the build wrote
	// it. A build that names no release asks nothing: it has nothing to
	// compare an answer with.
	Build string
	// Off is the setting that turns the check off: no request goes out.
	Off bool
	// URL is the address asked, LatestURL outside the tests.
	URL    string
	Client *http.Client
	// First is the pause after the start, Every the one between questions.
	First, Every time.Duration

	mu       sync.Mutex
	answered bool
	latest   *Release
}

// NewCheck is the check of a service built from build, asking GitHub
// itself unless off.
func NewCheck(build string, off bool) *Check {
	return &Check{Build: build, Off: off, URL: LatestURL, Client: http.DefaultClient, First: checkFirst, Every: checkEvery}
}

// Asks says whether the check sends anything at all.
func (c *Check) Asks() bool {
	_, ok := Parse(c.Build)
	return ok && !c.Off
}

// Run asks on its schedule until ctx ends. A check that does not ask
// returns at once, having sent nothing.
func (c *Check) Run(ctx context.Context) {
	if !c.Asks() {
		return
	}
	t := time.NewTimer(c.First)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := c.Once(ctx); err != nil && ctx.Err() == nil {
			log.Printf("release: the latest release was not read, asked again in %s: %v", c.Every, err)
		}
		t.Reset(c.Every)
	}
}

// Once asks GitHub once. A repository with no release yet answers 404,
// which is an answer too: there is no news.
func (c *Check) Once(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "aacpanel/"+c.Build)
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		c.know(nil)
		return nil
	default:
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, answerLimit)).Decode(&body); err != nil {
		return fmt.Errorf("the answer does not parse: %w", err)
	}
	latest, ok := Parse(body.Tag)
	if !ok {
		return fmt.Errorf("the latest release is tagged %q, which is no release of the panel", body.Tag)
	}
	c.know(&latest)
	return nil
}

func (c *Check) know(latest *Release) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.answered, c.latest = true, latest
}

// Answered says whether GitHub has answered since the start: until it has,
// no news says nothing.
func (c *Check) Answered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answered
}

// Newer is the latest release when it is newer than the build. A build of
// the branch between releases is on the latest one: its RELEASE= line
// changes only in the commit a release tags, so it names the last release.
// One ahead of the latest — a release tagged here and not published yet —
// has no news either, and neither has a build that names no release.
func (c *Check) Newer() (Release, bool) {
	build, ok := Parse(c.Build)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !ok || c.latest == nil || c.latest.Compare(build) <= 0 {
		return Release{}, false
	}
	return *c.latest, true
}
