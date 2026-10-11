package rules

import (
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"time"
)

const certificateSource = "tls.remaining_days"

// Certificate reads the leaf the LAN listener actually serves. A failed reload
// must keep watching the old leaf, which is also what the listener keeps using.
// Nil means this install has no LAN TLS listener.
type Certificate func() (*x509.Certificate, error)

func (e *Engine) certificate(rule Rule, now time.Time) ([]measurement, error) {
	if e.Certificate == nil {
		return nil, nil
	}
	leaf, err := e.Certificate()
	if err != nil || leaf == nil {
		return nil, err
	}
	name := leaf.Subject.CommonName
	if len(leaf.DNSNames) > 0 {
		name = leaf.DNSNames[0]
	}
	days := leaf.NotAfter.Sub(now).Hours() / 24
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(leaf.Raw))
	// The identity includes the certificate: a replacement can expire in turn,
	// and a service restart or an acknowledgement cannot reopen the old one.
	subject := fmt.Sprintf("%s · %s", name, fingerprint[:16])
	if rule.Target != nil && *rule.Target != "" && *rule.Target != subject {
		return nil, nil
	}
	return []measurement{{subject: subject, subjectName: name, last: days, held: rule.Match(days), recovered: !rule.Match(days)}}, nil
}
