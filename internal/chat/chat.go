// Package chat is the client to the chat socket of the host agent.
package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// Item is one row of the feed.
type Item struct {
	Role    string      `json:"role"`
	Text    string      `json:"text,omitempty"`
	Name    string      `json:"name,omitempty"`
	Arg     string      `json:"arg,omitempty"`
	Cut     bool        `json:"cut,omitempty"`
	State   string      `json:"state,omitempty"`
	Fixes   string      `json:"fixes,omitempty"`
	At      string      `json:"at,omitempty"`
	Shots   []Shot      `json:"shots,omitempty"`
	Calls   []ToolCall  `json:"calls,omitempty"`
	Kind    string      `json:"kind,omitempty"`
	Run     int64       `json:"run,omitempty"`
	From    string      `json:"from,omitempty"`
	Source  string      `json:"source,omitempty"`
	Dir     string      `json:"dir,omitempty"`
	Count   int         `json:"count,omitempty"`
	Tokens  int         `json:"tokens,omitempty"`
	Spots   []ThinkSpot `json:"spots,omitempty"`
	Title   string      `json:"title,omitempty"`
	File    string      `json:"file,omitempty"`
	Desc    string      `json:"desc,omitempty"`
	Icon    string      `json:"icon,omitempty"`
	Label   string      `json:"label,omitempty"`
	Note    string      `json:"note,omitempty"`
	URL     string      `json:"url,omitempty"`
	Status  string      `json:"status,omitempty"`
	Summary string      `json:"summary,omitempty"`
	Use     string      `json:"use,omitempty"`
	Asked   []Asked     `json:"asked,omitempty"`
	Files   []FileRef   `json:"files,omitempty"`
	Pos     int64       `json:"pos"`
}

// FileRef is a file attachment named in a reply.
type FileRef struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Size  int64  `json:"size,omitempty"`
	Media string `json:"media,omitempty"`
}

// Asked is one question of a round together with the answer given.
type Asked struct {
	Text   string   `json:"text"`
	Header string   `json:"header,omitempty"`
	Answer []string `json:"answer,omitempty"`
}

// ArchiveRow is one archived session.
type ArchiveRow struct {
	SessionID     string          `json:"sessionId"`
	Name          string          `json:"name"`
	NameGuessed   bool            `json:"nameGuessed,omitempty"`
	CWD           string          `json:"cwd,omitempty"`
	Slug          string          `json:"slug,omitempty"`
	Profile       string          `json:"profile,omitempty"`
	Project       *ArchiveProject `json:"project,omitempty"`
	Home          bool            `json:"home,omitempty"`
	Model         string          `json:"model,omitempty"`
	Effort        string          `json:"effort,omitempty"`
	Pct           float64         `json:"pct"`
	PctMax        float64         `json:"pctMax"`
	Tokens        int64           `json:"tokens"`
	TokensMax     int64           `json:"tokensMax"`
	Limit         int64           `json:"limit,omitempty"`
	LimitKnown    bool            `json:"limitKnown,omitempty"`
	Messages      int             `json:"messages"`
	Compacts      int             `json:"compacts,omitempty"`
	Stale         bool            `json:"stale,omitempty"`
	StartedAt     string          `json:"startedAt,omitempty"`
	LastAt        string          `json:"lastAt,omitempty"`
	LastRequestAt string          `json:"lastRequestAt,omitempty"`
	NoRequests    bool            `json:"noRequests,omitempty"`
}

// ArchiveProject is the map project the conversation ran in.
type ArchiveProject struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Group string `json:"group"`
}

