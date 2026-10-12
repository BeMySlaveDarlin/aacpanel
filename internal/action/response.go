package action

import (
	"errors"
	"time"
)

var (
	ErrSessionNotFound  = errors.New("there is no live session")
	ErrSessionAmbiguous = errors.New("the session name is ambiguous")
)

const (
	SessionNotFound  = "session_not_found"
	SessionAmbiguous = "session_ambiguous"
)

// Response is what came out.
type Response struct {
	ID string `json:"id"`
	OK bool   `json:"ok"`

	Error string `json:"error,omitempty"`
	// Lookup codes survive the executor socket so callers need not parse
	// human wording to distinguish a missing name from an ambiguous one.
	Code string `json:"code,omitempty"`

	DurationMs int64 `json:"durationMs"`

	Detail string `json:"detail,omitempty"`

	// Session names the session a New or a resume of codex brought up, where
	// the executor knows it: a codex thread is named by its id, which nobody
	// knows before.
	Session string `json:"session,omitempty"`

	Kinds []Kind `json:"kinds,omitempty"`

	Permission *Permission `json:"permission,omitempty"`

	Window *Window `json:"window,omitempty"`

	Models *Models `json:"models,omitempty"`

	Mcp *Mcp `json:"mcp,omitempty"`

	Status *Status `json:"status,omitempty"`

	Setup *Setup `json:"setup,omitempty"`

	Side *Side `json:"side,omitempty"`

	Commands *SessionCommands `json:"commands,omitempty"`

	Terms []Term `json:"terms,omitempty"`

	Secrets *Secrets `json:"secrets,omitempty"`

	CodexModels []CodexModel `json:"codexModels,omitempty"`

	Processes []Process `json:"processes,omitempty"`
}

// Failed builds a refusal response.
func Failed(id string, err error, took time.Duration) Response {
	resp := Response{ID: id, OK: false, Error: err.Error(), DurationMs: took.Milliseconds()}
	switch {
	case errors.Is(err, ErrSessionNotFound):
		resp.Code = SessionNotFound
	case errors.Is(err, ErrSessionAmbiguous):
		resp.Code = SessionAmbiguous
	}
	return resp
}

// Done builds a success response.
func Done(id, detail string, took time.Duration) Response {
	return Response{ID: id, OK: true, Detail: detail, DurationMs: took.Milliseconds()}
}
