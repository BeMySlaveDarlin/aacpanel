package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"
)

const (
	dialTimeout = 3 * time.Second
	maxResponse = 4 << 10
)

// ErrUnavailable means the executor does not answer.
var ErrUnavailable = errors.New("executor is unavailable")

// Client is the executor client.
type Client struct {
	path    string
	timeout time.Duration
}

// NewClient prepares a client for the executor socket.
func NewClient(path string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = time.Minute
	}
	return &Client{path: path, timeout: timeout}
}

// Available reports whether the executor answers.
func (c *Client) Available(ctx context.Context) bool {
	return c.Reach(ctx) == nil
}

// Reach reports whether the executor answers, and if not, why.
func (c *Client) Reach(ctx context.Context) error {
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.path)
	if err != nil {
		return explainDial(c.path, err)
	}
	conn.Close()
	return nil
}

func explainDial(path string, err error) error {
	switch {
	case errors.Is(err, syscall.EACCES), errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%w: the executor socket %s belongs to another user — the service runs under uid %d, "+
			"check AACP_UID and AACP_GID in .env", ErrUnavailable, path, os.Getuid())
	case errors.Is(err, syscall.ENOENT), errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: there is no executor socket %s — aacpanel-exec is not running, "+
			"or AACP_EXEC_DIR in .env points at the wrong directory", ErrUnavailable, path)
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("%w: the executor socket %s exists, but nobody listens on it — aacpanel-exec is stopped",
			ErrUnavailable, path)
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

// Kinds asks the executor what it can do.
func (c *Client) Kinds(ctx context.Context) ([]Kind, error) {
	resp, err := c.Do(ctx, Request{Ask: AskKinds})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("the executor did not name its actions: %s", resp.Error)
	}
	return resp.Kinds, nil
}

// Permission asks which permission prompt a session is standing on.
func (c *Client) Permission(ctx context.Context, target string) (*Permission, error) {
	resp, err := c.Do(ctx, Request{Ask: AskPermission, Target: target})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Permission, nil
}

// Window asks whether a session has a terminal window open on the host.
func (c *Client) Window(ctx context.Context, target string) (*Window, error) {
	resp, err := c.Do(ctx, Request{Ask: AskWindow, Target: target})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Window, nil
}

// Models asks what a session can be switched to.
func (c *Client) Models(ctx context.Context, target string) (*Models, error) {
	resp, err := c.Do(ctx, Request{Ask: AskModels, Target: target})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Models, nil
}

// Do sends a request and waits for the answer.
func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if err := req.Validate(); err != nil {
		return Response{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.path)
	if err != nil {
		return Response{}, explainDial(c.path, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("sending the request: %w", err)
	}
	if half, ok := conn.(*net.UnixConn); ok {
		half.CloseWrite()
	}

	var resp Response
	if err := json.NewDecoder(io.LimitReader(conn, maxResponse)).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("the answer was not parsed: %w", err)
	}
	if resp.ID != "" && resp.ID != req.ID {
		return Response{}, fmt.Errorf("an answer to another request %q instead of %q", resp.ID, req.ID)
	}
	return resp, nil
}
