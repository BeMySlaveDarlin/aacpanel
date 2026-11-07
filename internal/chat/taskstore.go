package chat

import (
	"context"
	"errors"

	"aacpanel/internal/taskreg"
)

type TaskStoreReq struct {
	CWD   string `json:"cwd"`
	After string `json:"after,omitempty"`
}

// TaskStore reads every page before handing a complete census to the importer.
// A lost page must never turn the unread files into deletions.
func (c *Client) TaskStore(ctx context.Context, cwd string) (taskreg.Snapshot, error) {
	out := taskreg.Snapshot{Files: []taskreg.File{}, Errors: []string{}}
	after := ""
	for {
		reply, err := c.Feed(ctx, Req{TaskStore: &TaskStoreReq{CWD: cwd, After: after}})
		if err != nil {
			return taskreg.Snapshot{}, err
		}
		if reply.TaskStore == nil {
			return taskreg.Snapshot{}, errors.New("the collector does not read task stores")
		}
		page := reply.TaskStore
		if after == "" {
			out = *page
			out.Files = nil
		} else if page.Root != out.Root || page.Present != out.Present {
			return taskreg.Snapshot{}, errors.New("the task store moved while it was being read")
		}
		out.TasksComplete = out.TasksComplete && page.TasksComplete
		out.HandoffsComplete = out.HandoffsComplete && page.HandoffsComplete
		out.Files = append(out.Files, page.Files...)
		if after != "" {
			out.Errors = append(out.Errors, page.Errors...)
		}
		out.Next = ""
		if page.Next == "" {
			return out, nil
		}
		if page.Next <= after {
			return taskreg.Snapshot{}, errors.New("the task store cursor did not advance")
		}
		after = page.Next
	}
}
