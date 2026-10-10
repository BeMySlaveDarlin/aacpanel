package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// The commands of codex's own terminal client a thread takes through the
// protocol: a compaction, a review, a name, a goal, the background terminals
// a turn left running, and what the thread has in MCP servers and skills.

// extrasEvery is how often a thread nothing happens in has its goal and its
// background terminals read again. A thread that runs, that changed since the
// last read or that has terminals running is read at every poll: those are
// what change them.
var extrasEvery = 30 * time.Second

// Goal is the goal of a thread, as the daemon tells it: what codex works
// towards across turns, how it stands, what it has spent and its budget.
type Goal struct {
	Objective string `json:"objective"`
	// Status is active, paused, blocked, usageLimited, budgetLimited or
	// complete.
	Status      string `json:"status"`
	TokensUsed  int64  `json:"tokensUsed"`
	TokenBudget *int64 `json:"tokenBudget"`
	TimeUsed    int64  `json:"timeUsedSeconds"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// The statuses a person sets a goal to; the others are codex's own word on
// how the goal stands.
const (
	GoalActive = "active"
	GoalPaused = "paused"
)

// Process is a background terminal of a thread: a command a turn started and
// left running. CPU is a percentage and RSS kilobytes, absent where codex
// could not read them.
type Process struct {
	ID      string   `json:"processId"`
	Command string   `json:"command"`
	CWD     string   `json:"cwd"`
	PID     *int64   `json:"osPid"`
	CPU     *float64 `json:"cpuPercent"`
	RSS     *int64   `json:"rssKb"`
}

// Review is what a review looks at: the uncommitted changes, a branch the
// work is compared against, one commit, or what the person describes.
type Review struct {
	// Target is uncommittedChanges, baseBranch, commit or custom, as codex
	// names them.
	Target       string
	Branch       string
	SHA          string
	Title        string
	Instructions string
}

// The targets of a review, as codex names them.
const (
	ReviewUncommitted = "uncommittedChanges"
	ReviewBranch      = "baseBranch"
	ReviewCommit      = "commit"
	ReviewCustom      = "custom"
)

func (r Review) wire() (map[string]any, error) {
	switch r.Target {
	case ReviewUncommitted:
		return map[string]any{"type": r.Target}, nil
	case ReviewBranch:
		if r.Branch == "" {
			return nil, errors.New("a review against a branch names the branch")
		}
		return map[string]any{"type": r.Target, "branch": r.Branch}, nil
	case ReviewCommit:
		if r.SHA == "" {
			return nil, errors.New("a review of a commit names the commit")
		}
		return map[string]any{"type": r.Target, "sha": r.SHA, "title": nullable(r.Title)}, nil
	case ReviewCustom:
		if strings.TrimSpace(r.Instructions) == "" {
			return nil, errors.New("a review of the person's own says what to look at")
		}
		return map[string]any{"type": r.Target, "instructions": r.Instructions}, nil
	}
	return nil, fmt.Errorf("codex reviews %s, %s, %s or %s, not %q", ReviewUncommitted, ReviewBranch, ReviewCommit,
		ReviewCustom, r.Target)
}

// Compact compacts the context of a thread now, as /compact does in codex's
// terminal: the daemon summarises the conversation so far and goes on from
// the summary.
func (l *Link) Compact(ctx context.Context, threadID string) error {
	c, err := l.client()
	if err != nil {
		return err
	}
	return within(ctx, c, "thread/compact/start", map[string]any{"threadId": threadID}, nil)
}

// StartReview starts a review in the thread, inline: a turn of its own whose
// result is the findings. A review is a turn, so a thread that runs one is not
// free for another, and the panel follows it as it follows a turn it started.
func (l *Link) StartReview(ctx context.Context, threadID string, r Review) error {
	target, err := r.wire()
	if err != nil {
		return err
	}
	c, err := l.client()
	if err != nil {
		return err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	free, err := l.free(ctx, c, threadID)
	if err != nil {
		return err
	}
	if !free {
		return errors.New("a turn runs in the thread, and a review is a turn of its own: start it once the thread is free")
	}
	params := map[string]any{"threadId": threadID, "target": target, "delivery": "inline"}
	if err := within(ctx, c, "review/start", params, nil); err != nil {
		return err
	}
	l.set(threadID, func(t *thread) { t.ours, t.subscribed = true, true })
	return nil
}

// Rename names a thread, as /rename does in codex's terminal. The name is
// what the panel shows for the session; the session keeps the name the panel
// addresses it by.
func (l *Link) Rename(ctx context.Context, threadID, name string) error {
	c, err := l.client()
	if err != nil {
		return err
	}
	if err := within(ctx, c, "thread/name/set", map[string]any{"threadId": threadID, "name": name}, nil); err != nil {
		return err
	}
	l.set(threadID, func(t *thread) { t.info.Name = &name })
	l.save(threadID)
	return nil
}

// SetGoal gives a thread a goal, or changes how its goal stands: an objective
// with its budget of tokens, or a status alone — paused, or active again. The
// change is the person's, and says so: the daemon takes a change of unknown
// origin as no authorisation of a person. An active goal starts a turn at
// once, and the next ones after it, until the goal is met, paused or out of
// its budget; pausing does not stop a turn already running.
func (l *Link) SetGoal(ctx context.Context, threadID, objective, status string, budget *int64) (Goal, error) {
	c, err := l.client()
	if err != nil {
		return Goal{}, err
	}
	params := map[string]any{"threadId": threadID, "origin": "user"}
	if objective != "" {
		params["objective"] = objective
	}
	if status != "" {
		params["status"] = status
	}
	if budget != nil {
		params["tokenBudget"] = *budget
	}
	var out struct {
		Goal Goal `json:"goal"`
	}
	if err := within(ctx, c, "thread/goal/set", params, &out); err != nil {
		return Goal{}, err
	}
	l.onGoal(threadID, &out.Goal)
	return out.Goal, nil
}

// ClearGoal takes the goal of a thread away; false when it had none.
func (l *Link) ClearGoal(ctx context.Context, threadID string) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	var out struct {
		Cleared bool `json:"cleared"`
	}
	if err := within(ctx, c, "thread/goal/clear", map[string]any{"threadId": threadID, "origin": "user"}, &out); err != nil {
		return false, err
	}
	l.onGoal(threadID, nil)
	return out.Cleared, nil
}

// Processes lists the background terminals of a thread.
//
// thread/backgroundTerminals/* are experimental: the daemon updates itself, and
// a release may drop or change them without a word. Their refusal is passed on
// with what it is.
func (l *Link) Processes(ctx context.Context, threadID string) ([]Process, error) {
	c, err := l.client()
	if err != nil {
		return nil, err
	}
	out, err := processes(ctx, c, threadID)
	if err != nil {
		return nil, experimental(err)
	}
	n := len(out)
	l.set(threadID, func(t *thread) { t.processes = &n })
	l.save(threadID)
	return out, nil
}

func processes(ctx context.Context, c *conn, threadID string) ([]Process, error) {
	out := []Process{}
	cursor := ""
	for range 20 {
		params := map[string]any{"threadId": threadID}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []Process `json:"data"`
			Next string    `json:"nextCursor"`
		}
		if err := within(ctx, c, "thread/backgroundTerminals/list", params, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Data...)
		if page.Next == "" || page.Next == cursor {
			break
		}
		cursor = page.Next
	}
	return out, nil
}

// Terminate stops one background terminal of a thread; false when it was not
// running any more. Experimental, as Processes.
func (l *Link) Terminate(ctx context.Context, threadID, processID string) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	var out struct {
		Terminated bool `json:"terminated"`
	}
	params := map[string]any{"threadId": threadID, "processId": processID}
	if err := within(ctx, c, "thread/backgroundTerminals/terminate", params, &out); err != nil {
		return false, experimental(err)
	}
	l.nudge()
	return out.Terminated, nil
}

// Clean stops every background terminal of a thread, as /stop does in codex's
// terminal. Experimental, as Processes.
func (l *Link) Clean(ctx context.Context, threadID string) error {
	c, err := l.client()
	if err != nil {
		return err
	}
	if err := within(ctx, c, "thread/backgroundTerminals/clean", map[string]any{"threadId": threadID}, nil); err != nil {
		return experimental(err)
	}
	zero := 0
	l.set(threadID, func(t *thread) { t.processes = &zero })
	l.save(threadID)
	return nil
}

// unknownMethod says the daemon refused a call as a method it does not know.
func unknownMethod(err error) bool {
	return refused(err) && slices.ContainsFunc([]string{"unknown variant", "method not found"},
		func(s string) bool { return strings.Contains(err.Error(), s) })
}

// experimental says what a refusal of an experimental method is: most likely
// a daemon that updated itself past it.
func experimental(err error) error {
	if refused(err) {
		return fmt.Errorf("the codex daemon refused it — the method is experimental, and the daemon may have "+
			"updated itself past it: %w", err)
	}
	return err
}

// McpServer is an MCP server of a thread as codex reports it: how it stands,
// whether it is signed in, where it lives and what it offers. Only what is
// shown comes along: codex already leaves the credentials, the path and the
// query out of the address.
type McpServer struct {
	Name        string
	Status      string
	Auth        string
	Origin      string
	Plugin      string
	Title       string
	Version     string
	Description string
	Error       string
	Tools       []string
}

// McpServers lists the MCP servers of a thread, with the thread's own
// connections to them.
func (l *Link) McpServers(ctx context.Context, threadID string) ([]McpServer, error) {
	c, err := l.client()
	if err != nil {
		return nil, err
	}
	out := []McpServer{}
	cursor := ""
	for range 20 {
		// The full detail reads every resource of every server, seconds of
		// work; tools and the sign-in are what the panel shows.
		params := map[string]any{"threadId": threadID, "detail": "toolsAndAuthOnly"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				Name    string                     `json:"name"`
				Status  string                     `json:"runtimeStatus"`
				Auth    string                     `json:"authStatus"`
				Origin  string                     `json:"httpOrigin"`
				Plugin  string                     `json:"pluginId"`
				Tools   map[string]json.RawMessage `json:"tools"`
				Failure string                     `json:"toolsError"`
				Info    *struct {
					Name        string `json:"name"`
					Title       string `json:"title"`
					Version     string `json:"version"`
					Description string `json:"description"`
				} `json:"serverInfo"`
			} `json:"data"`
			Next string `json:"nextCursor"`
		}
		if err := within(ctx, c, "mcpServerStatus/list", params, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Data {
			s := McpServer{Name: m.Name, Status: m.Status, Auth: m.Auth, Origin: m.Origin, Plugin: m.Plugin,
				Error: m.Failure, Tools: []string{}}
			for name := range m.Tools {
				s.Tools = append(s.Tools, name)
			}
			sort.Strings(s.Tools)
			if m.Info != nil {
				s.Title, s.Version, s.Description = m.Info.Title, m.Info.Version, m.Info.Description
				if s.Title == "" {
					s.Title = m.Info.Name
				}
			}
			out = append(out, s)
		}
		if page.Next == "" || page.Next == cursor {
			break
		}
		cursor = page.Next
	}
	return out, nil
}

// Skill is a skill codex offers a thread: its name and what it is for, where
// it comes from — the user, the repository, codex itself, an administrator,
// or a plugin — and whether it is on.
type Skill struct {
	Name        string
	Description string
	Scope       string
	Plugin      string
	Enabled     bool
}

// Skills lists the skills codex offers in the directory of a thread.
func (l *Link) Skills(ctx context.Context, threadID string) ([]Skill, error) {
	c, err := l.client()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	cwd := ""
	if t := l.threads[threadID]; t != nil {
		cwd = t.info.CWD
	}
	l.mu.Unlock()
	if cwd == "" {
		return nil, errors.New("codex does not say the directory of the thread, and its skills are those of a directory")
	}
	var out struct {
		Data []struct {
			CWD    string `json:"cwd"`
			Skills []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Short       string `json:"shortDescription"`
				Scope       string `json:"scope"`
				Plugin      string `json:"pluginId"`
				Enabled     bool   `json:"enabled"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := within(ctx, c, "skills/list", map[string]any{"cwds": []string{cwd}}, &out); err != nil {
		return nil, err
	}
	list := []Skill{}
	for _, entry := range out.Data {
		for _, s := range entry.Skills {
			about := s.Description
			if about == "" {
				about = s.Short
			}
			list = append(list, Skill{Name: s.Name, Description: about, Scope: s.Scope, Plugin: s.Plugin,
				Enabled: s.Enabled})
		}
	}
	return list, nil
}