// ArchivePage is one page of the archive.
type ArchivePage struct {
	Rows   []ArchiveRow `json:"rows"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

// Archive returns a page of the session archive.
func (c *Client) Archive(ctx context.Context, want ArchiveReq) (ArchivePage, error) {
	reply, err := c.Feed(ctx, Req{Archive: &want})
	if err != nil {
		return ArchivePage{}, err
	}
	if reply.Archive == nil {
		return ArchivePage{}, errors.New("the collector returned no archive page")
	}
	return *reply.Archive, nil
}

// ToolCall is one row of the tool call list.
type ToolCall struct {
	Name  string `json:"name"`
	Arg   string `json:"arg,omitempty"`
	At    string `json:"at,omitempty"`
	Seq   int    `json:"seq"`
	Pos   int64  `json:"pos"`
	Index int    `json:"index"`
	Use   string `json:"use,omitempty"`
}

// ThinkSpot is one thinking block inside a run: where it stood and how long it took.
type ThinkSpot struct {
	Seq    int    `json:"seq"`
	At     string `json:"at,omitempty"`
	Tokens int    `json:"tokens,omitempty"`
	Pos    int64  `json:"pos,omitempty"`
	Index  int    `json:"index"`
}

// Shot is one attachment of a reply.
type Shot struct {
	Index int    `json:"index"`
	Media string `json:"media,omitempty"`
	Bytes int    `json:"bytes,omitempty"`
}

// Work is the state of a session: what it planned and what it waits for.
type Work struct {
	Plan      []WorkItem  `json:"plan"`
	Tasks     []WorkTask  `json:"tasks"`
	Agents    []WorkAgent `json:"agents"`
	Ask       *Ask        `json:"ask,omitempty"`
	Artifacts []Artifact  `json:"artifacts,omitempty"`
	Docs      []Doc       `json:"docs,omitempty"`
}

// Artifact is a published page.
type Artifact struct {
	Path  string `json:"path,omitempty"`
	File  string `json:"file"`
	Title string `json:"title"`
	Desc  string `json:"desc,omitempty"`
	Label string `json:"label,omitempty"`
	Note  string `json:"note,omitempty"`
	Icon  string `json:"icon,omitempty"`
	URL   string `json:"url,omitempty"`
	At    string `json:"at,omitempty"`
	Count int    `json:"count,omitempty"`
}

// Doc is a document the session wrote to disk.
type Doc struct {
	Path  string `json:"path"`
	File  string `json:"file"`
	Dir   string `json:"dir,omitempty"`
	At    string `json:"at,omitempty"`
	Count int    `json:"count,omitempty"`
}

// Ask is a pending session question with the options to choose from.
type Ask struct {
	SessionID string        `json:"sessionId"`
	ToolUseID string        `json:"toolUseId,omitempty"`
	CWD       string        `json:"cwd,omitempty"`
	At        string        `json:"at,omitempty"`
	Questions []AskQuestion `json:"questions"`
}

// AskQuestion is one question of a round.
type AskQuestion struct {
	Text    string      `json:"text"`
	Header  string      `json:"header,omitempty"`
	Multi   bool        `json:"multi"`
	Options []AskOption `json:"options"`
}

// AskOption is one answer option.
type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

// WorkItem is one plan entry.
type WorkItem struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// WorkTask is one background job.
type WorkTask struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	At    string `json:"at,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Due   string `json:"due,omitempty"`
	Event string `json:"event,omitempty"`
	Line  string `json:"line,omitempty"`
	// Done tells a shell that is over from one still running. A shell stays in
	// the list after its command ends: its output is still readable, and the
	// screen of the session counts it among the ones it holds.
	Done   bool   `json:"done,omitempty"`
	DoneAt string `json:"doneAt,omitempty"`
}

// WorkAgent is a subagent the session started.
type WorkAgent struct {
	Name       string `json:"name"`
	Text       string `json:"text,omitempty"`
	At         string `json:"at,omitempty"`
	Status     string `json:"status"`
	ReportedAt string `json:"reportedAt,omitempty"`
	Model      string `json:"model,omitempty"`
	Color      string `json:"color,omitempty"`
	Last       string `json:"last,omitempty"`
	ID         string `json:"id,omitempty"`
	Kind       string `json:"kind,omitempty"`
}

// Reply is a window of the feed and its bounds.
type Reply struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Session    string `json:"session,omitempty"`
	Subagent   string `json:"subagent,omitempty"`
	Items      []Item `json:"items"`
	Total      int    `json:"total"`
	MoreBefore bool   `json:"moreBefore"`
	First      *int64 `json:"first"`
	Last       *int64 `json:"last"`
	Size       int64  `json:"size,omitempty"`
	Media      string `json:"media,omitempty"`
	Data       string `json:"data,omitempty"`
	Call
	Archive *ArchivePage `json:"archive,omitempty"`
	State   *Work        `json:"state,omitempty"`
	Text    string       `json:"text,omitempty"`
	Cut     bool         `json:"cut,omitempty"`
	Letters []Letter     `json:"letters,omitempty"`
	Name    string       `json:"name,omitempty"`
	Binary  bool         `json:"binary,omitempty"`
	Kind    string       `json:"kind,omitempty"`
	Mode    string       `json:"mode,omitempty"`
	Offset  int64        `json:"offset"`
	Next    int64        `json:"next,omitempty"`
	TooBig  bool         `json:"tooBig,omitempty"`
}

// Letter is one message from a subagent.
type Letter struct {
	At   string `json:"at,omitempty"`
	Text string `json:"text,omitempty"`
}

