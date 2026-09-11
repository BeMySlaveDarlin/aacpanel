package notify

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"
)

const vapidTTL = 12 * time.Hour

var b64 = base64.RawURLEncoding

// Keys is the VAPID pair of the service.
type Keys struct {
	private    *ecdsa.PrivateKey
	Public     []byte
	PrivateDER []byte
}

// NewKeys generates a pair.
func NewKeys() (*Keys, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("the VAPID key: %w", err)
	}
	return keysFrom(priv)
}

// LoadKeys reads a pair from PKCS#8.
func LoadKeys(der []byte) (*Keys, error) {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("parsing the VAPID key: %w", err)
	}
	priv, ok := key.(*ecdsa.PrivateKey)
	if !ok || priv.Curve != elliptic.P256() {
		return nil, errors.New("the VAPID key is not P-256")
	}
	return keysFrom(priv)
}

func keysFrom(priv *ecdsa.PrivateKey) (*Keys, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("serializing the VAPID key: %w", err)
	}
	pub, err := priv.PublicKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("the public VAPID key: %w", err)
	}
	return &Keys{private: priv, Public: pub.Bytes(), PrivateDER: der}, nil
}

// PublicBase64 returns the key for pushManager.subscribe().
func (k *Keys) PublicBase64() string { return b64.EncodeToString(k.Public) }

func (k *Keys) authorization(endpoint, contact string, now time.Time) (string, error) {
	audience, err := origin(endpoint)
	if err != nil {
		return "", err
	}
	token, err := k.token(audience, contact, now)
	if err != nil {
		return "", err
	}
	return "vapid t=" + token + ", k=" + k.PublicBase64(), nil
}

func (k *Keys) token(audience, contact string, now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"aud": audience,
		"exp": now.Add(vapidTTL).Unix(),
		"sub": contact,
	})
	if err != nil {
		return "", err
	}

	signing := b64.EncodeToString(header) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))

	r, s, err := ecdsa.Sign(rand.Reader, k.private, digest[:])
	if err != nil {
		return "", fmt.Errorf("signing the token: %w", err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])

	return signing + "." + b64.EncodeToString(sig), nil
}

func origin(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("the subscription address: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("the subscription address %q: https is expected", endpoint)
	}
	return u.Scheme + "://" + u.Host, nil
}