// onGoal takes the goal of a thread as the daemon told it; nil is no goal.
func (l *Link) onGoal(threadID string, g *Goal) {
	l.set(threadID, func(t *thread) { t.goal, t.goalKnown = g, true })
	l.save(threadID)
}

// extras reads what thread/read does not say of a thread: its goal and its
// background terminals. They are read when the thread runs, has changed since
// the last read or has terminals running, and a while after the last read
// otherwise. A daemon that refuses either is not asked it again on this
// connection.
func (l *Link) extras(ctx context.Context, c *conn, id string) {
	l.mu.Lock()
	t := l.threads[id]
	if t == nil {
		l.mu.Unlock()
		return
	}
	due := t.info.Status.Type == "active" || t.info.UpdatedAt != t.extrasSeen ||
		(t.processes != nil && *t.processes > 0) || time.Since(t.extrasAt) >= extrasEvery
	noGoals, noProcesses := l.noGoals, l.noProcesses
	updated := t.info.UpdatedAt
	l.mu.Unlock()
	if !due {
		return
	}
	if !noGoals {
		var out struct {
			Goal *Goal `json:"goal"`
		}
		err := within(ctx, c, "thread/goal/get", map[string]any{"threadId": id}, &out)
		switch {
		case err == nil:
			l.set(id, func(t *thread) { t.goal, t.goalKnown = out.Goal, true })
		case unknownMethod(err):
			l.mu.Lock()
			l.noGoals = true
			l.mu.Unlock()
		}
	}
	if !noProcesses {
		list, err := processes(ctx, c, id)
		switch {
		case err == nil:
			n := len(list)
			l.set(id, func(t *thread) { t.processes = &n })
		case unknownMethod(err):
			l.mu.Lock()
			l.noProcesses = true
			l.mu.Unlock()
		}
	}
	l.set(id, func(t *thread) { t.extrasAt, t.extrasSeen = time.Now(), updated })
}
