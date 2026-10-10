package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
)

// readLimit bounds one message from the daemon. The default of the library is
// 32 KB, and a list of the items of a turn carries whole diffs: a message past
// the limit closes the connection, and the link is down until the next dial.
const readLimit = 64 << 20

// message is one JSON-RPC message of the app-server, as it travels: without
// the "jsonrpc" field. A request of the server carries both an id and a
// method, a notification only the method, a response only the id.
type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error is what the daemon said, up to the list of the methods it knows: a
// method it does not know is refused with every method it does, a hundred
// names that say nothing to the person who reads the refusal.
func (e *rpcError) Error() string {
	text, _, _ := strings.Cut(e.Message, ", expected one of")
	return text
}

// refused says the daemon answered a call with a refusal, as against a call
// that never got an answer: a refusal leaves everything as it was.
func refused(err error) bool {
	var e *rpcError
	return errors.As(err, &e)
}

// conn is one connection to a daemon: WebSocket over its unix socket, a
// message a frame. Raw lines of JSON on that socket are dropped by the daemon.
type conn struct {
	ws     *websocket.Conn
	handle func(message)

	next  atomic.Int64
	mu    sync.Mutex
	calls map[int64]chan message

	closed chan struct{}
	err    error
}

// dial connects to the socket of a daemon. Everything the daemon sends that
// is not an answer to a call goes to handle, from the one goroutine that
// reads: handle must not wait on a call.
func dial(ctx context.Context, socket string, handle func(message)) (*conn, error) {
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	ws, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(readLimit)
	c := &conn{ws: ws, handle: handle, calls: map[int64]chan message{}, closed: make(chan struct{})}
	go c.read()
	return c, nil
}

func (c *conn) read() {
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.fail(err)
			return
		}
		var msg message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Method != "" {
			c.handle(msg)
			continue
		}
		var id int64
		if json.Unmarshal(msg.ID, &id) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.calls[id]
		delete(c.calls, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func (c *conn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
		return
	default:
	}
	c.err = err
	close(c.closed)
}

// call sends a request and waits for its answer; out may be nil.
func (c *conn) call(ctx context.Context, method string, params, out any) error {
	id := c.next.Add(1)
	ch := make(chan message, 1)
	c.mu.Lock()
	c.calls[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.calls, id)
		c.mu.Unlock()
	}()
	if err := c.send(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return fmt.Errorf("codex refused %s: %w", method, msg.Error)
		}
		if out == nil || len(msg.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(msg.Result, out); err != nil {
			return fmt.Errorf("the answer of codex to %s was not read: %w", method, err)
		}
		return nil
	case <-c.closed:
		return c.lost()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notify sends a notification: no id, no answer.
func (c *conn) notify(ctx context.Context, method string) error {
	return c.send(ctx, map[string]any{"method": method})
}

// reply answers a request of the server with the id it came with.
func (c *conn) reply(ctx context.Context, id json.RawMessage, result any) error {
	return c.send(ctx, map[string]any{"id": id, "result": result})
}

func (c *conn) send(ctx context.Context, msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, body); err != nil {
		select {
		case <-c.closed:
			return c.lost()
		default:
		}
		return err
	}
	return nil
}

// lost is the error a call gets once the connection is gone.
func (c *conn) lost() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return fmt.Errorf("the connection to the codex daemon dropped: %w", c.err)
}

func (c *conn) down() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *conn) close() {
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
	c.fail(errors.New("closed by the panel"))
}
