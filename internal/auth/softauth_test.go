package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"
)

const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttested     = 0x40
)

type softKey struct {
	priv   *ecdsa.PrivateKey
	credID []byte
	aaguid []byte
	handle []byte
	count  uint32
}

func newSoftKey(t *testing.T) *softKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatalf("credential id: %v", err)
	}
	return &softKey{priv: priv, credID: id, aaguid: make([]byte, 16)}
}

func clientData(ceremonyType, challenge, origin string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"type":        ceremonyType,
		"challenge":   challenge,
		"origin":      origin,
		"crossOrigin": false,
	})
	return raw
}

func (k *softKey) authData(rpID string, flags byte, count uint32, attested bool) []byte {
	sum := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, sum[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, count)
	if attested {
		out = append(out, k.aaguid...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(k.credID)))
		out = append(out, k.credID...)
		out = append(out, k.coseKey()...)
	}
	return out
}

func (k *softKey) coseKey() []byte {
	x := make([]byte, 32)
	y := make([]byte, 32)
	k.priv.PublicKey.X.FillBytes(x)
	k.priv.PublicKey.Y.FillBytes(y)

	out := []byte{0xa5}
	out = append(out, cborUint(1)...)
	out = append(out, cborUint(2)...)
	out = append(out, cborUint(3)...)
	out = append(out, cborNegInt(7)...)
	out = append(out, cborNegInt(1)...)
	out = append(out, cborUint(1)...)
	out = append(out, cborNegInt(2)...)
	out = append(out, cborBytes(x)...)
	out = append(out, cborNegInt(3)...)
	out = append(out, cborBytes(y)...)
	return out
}

func (k *softKey) register(t *testing.T, rpID, challenge, origin string) json.RawMessage {
	t.Helper()
	k.count++
	cd := clientData("webauthn.create", challenge, origin)
	ad := k.authData(rpID, flagUserPresent|flagUserVerified|flagAttested, k.count, true)

	att := []byte{0xa3}
	att = append(att, cborText("fmt")...)
	att = append(att, cborText("none")...)
	att = append(att, cborText("attStmt")...)
	att = append(att, 0xa0)
	att = append(att, cborText("authData")...)
	att = append(att, cborBytes(ad)...)

	raw, err := json.Marshal(map[string]any{
		"id":                      b64url(k.credID),
		"rawId":                   b64url(k.credID),
		"type":                    "public-key",
		"authenticatorAttachment": "platform",
		"response": map[string]any{
			"clientDataJSON":    b64url(cd),
			"attestationObject": b64url(att),
			"transports":        []string{"internal"},
		},
	})
	if err != nil {
		t.Fatalf("registration response: %v", err)
	}
	return raw
}

func (k *softKey) login(t *testing.T, rpID, challenge, origin string, userHandle []byte, count uint32) []byte {
	t.Helper()
	cd := clientData("webauthn.get", challenge, origin)
	ad := k.authData(rpID, flagUserPresent|flagUserVerified, count, false)

	cdHash := sha256.Sum256(cd)
	signed := append(append([]byte{}, ad...), cdHash[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatalf("signature: %v", err)
	}

	raw, err := json.Marshal(map[string]any{
		"id":                      b64url(k.credID),
		"rawId":                   b64url(k.credID),
		"type":                    "public-key",
		"authenticatorAttachment": "platform",
		"response": map[string]any{
			"clientDataJSON":    b64url(cd),
			"authenticatorData": b64url(ad),
			"signature":         b64url(sig),
			"userHandle":        b64url(userHandle),
		},
	})
	if err != nil {
		t.Fatalf("login response: %v", err)
	}
	return raw
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 1<<8:
		return []byte{major<<5 | 24, byte(n)}
	case n < 1<<16:
		return append([]byte{major<<5 | 25}, byte(n>>8), byte(n))
	default:
		return append([]byte{major<<5 | 26}, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
}

func cborUint(n uint64) []byte { return cborHead(0, n) }

func cborNegInt(n uint64) []byte { return cborHead(1, n-1) }

func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }

func cborText(s string) []byte { return append(cborHead(3, uint64(len(s))), s...) }
