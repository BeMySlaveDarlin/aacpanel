package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestUnavailable(t *testing.T) {
	s, err := New("postgres://postgres:test@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	connErr := s.connect(context.Background(), true, false)
	if connErr == nil {
		t.Fatal("connecting to a closed port succeeded")
	}
	if got := Unavailable(connErr); !errors.Is(got, ErrUnavailable) {
		t.Errorf("a connection error was not recognised as unavailability: %v", got)
	}

	if got := Unavailable(ErrClosed); !errors.Is(got, ErrClosed) {
		t.Error("ErrClosed was substituted")
	}
	if got := Unavailable(nil); got != nil {
		t.Error("nil turned into an error")
	}
	plain := fmt.Errorf("a syntax error in the query")
	if got := Unavailable(plain); errors.Is(got, ErrUnavailable) {
		t.Error("an ordinary error was passed off as unavailability — the 500 will turn into a 503 and the incident will be lost")
	}
}
