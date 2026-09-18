package chat

import (
	"context"
	"errors"
)

// ErrNoRepo is what an older collector answers with: it knows nothing of
// repositories and returns a feed. Saying so beats handing back an empty
// listing, which reads as "this project has no files".
var ErrNoRepo = errors.New("the collector on the host does not read repositories yet")

// RepoReq asks the agent for one thing about a repository. Everything the
// panel knows about a working tree arrives through here, because the service
// has no rights on the host and git runs only in the agent.
type RepoReq struct {
	Op    string `json:"op"`
	Cwd   string `json:"cwd"`
	Path  string `json:"path,omitempty"`
	Base  string `json:"base,omitempty"`
	Rev   string `json:"rev,omitempty"`
	Layer string `json:"layer,omitempty"`
	Hash  string `json:"hash,omitempty"`
	First int    `json:"first,omitempty"`
	Lines int    `json:"lines,omitempty"`
}

// RepoChange is one file this branch changed.
type RepoChange struct {
	Path   string `json:"path"`
	Layer  string `json:"layer"`
	Add    int    `json:"add"`
	Delete int    `json:"delete"`
	Status string `json:"status,omitempty"`
}

// RepoEntry is one name inside a directory of the working tree.
type RepoEntry struct {
	Name      string `json:"name"`
	Dir       bool   `json:"dir"`
	Untracked bool   `json:"untracked,omitempty"`
}

// RepoBranch is a branch of the repository and what it points at.
type RepoBranch struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream,omitempty"`
}

// RepoTree is one worktree of the repository.
type RepoTree struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
}

// RepoOut is every field the agent may answer a repo request with. One shape
// for all six operations: the alternative is six replies that differ in a
// field each, and a reader has to know which is which before reading it.
type RepoOut struct {
	// A window asked for under a revision the repository has moved past. The
	// screen asks again under the revision named here rather than drawing a
	// diff stitched out of two states.
	Stale bool   `json:"stale,omitempty"`
	Rev   string `json:"rev,omitempty"`

	// A directory that is not a repository at all. Not a failure: a project
	// can be a shelf of notes or a stand, and the screen says so plainly
	// instead of showing it what git shouted.
	NoRepo bool `json:"noRepo,omitempty"`

	Root      string       `json:"root,omitempty"`
	Branch    string       `json:"branch,omitempty"`
	Branches  []RepoBranch `json:"branches,omitempty"`
	Worktrees []RepoTree   `json:"worktrees,omitempty"`

	Base     string       `json:"base,omitempty"`
	BaseFrom string       `json:"baseFrom,omitempty"`
	Head     string       `json:"head,omitempty"`
	Files    []RepoChange `json:"files,omitempty"`

	Path    string      `json:"path,omitempty"`
	Entries []RepoEntry `json:"entries,omitempty"`
	Total   int         `json:"total,omitempty"`
	Cut     bool        `json:"cut,omitempty"`

	// The id of a blob: what a coloured copy of it is cached under.
	OID    string   `json:"oid,omitempty"`
	Size   int64    `json:"size,omitempty"`
	First  int      `json:"first,omitempty"`
	Lines  []string `json:"lines,omitempty"`
	More   bool     `json:"more,omitempty"`
	Binary bool     `json:"binary,omitempty"`
	TooBig bool     `json:"tooBig,omitempty"`

	Committed    string `json:"committed,omitempty"`
	CommittedCut bool   `json:"committedCut,omitempty"`
	Worktree     string `json:"worktree,omitempty"`
	WorktreeCut  bool   `json:"worktreeCut,omitempty"`

	Hash    string `json:"hash,omitempty"`
	Author  string `json:"author,omitempty"`
	At      string `json:"at,omitempty"`
	Subject string `json:"subject,omitempty"`
	Session string `json:"session,omitempty"`
}

// Repo asks the agent one question about a repository.
func (c *Client) Repo(ctx context.Context, req RepoReq) (*RepoOut, error) {
	reply, err := c.Feed(ctx, Req{Repo: &req})
	if err != nil {
		return nil, err
	}
	if reply.Repo == nil {
		return nil, ErrNoRepo
	}
	return reply.Repo, nil
}
