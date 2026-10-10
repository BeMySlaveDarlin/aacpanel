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
	Role   string     `json:"role"`
	Text   string     `json:"text,omitempty"`
	Err    string     `json:"err,omitempty"`
	Name   string     `json:"name,omitempty"`
	Arg    string     `json:"arg,omitempty"`
	Cut    bool       `json:"cut,omitempty"`
	State  string     `json:"state,omitempty"`
	Fixes  string     `json:"fixes,omitempty"`
	At     string     `json:"at,omitempty"`
	Shots  []Shot     `json:"shots,omitempty"`
	Calls  []ToolCall `json:"calls,omitempty"`
	Kind   string     `json:"kind,omitempty"`
	Run    int64      `json:"run,omitempty"`
	From   string     `json:"from,omitempty"`
	Source string     `json:"source,omitempty"`
	Dir    string     `json:"dir,omitempty"`
	Count  int        `json:"count,omitempty"`
	Tokens int        `json:"tokens,omitempty"`
	// Undelivered is why a letter sent reached nobody, in claude's words.
	Undelivered string `json:"undelivered,omitempty"`
	// How long a turn or a background task took, and how loud a line is.
	MS int64 `json:"ms,omitempty"`
	// Agents is how many agents a turn left working in the background.
	Agents  int         `json:"agents,omitempty"`
	Level   string      `json:"level,omitempty"`
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
	// A background task done: its id, by which its output or its agent opens.
	Task string `json:"task,omitempty"`
	// A brief card: which document it is and how much of it asks something.
	BriefID   string  `json:"id,omitempty"`
	Eyebrow   string  `json:"eyebrow,omitempty"`
	Questions int     `json:"questions,omitempty"`
	Asked     []Asked `json:"asked,omitempty"`
	// A card of a secret asked for: the notepad the person starts from. The
	// secret is named in Name and said in Title.
	Template string `json:"template,omitempty"`
	// A card of permissions: each call a person was asked about and the answer.
	Rows  []Permitted `json:"rows,omitempty"`
	Files []FileRef   `json:"files,omitempty"`
	// A local command: what it printed, its errors in Err, and Done once its
	// answer has come — an answer may print nothing.
	Out  string `json:"out,omitempty"`
	Done bool   `json:"done,omitempty"`
	// A shell command run with "!": the code it exited with, and on its output
	// the command, for the sheet that shows the whole of it. A command that
	// succeeded exits with 0, which is not an absent code.
	Code    *int   `json:"code,omitempty"`
	Command string `json:"command,omitempty"`
	// The end of a review of codex: its verdict, how sure codex is of it, and
	// the findings.
	Verdict    string    `json:"verdict,omitempty"`
	Confidence *float64  `json:"confidence,omitempty"`
	Findings   []Finding `json:"findings,omitempty"`
	// A goal of codex: the tokens it has spent, its budget, none for none,
	// and the seconds it has taken.
	TokensUsed      int64  `json:"tokensUsed,omitempty"`
	TokenBudget     *int64 `json:"tokenBudget,omitempty"`
	TimeUsedSeconds int64  `json:"timeUsedSeconds,omitempty"`
	// The agents codex started, on the card of their start, and how they
	// stand after a later call to them, on a row the screen does not draw.
	Spawned []Spawned `json:"spawned,omitempty"`
	// The questions codex asked without waiting, on their card: each with
	// the options it offers.
	Asks []AsyncAsk `json:"asks,omitempty"`
	// The answer of a slash command, read into numbers by the collector. The
	// panel carries it to the screen and never looks inside.
	Data json.RawMessage `json:"data,omitempty"`
	Pos  int64           `json:"pos"`
	// Nth tells apart rows of one role a record gives beside one another —
	// the letters of several agents at once, the blocks of one answer: each
	// after the first carries its number among them. A row is found again by
	// Pos, Role and Nth, and one drawn again in its place has no number.
	Nth int `json:"nth,omitempty"`
}

// FileRef is a file attachment named in a reply, or a file the panel sent
// that a message of the person names.
type FileRef struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Size  int64  `json:"size,omitempty"`
	Media string `json:"media,omitempty"`
	// Outside is a file sent from outside the directory of the conversation:
	// the reader would refuse to open it, and its row says so.
	Outside bool `json:"outside,omitempty"`
}

