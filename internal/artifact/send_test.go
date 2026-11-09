package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionfile"
)

func put(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func send(t *testing.T, cwd string, files ...string) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"files": files, "caption": "The finished report"})
	if err != nil {
		t.Fatal(err)
	}
	return Tool().Call(context.Background(), func() (mcp.Binding, error) {
		return mcp.Binding{Place: mcp.Place{Dir: cwd}}, nil
	}, raw)
}

func receipt(t *testing.T, cwd string, paths ...string) []attachment {
	t.Helper()
	text, failed := send(t, cwd, paths...)
	if failed {
		t.Fatalf("the delivery failed: %s", text)
	}
	var result struct {
		Caption     string       `json:"caption"`
		Attachments []attachment `json:"attachments"`
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || len(result.Attachments) == 0 {
		t.Fatalf("the receipt is not an attachments object: %q, %v", text, err)
	}
	return result.Attachments
}

func TestTheReceiptKeepsATrimmedCaptionOnlyWhenPresent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := put(t, filepath.Join(t.TempDir(), "report.pdf"), "%PDF-1.4\n")
	for _, input := range []map[string]any{{}, {"caption": ""}, {"caption": " \t\n"},
		{"caption": " \t The finished report \n"}} {
		input["files"] = []string{path}
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		text, failed := Tool().Call(t.Context(), nil, raw)
		if failed {
			t.Fatalf("the delivery failed: %s", text)
		}
		var result struct {
			Caption     *string      `json:"caption"`
			Attachments []attachment `json:"attachments"`
		}
		if err := json.Unmarshal([]byte(text), &result); err != nil || len(result.Attachments) != 1 {
			t.Fatalf("the receipt is not a file delivery: %s, %v", text, err)
		}
		want, _ := input["caption"].(string)
		want = strings.TrimSpace(want)
		if (result.Caption != nil) != (want != "") || result.Caption != nil && *result.Caption != want {
			t.Errorf("caption of %v: %s", input, text)
		}
	}
}

func TestFilesResolveFromTheSessionAndKeepTheirNamedPaths(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	pdf := put(t, filepath.Join(cwd, "report.pdf"), "%PDF-1.4\n")
	txt := put(t, filepath.Join(home, "notes.txt"), "notes\n")
	for _, tc := range []struct {
		name, input, dir, want, media string
	}{
		{"relative", "./out/../report.pdf", cwd, pdf, "application/pdf"},
		{"absolute without a session directory", pdf, "", pdf, "application/pdf"},
		{"home", "~/notes.txt", "", txt, "text/plain; charset=utf-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := receipt(t, tc.dir, tc.input)
			info, err := os.Stat(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 || files[0] != (attachment{Path: tc.want, Size: info.Size(), MediaType: tc.media}) {
				t.Errorf("receipt: %+v", files)
			}
		})
	}
	text, failed := Tool().Call(t.Context(), func() (mcp.Binding, error) {
		return mcp.Binding{}, errors.New("no session")
	}, json.RawMessage(`{"files":["report.pdf"]}`))
	if !failed || !strings.Contains(text, "give an absolute path") {
		t.Fatalf("a relative path without a session answered %q, error %v", text, failed)
	}
	_, failed = Tool().Call(t.Context(), nil, json.RawMessage(`{"files":[`+strconv.Quote(pdf)+`]}`))
	if failed {
		t.Fatal("an absolute path requires no binding")
	}
}

func TestLinksToFilesAreSentOnceAndUnknownTypesAreSniffed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cwd := t.TempDir()
	target := put(t, filepath.Join(cwd, "report.unknown-extension"), "%PDF-1.4\n")
	link := filepath.Join(cwd, "link.unknown-extension")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(cwd, "another.unknown-extension")
	if err := os.Link(target, hardlink); err != nil {
		t.Fatal(err)
	}
	files := receipt(t, cwd, link, target, "./link.unknown-extension", hardlink)
	if len(files) != 1 || files[0].Path != link || files[0].MediaType != "application/pdf" {
		t.Fatalf("links or repeated paths made different files: %+v", files)
	}
}

func TestEveryBadPathRefusesTheWholeDelivery(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cwd := t.TempDir()
	good := put(t, filepath.Join(cwd, "good.txt"), "ready")
	fifo := filepath.Join(cwd, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	bad := []string{filepath.Join(cwd, "missing"), cwd, fifo, ""}
	text, failed := send(t, cwd, append([]string{good}, bad...)...)
	if !failed || strings.Contains(text, `"attachments"`) {
		t.Fatalf("a partial delivery answered %q, error %v", text, failed)
	}
	for _, name := range bad {
		if !strings.Contains(text, strconv.Quote(name)+": ") {
			t.Errorf("the refusal does not name %q: %s", name, text)
		}
	}
	if !strings.Contains(text, "not a regular file") || !strings.Contains(text, "no such file") {
		t.Fatalf("the refusal does not explain the bad paths: %s", text)
	}
}

func TestSecretsAndLinksIntoTheirDirectoryAreRefused(t *testing.T) {
	for _, xdg := range []bool{false, true} {
		t.Run(map[bool]string{false: "default home", true: "XDG state home"}[xdg], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", "")
			if xdg {
				t.Setenv("XDG_STATE_HOME", t.TempDir())
			}
			secret := put(t, filepath.Join(sessionfile.SecretsDir(), "token.txt"), "private")
			link := filepath.Join(t.TempDir(), "notes.txt")
			if err := os.Symlink(secret, link); err != nil {
				t.Fatal(err)
			}
			text, failed := send(t, "", secret, link)
			if !failed || strings.Count(text, "secrets directory") != 2 {
				t.Fatalf("secrets were not refused: %q, error %v", text, failed)
			}
			beside := put(t, filepath.Join(sessionfile.SecretsDir()+"-reports", "public.txt"), "ready")
			receipt(t, "", beside)
			stateLink := filepath.Join(t.TempDir(), "state")
			if err := os.Symlink(filepath.Dir(sessionfile.SecretsDir()), stateLink); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_STATE_HOME", filepath.Dir(stateLink))
			// Point the configured secrets directory through a link too: the
			// actual file is still private when both sides have different names.
			if err := os.Symlink(stateLink, filepath.Join(filepath.Dir(stateLink), "aacpanel")); err != nil {
				t.Fatal(err)
			}
			text, failed = send(t, "", secret)
			if !failed || !strings.Contains(text, "secrets directory") {
				t.Fatalf("a linked secrets directory answered %q, error %v", text, failed)
			}
		})
	}
}

func TestFileListsHaveBoundsAndRejectUnknownArguments(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, raw := range []string{`{}`, `{"files":[]}`, `{"files":null}`, `{"files":[1]}`,
		`{"files":[""]}`, `{"files":["a"],"other":true}`, `{"files":["a"],"caption":42}`} {
		if text, failed := Tool().Call(t.Context(), nil, json.RawMessage(raw)); !failed {
			t.Errorf("bad input %s was accepted: %s", raw, text)
		}
	}
	file := put(t, filepath.Join(t.TempDir(), "empty.txt"), "")
	paths := make([]string, 20)
	for i := range paths {
		paths[i] = file
	}
	files := receipt(t, "", paths...)
	if len(files) != 1 || files[0].Size != 0 {
		t.Fatalf("the limit or the empty file was not kept: %+v", files)
	}
	if text, failed := send(t, "", append(paths, file)...); !failed || !strings.Contains(text, "1–20") {
		t.Fatalf("too many paths answered %q, error %v", text, failed)
	}
}
