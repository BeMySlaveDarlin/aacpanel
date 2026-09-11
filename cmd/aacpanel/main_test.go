package main

import "testing"

func TestSafeNext(t *testing.T) {
	for _, ok := range []string{"/", "/app", "/api/tree", "/app?tab=res", "/a/b/c"} {
		if got := safeNext(ok); got != ok {
			t.Errorf("safeNext(%q) = %q, expected the path itself", ok, got)
		}
	}

	bad := []string{
		"//evil.example",
		"//evil.example/app",
		"https://evil.example",
		"http://evil.example",
		`/\evil.example`,
		`\\evil.example`,
		`/\/evil.example`,
		"evil.example",
		"",
		"javascript:alert(1)",
		"data:text/html,<script>",
	}
	for _, b := range bad {
		if got := safeNext(b); got != "/app" {
			t.Errorf("safeNext(%q) = %q, expected %q", b, got, "/app")
		}
	}
}
