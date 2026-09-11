package action

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReachNamesWhyTheSocketRefused(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root opens any socket — a refusal by permissions cannot be reproduced")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := NewClient(filepath.Join(dir, "missing", "sock"), time.Second).Reach(ctx)
	if err == nil || !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "there is no executor socket") {
		t.Errorf("missing socket: %v", err)
	}

	dead := filepath.Join(dir, "dead")
	if err := os.WriteFile(dead, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err = NewClient(dead, time.Second).Reach(ctx)
	if err == nil || !strings.Contains(err.Error(), "nobody listens on it") {
		t.Errorf("socket with nobody listening: %v", err)
	}

	closed := filepath.Join(dir, "closed")
	if err := os.Mkdir(closed, 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(closed, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.Chmod(closed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(closed, 0o700) })
	err = NewClient(filepath.Join(closed, "sock"), time.Second).Reach(ctx)
	if err == nil || !strings.Contains(err.Error(), "uid "+strconv.Itoa(os.Getuid())) ||
		!strings.Contains(err.Error(), "AACP_UID") {
		t.Errorf("a socket without permissions must name the service uid and the .env variable: %v", err)
	}

	alive, err := net.Listen("unix", filepath.Join(dir, "alive"))
	if err != nil {
		t.Fatal(err)
	}
	defer alive.Close()
	go func() {
		for {
			c, err := alive.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if err := NewClient(filepath.Join(dir, "alive"), time.Second).Reach(ctx); err != nil {
		t.Errorf("a live socket is reported unavailable: %v", err)
	}
}
