package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aacpanel/internal/host"
	"aacpanel/internal/notify"
	"aacpanel/internal/store"
)

func TestLANExpiryTracksTheCertificateActuallyServed(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "panel.crt"), filepath.Join(dir, "panel.key")
	write := func(days int) {
		t.Helper()
		pub, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(int64(days)), Subject: pkix.Name{CommonName: "panel.example.org"}, DNSNames: []string{"panel.example.org"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Duration(days) * 24 * time.Hour)}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
		if err != nil {
			t.Fatal(err)
		}
		priv, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		for path, raw := range map[string][]byte{certPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPath: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv})} {
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(13)
	s := &Server{}
	if _, err := s.lanServer(":0", certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	check := func(days int) {
		t.Helper()
		got, err := s.lanCertificate()
		if err != nil || got.Name != "panel.example.org" || got.DaysLeft < float64(days)-0.01 || got.DaysLeft > float64(days) {
			t.Fatalf("expiry: %+v, %v; want %d days", got, err, days)
		}
	}
	check(13)
	if err := os.WriteFile(certPath, []byte("broken replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.lanCert.Load().checked = time.Time{}
	check(13)
	write(60)
	s.lanCert.Load().checked = time.Time{}
	s.lanCert.Load().stamp = time.Time{}
	check(60)
	state := filepath.Join(dir, "state.json")
	if err := os.WriteFile(state, []byte(`{"host":{"cpuPct":0},"sessions":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.host = host.NewReader(state)
	raw, err := s.hostSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Certificate *lanCertificate `json:"lanCertificate"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || got.Certificate == nil || got.Certificate.ExpiresAt == 0 {
		t.Fatalf("host reply loses expiry: %s %v", raw, err)
	}
}

func TestCertificateAlertUsesTheRulePushChoice(t *testing.T) {
	value := 13.0
	a := alertEvent(store.Alert{ID: 12, RuleID: 7, RuleKey: "tls.expiring", Rule: "LAN certificate expires soon", Subject: "panel.example.org", Severity: "warning", Value: &value, Payload: json.RawMessage(`{"unit":"days","threshold":14,"op":"<=","forSec":10}`)})
	r := notify.Look(notify.World{}, notify.World{Alerts: []notify.Alert{a}})
	if len(r.Raise) != 1 {
		t.Fatalf("certificate alert produced %+v", r)
	}
	tracker := notify.NewTracker(nil)
	tracker.Step(context.Background(), notify.Report{})
	messages := tracker.Step(context.Background(), r)
	if len(messages) != 1 {
		t.Fatalf("push: %+v", messages)
	}
	if (notify.Prefs{Rules: []int64{7}}).Allows(messages[0]) || !(notify.Prefs{}).Allows(messages[0]) {
		t.Fatal("certificate push ignores the rule choice")
	}
	if again := tracker.Step(context.Background(), r); len(again) != 0 {
		t.Fatalf("poll repeats the push: %+v", again)
	}
	if r.Raise[0].Source.Rule != 7 {
		t.Fatalf("push lost its rule: %+v", r.Raise[0])
	}
}
