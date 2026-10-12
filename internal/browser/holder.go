package browser

import (
	"context"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"time"

	"aacpanel/internal/browserlink"
)

// Grace gives a viewer time to reconnect before its idle Chrome is stopped.
const Grace = 30 * time.Second

// Hold owns Chrome outside the executor's unit. Its socket is private to the
// host: only the executor's checked bridge is mounted into the service.
func Hold(ctx context.Context, c Config, id string) error { return hold(ctx, c, id, Grace) }

func hold(ctx context.Context, c Config, id string, grace time.Duration) error {
	if err := c.Check(); err != nil {
		return err
	}
	path := Socket(c, id)
	ln, err := browserlink.Listen(path)
	if err != nil {
		return err
	}
	defer ln.Close()
	defer os.Remove(path)
	b, err := startChrome(ctx, c, id)
	if err != nil {
		return err
	}
	defer b.close()
	var active atomic.Bool
	idle := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	context.AfterFunc(ctx, func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				l := browserlink.New(conn)
				defer l.Close()
				l.ReadDeadline(time.Now().Add(5 * time.Second))
				first, err := l.Read()
				if err != nil {
					return
				}
				if first.Type != "attach" ||
					first.Version != browserlink.Protocol ||
					first.Profile != id ||
					first.Size == nil ||
					first.Size.Check() != nil {
					l.Write(browserlink.Frame{Type: "end", Error: "invalid browser attachment"})
					return
				}
				if !active.CompareAndSwap(false, true) {
					l.Write(browserlink.Frame{Type: "end", Error: "this profile already has a controller"})
					return
				}
				defer func() {
					active.Store(false)
					select {
					case idle <- struct{}{}:
					default:
					}
				}()
				if err := serveViewer(ctx, b, l, *first.Size); err != nil && !errors.Is(err, net.ErrClosed) {
					l.Write(browserlink.Frame{Type: "end", Error: err.Error()})
				}
			}(conn)
		}
	}()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-b.failure:
			return err
		case <-b.pipe.Done():
			return b.pipe.Err()
		case <-idle:
			timer.Reset(grace)
		case <-timer.C:
			if !active.Load() {
				return nil
			}
			timer.Reset(grace)
		}
	}
}

// serveViewer separates Chrome acknowledgements from delivery over the WAN.
// One outstanding pull holds delivery, while Chrome replaces the latest slot;
// the ticker sends that final slot even after the page stops producing frames.
func serveViewer(ctx context.Context, b *chrome, l *browserlink.Link, size browserlink.Size) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	if err := b.cast(ctx, size); err != nil {
		return err
	}
	defer b.stopCast(context.Background())
	if err := l.Write(browserlink.Frame{Type: "ready", Version: browserlink.Protocol}); err != nil {
		return err
	}
	in := make(chan browserlink.Frame)
	readErr := make(chan error, 1)
	go func() {
		for {
			l.ReadDeadline(time.Now().Add(45 * time.Second))
			f, err := l.Read()
			if err != nil {
				readErr <- err
				return
			}
			select {
			case in <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	frameTick := time.NewTicker(time.Second / browserlink.FramesPerSecond)
	defer frameTick.Stop()
	stateTick := time.NewTicker(time.Second)
	defer stateTick.Stop()
	state := func() error {
		st, err := b.state(ctx)
		if err != nil {
			return err
		}
		return l.Write(browserlink.Frame{Type: "state", State: &st})
	}
	if err := state(); err != nil {
		return err
	}
	var sequence, waiting uint64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case <-b.pipe.Done():
			return b.pipe.Err()
		case <-frameTick.C:
			if waiting != 0 {
				continue
			}
			s := b.take()
			if s == nil {
				continue
			}
			sequence++
			waiting = sequence
			if err := l.Write(browserlink.Frame{
				Type: "frame", Sequence: sequence, Generation: s.generation,
				Size: &s.size, Data: s.data,
			}); err != nil {
				return err
			}
		case <-stateTick.C:
			if err := state(); err != nil {
				return err
			}
		case reason := <-b.refused:
			if err := l.Write(browserlink.Frame{Type: "refused", Error: reason}); err != nil {
				return err
			}
		case <-b.changed:
			if err := state(); err != nil {
				return err
			}
		case f := <-in:
			var err error
			switch f.Type {
			case "ack":
				if f.Sequence == waiting {
					waiting = 0
				}
			case "ping":
			case "size":
				if f.Size == nil {
					err = errors.New("missing browser size")
				} else {
					err = b.cast(ctx, *f.Size)
				}
			case "input":
				if f.Input == nil {
					err = errors.New("missing browser input")
				} else {
					err = b.input(ctx, *f.Input)
				}
			case "detach":
				return nil
			default:
				return errors.New("unknown browser frame")
			}
			if err != nil {
				if err = l.Write(browserlink.Frame{Type: "refused", Error: err.Error()}); err != nil {
					return err
				}
			}
		}
	}
}