// Asked is one question of a round together with the answer given, and the
// note a person added beside a pick.
type Asked struct {
	Text   string   `json:"text"`
	Header string   `json:"header,omitempty"`
	Answer []string `json:"answer,omitempty"`
	Note   string   `json:"note,omitempty"`
}

// AsyncAsk is one question codex asked without waiting, and the options it
// offers; none means the answer is words of one's own.
type AsyncAsk struct {
	Title   string   `json:"title"`
	Options []string `json:"options"`
}

// Finding is one finding of a review of codex: what is wrong, why, how much
// it matters (0 the most) and how sure codex is, and where in the code — the
// file and its first and last line.
type Finding struct {
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	Priority   *int     `json:"priority,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	Path       string   `json:"path,omitempty"`
	Lines      []int    `json:"lines,omitempty"`
}

// Spawned is one agent codex started: its thread, by which its feed opens,
// its nickname and role, the model and effort it runs on, and how it stands
// in codex's word.
type Spawned struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Role   string `json:"role,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	State  string `json:"state,omitempty"`
}

// Permitted is one call of a card of permissions: what it was about and what
// the person answered.
type Permitted struct {
	Tool     string `json:"tool"`
	Subject  string `json:"subject,omitempty"`
	Decision string `json:"decision"`
	Lasting  bool   `json:"lasting,omitempty"`
}

// ArchiveRow is one archived session: a conversation of claude, or a thread of
// codex — Agent says which, empty for claude.
type ArchiveRow struct {
	SessionID   string          `json:"sessionId"`
	Agent       string          `json:"agent,omitempty"`
	Name        string          `json:"name"`
	NameGuessed bool            `json:"nameGuessed,omitempty"`
	CWD         string          `json:"cwd,omitempty"`
	Slug        string          `json:"slug,omitempty"`
	Profile     string          `json:"profile,omitempty"`
	Project     *ArchiveProject `json:"project,omitempty"`
	Home        bool            `json:"home,omitempty"`
	Model       string          `json:"model,omitempty"`
	Effort      string          `json:"effort,omitempty"`
	Pct         float64         `json:"pct"`
	PctMax      float64         `json:"pctMax"`
	Tokens      int64           `json:"tokens"`
	TokensMax   int64           `json:"tokensMax"`
	// TokensUsed is what a thread of codex spent over its life, as codex
	// counts it; a thread's archive knows no fill of its context.
	TokensUsed    int64  `json:"tokensUsed,omitempty"`
	Limit         int64  `json:"limit,omitempty"`
	LimitKnown    bool   `json:"limitKnown,omitempty"`
	Messages      int    `json:"messages"`
	Compacts      int    `json:"compacts,omitempty"`
	Stale         bool   `json:"stale,omitempty"`
	StartedAt     string `json:"startedAt,omitempty"`
	LastAt        string `json:"lastAt,omitempty"`
	LastRequestAt string `json:"lastRequestAt,omitempty"`
	NoRequests    bool   `json:"noRequests,omitempty"`
	// Prompts are the person's last messages, oldest first: the screen names
	// the conversation by the last one that says something.
	Prompts []string `json:"prompts,omitempty"`
}

// ArchiveProject is the map project the conversation ran in. Session is the
// name a new session of it comes up under, which is not always its name: the
// map names a project for people and its session for the host.
type ArchiveProject struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Session string `json:"session"`
	Path    string `json:"path"`
	Group   string `json:"group"`
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
	// Open is a call whose result has not come and whose turn is not over:
	// the call the session is running now. Failed is a call whose result came
	// as an error. Each is absent rather than false when it does not hold.
	Open   bool `json:"open,omitempty"`
	Failed bool `json:"failed,omitempty"`
	// Shots are the pictures the call returned: a file it read, a page it
	// took a shot of.
	Shots []Shot `json:"shots,omitempty"`
}

// ThinkSpot is one thinking block inside a run: where it stood and how long it took.
type ThinkSpot struct {
	Seq    int    `json:"seq"`
	At     string `json:"at,omitempty"`
	Tokens int    `json:"tokens,omitempty"`
	Pos    int64  `json:"pos,omitempty"`
	Index  int    `json:"index"`
}

