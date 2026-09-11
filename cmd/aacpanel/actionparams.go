package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math"

	"aacpanel/internal/action"
)

const (
	actionBodyMax = action.TextMax*4 + 4<<10
	uploadBodyMax = action.FilesBytesMax/3*4 + 64<<10
)

type countingReader struct {
	from io.Reader
	read int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.from.Read(p)
	c.read += int64(n)
	return n, err
}

func filesFromParams(params map[string]any) ([]action.File, error) {
	list, ok := params["files"].([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("no files in the request")
	}
	out := make([]action.File, 0, len(list))
	for i, item := range list {
		raw, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("attachment %d did not arrive as an object", i+1)
		}
		name, _ := raw["name"].(string)
		encoded, ok := raw["data"].(string)
		if !ok {
			return nil, fmt.Errorf("the content of file %d did not arrive as a base64 string", i+1)
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("the content of file %d was not decoded: %w", i+1, err)
		}
		out = append(out, action.File{Name: name, Data: data})
	}
	return out, nil
}

func permitFromParams(params map[string]any) (*action.Permit, error) {
	num, ok := params["option"].(float64)
	if !ok || num != math.Trunc(num) {
		return nil, fmt.Errorf("the permission item number did not arrive as an integer")
	}
	print, _ := params["dialog"].(string)
	if print == "" {
		return nil, fmt.Errorf("a keypress without a dialog fingerprint: there would be nothing to check it against")
	}
	return &action.Permit{Option: int(num), Fingerprint: print}, nil
}

func answerFromParams(params map[string]any) (*action.Answer, error) {
	askID, _ := params["ask"].(string)
	raw, ok := params["picks"].([]any)
	if !ok {
		return nil, fmt.Errorf("the answer holds no picked items")
	}
	answer := &action.Answer{AskID: askID, Picks: make([][]int, 0, len(raw))}
	for _, item := range raw {
		list, ok := item.([]any)
		if !ok {
			return nil, fmt.Errorf("the picks for the question did not arrive as a list")
		}
		picks := make([]int, 0, len(list))
		for _, n := range list {
			num, ok := n.(float64)
			if !ok || num != math.Trunc(num) {
				return nil, fmt.Errorf("the item number did not arrive as an integer")
			}
			picks = append(picks, int(num))
		}
		answer.Picks = append(answer.Picks, picks)
	}
	if texts, ok := params["texts"].([]any); ok {
		answer.Texts = make([]string, 0, len(texts))
		for _, item := range texts {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("the free-form answer did not arrive as a string")
			}
			answer.Texts = append(answer.Texts, text)
		}
	}
	return answer, nil
}

func textLens(texts []string) []int {
	lens := make([]int, 0, len(texts))
	any := false
	for _, text := range texts {
		lens = append(lens, len([]rune(text)))
		if text != "" {
			any = true
		}
	}
	if !any {
		return nil
	}
	return lens
}

func dismissFromParams(params map[string]any) (*action.Answer, error) {
	askID, _ := params["ask"].(string)
	if askID == "" {
		return nil, fmt.Errorf("it is not said which question to dismiss")
	}
	return &action.Answer{AskID: askID}, nil
}

func requestID(journalID int64) string {
	if journalID > 0 {
		return fmt.Sprintf("a%d", journalID)
	}
	var buf [8]byte
	rand.Read(buf[:])
	return "x" + hex.EncodeToString(buf[:])
}

func deviceRef(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

func note(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}

func workFromParams(params map[string]any) (*action.Work, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("it is not said which background work to stop")
	}
	line, _ := params["line"].(string)
	return &action.Work{ID: id, Line: line}, nil
}
