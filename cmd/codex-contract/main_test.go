package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCodex is a codex that says its version and writes, as its schema, the
// file it is given: the reference, or the reference with a field taken away.
func fakeCodex(t *testing.T, bundle []byte) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(src, bundle, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "codex-cli 9.9.9"; exit 0; fi
[ "$1 $2 $3" = "app-server generate-json-schema --experimental" ] || { echo "unexpected: $*" >&2; exit 3; }
[ "$4" = "--out" ] || exit 3
cp "` + src + `" "$5/codex_app_server_protocol.schemas.json"
`
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

func referenceBundle(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "codex", "contract", "reference.json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTheReleaseOfTheReferenceHoldsAndExitsZero(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if code := run(fakeCodex(t, referenceBundle(t)), "", true, false); code != 0 {
		t.Fatalf("the release of the reference checked against it exits %d", code)
	}
}

func TestAReleaseWithoutAFieldThePanelReadsExitsOne(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	gone := strings.Replace(string(referenceBundle(t)), `"parentThreadId"`, `"parentThread"`, 1)
	if gone == string(referenceBundle(t)) {
		t.Fatal("the reference names no parentThreadId to take away")
	}
	bin := fakeCodex(t, []byte(gone))
	if code := run(bin, "", true, false); code != 1 {
		t.Fatalf("a release without a field the panel reads exits %d", code)
	}
	// Nor is it written as the reference.
	if code := run(bin, "", true, true); code != 1 {
		t.Fatalf("a release without a field the panel reads is written as the reference: %d", code)
	}
}

func TestNoCodexExitsTwo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if code := run(filepath.Join(t.TempDir(), "codex"), "", true, false); code != 2 {
		t.Fatalf("a check with no codex to ask exits %d", code)
	}
	if code := run("", t.TempDir(), true, false); code != 2 {
		t.Fatalf("a directory with no schema in it exits %d", code)
	}
}