// Call is one whole tool call: what was called and what came out.
type Call struct {
	Tool      string `json:"tool,omitempty"`
	At        string `json:"at,omitempty"`
	Args      string `json:"args,omitempty"`
	ArgsCut   bool   `json:"argsCut,omitempty"`
	Result    string `json:"result,omitempty"`
	ResultCut bool   `json:"resultCut,omitempty"`
	Failed    bool   `json:"failed,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
	ResultAt  string `json:"resultAt,omitempty"`
}

// Req is what is asked of the agent.
type Req struct {
	Session  string      `json:"session"`
	Limit    int         `json:"limit,omitempty"`
	Before   *int64      `json:"before,omitempty"`
	After    *int64      `json:"after,omitempty"`
	Image    *ImageRef   `json:"image,omitempty"`
	CallRef  *ImageRef   `json:"call,omitempty"`
	Archive  *ArchiveReq `json:"archive,omitempty"`
	Task     *TaskRef    `json:"task,omitempty"`
	File     string      `json:"file,omitempty"`
	Offset   int64       `json:"offset,omitempty"`
	Bytes    int         `json:"bytes,omitempty"`
	Agent    string      `json:"agent,omitempty"`
	State    bool        `json:"state,omitempty"`
	Subagent string      `json:"subagent,omitempty"`
}

// TaskRef says which background task is wanted.
type TaskRef struct {
	ID string `json:"id"`
}

// ArchiveReq says which page of the archive to return.
type ArchiveReq struct {
	Limit    int      `json:"limit,omitempty"`
	Offset   int      `json:"offset,omitempty"`
	Skip     []string `json:"skip,omitempty"`
	Started  bool     `json:"started,omitempty"`
	Profile  string   `json:"profile,omitempty"`
	Profiles []string `json:"profiles,omitempty"`
}

// ImageRef says where to find an attachment.
type ImageRef struct {
	Pos   int64 `json:"pos"`
	Index int   `json:"index"`
}

// ErrUnavailable means the agent does not answer.
var ErrUnavailable = errors.New("the session collector is not answering")

// ErrNoSubagents means the host collector does not know subagent feeds.
var ErrNoSubagents = errors.New("the session collector on the host knows nothing about subagent feeds: " +
	"update aacpanel-agent on the host (systemctl restart aacpanel-agent@<user>)")

const (
	dialTimeout  = 2 * time.Second
	replyTimeout = 10 * time.Second
	maxReply     = 8 << 20
)

// Client is the path to the socket.
type Client struct {
	Path string
}

func New(path string) *Client { return &Client{Path: path} }

// Available reports whether there is anywhere to go.
func (c *Client) Available() bool { return c != nil && c.Path != "" }

// Feed asks for a window of the feed.
func (c *Client) Feed(ctx context.Context, req Req) (Reply, error) {
	if !c.Available() {
		return Reply{}, ErrUnavailable
	}

	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "unix", c.Path)
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(replyTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	body, err := json.Marshal(req)
	if err != nil {
		return Reply{}, err
	}
	if _, err := conn.Write(body); err != nil {
		return Reply{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if half, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = half.CloseWrite()
	}

	var reply Reply
	dec := json.NewDecoder(&limitedReader{r: conn, left: maxReply})
	if err := dec.Decode(&reply); err != nil {
		return Reply{}, fmt.Errorf("%w: the reply did not parse: %v", ErrUnavailable, err)
	}
	if !reply.OK {
		return reply, errors.New(reply.Error)
	}
	if req.Subagent != "" && reply.Subagent != req.Subagent {
		return Reply{}, ErrNoSubagents
	}
	if reply.Items == nil {
		reply.Items = []Item{}
	}
	return reply, nil
}

type limitedReader struct {
	r    interface{ Read([]byte) (int, error) }
	left int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, errors.New("the agent reply is over the cap")
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

// Target says whose feed is being read.
type Target struct {
	Session  string
	Subagent string
}

// TaskOutput fetches the output tail of a background task.
func (c *Client) TaskOutput(ctx context.Context, t Target, id string) (Reply, error) {
	return c.Feed(ctx, Req{Session: t.Session, Subagent: t.Subagent, Task: &TaskRef{ID: id}})
}

// AgentMail fetches the letters of a subagent by name.
func (c *Client) AgentMail(ctx context.Context, session, name string) ([]Letter, error) {
	reply, err := c.Feed(ctx, Req{Session: session, Agent: name})
	if err != nil {
		return nil, err
	}
	return reply.Letters, nil
}

// FileOf fetches a chunk of a project file by the path from the feed.
func (c *Client) FileOf(ctx context.Context, t Target, path string, offset int64, size int) (Reply, error) {
	return c.Feed(ctx, Req{Session: t.Session, Subagent: t.Subagent, File: path,
		Offset: offset, Bytes: size})
}

func (c *Client) CallOf(ctx context.Context, t Target, pos int64, index int) (Call, error) {
	reply, err := c.Feed(ctx, Req{Session: t.Session, Subagent: t.Subagent,
		CallRef: &ImageRef{Pos: pos, Index: index}})
	if err != nil {
		return Call{}, err
	}
	return reply.Call, nil
}

// Image fetches one attachment.
func (c *Client) Image(ctx context.Context, t Target, pos int64, index int) (string, []byte, error) {
	reply, err := c.Feed(ctx, Req{Session: t.Session, Subagent: t.Subagent,
		Image: &ImageRef{Pos: pos, Index: index}})
	if err != nil {
		return "", nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(reply.Data)
	if err != nil {
		return "", nil, fmt.Errorf("the attachment did not decode: %w", err)
	}
	return reply.Media, raw, nil
}
