// Package usage collects claude transcript usage through the host agent.
package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	dialTimeout = 5 * time.Second
	idleTimeout = 5 * time.Minute
	maxLine     = 64 << 20
)

// ErrNoAgent means collection is not configured: there is no socket path.
var ErrNoAgent = errors.New("usage collection is not configured: the path to the agent socket is empty")

// Client is the link to the collection socket of the agent.
type Client struct {
	Path string
}

func New(path string) *Client { return &Client{Path: path} }

// Available reports whether there is anyone to talk to.
func (c *Client) Available() bool { return c != nil && c.Path != "" }

// File is what the agent knows about a transcript on disk.
type File struct {
	Path    string `json:"path"`
	Contour string `json:"contour"`
	Inode   int64  `json:"inode"`
	Size    int64  `json:"size"`
	Head    string `json:"head"`
	First   string `json:"first"`
}

// Task says what to do with one file: read on from a point, or reread it whole.
type Task struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
}

// Row is one hour of usage as the parser returns it.
type Row struct {
	Bucket        string `json:"bucket"`
	Model         string `json:"model"`
	Agent         string `json:"agent"`
	Speed         string `json:"speed"`
	ServiceTier   string `json:"serviceTier"`
	AgentKind     string `json:"agentKind"`
	Answers       int    `json:"answers"`
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
	CacheRead     int64  `json:"cacheRead"`
	CacheCreation int64  `json:"cacheCreation"`
	Cache1h       int64  `json:"cache1h"`
	Cache5m       int64  `json:"cache5m"`
	Iterations    int    `json:"iterations"`
	Thinking      int    `json:"thinking"`
	LatencySumMS  int64  `json:"latencyMsSum"`
	LatencyMaxMS  int    `json:"latencyMsMax"`
}

// Tool is the calls of one tool within an hour.
type Tool struct {
	Bucket string `json:"bucket"`
	Agent  string `json:"agent"`
	Tool   string `json:"tool"`
	Calls  int    `json:"calls"`
	Errors int    `json:"errors"`
}

// Event is one hour of session events.
type Event struct {
	Bucket         string `json:"bucket"`
	Agent          string `json:"agent"`
	Messages       int    `json:"messages"`
	Compacts       int    `json:"compacts"`
	Interrupts     int    `json:"interrupts"`
	InterruptsTool int    `json:"interruptsTool"`
	APIErrors      int    `json:"apiErrors"`
	Idle           Idle   `json:"idle"`
}

// Idle is the human pauses laid out into buckets.
type Idle struct {
	Under30s int   `json:"under30s"`
	Under2m  int   `json:"under2m"`
	Under10m int   `json:"under10m"`
	Under1h  int   `json:"under1h"`
	Under4h  int   `json:"under4h"`
	Over4h   int   `json:"over4h"`
	MaxMS    int64 `json:"maxMs"`
}

// Session is the conversation reference as this file saw it. Checkout is the
// main checkout of the git worktree the session ran in, empty anywhere else.
type Session struct {
	SessionID string   `json:"sessionId"`
	Contour   string   `json:"contour"`
	CWD       string   `json:"cwd"`
	Checkout  string   `json:"checkout"`
	GitBranch string   `json:"gitBranch"`
	Version   string   `json:"version"`
	StartedAt string   `json:"startedAt"`
	EndedAt   string   `json:"endedAt"`
	Agents    []string `json:"agents"`
}

// Result is what came out of one file.
type Result struct {
	Path    string  `json:"path"`
	Error   string  `json:"error,omitempty"`
	Session Session `json:"session"`
	Rows    []Row   `json:"rows"`
	Events  []Event `json:"events"`
	Tools   []Tool  `json:"tools"`
	Offset  int64   `json:"offset"`
	Since   string  `json:"since"`
	Rewind  bool    `json:"rewind"`
}

// Pong reports whether collection is alive and with how many workers.
type Pong struct {
	OK      bool `json:"pong"`
	Workers int  `json:"workers"`
}

type line struct {
	End    bool    `json:"end"`
	Error  string  `json:"error"`
	File   *File   `json:"file"`
	Result *Result `json:"result"`
	Pong   *bool   `json:"pong"`
	Worker int     `json:"workers"`
}

// Ping reports whether the agent answers and with how many parser processes.
func (c *Client) Ping(ctx context.Context) (Pong, error) {
	var out Pong
	err := c.stream(ctx, map[string]any{"op": "ping"}, func(l line) error {
		if l.Pong != nil {
			out = Pong{OK: *l.Pong, Workers: l.Worker}
		}
		return nil
	})
	return out, err
}

// List returns all transcripts of all contours with what is known from disk.
func (c *Client) List(ctx context.Context) ([]File, error) {
	var out []File
	err := c.stream(ctx, map[string]any{"op": "list"}, func(l line) error {
		if l.File != nil {
			out = append(out, *l.File)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Parse runs a parsing task and calls the handler for each file.
func (c *Client) Parse(ctx context.Context, tasks []Task, fn func(Result) error) error {
	if len(tasks) == 0 {
		return nil
	}
	return c.stream(ctx, map[string]any{"op": "parse", "files": tasks}, func(l line) error {
		if l.Result == nil {
			return nil
		}
		return fn(*l.Result)
	})
}

func (c *Client) stream(ctx context.Context, req any, onLine func(line) error) error {
	if !c.Available() {
		return ErrNoAgent
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	var d net.Dialer
	dialCtx, cancelDial := context.WithTimeout(ctx, dialTimeout)
	defer cancelDial()
	conn, err := d.DialContext(dialCtx, "unix", c.Path)
	if err != nil {
		return fmt.Errorf("usage collection is unavailable (%s): %w", c.Path, err)
	}
	defer conn.Close()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	if err := conn.SetWriteDeadline(time.Now().Add(dialTimeout)); err != nil {
		return err
	}
	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("the scan job was not sent: %w", err)
	}
	if half, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = half.CloseWrite()
	}

	reader := bufio.NewReaderSize(conn, 64<<10)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return err
		}
		raw, err := readLine(reader)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if errors.Is(err, io.EOF) {
				return errors.New("usage collection broke off: the agent closed the stream without saying it had finished")
			}
			return fmt.Errorf("usage collection broke off: %w", err)
		}
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			return fmt.Errorf("the agent reply cannot be parsed: %w", err)
		}
		if l.Error != "" {
			return fmt.Errorf("the agent refused: %s", l.Error)
		}
		if l.End {
			return nil
		}
		if err := onLine(l); err != nil {
			return err
		}
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		chunk, more, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if len(out) > maxLine {
			return nil, errors.New("the reply line is longer than the cap")
		}
		if !more {
			return out, nil
		}
	}
}
