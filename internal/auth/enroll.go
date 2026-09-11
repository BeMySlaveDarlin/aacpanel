package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrNoCode means the code is wrong, expired or already used.
var ErrNoCode = errors.New("no such code")

const (
	// EnrollTTL is how long an enrollment code lives.
	EnrollTTL    = 5 * time.Minute
	codeLen      = 10
	codeGroup    = 5
	codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	enrollIter   = 100_000
)

// EnrollCode is an active enrollment code as the check sees it.
type EnrollCode struct {
	ID   int64
	Hash string
}

// EnrollStore is the storage of enrollment codes.
type EnrollStore interface {
	AddCode(ctx context.Context, hash, source string, expires time.Time) error
	ActiveCodes(ctx context.Context) ([]EnrollCode, error)
	UseCode(ctx context.Context, id int64) (bool, error)
}

// OwnerStore returns the user handle of the owner.
type OwnerStore interface {
	UserHandle(ctx context.Context) ([]byte, error)
}

// NewEnrollCode issues an enrollment code and stores its hash.
func NewEnrollCode(ctx context.Context, store EnrollStore, source string) (string, time.Time, error) {
	code, err := generateCode()
	if err != nil {
		return "", time.Time{}, err
	}
	hash, err := hashCode(code)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(EnrollTTL)
	if err := store.AddCode(ctx, hash, source, expires); err != nil {
		return "", time.Time{}, err
	}
	return format(code), expires, nil
}

// MatchEnrollCode finds an entered code among the valid ones and returns its number.
func MatchEnrollCode(ctx context.Context, store EnrollStore, code string) (int64, error) {
	code = normalizeCode(code)
	if len(code) != codeLen {
		return 0, ErrNoCode
	}
	list, err := store.ActiveCodes(ctx)
	if err != nil {
		return 0, err
	}
	for _, c := range list {
		if verifyCode(c.Hash, code) {
			return c.ID, nil
		}
	}
	return 0, ErrNoCode
}

func generateCode() (string, error) {
	raw := make([]byte, codeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, codeLen)
	for i, b := range raw {
		out[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(out), nil
}

func format(code string) string {
	var b strings.Builder
	for i, c := range code {
		if i > 0 && i%codeGroup == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func normalizeCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		switch r {
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func hashCode(code string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, code, salt, enrollIter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256:%d:%s:%s", enrollIter, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

func verifyCode(hash, code string) bool {
	parts := strings.Split(hash, ":")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	salt, err := b64.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := b64.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, code, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
