// Package artifact gives the person files a session has made, by their paths
// on the host. The transcript keeps the receipt; the file stays where it is.
package artifact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionfile"
)

const maxFiles = 20

const instructions = "Use file_send to put a finished file before the person in the panel's feed — a screenshot, report or built artifact " +
	`("send me the file"; "скинь файл", "покажи скрин", "пришли в панель"); the card opens the path, so keep the file on disk.`

const description = "Puts 1–%d files before the person in the panel's feed, on phone and desktop. " +
	"Use it when a finished screenshot, report or artifact should be shown, not just mentioned. " +
	"Give files as paths and an optional caption. Relative paths start in this session's directory; ~ names your home. " +
	"Every path must be a regular file outside the executor's secrets directory; any refusal sends nothing. " +
	"Files are not copied: the card opens each path, so files that will soon be deleted do not suit the conversation's history."

type attachment struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type"`
}

func Tool() mcp.Tool {
	return mcp.Tool{
		Name: "file_send", Title: "Show files to the person", Allowed: true,
		Instructions: instructions, Description: fmt.Sprintf(description, maxFiles),
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"files": map[string]any{"type": "array", "minItems": 1, "maxItems": maxFiles,
					"items": map[string]any{"type": "string", "minLength": 1}},
				"caption": map[string]any{"type": "string"},
			},
			"required": []string{"files"},
		},
		Call: func(_ context.Context, bind mcp.Bind, raw json.RawMessage) (string, bool) {
			var input struct {
				Files   []string `json:"files"`
				Caption string   `json:"caption"`
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				return "Nothing was sent: " + err.Error(), true
			}
			if len(input.Files) < 1 || len(input.Files) > maxFiles {
				return fmt.Sprintf("Nothing was sent: files must contain 1–%d paths.", maxFiles), true
			}
			var cwd string
			if bind != nil {
				if caller, err := bind(); err == nil {
					cwd = caller.Place.Dir
				}
			}
			root, err := filepath.Abs(sessionfile.SecretsDir())
			if err != nil {
				return "Nothing was sent: the secrets directory is not known: " + err.Error(), true
			}
			if real, err := filepath.EvalSymlinks(root); err == nil {
				root = real
			} else if !os.IsNotExist(err) {
				return "Nothing was sent: the secrets directory could not be resolved: " + err.Error(), true
			}
			var files []attachment
			var seen []os.FileInfo
			var bad []string
			for _, name := range input.Files {
				file, info, err := inspect(name, cwd, root)
				if err != nil {
					bad = append(bad, fmt.Sprintf("%q: %v", name, err))
					continue
				}
				duplicate := false
				for _, previous := range seen {
					if os.SameFile(previous, info) {
						duplicate = true
						break
					}
				}
				if !duplicate {
					files = append(files, file)
					seen = append(seen, info)
				}
			}
			if len(bad) > 0 {
				return "Nothing was sent:\n" + strings.Join(bad, "\n"), true
			}
			result, err := json.Marshal(struct {
				Caption     string       `json:"caption,omitempty"`
				Attachments []attachment `json:"attachments"`
			}{strings.TrimSpace(input.Caption), files})
			if err != nil {
				return "Nothing was sent: " + err.Error(), true
			}
			return string(result), false
		},
	}
}

func inspect(name, cwd, secrets string) (attachment, os.FileInfo, error) {
	path := strings.TrimSpace(name)
	if path == "" {
		return attachment{}, nil, fmt.Errorf("the path is empty")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return attachment{}, nil, err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		if !filepath.IsAbs(cwd) {
			return attachment{}, nil, fmt.Errorf("the session directory is not known; give an absolute path")
		}
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return attachment{}, nil, err
	}
	if real == secrets || strings.HasPrefix(real, secrets+string(filepath.Separator)) {
		return attachment{}, nil, fmt.Errorf("files in the executor's secrets directory cannot be sent")
	}
	info, err := os.Stat(real)
	if err != nil {
		return attachment{}, nil, err
	}
	if !info.Mode().IsRegular() {
		return attachment{}, nil, fmt.Errorf("the path is not a regular file")
	}
	media := mime.TypeByExtension(filepath.Ext(path))
	if media == "" {
		file, err := os.Open(real)
		if err != nil {
			return attachment{}, nil, err
		}
		defer file.Close()
		var head [512]byte
		n, err := file.Read(head[:])
		if err != nil && err != io.EOF {
			return attachment{}, nil, err
		}
		media = http.DetectContentType(head[:n])
	}
	return attachment{Path: path, Size: info.Size(), MediaType: media}, info, nil
}
