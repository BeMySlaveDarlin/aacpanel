package notify

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

const (
	recordSize   = 4096
	keyLen       = 16
	nonceLen     = 12
	saltLen      = 16
	tagLen       = 16
	publicKeyLen = 65
	authLen      = 16
	headerLen    = saltLen + 4 + 1 + publicKeyLen
	maxPayload   = recordSize - headerLen - 1 - tagLen
)

var (
	keyInfoPrefix = []byte("WebPush: info\x00")
	cekInfo       = "Content-Encoding: aes128gcm\x00"
	nonceInfo     = "Content-Encoding: nonce\x00"
)

func encrypt(payload []byte, sub Subscription) ([]byte, error) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("the ephemeral key: %w", err)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("the salt: %w", err)
	}
	return encryptWith(payload, sub, ephemeral, salt)
}

func encryptWith(payload []byte, sub Subscription, ephemeral *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if err := sub.Validate(); err != nil {
		return nil, err
	}
	if len(payload) > maxPayload {
		return nil, fmt.Errorf("the push body is %d bytes, only %d fit", len(payload), maxPayload)
	}
	if len(salt) != saltLen {
		return nil, fmt.Errorf("the salt is %d bytes long, %d expected", len(salt), saltLen)
	}

	uaPublic, err := ecdh.P256().NewPublicKey(sub.P256dh)
	if err != nil {
		return nil, fmt.Errorf("the device key: %w", err)
	}
	shared, err := ephemeral.ECDH(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("the key agreement: %w", err)
	}

	asPublic := ephemeral.PublicKey().Bytes()
	keyInfo := make([]byte, 0, len(keyInfoPrefix)+2*publicKeyLen)
	keyInfo = append(keyInfo, keyInfoPrefix...)
	keyInfo = append(keyInfo, sub.P256dh...)
	keyInfo = append(keyInfo, asPublic...)

	prkKey, err := hkdf.Extract(sha256.New, shared, sub.Auth)
	if err != nil {
		return nil, fmt.Errorf("extracting the key: %w", err)
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, string(keyInfo), sha256.Size)
	if err != nil {
		return nil, fmt.Errorf("the key material: %w", err)
	}

	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, fmt.Errorf("extracting the record key: %w", err)
	}
	cek, err := hkdf.Expand(sha256.New, prk, cekInfo, keyLen)
	if err != nil {
		return nil, fmt.Errorf("the content key: %w", err)
	}
	nonce, err := hkdf.Expand(sha256.New, prk, nonceInfo, nonceLen)
	if err != nil {
		return nil, fmt.Errorf("the nonce: %w", err)
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext := make([]byte, 0, len(payload)+1)
	plaintext = append(plaintext, payload...)
	plaintext = append(plaintext, 0x02)

	out := make([]byte, 0, headerLen+len(plaintext)+tagLen)
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	return gcm.Seal(out, nonce, plaintext, nil), nil
}
