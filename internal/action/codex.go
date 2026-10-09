package action

import "context"

// AskCodexModels asks for the models codex offers: the catalogue the codex
// daemon of a home lists, with the efforts each model takes. It names no
// session — a launch parameter is picked before any session runs — and asks
// no account: the catalogue is codex's own.
const AskCodexModels = "codex.models"

// CodexModel is one model of the codex catalogue: the name codex takes, the
// one a person reads, the efforts it takes and the one it starts at.
type CodexModel struct {
	Model   string   `json:"model"`
	Name    string   `json:"name"`
	Efforts []string `json:"efforts"`
	Effort  string   `json:"effort,omitempty"`
}

// CodexModelsAsker is an executor that knows the models codex offers.
type CodexModelsAsker interface {
	CodexModels(ctx context.Context) ([]CodexModel, error)
}
