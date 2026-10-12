// Package cdp owns Chrome's debugging pipe; no debugging port is opened.
package cdp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Message is an answer or event on a flattened CDP session.
type Message struct {
	ID        int             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Error     *Error          `json:"error,omitempty"`
}

// Error preserves a CDP refusal separately from a transport failure.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error returns the browser explanation without exposing the request payload.
func (e *Error) Error() string { return e.Message }

// MaxMessage bounds the parser, including base64 images arriving from Chrome.
const MaxMessage = 16 << 20

// Pipe multiplexes calls and events. The event callback must not wait for a
// reply: the reader delivering it is also the reader delivering replies.
type Pipe struct {
	to      io.WriteCloser
	from    io.ReadCloser
	write   sync.Mutex
	mu      sync.Mutex
	seq     int
	waiting map[int]chan Message
	dead    error
	done    chan struct{}
	event   func(Message)
}

// New starts a single reader that routes replies and delivers unsolicited events.
func New(to io.WriteCloser, from io.ReadCloser, event func(Message)) *Pipe {
	p := &Pipe{
		to:      to,
		from:    from,
		waiting: make(map[int]chan Message),
		done:    make(chan struct{}),
		event:   event,
	}
	go p.read(from)
	return p
}

// Start connects descriptors 3 and 4 before Chrome runs. The caller owns Wait
// and process termination; a holder's unit owns the entire process tree.
func Start(cmd *exec.Cmd, event func(Message)) (*Pipe, error) {
	cr, to, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	from, cw, err := os.Pipe()
	if err != nil {
		cr.Close()
		to.Close()
		return nil, err
	}
	cmd.ExtraFiles = []*os.File{cr, cw}
	err = cmd.Start()
	cr.Close()
	cw.Close()
	if err != nil {
		to.Close()
		from.Close()
		return nil, err
	}
	return New(to, from, event), nil
}

// Done closes when the process pipe fails or its owner closes it.
func (p *Pipe) Done() <-chan struct{} { return p.done }

// Err reports the terminal pipe failure after Done closes.
func (p *Pipe) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dead
}

// Close wakes pending calls as well as blocked reads and writes.
func (p *Pipe) Close() error {
	p.fail(io.EOF)
	return nil
}

func (p *Pipe) fail(err error) {
	p.mu.Lock()
	if p.dead != nil {
		p.mu.Unlock()
		return
	}
	p.dead = err
	close(p.done)
	for id, ch := range p.waiting {
		close(ch)
		delete(p.waiting, id)
	}
	p.mu.Unlock()
	p.to.Close()
	p.from.Close()
}

func (p *Pipe) read(from io.ReadCloser) {
	defer from.Close()
	in := bufio.NewScanner(from)
	in.Buffer(make([]byte, 64<<10), MaxMessage)
	in.Split(func(data []byte, eof bool) (int, []byte, error) {
		for i, b := range data {
			if b == 0 {
				return i + 1, data[:i], nil
			}
		}
		if eof && len(data) > 0 {
			return 0, nil, io.ErrUnexpectedEOF
		}
		return 0, nil, nil
	})
	for in.Scan() {
		var m Message
		if err := json.Unmarshal(in.Bytes(), &m); err != nil {
			p.fail(err)
			return
		}
		if m.ID != 0 {
			p.mu.Lock()
			ch := p.waiting[m.ID]
			delete(p.waiting, m.ID)
			p.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		} else if p.event != nil {
			p.event(m)
		}
	}
	err := in.Err()
	if err == nil {
		err = io.EOF
	}
	p.fail(err)
}

func (p *Pipe) send(ctx context.Context, session, method string, params interface{}, wait bool) (int, chan Message, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return 0, nil, err
	}
	p.mu.Lock()
	if p.dead != nil {
		err = p.dead
		p.mu.Unlock()
		return 0, nil, err
	}
	p.seq++
	id := p.seq
	var ch chan Message
	if wait {
		ch = make(chan Message, 1)
		p.waiting[id] = ch
	}
	p.mu.Unlock()
	line, err := json.Marshal(Message{ID: id, SessionID: session, Method: method, Params: raw})
	if err != nil {
		return 0, nil, err
	}
	p.write.Lock()
	// A Chrome that stops reading cannot hold the holder's shutdown inside a
	// write, beyond the call's cancellation or the bounded control timeout.
	if w, ok := p.to.(interface{ SetWriteDeadline(time.Time) error }); ok {
		deadline := time.Now().Add(2 * time.Second)
		if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
			deadline = end
		}
		w.SetWriteDeadline(deadline)
	}
	_, err = p.to.Write(append(line, 0))
	p.write.Unlock()
	if err != nil {
		p.fail(err)
	}
	return id, ch, err
}

// Send is for acknowledgements whose replies carry no data.
func (p *Pipe) Send(session, method string, params interface{}) error {
	_, _, err := p.send(context.Background(), session, method, params, false)
	return err
}

// Call correlates one reply within a flattened session. Cancellation removes
// the waiter, so a late response cannot block delivery of another call.
func (p *Pipe) Call(ctx context.Context, session, method string, params interface{}, out interface{}) error {
	id, ch, err := p.send(ctx, session, method, params, true)
	if err != nil {
		return err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return p.Err()
		}
		if m.Error != nil {
			return fmt.Errorf("%s: %w", method, m.Error)
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.waiting, id)
		p.mu.Unlock()
		return fmt.Errorf("%s: %w", method, ctx.Err())
	}
}
