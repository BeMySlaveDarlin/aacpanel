package hook

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// copyCases are the cases the page copy shares with the delivery's
// artifact-copy.py, which sends the same bytes on the same payload.
type copyCases struct {
	Cases []struct {
		What       string  `json:"what"`
		Input      string  `json:"input"`
		Name       string  `json:"name"`
		File       *string `json:"file"`
		FileBase64 string  `json:"fileBase64"`
		Fill       []any   `json:"fill"`
		Sends      *string `json:"sends"`
		Copied     bool    `json:"copied"`
	} `json:"cases"`
}

func TestTheSharedCasesOfThePageCopy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "artifact-copy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c copyCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	col := listenCollector(t, "AACP_PAGE_SOCKET", false)
	for _, cs := range c.Cases {
		name := cs.Name
		if name == "" {
			name = "page.html"
		}
		page := filepath.Join(t.TempDir(), name)
		var body []byte
		switch {
		case cs.Fill != nil:
			body = []byte(strings.Repeat(cs.Fill[0].(string), int(cs.Fill[1].(float64))))
		case cs.FileBase64 != "":
			if body, err = base64.StdEncoding.DecodeString(cs.FileBase64); err != nil {
				t.Fatal(err)
			}
		case cs.File != nil:
			body = []byte(*cs.File)
		}
		if body != nil {
			if err := os.WriteFile(page, body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		var out, errs bytes.Buffer
		code := Run([]string{"artifact-copy"}, strings.NewReader(strings.ReplaceAll(cs.Input, "{page}", page)), &out, &errs)
		if code != 0 || out.Len() > 0 || errs.Len() > 0 {
			t.Errorf("%s: exit %d, answer %q, stderr %q", cs.What, code, out.String(), errs.String())
		}
		got := col.sent()
		switch {
		case cs.Copied:
			var msg map[string]string
			if err := json.Unmarshal(got, &msg); err != nil || msg["html"] != string(body) || msg["path"] != page {
				t.Errorf("%s: sent %.200s (%v)", cs.What, got, err)
			}
		case cs.Sends == nil && got != nil:
			t.Errorf("%s: sent %.300s", cs.What, got)
		case cs.Sends != nil && string(got) != strings.ReplaceAll(*cs.Sends, "{page}", page):
			t.Errorf("%s: sent\n%s\nwant\n%s", cs.What, got, *cs.Sends)
		}
	}
}

// A publish leaves no word and holds nothing whatever stands where the
// collector should, and the hook is done within its steps.
func TestThePageCopyNeverHoldsThePublish(t *testing.T) {
	was := copyStep
	copyStep = 100 * time.Millisecond
	t.Cleanup(func() { copyStep = was })
	page := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(page, []byte("<p>hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	publish := `{"tool_name":"Artifact","session_id":"s","tool_input":{"file_path":"` + page + `"}}`
	for what, place := range map[string]func(t *testing.T){
		"no collector at all":         func(t *testing.T) { t.Setenv("AACP_PAGE_SOCKET", filepath.Join(t.TempDir(), "none.sock")) },
		"a collector that never says": func(t *testing.T) { listenCollector(t, "AACP_PAGE_SOCKET", true) },
	} {
		t.Run(what, func(t *testing.T) {
			place(t)
			began := time.Now()
			var out, errs bytes.Buffer
			if code := Run([]string{"artifact-copy"}, strings.NewReader(publish), &out, &errs); code != 0 || out.Len() > 0 {
				t.Errorf("exit %d, answer %q", code, out.String())
			}
			if took := time.Since(began); took > 3*copyStep {
				t.Errorf("the copy took %v", took)
			}
		})
	}
}
