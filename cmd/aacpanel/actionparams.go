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
	uploadBodyMax = action.UploadBytesMax/3*4 + 64<<10
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
		file := action.File{Name: name, Data: data}
		if given, sent := raw["preview"]; sent {
			encoded, ok := given.(string)
			if !ok {
				return nil, fmt.Errorf("the copy of file %d for the feed did not arrive as a base64 string", i+1)
			}
			if file.Preview, err = base64.StdEncoding.DecodeString(encoded); err != nil {
				return nil, fmt.Errorf("the copy of file %d for the feed was not decoded: %w", i+1, err)
			}
		}
		out = append(out, file)
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
	tail, _ := params["tail"].(string)
	return &action.Permit{Option: int(num), Fingerprint: print, Tail: tail}, nil
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
	if notes, ok := params["notes"].([]any); ok {
		answer.Notes = make([]string, 0, len(notes))
		for _, item := range notes {
			note, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("the note did not arrive as a string")
			}
			answer.Notes = append(answer.Notes, note)
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

// commandFromParams reads a slash command and what the journal keeps of it.
// A review and a goal of codex come as objects of their own; the words a person
// gives them — what to review, the objective — are kept as their length, the
// way a message is.
func commandFromParams(params map[string]any) (*action.Command, map[string]any, error) {
	name, _ := params["command"].(string)
	arg, _ := params["arg"].(string)
	cmd := &action.Command{Name: name, Arg: arg}
	logged := map[string]any{"command": name}
	if arg != "" {
		logged["arg"] = arg
	}
	if raw, ok := params["review"]; ok {
		review, isMap := raw.(map[string]any)
		if !isMap {
			return nil, nil, fmt.Errorf("what a review looks at did not arrive as an object")
		}
		r := &action.Review{}
		r.Target, _ = review["target"].(string)
		r.Branch, _ = review["branch"].(string)
		r.Commit, _ = review["commit"].(string)
		r.Title, _ = review["title"].(string)
		r.Instructions, _ = review["instructions"].(string)
		cmd.Review = r
		kept := map[string]any{"target": r.Target}
		for key, v := range map[string]string{"branch": r.Branch, "commit": r.Commit} {
			if v != "" {
				kept[key] = v
			}
		}
		if r.Instructions != "" {
			kept["chars"] = len([]rune(r.Instructions))
		}
		logged["review"] = kept
	}
	if raw, ok := params["goal"]; ok {
		goal, isMap := raw.(map[string]any)
		if !isMap {
			return nil, nil, fmt.Errorf("what a goal does did not arrive as an object")
		}
		g := &action.Goal{}
		g.Do, _ = goal["do"].(string)
		g.Objective, _ = goal["objective"].(string)
		if budget, ok := goal["budget"].(float64); ok {
			if budget != math.Trunc(budget) {
				return nil, nil, fmt.Errorf("the budget of a goal did not arrive as a whole number of tokens")
			}
			g.Budget = int64(budget)
		}
		cmd.Goal = g
		kept := map[string]any{"do": g.Do}
		if g.Objective != "" {
			kept["chars"] = len([]rune(g.Objective))
		}
		if g.Budget != 0 {
			kept["budget"] = g.Budget
		}
		logged["goal"] = kept
	}
	return cmd, logged, nil
}

func workFromParams(params map[string]any) (*action.Work, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("it is not said which background work to stop")
	}
	line, _ := params["line"].(string)
	return &action.Work{ID: id, Line: line}, nil
}
