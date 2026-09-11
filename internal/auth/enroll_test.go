package auth

import (
	"strings"
	"testing"
)

func TestVerifyCode(t *testing.T) {
	good, err := hashCode("abc-def")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyCode(good, "abc-def") {
		t.Fatal("a code did not pass the check against its own hash")
	}
	if verifyCode(good, "abc-deg") {
		t.Error("a different code was accepted")
	}

	parts := strings.Split(good, ":")
	if len(parts) != 4 {
		t.Fatalf("the hash format is not the one this test knows: %q", good)
	}

	broken := map[string]string{
		"empty string":                "",
		"no fields":                   "pbkdf2-sha256",
		"too few fields":              strings.Join(parts[:3], ":"),
		"too many fields":             good + ":extra",
		"another algorithm":           "scrypt:" + strings.Join(parts[1:], ":"),
		"iterations not a number":     "pbkdf2-sha256:many:" + parts[2] + ":" + parts[3],
		"zero iterations":             "pbkdf2-sha256:0:" + parts[2] + ":" + parts[3],
		"negative iterations":         "pbkdf2-sha256:-1:" + parts[2] + ":" + parts[3],
		"salt is not base64":          "pbkdf2-sha256:" + parts[1] + ":not-base64!:" + parts[3],
		"hash is not base64":          "pbkdf2-sha256:" + parts[1] + ":" + parts[2] + ":not-base64!",
		"a different iteration count": "pbkdf2-sha256:1:" + parts[2] + ":" + parts[3],
		"a different salt":            "pbkdf2-sha256:" + parts[1] + ":" + parts[3] + ":" + parts[3],
		"empty hash":                  "pbkdf2-sha256:" + parts[1] + ":" + parts[2] + ":",
	}
	for name, hash := range broken {
		t.Run(name, func(t *testing.T) {
			if verifyCode(hash, "abc-def") {
				t.Errorf("a broken hash was accepted: %q", hash)
			}
		})
	}
}

func TestHashCodeIsSalted(t *testing.T) {
	a, err := hashCode("the-very-same-one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashCode("the-very-same-one")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two hashes of one code came out equal — the salt does nothing")
	}
	if !verifyCode(a, "the-very-same-one") || !verifyCode(b, "the-very-same-one") {
		t.Error("a code does not pass against its own hash")
	}
}
