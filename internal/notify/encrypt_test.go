package notify

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"testing"
)

func b64d(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return raw
}

func TestEncryptMatchesRFC8291Vector(t *testing.T) {
	const (
		plaintext = "When I grow up, I want to be a watermelon"
		asPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
		uaPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
		auth      = "BTBZMqHH6r4Tts7J_aSIgg"
		salt      = "DGv6ra1nlYgDCS1FRnbzlw"
		want      = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLoc" +
			"InmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	)

	ephemeral, err := ecdh.P256().NewPrivateKey(b64d(t, asPrivate))
	if err != nil {
		t.Fatalf("the sender key: %v", err)
	}
	sub := Subscription{
		Endpoint: "https://push.example.net/x",
		P256dh:   b64d(t, uaPublic),
		Auth:     b64d(t, auth),
	}

	got, err := encryptWith([]byte(plaintext), sub, ephemeral, b64d(t, salt))
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	if enc := base64.RawURLEncoding.EncodeToString(got); enc != want {
		t.Fatalf("the ciphertext diverged from the RFC 8291 vector\ngot: %s\nexpected: %s", enc, want)
	}
}

func TestEncryptHeaderLayout(t *testing.T) {
	sub, _ := testSubscription(t)
	out, err := encrypt([]byte("hello"), sub)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	if len(out) < headerLen+tagLen {
		t.Fatalf("%d bytes came out, less than the header", len(out))
	}
	if rs := binary.BigEndian.Uint32(out[saltLen : saltLen+4]); rs != recordSize {
		t.Fatalf("the record size in the header is %d, expected %d", rs, recordSize)
	}
	if idlen := out[saltLen+4]; idlen != publicKeyLen {
		t.Fatalf("the key length in the header is %d, expected %d", idlen, publicKeyLen)
	}
	if _, err := ecdh.P256().NewPublicKey(out[saltLen+5 : headerLen]); err != nil {
		t.Fatalf("the key in the header does not parse: %v", err)
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	sub, uaPrivate := testSubscription(t)
	message := []byte(`{"title":"shop is stopped","body":"5 containers"}`)

	out, err := encrypt(message, sub)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	got, err := decryptAsDevice(t, out, uaPrivate, sub.Auth)
	if err != nil {
		t.Fatalf("the device could not decrypt: %v", err)
	}
	if !bytes.Equal(got, message) {
		t.Fatalf("decrypted %q, sent %q", got, message)
	}
}

func TestEncryptRejectsBadSubscription(t *testing.T) {
	good, _ := testSubscription(t)

	cases := map[string]Subscription{
		"without an endpoint":     {P256dh: good.P256dh, Auth: good.Auth},
		"not https":               {Endpoint: "http://push.example.net/x", P256dh: good.P256dh, Auth: good.Auth},
		"a short key":             {Endpoint: good.Endpoint, P256dh: good.P256dh[:10], Auth: good.Auth},
		"a short auth":            {Endpoint: good.Endpoint, P256dh: good.P256dh, Auth: good.Auth[:4]},
		"someone else's 65 bytes": {Endpoint: good.Endpoint, P256dh: bytes.Repeat([]byte{9}, publicKeyLen), Auth: good.Auth},
		"an empty subscription":   {},
	}
	for name, sub := range cases {
		if _, err := encrypt([]byte("x"), sub); err == nil {
			t.Errorf("%s: the subscription is accepted while it must not be", name)
		}
	}

	if _, err := encrypt(bytes.Repeat([]byte("a"), maxPayload+1), good); err == nil {
		t.Error("a body that is too long is accepted")
	}
	if _, err := encrypt(bytes.Repeat([]byte("a"), maxPayload), good); err != nil {
		t.Errorf("a body at the length limit is rejected: %v", err)
	}
}

func testSubscription(t *testing.T) (Subscription, *ecdh.PrivateKey) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("the device key: %v", err)
	}
	auth := make([]byte, authLen)
	for i := range auth {
		auth[i] = byte(i + 1)
	}
	return Subscription{
		Endpoint: "https://push.example.net/x",
		P256dh:   priv.PublicKey().Bytes(),
		Auth:     auth,
	}, priv
}

func decryptAsDevice(t *testing.T, body []byte, uaPrivate *ecdh.PrivateKey, auth []byte) ([]byte, error) {
	t.Helper()
	salt := body[:saltLen]
	asPublicRaw := body[saltLen+5 : headerLen]
	ciphertext := body[headerLen:]

	asPublic, err := ecdh.P256().NewPublicKey(asPublicRaw)
	if err != nil {
		return nil, err
	}
	shared, err := uaPrivate.ECDH(asPublic)
	if err != nil {
		return nil, err
	}

	keyInfo := append(append(append([]byte{}, keyInfoPrefix...), uaPrivate.PublicKey().Bytes()...), asPublicRaw...)
	prkKey, err := hkdf.Extract(sha256.New, shared, auth)
	if err != nil {
		return nil, err
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, string(keyInfo), sha256.Size)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, cekInfo, keyLen)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, nonceInfo, nonceLen)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plain[:len(plain)-1], nil
}
