package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// Commands answers which commands a session takes. A session on the stream
// is asked nothing new: its holder kept what claude listed at the handshake,
// the skills among the commands. A terminal lists nothing, and says where it
// lives so the panel keeps to its own list.
func (e *Executor) Commands(ctx context.Context, target string) (*action.SessionCommands, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}
	if !onStream(s) {
		return &action.SessionCommands{Transport: action.SwitchConsole}, nil
	}
	st, err := streamState(ctx, s)
	if err != nil {
		return nil, err
	}
	return &action.SessionCommands{Transport: action.SwitchStream, List: initCommands(st.Init)}, nil
}

// initCommands reads the commands out of claude's answer to the handshake.
func initCommands(init json.RawMessage) []action.SlashCommand {
	var body struct {
		Commands []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Hint        string `json:"argumentHint"`
		} `json:"commands"`
	}
	if len(init) == 0 || json.Unmarshal(init, &body) != nil {
		return nil
	}
	out := make([]action.SlashCommand, 0, len(body.Commands))
	for _, c := range body.Commands {
		if c.Name == "" {
			continue
		}
		out = append(out, action.SlashCommand{Name: c.Name, Description: c.Description, Hint: c.Hint})
	}
	return out
}

// Side asks a session on the stream a question aside. Claude answers from what
// the conversation holds and keeps neither the question nor the answer in it,
// and it remembers no side chat either: the one so far comes with every
// question.
func (e *Executor) Side(ctx context.Context, target, question string, history []action.SideTurn) (*action.Side, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}
	if !onStream(s) {
		return nil, fmt.Errorf("session %s runs in a terminal: a question aside is asked there with /btw on its own "+
			"screen, and the answer stays on that screen", s.Name)
	}
	turns := make([]any, 0, len(history))
	for _, t := range history {
		turns = append(turns, map[string]any{"question": t.Question, "response": t.Response})
	}
	reply, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "side_question",
		Fields: map[string]any{"question": question, "history": turns}})
	if err != nil {
		return nil, err
	}
	var body struct {
		Response struct {
			Answer string `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(reply.Response, &body) != nil {
		return nil, fmt.Errorf("session %s answered the question aside in a shape the panel does not read", s.Name)
	}
	return &action.Side{Answer: body.Response.Answer}, nil
}