// Shot is one picture of the feed: a block of a prompt, a picture a call
// returned or a file the panel sent that a message names.
type Shot struct {
	Index int    `json:"index"`
	Media string `json:"media,omitempty"`
	Bytes int    `json:"bytes,omitempty"`
	// Pos and Part place a picture a call returned: the record of the result,
	// and the place of the picture in the result standing at Index.
	Pos  int64 `json:"pos,omitempty"`
	Part *int  `json:"part,omitempty"`
	// Upload is the name of a file the panel sent, in the directory the
	// executor keeps them in; Path is the line of the message naming it.
	Upload string `json:"upload,omitempty"`
	Path   string `json:"path,omitempty"`
	// Preview says the picture served under Upload is the smaller JPEG the
	// phone drew of the file, not the file itself.
	Preview bool `json:"preview,omitempty"`
}

// Work is the state of a session: what it runs and what it waits for.
type Work struct {
	Tasks     []WorkTask  `json:"tasks"`
	Agents    []WorkAgent `json:"agents"`
	Workflows []WorkFlow  `json:"workflows,omitempty"`
	Ask       *Ask        `json:"ask,omitempty"`
	Artifacts []Artifact  `json:"artifacts,omitempty"`
	Docs      []Doc       `json:"docs,omitempty"`
	Sent      []Sent      `json:"sent,omitempty"`
	Checklist *Checklist  `json:"checklist,omitempty"`
}

// Checklist is the list of steps the session keeps of its work through the
// panel's checklist tool, as it last sent it, with a line about it as a whole.
type Checklist struct {
	Items []ChecklistItem `json:"items"`
	At    string          `json:"at"`
	Note  string          `json:"note,omitempty"`
}

// ChecklistItem is one step: pending, active, done or dropped. Since is when a
// step at work or done took its status.
type ChecklistItem struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	Since  string `json:"since,omitempty"`
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

// Sent is a file the session delivered to the human.
type Sent struct {
	Path  string `json:"path"`
	File  string `json:"file"`
	Size  int64  `json:"size,omitempty"`
	Media string `json:"media,omitempty"`
	At    string `json:"at,omitempty"`
	Count int    `json:"count,omitempty"`
	// Outside marks a file that lies outside the directory of the
	// conversation: the reader will refuse it, and the list says so
	// instead of offering a tap.
	Outside bool `json:"outside,omitempty"`
}

// Ask is a pending session question with the options to choose from.
type Ask struct {
	SessionID string        `json:"sessionId"`
	ToolUseID string        `json:"toolUseId,omitempty"`
	CWD       string        `json:"cwd,omitempty"`
	At        string        `json:"at,omitempty"`
	Questions []AskQuestion `json:"questions"`
	// Server and Message are what an MCP server asks through codex: its name
	// and its words above the fields of its form.
	Server  string `json:"server,omitempty"`
	Message string `json:"message,omitempty"`
}

// AskQuestion is one question of a round. A question of codex is answered by
// its ID, and says whether it takes words of the person's own (Other), whether
// they are not shown as they are typed (Secret), and whether a form goes
// without it (Required); absent is false.
type AskQuestion struct {
	ID       string      `json:"id,omitempty"`
	Text     string      `json:"text"`
	Header   string      `json:"header,omitempty"`
	Multi    bool        `json:"multi"`
	Options  []AskOption `json:"options"`
	Other    bool        `json:"other,omitempty"`
	Secret   bool        `json:"secret,omitempty"`
	Required bool        `json:"required,omitempty"`
}

// AskOption is one answer option.
type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
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
	// Schedule and Repeats describe a job of the session's cron: how claude
	// words its cron expression, and whether it fires again after this time.
	Schedule string `json:"schedule,omitempty"`
	Repeats  bool   `json:"repeats,omitempty"`
	// Done tells a shell that is over from one still running. A shell stays in
	// the list after its command ends: its output is still readable, and the
	// screen of the session counts it among the ones it holds.
	Done   bool   `json:"done,omitempty"`
	DoneAt string `json:"doneAt,omitempty"`
	// Agent names the subagent that started the work. The session holds a
	// shell of its agent as its own, and the list would otherwise show
	// several identical waits with no way to tell whose each one is.
	Agent string `json:"agent,omitempty"`
}

