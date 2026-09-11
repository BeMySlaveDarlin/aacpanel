package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

type certReloader struct {
	certPath, keyPath string

	mu      sync.Mutex
	cert    *tls.Certificate
	stamp   time.Time
	checked time.Time
}

func newCertReloader(certPath, keyPath string) (*certReloader, error) {
	c := &certReloader{certPath: certPath, keyPath: keyPath}
	if _, err := c.get(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *certReloader) get() (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.cert != nil && now.Sub(c.checked) < time.Minute {
		return c.cert, nil
	}
	c.checked = now
	stamp, err := newestStamp(c.certPath, c.keyPath)
	if err != nil {
		if c.cert != nil {
			return c.cert, nil
		}
		return nil, err
	}
	if c.cert != nil && !stamp.After(c.stamp) {
		return c.cert, nil
	}
	cert, err := tls.LoadX509KeyPair(c.certPath, c.keyPath)
	if err != nil {
		if c.cert != nil {
			return c.cert, nil
		}
		return nil, fmt.Errorf("the local network certificate: %w", err)
	}
	c.cert, c.stamp = &cert, stamp
	return c.cert, nil
}

func newestStamp(paths ...string) (time.Time, error) {
	var newest time.Time
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return time.Time{}, err
		}
		if st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	return newest, nil
}

func (s *Server) lanServer(addr, certPath, keyPath string) (*http.Server, error) {
	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("the local network listener %s has no certificate: AACP_TLS_CERT and AACP_TLS_KEY are needed", addr)
	}
	reloader, err := newCertReloader(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:    addr,
		Handler: logRequests(s.handler(s.lanGate())),
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				return reloader.get()
			},
		},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}, nil
}

func (s *Server) tsServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           logRequests(s.handler(s.tsGate())),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
