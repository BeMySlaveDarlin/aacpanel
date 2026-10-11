package hook

import (
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// The page copy: a page a session publishes with the Artifact tool is handed
// to the panel's collector, which keeps a copy the panel opens without an
// account.
//
// An artifact goes out into the account the session works under. The person
// reads the panel from a phone under one account, and a contour publishes
// under another: the link on the card then opens for whoever matches and
// refuses everyone else. The page is a file on this machine at the moment it
// is published, and the hook hands that file to the collector over page.sock,
// in the collector's runtime directory. The collector keeps the copies and
// sweeps them by their age, their number and their size; the hook only sends.
//
// It answers PostToolUse on the Artifact tool and says nothing, sent or not:
// a hook that prints on every publish turns into noise the session learns to
// ignore. A panel that is down, or one not installed here, leaves the publish
// exactly as it was. The delivery's artifact-copy.py sends the same bytes on
// the same payload; the two share their cases in
// deploy/claude/testdata/artifact-copy.json.

const copyTool = "Artifact"

// maxPage is the largest page copied. Anything larger is left to its link:
// the copy is for reading on a phone, and the shelf refuses it anyway.
const maxPage = 2 << 20

// copyStep is how long each step of the exchange with the collector may
// take, as the script gives its socket; copyWait bounds the whole of it
// inside the kit's timeout.
var copyStep = 5 * time.Second

const copyWait = 9 * time.Second

// pageSocket is the collector's socket for pages.
func pageSocket() string {
	if path := os.Getenv("AACP_PAGE_SOCKET"); path != "" {
		return path
	}
	return "/run/aacpanel-agent/page.sock"
}

func copyPage(_ string, payload []byte, _ time.Time) Reply {
	v, ok := pyLoad(payload)
	if !ok {
		return Reply{}
	}
	page := pageOf(v)
	if page == nil || page.get("session") == "" {
		return Reply{}
	}
	_ = handOver(pageSocket(), pyDumps(page), copyStep, copyWait, true)
	return Reply{}
}

// pageOf is the message about a publish, or nil when there is nothing to
// send: another tool, an action other than a publish — reading, listing and
// deleting carry a file path too, and an action the hook does not know is
// no publish either — an upload into the asset store, which publishes a file
// but no page, a file that is not a page, is gone, is past maxPage or holds
// nothing.
func pageOf(v any) *pyObject {
	p, ok := v.(*pyObject)
	if !ok || p.get("tool_name") != copyTool {
		return nil
	}
	in, ok := p.get("tool_input").(*pyObject)
	if !ok {
		return nil
	}
	if action := strings.TrimFunc(pyText(in.get("action")), pySpace); action != "" && action != "publish" {
		return nil
	}
	if pyTruthy(in.get("asset")) {
		return nil
	}
	path := strings.TrimFunc(pyText(in.get("file_path")), pySpace)
	if lower := strings.ToLower(path); path == "" || !strings.HasSuffix(lower, ".html") && !strings.HasSuffix(lower, ".htm") {
		return nil
	}
	html, ok := readPage(path)
	if !ok || strings.TrimFunc(html, pySpace) == "" {
		return nil
	}
	page := &pyObject{vals: map[string]any{}}
	for _, kv := range [][2]string{
		{"session", pyText(p.get("session_id"))},
		{"cwd", pyText(p.get("cwd"))},
		{"path", path},
		{"title", pyText(in.get("title"))},
		{"desc", pyText(in.get("description"))},
		{"icon", pyText(in.get("favicon"))},
		{"url", urlOf(p.get("tool_response"))},
		{"html", html},
	} {
		page.keys = append(page.keys, kv[0])
		page.vals[kv[0]] = kv[1]
	}
	return page
}

// readPage is the page at the path as python reads a text file: UTF-8 or
// nothing, with \r\n and a lone \r read as \n.
func readPage(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxPage {
		return "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(raw) {
		return "", false
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n"), true
}

// urlOf is the address the artifact was published at, if the answer of the
// tool names one: a field of the address, or the first word of its text that
// is one — the tool answers in prose as often as in fields, and the card
// offers the link as a second way in.
func urlOf(response any) string {
	web := func(s string) bool { return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") }
	var text string
	switch r := response.(type) {
	case *pyObject:
		for _, key := range []string{"url", "artifact_url", "link"} {
			if s, ok := r.get(key).(string); ok && web(s) {
				return s
			}
		}
		var parts []string
		for _, k := range r.keys {
			if s, ok := r.vals[k].(string); ok {
				parts = append(parts, s)
			}
		}
		text = strings.Join(parts, " ")
	case string:
		text = r
	default:
		return ""
	}
	for _, word := range strings.FieldsFunc(text, pySpace) {
		if web(word) {
			return strings.TrimRight(word, ".,;)")
		}
	}
	return ""
}
