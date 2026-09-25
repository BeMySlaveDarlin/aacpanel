package action

import "context"

// AskSide asks a session on the stream a question aside: claude answers it
// from what the conversation holds and writes neither the question nor the
// answer into it. It is a question and not an action: it changes nothing, and
// what claude says to it is not kept in the journal of actions.
const AskSide = "side"

// AskCommands asks a session on the stream which commands it takes: its own
// list, the skills among them, with what each one does.
const AskCommands = "commands"

// The side chat is bounded the way a message is: a question is what a person
// types, and the history is the side chat so far, sent back with each question
// because claude keeps none of it.
const (
	sideHistoryMax = 40
	sideAnswerMax  = 100_000
)

// SideTurn is one question of a side chat and what claude answered to it.
type SideTurn struct {
	Question string `json:"question"`
	Response string `json:"response"`
}

// Side is claude's answer to a question asked aside.
type Side struct {
	Answer string `json:"answer"`
}

// SessionCommands is what a session takes as a command. A terminal gives no
// list, and the panel keeps to its own there.
type SessionCommands struct {
	Transport string         `json:"transport"`
	List      []SlashCommand `json:"list,omitempty"`
}

// SlashCommand is one command of a session: its name without the slash, what
// it does, and the hint for its argument.
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

// SideAsker is an executor that asks a session a question aside.
type SideAsker interface {
	Side(ctx context.Context, target, question string, history []SideTurn) (*Side, error)
}

// CommandsAsker is an executor that knows the commands of a session.
type CommandsAsker interface {
	Commands(ctx context.Context, target string) (*SessionCommands, error)
}

func validateSide(r Request) error {
	if r.Text == "" {
		return badRequest("a question aside without the question")
	}
	if len([]rune(r.Text)) > TextMax {
		return badRequest("the question is longer than %d characters", TextMax)
	}
	if len(r.History) > sideHistoryMax {
		return badRequest("the side chat is longer than %d questions: start it again", sideHistoryMax)
	}
	for _, turn := range r.History {
		if turn.Question == "" || len([]rune(turn.Question)) > TextMax || len([]rune(turn.Response)) > sideAnswerMax {
			return badRequest("a question of the side chat is empty or too long")
		}
	}
	return nil
}
