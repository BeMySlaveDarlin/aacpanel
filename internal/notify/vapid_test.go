package notify

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestTokenIsVerifiableES256(t *testing.T) {
	keys, err := NewKeys()
	if err != nil {
		t.Fatalf("the keys: %v", err)
	}
	now := time.Now()

	header, err := keys.authorization("https://fcm.googleapis.com/fcm/send/abc123", "mailto:owner@example.net", now)
	if err != nil {
		t.Fatalf("the header: %v", err)
	}

	token, key, ok := strings.Cut(header, ", k=")
	if !ok || !strings.HasPrefix(token, "vapid t=") {
		t.Fatalf("the header is not built per RFC 8292: %q", header)
	}
	if key != keys.PublicBase64() {
		t.Fatalf("the header carries someone else's public key")
	}

	parts := strings.Split(strings.TrimPrefix(token, "vapid t="), ".")
	if len(parts) != 3 {
		t.Fatalf("%d parts in the token, three expected", len(parts))
	}

	var head struct{ Typ, Alg string }
	if err := json.Unmarshal(b64d(t, parts[0]), &head); err != nil {
		t.Fatalf("the token header: %v", err)
	}
	if head.Alg != "ES256" || head.Typ != "JWT" {
		t.Fatalf("the token header: %+v", head)
	}

	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(b64d(t, parts[1]), &claims); err != nil {
		t.Fatalf("the token fields: %v", err)
	}
	if claims.Aud != "https://fcm.googleapis.com" {
		t.Fatalf("aud: %q", claims.Aud)
	}
	if claims.Sub != "mailto:owner@example.net" {
		t.Fatalf("sub: %q", claims.Sub)
	}
	if got := time.Unix(claims.Exp, 0); got.After(now.Add(24*time.Hour)) || !got.After(now) {
		t.Fatalf("exp %s is not within a day of %s", got, now)
	}

	sig := b64d(t, parts[2])
	if len(sig) != 64 {
		t.Fatalf("the signature is %d bytes, 64 expected", len(sig))
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	pub, err := ecdh.P256().NewPublicKey(keys.Public)
	if err != nil {
		t.Fatalf("the public key: %v", err)
	}
	x, y := new(big.Int).SetBytes(pub.Bytes()[1:33]), new(big.Int).SetBytes(pub.Bytes()[33:])
	verifier := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(verifier, digest[:], r, s) {
		t.Fatal("the token signature does not verify against the public key")
	}
}

func TestKeysSurviveReload(t *testing.T) {
	keys, err := NewKeys()
	if err != nil {
		t.Fatalf("the keys: %v", err)
	}
	same, err := LoadKeys(keys.PrivateDER)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if same.PublicBase64() != keys.PublicBase64() {
		t.Fatal("the public key differs after loading")
	}

	if _, err := LoadKeys([]byte("not a key")); err == nil {
		t.Error("garbage is taken for a VAPID key")
	}
}

func TestOriginRejectsBadEndpoints(t *testing.T) {
	for _, endpoint := range []string{"", "http://push.example.net/x", "ftp://a/b", "not an address", "https://"} {
		if _, err := origin(endpoint); err == nil {
			t.Errorf("address %q is accepted", endpoint)
		}
	}
	got, err := origin("https://push.example.net:8443/path?x=1")
	if err != nil || got != "https://push.example.net:8443" {
		t.Fatalf("origin = %q, %v", got, err)
	}
}
