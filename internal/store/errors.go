package store

import (
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
)

// Unavailable wraps a pgx connection failure into ErrUnavailable.
func Unavailable(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrClosed) {
		return err
	}

	var connErr *pgconn.ConnectError
	var netErr net.Error
	switch {
	case errors.As(err, &connErr), errors.As(err, &netErr),
		errors.Is(err, net.ErrClosed), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return err
}