// WorkAgent is a subagent the session started. Status is active while it
// works; a teammate that wrote is reported, and an agent sent to the
// background ends the way its task did — completed, failed, stopped or killed —
// at DoneAt.
type WorkAgent struct {
	Name       string `json:"name"`
	Text       string `json:"text,omitempty"`
	At         string `json:"at,omitempty"`
	Status     string `json:"status"`
	ReportedAt string `json:"reportedAt,omitempty"`
	DoneAt     string `json:"doneAt,omitempty"`
	Model      string `json:"model,omitempty"`
	Color      string `json:"color,omitempty"`
	Last       string `json:"last,omitempty"`
	ID         string `json:"id,omitempty"`
	Kind       string `json:"kind,omitempty"`
	// Tokens is the context of the agent: the input of its last request.
	// Limit is the window of its model, guessed when LimitKnown is false.
	Tokens     int64 `json:"tokens,omitempty"`
	Limit      int64 `json:"limit,omitempty"`
	LimitKnown bool  `json:"limitKnown,omitempty"`
	// Agent is codex for a run of codex exec the session started, empty for
	// an agent of claude's. Such a run is at work while its process lives,
	// and ID is its thread: its feed is the rollout of that thread, opened as
	// a conversation of its own.
	Agent string `json:"agent,omitempty"`
}

// WorkFlow is a workflow run: one script, its phases and the agents it drives.
// It stands apart from both lists beside it — among the subagents a run of
// ninety would bury them, among the background jobs it would say nothing but
// that something is running.
type WorkFlow struct {
	ID     string `json:"id"`
	Task   string `json:"task,omitempty"`
	Name   string `json:"name,omitempty"`
	Text   string `json:"text,omitempty"`
	At     string `json:"at,omitempty"`
	Status string `json:"status"`
	DoneAt string `json:"doneAt,omitempty"`
	Event  string `json:"event,omitempty"`
	Dir    string `json:"dir,omitempty"`
	Script string `json:"script,omitempty"`
	// Agents is how many the run has started, Tokens what they have read and
	// Calls how many tools they have used. MS is how long the run took.
	Agents int         `json:"agents,omitempty"`
	Tokens int64       `json:"tokens,omitempty"`
	Calls  int         `json:"calls,omitempty"`
	MS     int64       `json:"ms,omitempty"`
	Phases []WorkPhase `json:"phases,omitempty"`
	// Logs is what the run said about itself along the way — the agents that
	// stalled and were retried — and Result what its script returned.
	Logs   []string `json:"logs,omitempty"`
	Result string   `json:"result,omitempty"`
}

// WorkPhase is one phase of a workflow, as the script declares it.
type WorkPhase struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
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
	Briefs  []BriefCard  `json:"briefs,omitempty"`
	Brief   *Brief       `json:"brief,omitempty"`
	Repo    *RepoOut     `json:"repo,omitempty"`
	Pages   []PageCard   `json:"pages,omitempty"`
	Page    *Page        `json:"page,omitempty"`
	Dropped string       `json:"dropped,omitempty"`
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
	Mtime   string       `json:"mtime,omitempty"`

	// Notes are the calls standing on the collector, and Seq the count of
	// calls it has taken.
	Notes []SessionNote `json:"notes,omitempty"`
	Seq   *int64        `json:"seq,omitempty"`

	// Matches are the places of a conversation a search found: Total counts
	// them all, and Cut says there are more than came.
	Matches []Match `json:"matches,omitempty"`
}

// Match is one hit of a search in a conversation: the row of the feed it is
// in, by the position and the number the feed knows that row by, and the
// words around it. Hit is where the hit stands in Snippet, as a start and a
// length in UTF-16 units — the way the screen counts a string.
type Match struct {
	Pos     int64  `json:"pos"`
	Nth     int    `json:"nth,omitempty"`
	At      string `json:"at"`
	Role    string `json:"role"`
	Snippet string `json:"snippet"`
	Hit     [2]int `json:"hit"`
}

// Found is what a search of a conversation found.
type Found struct {
	Matches []Match `json:"matches"`
	Total   int     `json:"total"`
	Cut     bool    `json:"cut"`
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
	Session   string      `json:"session"`
	Limit     int         `json:"limit,omitempty"`
	Before    *int64      `json:"before,omitempty"`
	After     *int64      `json:"after,omitempty"`
	Image     *ImageRef   `json:"image,omitempty"`
	CallRef   *ImageRef   `json:"call,omitempty"`
	Archive   *ArchiveReq `json:"archive,omitempty"`
	Briefs    *BriefsReq  `json:"briefs,omitempty"`
	Brief     string      `json:"brief,omitempty"`
	Pages     *PagesReq   `json:"pages,omitempty"`
	Page      string      `json:"page,omitempty"`
	DropBrief *DropBrief  `json:"dropBrief,omitempty"`
	Notes     *NotesReq   `json:"notes,omitempty"`
	Task      *TaskRef    `json:"task,omitempty"`
	File      string      `json:"file,omitempty"`
	Raw       string      `json:"raw,omitempty"`
	Offset    int64       `json:"offset,omitempty"`
	Bytes     int         `json:"bytes,omitempty"`
	Agent     string      `json:"agent,omitempty"`
	State     bool        `json:"state,omitempty"`
	Repo      *RepoReq    `json:"repo,omitempty"`
	Subagent  string      `json:"subagent,omitempty"`
	Search    *SearchReq  `json:"search,omitempty"`
}

