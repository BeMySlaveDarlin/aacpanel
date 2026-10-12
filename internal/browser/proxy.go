package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Egress resolves once and dials the checked address itself. Checking a name
// and leaving its second resolution to Chrome would allow DNS rebinding.
type Egress struct {
	lookup     func(context.Context, string, string) ([]netip.Addr, error)
	dial       func(context.Context, string, string) (net.Conn, error)
	interfaces func() ([]net.Addr, error)
	transport  *http.Transport
	mu         sync.Mutex
	tunnels    map[net.Conn]struct{}
	closed     bool
}

var privateNetworks = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001::/32"),
}

func publicAddress(ip netip.Addr, own []net.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" {
		return false
	}
	for _, p := range privateNetworks {
		if p.Contains(ip) {
			return false
		}
	}
	for _, a := range own {
		host, _, ok := strings.Cut(a.String(), "/")
		if !ok {
			host = a.String()
		}
		if p, err := netip.ParseAddr(host); err == nil && p.Unmap() == ip {
			return false
		}
	}
	return true
}

// NewEgress owns a transport whose connections use the checked IP directly.
// It never inherits the host proxy environment or delegates DNS to Chrome.
func NewEgress() *Egress {
	e := &Egress{
		lookup:     net.DefaultResolver.LookupNetIP,
		dial:       (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		interfaces: net.InterfaceAddrs,
		tunnels:    make(map[net.Conn]struct{}),
	}
	e.transport = &http.Transport{
		DialContext:           e.connect,
		Proxy:                 nil,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
	}
	return e
}

func (e *Egress) connect(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := e.lookup(ctx, "ip", host)
	if err != nil {
		return nil, errors.New("the browser destination did not resolve")
	}
	own, err := e.interfaces()
	if err != nil {
		return nil, errors.New("the host addresses could not be checked")
	}
	if len(ips) == 0 {
		return nil, errors.New("the browser destination has no address")
	}
	// Reject the entire answer: choosing only its public half would hide a
	// hostname that also names this machine or an internal service.
	for _, ip := range ips {
		if !publicAddress(ip, own) {
			return nil, errors.New("the browser reaches public internet addresses only")
		}
	}
	var last error
	for _, ip := range ips {
		conn, err := e.dial(ctx, "tcp", net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

// ServeHTTP forwards plain HTTP or tunnels CONNECT after the same address
// check. HTTPS stays encrypted between Chrome and its destination.
func (e *Egress) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		e.tunnel(w, r)
		return
	}
	if r.URL.Scheme != "http" || r.URL.Host == "" {
		http.Error(w, "the browser proxy takes HTTP or CONNECT", http.StatusBadRequest)
		return
	}
	proxy := httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.Header.Del("Proxy-Authorization")
			req.Header.Del("Proxy-Connection")
		},
		Transport: e.transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "the browser destination is unavailable or not public", http.StatusForbidden)
		},
	}
	proxy.ServeHTTP(w, r)
}

func (e *Egress) tunnel(w http.ResponseWriter, r *http.Request) {
	conn, err := e.connect(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, "the browser destination is unavailable or not public", http.StatusForbidden)
		return
	}
	defer conn.Close()
	h, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "CONNECT is unavailable", http.StatusInternalServerError)
		return
	}
	peer, buffer, err := h.Hijack()
	if err != nil {
		return
	}
	defer peer.Close()
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.tunnels[peer] = struct{}{}
	e.tunnels[conn] = struct{}{}
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.tunnels, peer)
		delete(e.tunnels, conn)
		e.mu.Unlock()
	}()
	if _, err = fmt.Fprint(buffer, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffer.Flush(); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		io.Copy(conn, buffer)
		conn.Close()
		close(done)
	}()
	io.Copy(peer, conn)
	peer.Close()
	<-done
}

// Close releases idle connections and both ends of every hijacked tunnel.
// Closing the HTTP server alone would leave CONNECT connections alive.
func (e *Egress) Close() {
	e.transport.CloseIdleConnections()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	for c := range e.tunnels {
		c.Close()
	}
}
