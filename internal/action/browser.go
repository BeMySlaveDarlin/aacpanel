package action

import (
	"context"
	"fmt"

	"aacpanel/internal/browserlink"
)

const (
	// BrowserCreate creates metadata; Chrome starts only on BrowserOpen.
	BrowserCreate Kind = "browser.create"
	// BrowserDelete removes a stopped profile and its saved sign-ins.
	BrowserDelete Kind = "browser.delete"
	// BrowserOpen starts a bounded holder for an existing profile.
	BrowserOpen Kind = "browser.open"
	// BrowserClose stops its holder while keeping the profile data.
	BrowserClose Kind = "browser.close"
	// AskBrowser is the read-only capability and profile query.
	AskBrowser = "browser"
)

// BrowserKind identifies lifecycle actions gated by the host browser switch.
func BrowserKind(k Kind) bool {
	return k == BrowserCreate || k == BrowserDelete || k == BrowserOpen || k == BrowserClose
}

// BrowserAsker reports the host capability and owned profiles without launching one.
type BrowserAsker interface {
	Browser(context.Context) (browserlink.Status, error)
}

func validateBrowser(r Request) error {
	if !browserlink.ID(r.Target) {
		return badRequest("invalid browser profile id")
	}
	if r.Kind == BrowserCreate {
		return browserlink.Name(r.BrowserName)
	}
	if r.BrowserName != "" {
		return badRequest("only a new browser profile takes a name")
	}
	return nil
}

// Browser asks for an explicit capability; older executors cannot imply it
// through the compatibility rule for other action kinds.
func (c *Client) Browser(ctx context.Context) (browserlink.Status, error) {
	r, err := c.Do(ctx, Request{Ask: AskBrowser})
	if err != nil {
		return browserlink.Status{}, err
	}
	if !r.OK || r.Browser == nil {
		return browserlink.Status{}, fmt.Errorf("the browser is unavailable: %s", r.Error)
	}
	return *r.Browser, nil
}
