package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/webbuild"
)

// The screen decides on a copy by what the collector serves and what the
// protocol takes: the ceiling of one answer of the collector's socket, the
// pictures it serves itself, and the ceiling of a copy.
func TestPreviewRulesMatchCollectorAndProtocol(t *testing.T) {
	const toolsFile = "src/screens/chat/tools.js"
	tools := srcFiles(t)[toolsFile]
	if tools == "" {
		t.Fatalf("%s not found", toolsFile)
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	read := func(rel string) string {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	megabytes := func(src, file, name string) int {
		m := regexp.MustCompile(`(?m)^(?:const )?` + name + ` = (\d+) \* 1024 \* 1024;?$`).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("%s has no %s", file, name)
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		return n << 20
	}

	if got := megabytes(tools, toolsFile, "PREVIEW_MAX"); got != action.PreviewMax {
		t.Errorf("the screen holds a copy to %d bytes while the protocol says %d", got, action.PreviewMax)
	}
	const diskFile = "agent/chat/disk.py"
	if got, want := megabytes(tools, toolsFile, "PREVIEW_OVER"), megabytes(read(diskFile), diskFile, "MAX_MEDIA"); got != want {
		t.Errorf("the screen draws a copy past %d bytes while the collector serves a picture itself up to %d", got, want)
	}

	m := regexp.MustCompile(`const DRAWN = /(.+)/i;`).FindStringSubmatch(tools)
	if m == nil {
		t.Fatalf("%s has no DRAWN — the screen does not know which pictures the feed draws", toolsFile)
	}
	drawn := regexp.MustCompile("(?i)" + m[1])
	const uploadsFile = "agent/chat/uploads.py"
	uploads := read(uploadsFile)
	block := regexp.MustCompile(`(?s)UPLOAD_MEDIA = \{(.*?)\}`).FindStringSubmatch(uploads)
	if block == nil {
		t.Fatalf("%s has no UPLOAD_MEDIA", uploadsFile)
	}
	exts := regexp.MustCompile(`"(\.[a-z0-9]+)":`).FindAllStringSubmatch(block[1], -1)
	if len(exts) == 0 {
		t.Fatalf("%s: UPLOAD_MEDIA lists nothing", uploadsFile)
	}
	for _, ext := range exts {
		if !drawn.MatchString("IMG_0001" + ext[1]) {
			t.Errorf("the collector serves %s, and the screen draws no copy of one over its ceiling", ext[1])
		}
	}
	for _, ext := range []string{".heic", ".svg", ".tiff", ".bin"} {
		if drawn.MatchString("IMG_0001" + ext) {
			t.Errorf("the screen takes %s for a picture the collector serves itself", ext)
		}
	}
}

type filePreviewShot struct {
	Toasts []string `json:"toasts"`
	Files  []struct {
		Name    string `json:"name"`
		Whole   bool   `json:"whole"`
		Preview bool   `json:"preview"`
		JPEG    bool   `json:"jpeg"`
		Box     []int  `json:"box"`
		Weight  int    `json:"weight"`
	} `json:"files"`
	Kind string `json:"kind"`
	Sent []struct {
		Keys string `json:"keys"`
		Same bool   `json:"same"`
	} `json:"sent"`
}

// A picture the feed cannot show from the file itself — one over what the
// collector serves, or a HEIC — goes with a JPEG the phone draws of it, at
// most 1600 pixels on its long side. Any other file goes alone, and so does a
// picture this browser does not decode or whose copy comes out too heavy.
// The file itself goes as it was picked.
func TestAPictureTheFeedCannotShowGoesWithACopy(t *testing.T) {
	var got filePreviewShot
	runFixture(t, "filepreview.html", &got)

	if len(got.Toasts) != 0 {
		t.Fatalf("the files were refused: %v", got.Toasts)
	}
	want := []struct {
		name    string
		preview bool
		box     []int
		why     string
	}{
		{"IMG_20260930_101010.jpg", true, []int{1600, 667}, "a photo over 4 MB is drawn down to 1600 on its long side"},
		{"IMG_20260930_101011.jpg", true, []int{533, 1600}, "a standing photo is drawn down by its height"},
		{"shot.png", false, nil, "a picture the collector serves itself needs no copy"},
		{"IMG_0002.HEIC", true, []int{1200, 900}, "a HEIC goes with a copy whatever its size, and is not drawn up"},
		{"IMG_0003.heif", false, nil, "a HEIC this browser does not decode goes alone"},
		{"dump.bin", false, nil, "a file the feed does not draw needs no copy, whatever it holds"},
		{"IMG_20260930_101012.jpg", false, nil, "a copy over its ceiling is not sent"},
	}
	if len(got.Files) != len(want) {
		t.Fatalf("%d files were taken out of %d: %+v", len(got.Files), len(want), got.Files)
	}
	for i, w := range want {
		f := got.Files[i]
		if f.Name != w.name {
			t.Errorf("file %d is %q, expected %q", i, f.Name, w.name)
			continue
		}
		if !f.Whole {
			t.Errorf("%s: the file itself did not go as it was picked", f.Name)
		}
		if f.Preview != w.preview {
			t.Errorf("%s: a copy %v, expected %v — %s", f.Name, f.Preview, w.preview, w.why)
			continue
		}
		if !w.preview {
			continue
		}
		if !f.JPEG {
			t.Errorf("%s: the copy is not a JPEG", f.Name)
		}
		if fmt.Sprint(f.Box) != fmt.Sprint(w.box) {
			t.Errorf("%s: the copy is %v, expected %v — %s", f.Name, f.Box, w.box, w.why)
		}
		if f.Weight > 1<<20 {
			t.Errorf("%s: the copy weighs %d bytes, over the ceiling of the protocol", f.Name, f.Weight)
		}
	}

	if got.Kind != "session.file" || len(got.Sent) != len(want) {
		t.Fatalf("the files went as %q, %d of %d", got.Kind, len(got.Sent), len(want))
	}
	for i, w := range want {
		keys := "data,name"
		if w.preview {
			keys = "data,name,preview"
		}
		if got.Sent[i].Keys != keys || !got.Sent[i].Same {
			t.Errorf("%s went with %q, the same as taken %v: expected %q", w.name, got.Sent[i].Keys, got.Sent[i].Same, keys)
		}
	}
}
