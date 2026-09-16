package chat

import (
	"context"
	"errors"
)

// ErrNoPages is the answer of a collector that does not keep copies of
// published pages. It is not the same as a machine where nothing was
// published, and the screens say so differently.
var ErrNoPages = errors.New("this collector does not keep the pages a session published")

// PageCard is what the shelf shows about a published page without opening it.
type PageCard struct {
	ID      string `json:"id"`
	Session string `json:"session,omitempty"`
	CWD     string `json:"cwd,omitempty"`
	File    string `json:"file,omitempty"`
	Title   string `json:"title"`
	Desc    string `json:"desc,omitempty"`
	Icon    string `json:"icon,omitempty"`
	// The address it was published at. The panel shows the copy; this is for
	// the person who is signed into the account that owns it.
	URL      string `json:"url,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
	Versions int    `json:"versions,omitempty"`
	At       string `json:"at,omitempty"`
}

// Page is a copy of a published page, whole.
type Page struct {
	PageCard
	HTML    string `json:"html"`
	FirstAt string `json:"firstAt,omitempty"`
}

// PagesReq asks for the shelf of pages, of one session or of the machine.
type PagesReq struct {
	Session string `json:"session,omitempty"`
}

// Pages returns the card of every kept page, newest first.
func (c *Client) Pages(ctx context.Context, session string) ([]PageCard, error) {
	reply, err := c.Feed(ctx, Req{Pages: &PagesReq{Session: session}})
	if err != nil {
		return nil, err
	}
	// A collector that predates the copies answers the request as a feed and
	// returns no shelf. An empty list would read as "nothing was published"
	// and send nobody to update anything.
	if reply.Pages == nil {
		return nil, ErrNoPages
	}
	return reply.Pages, nil
}

// PageOf returns one page whole, with the html it was published as.
func (c *Client) PageOf(ctx context.Context, id string) (*Page, error) {
	reply, err := c.Feed(ctx, Req{Page: id})
	if err != nil {
		return nil, err
	}
	if reply.Page == nil {
		return nil, ErrNoPages
	}
	return reply.Page, nil
}