// SearchReq is a question asked of a whole conversation, and how many of its
// matches to return at most: the newest of them.
type SearchReq struct {
	Q     string `json:"q"`
	Limit int    `json:"limit,omitempty"`
}

// The length of a question, in characters after the space around it is
// trimmed, and how many matches one answer carries.
const (
	MinQuestion = 2
	MaxQuestion = 200
	MaxMatches  = 300
)

// TaskRef says which background task is wanted.
type TaskRef struct {
	ID string `json:"id"`
}

// ArchiveReq says which page of the archive to return.
type ArchiveReq struct {
	Limit    int      `json:"limit,omitempty"`
	Offset   int      `json:"offset,omitempty"`
	Skip     []string `json:"skip,omitempty"`
	Profile  string   `json:"profile,omitempty"`
	Profiles []string `json:"profiles,omitempty"`
	// Under keeps only the conversations that ran in a directory or below it:
	// the archive of one project.
	Under string `json:"under,omitempty"`
}

// ImageRef says where to find an attachment: a block of a record, a picture
// at Part in the result of a call, or a file the panel sent, by its name.
type ImageRef struct {
	Pos    int64  `json:"pos"`
	Index  int    `json:"index"`
	Part   *int   `json:"part,omitempty"`
	Upload string `json:"upload,omitempty"`
}

// ErrUnavailable means the agent does not answer.
var ErrUnavailable = errors.New("the session collector is not answering")

// ErrNoSearch means the host collector does not search conversations.
var ErrNoSearch = errors.New("the session collector on the host does not search conversations: " +
	"update aacpanel-agent on the host (systemctl restart aacpanel-agent@<user>)")

// ErrNoSubagents means the host collector does not know subagent feeds.
var ErrNoSubagents = errors.New("the session collector on the host knows nothing about subagent feeds: " +
	"update aacpanel-agent on the host (systemctl restart aacpanel-agent@<user>)")

const (
	dialTimeout  = 2 * time.Second
	replyTimeout = 10 * time.Second
	// maxReply is the largest reply read from the collector. The largest it
	// sends are the windows of a file: text of up to 1 MB escaped for JSON,
	// six bytes at most for one on disk, and a picture of up to 4 MB in base64.
	maxReply = 8 << 20
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

// Search asks for the places in a conversation that say the question. A
// collector that answers without matches does not know the question: it took
// the request for one of a window of the feed.
func (c *Client) Search(ctx context.Context, t Target, q string) (Found, error) {
	reply, err := c.Feed(ctx, Req{Session: t.Session, Subagent: t.Subagent,
		Search: &SearchReq{Q: q, Limit: MaxMatches}})
	if err != nil {
		return Found{}, err
	}
	if reply.Matches == nil {
		return Found{}, ErrNoSearch
	}
	return Found{Matches: reply.Matches, Total: reply.Total, Cut: reply.Cut}, nil
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

// RawFile fetches a range of the bytes of a project file as they lie on disk.
// Every kind of file travels this way, an executable and a binary included: it
// is asked for to save the file on a device, not to show it on a screen.
func (c *Client) RawFile(ctx context.Context, t Target, path string, offset int64, size int) (Reply, error) {
	return c.Feed(ctx, Req{Session: t.Session, Subagent: t.Subagent, Raw: path,
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
func (c *Client) Image(ctx context.Context, t Target, ref ImageRef) (string, []byte, error) {
	reply, err := c.Feed(ctx, Req{Session: t.Session, Subagent: t.Subagent, Image: &ref})
	if err != nil {
		return "", nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(reply.Data)
	if err != nil {
		return "", nil, fmt.Errorf("the attachment did not decode: %w", err)
	}
	return reply.Media, raw, nil
}
